package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

// @title           LoKey: True Random Number Generation Service API
// @version         1.0
// @description     LoKey is a high-availability, high-bandwidth true random number generation service named after Loki, the Norse god of chaos, reflecting the unpredictable nature of true randomness. This project provides affordable and accessible hardware-based true randomness through off-the-shelf components with a Go implementation.
// @description
// @description     ## Hardware Architecture
// @description     The system uses a Raspberry Pi Zero 2W and an ATECC608A cryptographic chip, creating a hardware-based solution with a modest bill of materials costing approximately €50. This provides true random number generation (TRNG) from physical entropy sources rather than algorithmic pseudo-random generation.
// @description
// @description     ## System Architecture
// @description     LoKey consists of three microservices:
// @description     - **Controller Service**: Interfaces with the ATECC608A chip to harvest true random numbers and process SHA-256 hashes
// @description     - **Fortuna Service**: Amplifies the entropy using the Fortuna algorithm for enhanced randomness
// @description     - **API Service**: Provides endpoints for configuration and both raw TRNG and Fortuna-amplified data retrieval
// @description
// @description     ## Use Cases
// @description     This service is valuable for cryptographic key generation, password management, blockchain applications, Monte Carlo simulations, gaming systems, IoT device authentication, secure communications, financial services, and scientific research requiring provably random data.
// @description
// @description     ## Data Formats
// @description     Supports multiple output formats: int8, int16, int32, int64, uint8, uint16, uint32, uint64, and raw binary data with configurable chunk sizes and pagination.
// @description
// @description     ## Repository & Support
// @description     For issues, feature requests, contributions, and detailed hardware setup instructions, visit: https://github.com/LokeyTRaaS/Lokey
// @description     For more information about the OSS initiative, please visit our website: https://lokey.cloud

// @contact.name    GitHub Repository
// @contact.url     https://github.com/LokeyTRaaS/Lokey

// @license.name    MIT License
// @license.url     https://opensource.org/licenses/MIT

// @BasePath        /api/v1
// @schemes         http https

const (
	DefaultPort                = 8080
	DefaultDbPath              = "/data/api.db"
	DefaultControllerAddr      = "http://controller:8081"
	DefaultFortunaAddr         = "http://fortuna:8082"
	DefaultTRNGQueueSize       = 1000000
	DefaultFortunaQueueSize    = 1000000
	DefaultTRNGPollInterval    = 100 * time.Millisecond
	DefaultFortunaPollInterval = 100 * time.Millisecond
)

func main() {
	// Read configuration from environment variables
	port := DefaultPort
	if val, ok := os.LookupEnv("PORT"); ok {
		if n, err := fmt.Sscanf(val, "%d", &port); n != 1 || err != nil {
			log.Printf("Invalid PORT, using default: %d", DefaultPort)
			port = DefaultPort
		}
	}

	dbPath := DefaultDbPath
	if val, ok := os.LookupEnv("DB_PATH"); ok && val != "" {
		dbPath = val
	}

	controllerAddr := DefaultControllerAddr
	if val, ok := os.LookupEnv("CONTROLLER_ADDR"); ok && val != "" {
		controllerAddr = val
	}

	fortunaAddr := DefaultFortunaAddr
	if val, ok := os.LookupEnv("FORTUNA_ADDR"); ok && val != "" {
		fortunaAddr = val
	}

	trngQueueSize := DefaultTRNGQueueSize
	if val, ok := os.LookupEnv("TRNG_QUEUE_SIZE"); ok {
		if n, err := fmt.Sscanf(val, "%d", &trngQueueSize); n != 1 || err != nil {
			log.Printf("Invalid TRNG_QUEUE_SIZE, using default: %d", DefaultTRNGQueueSize)
			trngQueueSize = DefaultTRNGQueueSize
		}
	}

	fortunaQueueSize := DefaultFortunaQueueSize
	if val, ok := os.LookupEnv("FORTUNA_QUEUE_SIZE"); ok {
		if n, err := fmt.Sscanf(val, "%d", &fortunaQueueSize); n != 1 || err != nil {
			log.Printf("Invalid FORTUNA_QUEUE_SIZE, using default: %d", DefaultFortunaQueueSize)
			fortunaQueueSize = DefaultFortunaQueueSize
		}
	}

	trngPollIntervalMs := DefaultTRNGPollInterval.Milliseconds()
	if val, ok := os.LookupEnv("TRNG_POLL_INTERVAL_MS"); ok {
		if n, err := fmt.Sscanf(val, "%d", &trngPollIntervalMs); n != 1 || err != nil {
			log.Printf("Invalid TRNG_POLL_INTERVAL_MS, using default: %d", DefaultTRNGPollInterval.Milliseconds())
			trngPollIntervalMs = DefaultTRNGPollInterval.Milliseconds()
		}
	}
	trngPollInterval := time.Duration(trngPollIntervalMs) * time.Millisecond

	fortunaPollIntervalMs := DefaultFortunaPollInterval.Milliseconds()
	if val, ok := os.LookupEnv("FORTUNA_POLL_INTERVAL_MS"); ok {
		if n, err := fmt.Sscanf(val, "%d", &fortunaPollIntervalMs); n != 1 || err != nil {
			log.Printf("Invalid FORTUNA_POLL_INTERVAL_MS, using default: %d", DefaultFortunaPollInterval.Milliseconds())
			fortunaPollIntervalMs = DefaultFortunaPollInterval.Milliseconds()
		}
	}
	fortunaPollInterval := time.Duration(fortunaPollIntervalMs) * time.Millisecond

	// Initialize database using the factory function
	db, err := database.NewDBHandler(dbPath, trngQueueSize, fortunaQueueSize)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create API server
	server := api.NewServer(db, controllerAddr, fortunaAddr, port)

	// Create context for polling that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start polling in the background
	server.StartPolling(ctx, trngPollInterval, fortunaPollInterval)

	// Setup graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s, shutting down gracefully", sig)
		cancel() // Stop polling
		// Give polling goroutines time to shut down
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	log.Printf("Starting API server with configuration:")
	log.Printf("  Database Path: %s", dbPath)
	log.Printf("  Port: %d", port)
	log.Printf("  Controller Address: %s", controllerAddr)
	log.Printf("  Fortuna Address: %s", fortunaAddr)
	log.Printf("  TRNG Queue Size: %d", trngQueueSize)
	log.Printf("  Fortuna Queue Size: %d", fortunaQueueSize)
	log.Printf("  TRNG Poll Interval: %s", trngPollInterval)
	log.Printf("  Fortuna Poll Interval: %s", fortunaPollInterval)

	if err := server.Run(); err != nil {
		log.Fatalf("API server error: %v", err)
	}
}
