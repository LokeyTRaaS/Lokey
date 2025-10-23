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
	"github.com/lokey/rng-service/pkg/atecc608a"
)

const (
	DefaultPort         = 8081
	DefaultI2CBusNumber = 1
)

type Controller struct {
	device *atecc608a.Controller
	port   int
	router *gin.Engine
}

func NewController(i2cBusNumber int, port int) (*Controller, error) {
	// Initialize ATECC608A controller
	device, err := atecc608a.NewController(i2cBusNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ATECC608A: %w", err)
	}

	// Set Gin mode based on LOG_LEVEL
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "ERROR" || logLevel == "WARN" {
		gin.SetMode(gin.ReleaseMode)
	} else if logLevel == "DEBUG" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize router
	router := gin.Default()

	return &Controller{
		device: device,
		port:   port,
		router: router,
	}, nil
}

func (c *Controller) setupRoutes() {
	// API routes
	c.router.GET("/health", c.healthCheckHandler)
	c.router.GET("/info", c.infoHandler)
	c.router.GET("/generate", c.generateHandler)
}

func (c *Controller) Start() error {
	// Setup routes
	c.setupRoutes()

	// Start HTTP server
	serverErr := make(chan error, 1)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", c.port),
		Handler: c.router,
	}

	go func() {
		log.Printf("[INFO] Starting controller server on port %d", c.port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigCh:
		log.Printf("[INFO] Received signal %s, shutting down", sig)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	// Close resources
	if err := c.device.Close(); err != nil {
		log.Printf("[WARN] Error closing ATECC608A: %v", err)
	}

	return nil
}

// HTTP Handlers

func (c *Controller) healthCheckHandler(ctx *gin.Context) {
	healthy := c.device.HealthCheck()
	if healthy {
		ctx.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	} else {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    "unhealthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"details": gin.H{
				"device": c.device.HealthCheck(),
			},
		})
	}
}

func (c *Controller) infoHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status": "running",
	})
}

func (c *Controller) generateHandler(ctx *gin.Context) {
	// Get count parameter (optional, default 1)
	countStr := ctx.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 100 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid count parameter (1-100)"})
		return
	}

	// Generate multiple raw random data if requested
	data := make([]string, 0, count)

	for i := 0; i < count; i++ {
		// Call GenerateRandom() to get raw random data
		randomData, err := c.device.GenerateRandom()
		if err != nil {
			log.Printf("[ERROR] Failed to generate random data: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate random data"})
			return
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

func main() {
	// Read configuration from environment variables
	i2cBusNumber := DefaultI2CBusNumber
	if val, ok := os.LookupEnv("I2C_BUS_NUMBER"); ok {
		if n, err := fmt.Sscanf(val, "%d", &i2cBusNumber); n != 1 || err != nil {
			log.Printf("[WARN] Invalid I2C_BUS_NUMBER, using default: %d", DefaultI2CBusNumber)
			i2cBusNumber = DefaultI2CBusNumber
		}
	}

	port := DefaultPort
	if val, ok := os.LookupEnv("PORT"); ok {
		if n, err := fmt.Sscanf(val, "%d", &port); n != 1 || err != nil {
			log.Printf("[WARN] Invalid PORT, using default: %d", DefaultPort)
			port = DefaultPort
		}
	}

	// Create and start controller
	controller, err := NewController(i2cBusNumber, port)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create controller: %v", err)
	}

	log.Printf("[INFO] Starting TRNG controller with configuration:")
	log.Printf("[INFO]   I2C Bus Number: %d", i2cBusNumber)
	log.Printf("[INFO]   Port: %d", port)
	log.Printf("[INFO]   Log Level: %s", os.Getenv("LOG_LEVEL"))

	err = controller.Start()
	if err != nil {
		log.Fatalf("[ERROR] Controller error: %v", err)
	}

	log.Println("[INFO] Controller gracefully shut down")
}
