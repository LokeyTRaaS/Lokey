package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lokey/rng-service/pkg/virtio"
)

const (
	DefaultPort       = 8083
	DefaultQueueSize  = 15000 // ~480 KB at 32 bytes/item
	DefaultPipePath   = "/var/run/lokey/virtio-rng"
	DefaultDevicePath = "/dev/hwrng"
)

type VirtIOService struct {
	device *virtio.VirtIORNG
	port   int
	pipe   *virtio.NamedPipe
	router *gin.Engine
}

// customLogger only logs non-200 responses
func customLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Only log if status is not 200
		if c.Writer.Status() != http.StatusOK {
			end := time.Now()
			latency := end.Sub(start)

			if raw != "" {
				path = path + "?" + raw
			}

			log.Printf("[GIN] %v | %3d | %13v | %15s | %-7s %s",
				end.Format("2006/01/02 - 15:04:05"),
				c.Writer.Status(),
				latency,
				c.ClientIP(),
				c.Request.Method,
				path,
			)
		}
	}
}

// QueueConfig represents the queue configuration request/response
type QueueConfig struct {
	QueueSize int `json:"queue_size" binding:"required"`
}

func NewVirtIOService(devicePath string, queueSize int, port int, pipePath string) (*VirtIOService, error) {
	// Initialize VirtIO RNG device
	device, err := virtio.NewVirtIORNG(devicePath, queueSize)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize VirtIO RNG: %w", err)
	}

	// Initialize named pipe (optional - only needed for filesystem access)
	var pipe *virtio.NamedPipe
	if pipePath != "" {
		var err error
		pipe, err = virtio.NewNamedPipe(pipePath)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize named pipe: %w", err)
		}
	}

	// Initialize router based on log level
	var router *gin.Engine
	logLevel := os.Getenv("LOG_LEVEL")

	if logLevel == "WARN" || logLevel == "ERROR" {
		// Production mode: no default middleware, only log errors
		router = gin.New()
		router.Use(gin.Recovery())
		router.Use(customLogger()) // Only logs non-200
	} else {
		// Debug/Info mode: use default logger
		router = gin.Default()
	}

	return &VirtIOService{
		device: device,
		port:   port,
		pipe:   pipe,
		router: router,
	}, nil
}

func (v *VirtIOService) setupRoutes() {
	// API routes
	v.router.GET("/health", v.healthCheckHandler)
	v.router.GET("/info", v.infoHandler)
	v.router.GET("/generate", v.generateHandler)
	v.router.GET("/stream", v.streamHandler)
	v.router.POST("/seed", v.seedHandler)
	v.router.GET("/config/queue", v.getQueueConfigHandler)
	v.router.PUT("/config/queue", v.updateQueueConfigHandler)
}

func (v *VirtIOService) Start() error {
	// Setup routes
	v.setupRoutes()

	// Start named pipe (if enabled)
	if v.pipe != nil {
		if err := v.pipe.Start(v.device); err != nil {
			return fmt.Errorf("failed to start named pipe: %w", err)
		}
	}

	// Start HTTP server
	serverErr := make(chan error, 1)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", v.port),
		Handler: v.router,
	}

	logLevel := os.Getenv("LOG_LEVEL")

	go func() {
		if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
			log.Printf("[INFO] Starting VirtIO HTTP server on port %d", v.port)
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		// Stop pipe on HTTP server error
		if stopErr := v.pipe.Stop(); stopErr != nil {
			log.Printf("[WARN] Error stopping named pipe: %v", stopErr)
		}
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigCh:
		if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
			log.Printf("[INFO] Received signal %s, shutting down", sig)
		}
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop named pipe first (if enabled)
	if v.pipe != nil {
		if err := v.pipe.Stop(); err != nil {
			log.Printf("[WARN] Error stopping named pipe: %v", err)
		}
	}

	// Then shutdown HTTP server
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	// Close resources
	if err := v.device.Close(); err != nil {
		log.Printf("[WARN] Error closing VirtIO device: %v", err)
	}

	return nil
}

