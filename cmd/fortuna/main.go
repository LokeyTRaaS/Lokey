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
	"github.com/lokey/rng-service/pkg/fortuna"
)

const (
	DefaultPort                = 8082
	DefaultAmplificationFactor = 4
)

type FortunaProcessor struct {
	generator           *fortuna.Generator
	port                int
	amplificationFactor int
	router              *gin.Engine
	lastReseedTime      time.Time
}

func NewFortunaProcessor(port int, amplificationFactor int) (*FortunaProcessor, error) {
	// Initialize router
	router := gin.Default()

	// Initialize Fortuna with a temporary seed (will be reseeded by API service)
	initialSeed := make([]byte, 32)
	for i := range initialSeed {
		initialSeed[i] = byte(i)
	}

	generator, err := fortuna.NewGenerator(initialSeed)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Fortuna generator: %w", err)
	}

	return &FortunaProcessor{
		generator:           generator,
		port:                port,
		amplificationFactor: amplificationFactor,
		router:              router,
		lastReseedTime:      time.Now(),
	}, nil
}

func (p *FortunaProcessor) setupRoutes() {
	// API routes
	p.router.GET("/health", p.healthCheckHandler)
	p.router.GET("/info", p.infoHandler)
	p.router.GET("/generate", p.generateDataHandler)
	p.router.POST("/seed", p.seedHandler)
	p.router.POST("/amplify", p.amplifyDataHandler)
}

func (p *FortunaProcessor) Start() error {
	// Setup routes
	p.setupRoutes()

	// Start HTTP server
	serverErr := make(chan error, 1)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", p.port),
		Handler: p.router,
	}

	go func() {
		log.Printf("Starting Fortuna processor server on port %d", p.port)
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
		log.Printf("Received signal %s, shutting down", sig)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	return nil
}

//---------------------- HTTP Handlers ----------------------

// healthCheckHandler checks if the generator is healthy
func (p *FortunaProcessor) healthCheckHandler(ctx *gin.Context) {
	healthy := p.generator.HealthCheck()
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
				"generator": p.generator.HealthCheck(),
			},
		})
	}
}

// infoHandler returns information about the Fortuna processor
func (p *FortunaProcessor) infoHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status":               "running",
		"amplification_factor": p.amplificationFactor,
		"last_reseeded":        p.lastReseedTime.Format(time.RFC3339),
	})
}

// generateDataHandler generates random data using the Fortuna algorithm
func (p *FortunaProcessor) generateDataHandler(ctx *gin.Context) {
	// Get requested size parameter
	sizeStr := ctx.DefaultQuery("size", "128")
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size <= 0 || size > 1024*1024 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid size parameter (1-1048576)"})
		return
	}

	// Generate random data
	data, err := p.generator.GenerateRandomData(size)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate data"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"data": hex.EncodeToString(data),
		"size": len(data),
	})
}

// seedHandler processes incoming TRNG seeds to reseed the Fortuna generator
func (p *FortunaProcessor) seedHandler(ctx *gin.Context) {
	// Parse request body
	var request struct {
		Seeds []string `json:"seeds" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	if len(request.Seeds) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "No seeds provided"})
		return
	}

	// Add each seed to a different pool
	for i, seedHex := range request.Seeds {
		seed, err := hex.DecodeString(seedHex)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seed format (not hex)"})
			return
		}

		p.generator.AddRandomEvent(byte(i%32), seed)
	}

	// Reseed the generator
	err := p.generator.ReseedFromPools()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reseed generator"})
		return
	}

	// Update last reseed time
	p.lastReseedTime = time.Now()

	ctx.JSON(http.StatusOK, gin.H{
		"status":        "reseeded",
		"count":         len(request.Seeds),
		"last_reseeded": p.lastReseedTime.Format(time.RFC3339),
	})
}

// amplifyDataHandler amplifies provided seed data using the Fortuna algorithm
func (p *FortunaProcessor) amplifyDataHandler(ctx *gin.Context) {
	// Parse request body
	var request struct {
		Seed string `json:"seed" binding:"required"`
		Size int    `json:"size" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Decode seed
	seed, err := hex.DecodeString(request.Seed)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seed format (not hex)"})
		return
	}

	// Validate size
	if request.Size <= 0 || request.Size > 1024*1024 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid size parameter (1-1048576)"})
		return
	}

	// Amplify the data
	outputLength := len(seed) * p.amplificationFactor
	if request.Size > 0 {
		outputLength = request.Size
	}

	amplifiedData, err := p.generator.AmplifyRandomData(seed, outputLength)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to amplify data"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"data": hex.EncodeToString(amplifiedData),
		"size": len(amplifiedData),
	})
}

//---------------------- Main ----------------------

func main() {
	// Read configuration from environment variables
	port := DefaultPort
	if val, ok := os.LookupEnv("PORT"); ok {
		if n, err := fmt.Sscanf(val, "%d", &port); n != 1 || err != nil {
			log.Printf("Invalid PORT, using default: %d", DefaultPort)
			port = DefaultPort
		}
	}

	amplificationFactor := DefaultAmplificationFactor
	if val, ok := os.LookupEnv("AMPLIFICATION_FACTOR"); ok {
		if n, err := fmt.Sscanf(val, "%d", &amplificationFactor); n != 1 || err != nil {
			log.Printf("Invalid AMPLIFICATION_FACTOR, using default: %d", DefaultAmplificationFactor)
			amplificationFactor = DefaultAmplificationFactor
		}
	}

	// Create and start Fortuna processor
	processor, err := NewFortunaProcessor(port, amplificationFactor)
	if err != nil {
		log.Fatalf("Failed to create Fortuna processor: %v", err)
	}

	log.Printf("Starting Fortuna processor with configuration:")
	log.Printf("  Port: %d", port)
	log.Printf("  Amplification Factor: %d", amplificationFactor)
	log.Printf("Note: Fortuna will be seeded by the API service via /seed endpoint")

	err = processor.Start()
	if err != nil {
		log.Fatalf("Fortuna processor error: %v", err)
	}

	log.Println("Fortuna processor gracefully shut down")
}
