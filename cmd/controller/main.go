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
	c.router.GET("/generate", c.generateHashHandler)
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
		log.Printf("Starting controller server on port %d", c.port)
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

	// Close resources
	if err := c.device.Close(); err != nil {
		log.Printf("Error closing ATECC608A: %v", err)
	}

	return nil
}

// Background processing methods removed as they're no longer needed in the stateless design

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

func (c *Controller) generateHashHandler(ctx *gin.Context) {
	// Get count parameter (optional, default 1)
	countStr := ctx.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 100 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid count parameter (1-100)"})
		return
	}

	// Generate multiple hashes if requested
	hashes := make([]string, 0, count)

	for i := 0; i < count; i++ {
		hash, err := c.device.GenerateHashFromRandom()
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate hash"})
			return
		}
		hashes = append(hashes, hex.EncodeToString(hash))
	}

	// Return single hash or array based on count
	if count == 1 {
		ctx.JSON(http.StatusOK, gin.H{
			"hash": hashes[0],
		})
	} else {
		ctx.JSON(http.StatusOK, gin.H{
			"hashes": hashes,
		})
	}
}

func main() {
	// Read configuration from environment variables
	i2cBusNumber := DefaultI2CBusNumber
	if val, ok := os.LookupEnv("I2C_BUS_NUMBER"); ok {
		if n, err := fmt.Sscanf(val, "%d", &i2cBusNumber); n != 1 || err != nil {
			log.Printf("Invalid I2C_BUS_NUMBER, using default: %d", DefaultI2CBusNumber)
			i2cBusNumber = DefaultI2CBusNumber
		}
	}

	port := DefaultPort
	if val, ok := os.LookupEnv("PORT"); ok {
		if n, err := fmt.Sscanf(val, "%d", &port); n != 1 || err != nil {
			log.Printf("Invalid PORT, using default: %d", DefaultPort)
			port = DefaultPort
		}
	}

	// Create and start controller
	controller, err := NewController(i2cBusNumber, port)
	if err != nil {
		log.Fatalf("Failed to create controller: %v", err)
	}

	log.Printf("Starting TRNG controller with configuration:")
	log.Printf("  I2C Bus Number: %d", i2cBusNumber)
	log.Printf("  Port: %d", port)

	err = controller.Start()
	if err != nil {
		log.Fatalf("Controller error: %v", err)
	}

	log.Println("Controller gracefully shut down")
}