// HTTP Handlers

func (v *VirtIOService) healthCheckHandler(ctx *gin.Context) {
	healthy := v.device.HealthCheck()
	if healthy {
		ctx.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	} else {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    "unhealthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

func (v *VirtIOService) infoHandler(ctx *gin.Context) {
	info := gin.H{
		"status": "running",
	}
	if v.pipe != nil {
		info["pipe_path"] = v.pipe.Path()
	}
	ctx.JSON(http.StatusOK, info)
}

func (v *VirtIOService) generateHandler(ctx *gin.Context) {
	// Get count parameter (optional, default 1)
	countStr := ctx.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 100 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid count parameter (1-100)"})
		return
	}

	// Pull data from queue instead of reading from device
	// Each item is 32 bytes, so we need count items
	data := make([]string, 0, count)

	for i := 0; i < count; i++ {
		// Pull 32 bytes from queue (consuming items)
		randomData, _, err := v.device.PullBytes(32)
		if err != nil {
			log.Printf("[ERROR] Failed to pull data from queue: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate random data: queue may be empty"})
			return
		}

		// Ensure we have exactly 32 bytes (pad if needed, though this shouldn't happen)
		if len(randomData) < 32 {
			log.Printf("[WARN] Pulled only %d bytes, expected 32", len(randomData))
			// Pad with zeros (shouldn't happen but handle gracefully)
			padded := make([]byte, 32)
			copy(padded, randomData)
			randomData = padded
		} else if len(randomData) > 32 {
			randomData = randomData[:32]
		}

		data = append(data, hex.EncodeToString(randomData))
	}

	// Return single data or array based on count
	if count == 1 {
		ctx.JSON(http.StatusOK, gin.H{
			"data": data[0],
		})
	} else {
		ctx.JSON(http.StatusOK, gin.H{
			"data": data,
		})
	}
}

func (v *VirtIOService) streamHandler(ctx *gin.Context) {
	// Get optional query parameters
	chunkSizeStr := ctx.DefaultQuery("chunk_size", "1024")
	chunkSize, err := strconv.Atoi(chunkSizeStr)
	if err != nil || chunkSize < 1 || chunkSize > 65536 {
		chunkSize = 1024 // Default 1KB chunks
	}

	maxBytesStr := ctx.Query("max_bytes")
	var maxBytes int64 = -1 // -1 means no limit
	if maxBytesStr != "" {
		if mb, err := strconv.ParseInt(maxBytesStr, 10, 64); err == nil && mb > 0 {
			maxBytes = mb
		}
	}

	// Set headers for streaming
	ctx.Header("Content-Type", "application/octet-stream")
	ctx.Header("Transfer-Encoding", "chunked")
	ctx.Writer.WriteHeader(http.StatusOK)
	ctx.Writer.Flush()

	// Stream data from queue
	bytesWritten := int64(0)
	ticker := time.NewTicker(10 * time.Millisecond) // Check queue every 10ms
	defer ticker.Stop()

	for {
		// Check if client disconnected
		select {
		case <-ctx.Request.Context().Done():
			return
		case <-ticker.C:
			// Check if we've reached max bytes
			if maxBytes > 0 && bytesWritten >= maxBytes {
				return
			}

			// Try to pull data from queue
			queueCurrent := v.device.GetQueueCurrent()
			if queueCurrent == 0 {
				continue // No data available, wait for next tick
			}

			// Calculate how much to pull
			toPull := chunkSize
			if maxBytes > 0 {
				remaining := maxBytes - bytesWritten
				if int64(toPull) > remaining {
					toPull = int(remaining)
				}
			}

			// Pull data
			data, _, err := v.device.PullBytes(toPull)
			if err != nil {
				// Queue might be empty now, continue
				continue
			}

			// Write chunk to client
			if _, err := ctx.Writer.Write(data); err != nil {
				// Client disconnected
				return
			}
			ctx.Writer.Flush()

			bytesWritten += int64(len(data))

			// Check if we've reached max bytes
			if maxBytes > 0 && bytesWritten >= maxBytes {
				return
			}
		}
	}
}

