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
	"strings"
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
	device atecc608a.EntropySource
	port   int
	router *gin.Engine
}

// customLogger only logs non-200 responses
func customLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

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

func newRouter() *gin.Engine {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "WARN" || logLevel == "ERROR" {
		router := gin.New()
		router.Use(gin.Recovery())
		router.Use(customLogger())
		return router
	}
	return gin.Default()
}

func NewControllerFromSource(device atecc608a.EntropySource, port int) *Controller {
	return &Controller{
		device: device,
		port:   port,
		router: newRouter(),
	}
}

func NewController(i2cBusNumber int, port int) (*Controller, error) {
	device, err := atecc608a.NewController(i2cBusNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ATECC608A: %w", err)
	}
	return NewControllerFromSource(device, port), nil
}

func (c *Controller) setupRoutes() {
	c.router.GET("/health", c.healthCheckHandler)
	c.router.GET("/info", c.infoHandler)
	c.router.GET("/generate", c.generateHandler)
}

func (c *Controller) Start() error {
	c.setupRoutes()

	serverErr := make(chan error, 1)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.port),
		Handler:           c.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logLevel := os.Getenv("LOG_LEVEL")

	go func() {
		if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
			log.Printf("[INFO] Starting controller server on port %d (mode=%s device_id=%s)",
				c.port, c.device.Mode(), c.device.DeviceID())
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigCh:
		if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
			log.Printf("[INFO] Received signal %s, shutting down", sig)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	if err := c.device.Close(); err != nil {
		log.Printf("[WARN] Error closing entropy source: %v", err)
	}

	return nil
}

func (c *Controller) healthCheckHandler(ctx *gin.Context) {
	healthy := c.device.HealthCheck()
	payload := gin.H{
		"status":    map[bool]string{true: "healthy", false: "unhealthy"}[healthy],
		"timestamp": time.Now().Format(time.RFC3339),
		"mode":      c.device.Mode(),
		"device_id": c.device.DeviceID(),
	}
	if healthy {
		ctx.JSON(http.StatusOK, payload)
		return
	}
	ctx.JSON(http.StatusServiceUnavailable, payload)
}

func (c *Controller) infoHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status":    "running",
		"mode":      c.device.Mode(),
		"device_id": c.device.DeviceID(),
	})
}

func (c *Controller) generateHandler(ctx *gin.Context) {
	countStr := ctx.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 100 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid count parameter (1-100)"})
		return
	}

	data := make([]string, 0, count)
	for i := 0; i < count; i++ {
		randomData, err := c.device.GenerateRandom()
		if err != nil {
			log.Printf("[ERROR] Failed to generate random data: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate random data"})
			return
		}
		data = append(data, hex.EncodeToString(randomData))
	}

	if count == 1 {
		ctx.JSON(http.StatusOK, gin.H{"data": data[0], "mode": c.device.Mode(), "device_id": c.device.DeviceID()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": data, "mode": c.device.Mode(), "device_id": c.device.DeviceID()})
}

func main() {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("CONTROLLER_MODE")))
	if mode == "" {
		mode = "hardware"
	}

	port := DefaultPort
	if val, ok := os.LookupEnv("PORT"); ok {
		if n, err := fmt.Sscanf(val, "%d", &port); n != 1 || err != nil {
			log.Printf("[WARN] Invalid PORT, using default: %d", DefaultPort)
			port = DefaultPort
		}
	}

	var controller *Controller
	switch mode {
	case "emulated", "emulator", "soft":
		controller = NewControllerFromSource(atecc608a.NewEmulatedController(), port)
	case "hardware", "hw":
		i2cBusNumber := DefaultI2CBusNumber
		if val, ok := os.LookupEnv("I2C_BUS_NUMBER"); ok {
			if n, err := fmt.Sscanf(val, "%d", &i2cBusNumber); n != 1 || err != nil {
				log.Printf("[WARN] Invalid I2C_BUS_NUMBER, using default: %d", DefaultI2CBusNumber)
				i2cBusNumber = DefaultI2CBusNumber
			}
		}
		var err error
		controller, err = NewController(i2cBusNumber, port)
		if err != nil {
			log.Fatalf("[ERROR] Failed to create controller: %v", err)
		}
	default:
		log.Fatalf("[ERROR] Unknown CONTROLLER_MODE=%q (use emulated or hardware)", mode)
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
		log.Printf("[INFO] Starting TRNG controller mode=%s port=%d", mode, port)
	}

	if err := controller.Start(); err != nil {
		log.Fatalf("[ERROR] Controller error: %v", err)
	}

	if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
		log.Println("[INFO] Controller gracefully shut down")
	}
}