func (v *VirtIOService) seedHandler(ctx *gin.Context) {
	var request struct {
		Seeds []string `json:"seeds" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if len(request.Seeds) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "No seeds provided"})
		return
	}

	// Decode and store each seed
	seededCount := 0
	for _, seedHex := range request.Seeds {
		seed, err := hex.DecodeString(seedHex)
		if err != nil {
			log.Printf("[WARN] Failed to decode seed: %v", err)
			continue
		}

		if err := v.device.Seed(seed); err != nil {
			log.Printf("[WARN] Failed to seed: %v", err)
			continue
		}

		seededCount++
	}

	if seededCount == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Failed to seed any data"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "seeded",
		"count":  seededCount,
	})
}

func (v *VirtIOService) getQueueConfigHandler(ctx *gin.Context) {
	queueSize := v.device.GetQueueSize()
	queueCurrent := v.device.GetQueueCurrent()
	totalGenerated, consumedCount, droppedCount := v.device.GetQueueStats()

	ctx.JSON(http.StatusOK, gin.H{
		"queue_size":      queueSize,
		"queue_current":   queueCurrent,
		"total_generated": int64(totalGenerated),
		"consumed_count":  int64(consumedCount),
		"queue_dropped":   int64(droppedCount),
	})
}

func (v *VirtIOService) updateQueueConfigHandler(ctx *gin.Context) {
	var config QueueConfig
	if err := ctx.ShouldBindJSON(&config); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if config.QueueSize < 10 || config.QueueSize > 1000000 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Queue size must be between 10 and 1000000"})
		return
	}

	if err := v.device.UpdateQueueSize(config.QueueSize); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to update queue size: %v", err)})
		return
	}

	queueCurrent := v.device.GetQueueCurrent()

	ctx.JSON(http.StatusOK, gin.H{
		"queue_size":    config.QueueSize,
		"queue_current": queueCurrent,
	})
}

func main() {
	// Read configuration from environment variables
	devicePath := DefaultDevicePath
	if val, ok := os.LookupEnv("RNG_DEVICE"); ok && val != "" {
		devicePath = val
	}

	queueSize := DefaultQueueSize
	if val, ok := os.LookupEnv("VIRTIO_QUEUE_SIZE"); ok {
		if size, err := strconv.Atoi(val); err == nil && size >= 10 && size <= 1000000 {
			queueSize = size
		} else {
			log.Printf("[WARN] Invalid VIRTIO_QUEUE_SIZE, using default: %d", DefaultQueueSize)
		}
	}

	port := DefaultPort
	if val, ok := os.LookupEnv("PORT"); ok {
		if n, err := fmt.Sscanf(val, "%d", &port); n != 1 || err != nil {
			log.Printf("[WARN] Invalid PORT, using default: %d", DefaultPort)
			port = DefaultPort
		}
	}

	pipePath := ""
	if val, ok := os.LookupEnv("VIRTIO_PIPE_PATH"); ok && val != "" {
		pipePath = val
	}
	// Default to empty (pipe disabled) - use HTTP endpoints instead

	// Create and start VirtIO service
	service, err := NewVirtIOService(devicePath, queueSize, port, pipePath)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create VirtIO service: %v", err)
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
		log.Printf("[INFO] Starting VirtIO service with configuration:")
		log.Printf("[INFO]   Queue Size: %d items (~%d KB)", queueSize, (queueSize*32)/1024)
		log.Printf("[INFO]   HTTP Port: %d", port)
		log.Printf("[INFO]   Named Pipe: %s", pipePath)
		log.Printf("[INFO]   Log Level: %s", logLevel)
	}

	err = service.Start()
	if err != nil {
		log.Fatalf("[ERROR] VirtIO service error: %v", err)
	}

	if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
		log.Println("[INFO] VirtIO service gracefully shut down")
	}
}
