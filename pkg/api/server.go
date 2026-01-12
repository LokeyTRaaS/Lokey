package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/lokey/rng-service/pkg/api/docs"
	"github.com/lokey/rng-service/pkg/database"
)

// Server represents the HTTP API server for the random number generation service.
type Server struct {
	DB             database.DBHandler
	ControllerAddr string
	FortunaAddr    string
	VirtioAddr     string
	port           int
	Router         *gin.Engine
	validate       *validator.Validate
	metrics        *Metrics
	registry       *prometheus.Registry
	consumeMode    bool         // Global consume setting
	consumeMutex   sync.RWMutex // Protects consumeMode

	// Seeding circuit breaker
	seedingFailures    atomic.Int64
	seedingCircuitOpen atomic.Bool
	lastSeedingFailure atomic.Int64 // Unix timestamp
	seedingCooldown    time.Duration

	// VirtIO seeding circuit breaker
	virtioSeedingFailures    atomic.Int64
	virtioSeedingCircuitOpen atomic.Bool
	lastVirtIOSeedingFailure atomic.Int64 // Unix timestamp
	virtioSeedingCooldown    time.Duration

	// TRNG quality metrics tracker
	trngQualityTracker *TRNGQualityTracker
	// VirtIO quality metrics tracker
	virtioQualityTracker *TRNGQualityTracker

	// VirtIO configuration (thread-safe)
	virtioSeedingSource string
	virtioConfigMutex   sync.RWMutex
}

// QueueConfig represents the queue configuration
type QueueConfig struct {
	TRNGQueueSize    int `json:"trng_queue_size" validate:"required,min=10,max=1000000"`
	FortunaQueueSize int `json:"fortuna_queue_size" validate:"required,min=10,max=1000000"`
	VirtIOQueueSize  int `json:"virtio_queue_size" validate:"required,min=10,max=1000000"`
}

// VirtIOConfig represents the VirtIO configuration
type VirtIOConfig struct {
	SeedingSource string `json:"seeding_source" validate:"required,oneof=trng fortuna both"`
	QueueSize     int    `json:"queue_size" validate:"required,min=10,max=1000000"`
}

// ConsumeConfig represents the consume mode configuration
type ConsumeConfig struct {
	Consume bool `json:"consume"`
}

// DataRequest represents a request for random data
type DataRequest struct {
	Format string `json:"format" validate:"required,oneof=int8 int16 int32 int64 uint8 uint16 uint32 uint64 binary"`
	Count  int    `json:"limit" validate:"required,min=1,max=100000"`
	Offset int    `json:"offset" validate:"min=0"`
	Source string `json:"source" validate:"required,oneof=trng fortuna virtio"`
}

// HealthCheckResponse represents the health check response
type HealthCheckResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Details   struct {
		API        bool `json:"api"`
		Controller bool `json:"controller"`
		Fortuna    bool `json:"fortuna"`
		VirtIO     bool `json:"virtio"`
		Database   bool `json:"database"`
	} `json:"details"`
}

// Metrics holds Prometheus metrics for the API server.
type Metrics struct {
	TRNGQueueCurrent    prometheus.Gauge
	TRNGQueueCapacity   prometheus.Gauge
	TRNGQueuePercentage prometheus.Gauge
	TRNGConsumed        prometheus.Gauge
	TRNGUnconsumed      prometheus.Gauge

	FortunaQueueCurrent    prometheus.Gauge
	FortunaQueueCapacity   prometheus.Gauge
	FortunaQueuePercentage prometheus.Gauge
	FortunaConsumed        prometheus.Gauge
	FortunaUnconsumed      prometheus.Gauge

	VirtIOQueueCurrent    prometheus.Gauge
	VirtIOQueueCapacity   prometheus.Gauge
	VirtIOQueuePercentage prometheus.Gauge
	VirtIOConsumed        prometheus.Gauge
	VirtIOUnconsumed      prometheus.Gauge

	DatabaseSizeBytes prometheus.Gauge
}

// NewServer creates a new API server
func NewServer(db database.DBHandler, controllerAddr, fortunaAddr, virtioAddr string, port int, reg *prometheus.Registry) *Server {
	router := gin.Default()
	validate := validator.New()

	// Initialize Swagger documentation
	docs.SwaggerInfo.BasePath = "/api/v1"

	// Use default registry if none provided
	if reg == nil {
		reg, _ = prometheus.DefaultRegisterer.(*prometheus.Registry) //nolint:errcheck // Type assertion result handled by nil check
		if reg == nil {
			reg = prometheus.NewRegistry()
		}
	}

	metrics := &Metrics{
		TRNGQueueCurrent: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trng_queue_current",
			Help: "Current size of TRNG queue",
		}),
		TRNGQueueCapacity: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trng_queue_capacity",
			Help: "Capacity of TRNG queue",
		}),
		TRNGQueuePercentage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trng_queue_percentage",
			Help: "Percentage of TRNG queue used",
		}),
		TRNGConsumed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trng_consumed",
			Help: "Number of TRNG items consumed",
		}),
		TRNGUnconsumed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "trng_unconsumed",
			Help: "Number of TRNG items unconsumed",
		}),

		FortunaQueueCurrent: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "fortuna_queue_current",
			Help: "Current size of Fortuna queue",
		}),
		FortunaQueueCapacity: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "fortuna_queue_capacity",
			Help: "Capacity of Fortuna queue",
		}),
		FortunaQueuePercentage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "fortuna_queue_percentage",
			Help: "Percentage of Fortuna queue used",
		}),
		FortunaConsumed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "fortuna_consumed",
			Help: "Number of Fortuna items consumed",
		}),
		FortunaUnconsumed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "fortuna_unconsumed",
			Help: "Number of Fortuna items unconsumed",
		}),

		VirtIOQueueCurrent: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "virtio_queue_current",
			Help: "Current size of VirtIO queue",
		}),
		VirtIOQueueCapacity: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "virtio_queue_capacity",
			Help: "Capacity of VirtIO queue",
		}),
		VirtIOQueuePercentage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "virtio_queue_percentage",
			Help: "Percentage of VirtIO queue used",
		}),
		VirtIOConsumed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "virtio_consumed",
			Help: "Number of VirtIO items consumed",
		}),
		VirtIOUnconsumed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "virtio_unconsumed",
			Help: "Number of VirtIO items unconsumed",
		}),

		DatabaseSizeBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "database_size_bytes",
			Help: "Size of database in bytes",
		}),
	}

	// Register metrics to the provided registry
	reg.MustRegister(
		metrics.TRNGQueueCurrent,
		metrics.TRNGQueueCapacity,
		metrics.TRNGQueuePercentage,
		metrics.TRNGConsumed,
		metrics.TRNGUnconsumed,
		metrics.FortunaQueueCurrent,
		metrics.FortunaQueueCapacity,
		metrics.FortunaQueuePercentage,
		metrics.FortunaConsumed,
		metrics.FortunaUnconsumed,
		metrics.VirtIOQueueCurrent,
		metrics.VirtIOQueueCapacity,
		metrics.VirtIOQueuePercentage,
		metrics.VirtIOConsumed,
		metrics.VirtIOUnconsumed,
		metrics.DatabaseSizeBytes,
	)

	// Read APT window size from environment (default: 512)
	aptWindowSize := 512
	if val, ok := os.LookupEnv("TRNG_APT_WINDOW_SIZE"); ok {
		if size, err := strconv.Atoi(val); err == nil && size >= 256 && size <= 2048 {
			aptWindowSize = size
		}
	}

	server := &Server{
		DB:                    db,
		ControllerAddr:        controllerAddr,
		FortunaAddr:           fortunaAddr,
		VirtioAddr:            virtioAddr,
		port:                  port,
		Router:                router,
		validate:              validate,
		metrics:               metrics,
		registry:              reg,
		consumeMode:           false,           // Default: don't consume (read-only mode)
		seedingCooldown:       5 * time.Minute, // Default cooldown for seeding circuit breaker
		virtioSeedingCooldown: 5 * time.Minute, // Default cooldown for VirtIO seeding circuit breaker
		trngQualityTracker:    NewTRNGQualityTracker(aptWindowSize),
		virtioQualityTracker:  NewTRNGQualityTracker(aptWindowSize),
		virtioSeedingSource:   "trng", // Default seeding source
	}

	server.setupRoutes()
	return server
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Add CORS middleware
	s.Router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Swagger UI - uses embedded docs from swaggo
	s.Router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Prometheus metrics endpoint
	s.Router.GET("/metrics", s.MetricsHandler)

	// API v1 group
	api := s.Router.Group("/api/v1")
	{
		// Configuration endpoints
		api.GET("/config/queue", s.GetQueueConfig)
		api.PUT("/config/queue", s.UpdateQueueConfig)
		api.GET("/config/consume", s.GetConsumeConfig)
		api.PUT("/config/consume", s.UpdateConsumeConfig)
		// VirtIO configuration endpoints (also under /api/v1 for Swagger compatibility)
		api.GET("/config/virtio", s.GetVirtIOConfig)
		api.PUT("/config/virtio", s.UpdateVirtIOConfig)

		// Data retrieval endpoints
		api.POST("/data", s.GetRandomData)

		// Status endpoints
		api.GET("/status", s.GetStatus)
		api.GET("/health", s.HealthCheck)
		api.GET("/metrics", s.MetricsHandler)
	}

	// VirtIO configuration endpoints (top-level, without /api/v1 prefix for direct access)
	s.Router.GET("/config/virtio", s.GetVirtIOConfig)
	s.Router.PUT("/config/virtio", s.UpdateVirtIOConfig)
}

// Run starts the API server
func (s *Server) Run() error {
	return s.Router.Run(fmt.Sprintf(":%d", s.port))
}

// GetQueueConfig returns the current queue configuration.
// @Summary Get queue configuration
// @Description Get current queue size configuration for TRNG, Fortuna, and VirtIO data
// @Tags            configuration
// @Accept          json
// @Produce         json
// @Success         200 {object} QueueConfig
// @Failure         500 {object} map[string]string "Database error"
// @Router          /config/queue [get]
func (s *Server) GetQueueConfig(c *gin.Context) {
	if !s.DB.HealthCheck() {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database is not healthy",
		})
		return
	}

	queueInfo, err := s.DB.GetQueueInfo()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get queue info: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, QueueConfig{
		TRNGQueueSize:    queueInfo["trng_queue_capacity"],
		FortunaQueueSize: queueInfo["fortuna_queue_capacity"],
		VirtIOQueueSize:  queueInfo["virtio_queue_capacity"],
	})
}

// UpdateQueueConfig updates the queue configuration.
// @Summary Update queue configuration
// @Description Update queue size configuration for TRNG, Fortuna, and VirtIO data. VirtIO queue size changes are forwarded to the VirtIO service.
// @Tags configuration
// @Accept json
// @Produce json
// @Param config body QueueConfig true "Queue configuration"
// @Success 200 {object} QueueConfig
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Server error"
// @Router /config/queue [put]
func (s *Server) UpdateQueueConfig(c *gin.Context) {
	var config QueueConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := s.validate.Struct(config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.DB.UpdateQueueSizes(config.TRNGQueueSize, config.FortunaQueueSize, config.VirtIOQueueSize); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update queue configuration"})
		return
	}

	// Send queue size update to VirtIO service if it changed
	if s.VirtioAddr != "" && config.VirtIOQueueSize > 0 {
		client := &http.Client{Timeout: 5 * time.Second}
		queueUpdateURL := fmt.Sprintf("%s/config/queue", s.VirtioAddr)
		queueUpdateBody := map[string]int{"queue_size": config.VirtIOQueueSize}
		jsonBody, _ := json.Marshal(queueUpdateBody)
		req, _ := http.NewRequest("PUT", queueUpdateURL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		_, _ = client.Do(req) // Ignore errors - VirtIO service might not be available
	}

	c.JSON(http.StatusOK, config)
}

// GetConsumeConfig returns the current consume mode configuration.
// @Summary Get consume configuration
// @Description Get current consume mode setting (true = delete-on-read, false = keep data in queue)
// @Tags            configuration
// @Accept          json
// @Produce         json
// @Success         200 {object} ConsumeConfig
// @Router          /config/consume [get]
func (s *Server) GetConsumeConfig(c *gin.Context) {
	s.consumeMutex.RLock()
	consumeMode := s.consumeMode
	s.consumeMutex.RUnlock()

	c.JSON(http.StatusOK, ConsumeConfig{
		Consume: consumeMode,
	})
}

// UpdateConsumeConfig updates the consume mode configuration.
// @Summary Update consume configuration
// @Description Update consume mode setting (true = delete-on-read, false = keep data in queue)
// @Tags            configuration
// @Accept          json
// @Produce         json
// @Param           config body ConsumeConfig true "Consume configuration"
// @Success         200 {object} ConsumeConfig
// @Failure         400 {object} map[string]string "Invalid request"
// @Router          /config/consume [put]
func (s *Server) UpdateConsumeConfig(c *gin.Context) {
	var config ConsumeConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	s.consumeMutex.Lock()
	s.consumeMode = config.Consume
	s.consumeMutex.Unlock()

	log.Printf("Consume mode updated to: %v", config.Consume)

	c.JSON(http.StatusOK, config)
}

// GetRandomData retrieves random data in various formats with pagination.
// @Summary Get random data
// @Description Retrieve random data in various formats with pagination. Valid sources are "trng" (True Random Number Generator) and "fortuna" (Fortuna CSPRNG). Note: VirtIO is a consumer endpoint for virtualization (named pipe/HTTP streaming) and cannot be used as a data source here. To access VirtIO data, use the named pipe or HTTP streaming endpoint directly.
// @Tags data
// @Accept json
// @Produce json
// @Produce application/octet-stream
// @Param request body DataRequest true "Data request parameters"
// @Success 200 {array} interface{} "Random data in requested format"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 404 {object} map[string]string "Not enough data available"
// @Failure 500 {object} map[string]string "Server error"
// @Router /data [post]
func (s *Server) GetRandomData(c *gin.Context) {
	var request DataRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := s.validate.Struct(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Use global consume mode setting
	s.consumeMutex.RLock()
	consumeData := s.consumeMode
	s.consumeMutex.RUnlock()

	// Calculate chunks needed
	bytesPerValue := getBytesPerValue(request.Format)
	estimatedBytesNeeded := request.Count * bytesPerValue
	estimatedChunksNeeded := (estimatedBytesNeeded / 31) + 5

	// Retrieve data
	var rawData [][]byte
	var err error
	switch request.Source {
	case "trng":
		rawData, err = s.DB.GetTRNGData(estimatedChunksNeeded, request.Offset, consumeData)
	case "fortuna":
		rawData, err = s.DB.GetFortunaData(estimatedChunksNeeded, request.Offset, consumeData)
	case "virtio":
		// VirtIO is a consumer endpoint (for VMs via named pipe/HTTP streaming), not a data source for the API
		// Data flows one way: API seeds VirtIO, VirtIO serves VMs
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "VirtIO is a consumer endpoint for virtualization (named pipe/HTTP streaming), not a data source. " +
				"Use 'trng' or 'fortuna' as the source instead. " +
				"To access VirtIO data, use the named pipe (/var/run/lokey/virtio-rng) or HTTP streaming endpoint (GET /stream).",
		})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve data"})
		return
	}

	if len(rawData) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No data available"})
		return
	}

	// Process data based on format
	switch request.Format {
	case "binary":
		binaryData := make([]byte, 0, request.Count)
		for _, data := range rawData {
			if len(binaryData) >= request.Count {
				break
			}
			remaining := request.Count - len(binaryData)
			if len(data) <= remaining {
				binaryData = append(binaryData, data...)
			} else {
				binaryData = append(binaryData, data[:remaining]...)
			}
		}

		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", "attachment; filename=random.bin")
		c.Data(http.StatusOK, "application/octet-stream", binaryData)

	case "int8":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 1, true))
	case "uint8":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 1, false))
	case "int16":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 2, true))
	case "uint16":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 2, false))
	case "int32":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 4, true))
	case "uint32":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 4, false))
	case "int64":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 8, true))
	case "uint64":
		c.JSON(http.StatusOK, convertToIntFormat(rawData, request.Count, 8, false))
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported format"})
	}
}

// getBytesPerValue returns bytes needed per value for a given format
func getBytesPerValue(format string) int {
	switch format {
	case "int8", "uint8":
		return 1
	case "int16", "uint16":
		return 2
	case "int32", "uint32":
		return 4
	case "int64", "uint64":
		return 8
	default:
		return 1
	}
}

// convertToIntFormat converts raw byte data to various integer formats
func convertToIntFormat(data [][]byte, maxCount, bytesPerValue int, signed bool) []interface{} {
	var result []interface{}
	valuesGenerated := 0

	for _, chunk := range data {
		for i := 0; i <= len(chunk)-bytesPerValue && valuesGenerated < maxCount; i += bytesPerValue {
			switch bytesPerValue {
			case 1:
				if signed {
					result = append(result, int8(chunk[i]))
				} else {
					result = append(result, uint8(chunk[i]))
				}
			case 2:
				if signed {
					// Safe conversion - reading from byte slice
					result = append(result, int16(binary.BigEndian.Uint16(chunk[i:i+2]))) // #nosec G115
				} else {
					result = append(result, binary.BigEndian.Uint16(chunk[i:i+2]))
				}
			case 4:
				if signed {
					// Safe conversion - reading from byte slice
					result = append(result, int32(binary.BigEndian.Uint32(chunk[i:i+4]))) // #nosec G115
				} else {
					result = append(result, binary.BigEndian.Uint32(chunk[i:i+4]))
				}
			case 8:
				if signed {
					// Safe conversion - reading from byte slice
					result = append(result, int64(binary.BigEndian.Uint64(chunk[i:i+8]))) // #nosec G115
				} else {
					result = append(result, binary.BigEndian.Uint64(chunk[i:i+8]))
				}
			}
			valuesGenerated++
		}

		if valuesGenerated >= maxCount {
			break
		}
	}

	return result
}

// getVirtIOQueueMetrics queries the VirtIO service for queue metrics
func (s *Server) getVirtIOQueueMetrics() database.DataSourceStats {
	stats := database.DataSourceStats{}

	if s.VirtioAddr == "" {
		return stats // Return zero stats if VirtIO not configured
	}

	client := &http.Client{Timeout: 5 * time.Second}
	queueConfigURL := fmt.Sprintf("%s/config/queue", s.VirtioAddr)
	resp, err := client.Get(queueConfigURL)
	if err != nil {
		// VirtIO service unavailable - return zero stats
		return stats
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Warning: failed to close VirtIO queue metrics response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return stats // Return zero stats on error
	}

	// Use map[string]interface{} to handle both int and int64 values from JSON
	var queueInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&queueInfo); err != nil {
		log.Printf("Warning: failed to decode VirtIO queue metrics: %v", err)
		return stats
	}

	// Extract queue_size and queue_current (always int)
	if size, ok := queueInfo["queue_size"].(float64); ok {
		stats.QueueCapacity = int(size)
	}
	if current, ok := queueInfo["queue_current"].(float64); ok {
		stats.QueueCurrent = int(current)
	}

	// Extract metrics that may be int64 (total_generated, consumed_count, queue_dropped)
	if totalGen, ok := queueInfo["total_generated"].(float64); ok {
		stats.TotalGenerated = int64(totalGen)
	}
	if consumed, ok := queueInfo["consumed_count"].(float64); ok {
		stats.ConsumedCount = int64(consumed)
	}
	if dropped, ok := queueInfo["queue_dropped"].(float64); ok {
		stats.QueueDropped = int64(dropped)
	}

	// Calculate derived metrics
	stats.UnconsumedCount = stats.QueueCurrent
	// Note: PollingCount is not set - it will be omitted from VirtIO response

	if stats.QueueCapacity > 0 {
		stats.QueuePercentage = float64(stats.QueueCurrent) / float64(stats.QueueCapacity) * 100
	}

	return stats
}

// VirtIOStats represents VirtIO statistics without polling_count (VirtIO is not polled, only seeded)
type VirtIOStats struct {
	QueueCurrent    int     `json:"queue_current"`
	QueueCapacity   int     `json:"queue_capacity"`
	QueuePercentage float64 `json:"queue_percentage"`
	QueueDropped    int64   `json:"queue_dropped"`
	ConsumedCount   int64   `json:"consumed_count"`
	UnconsumedCount int     `json:"unconsumed_count"`
	TotalGenerated  int64   `json:"total_generated"`
	// Note: polling_count is intentionally omitted - VirtIO is not polled by the API
}

// StatusResponse represents the status response with custom VirtIO stats
type StatusResponse struct {
	TRNG        database.DataSourceStats `json:"trng"`
	Fortuna     database.DataSourceStats `json:"fortuna"`
	VirtIO      VirtIOStats              `json:"virtio"`
	Database    database.Stats           `json:"database"`
	TRNGQuality database.QualityMetrics  `json:"trng_quality"`
}

// GetStatus returns the system status.
// @Summary Get system status
// @Description Get detailed status of TRNG, Fortuna, and VirtIO systems with comprehensive metrics. VirtIO metrics are queried directly from the VirtIO service. Note: VirtIO does not have polling_count as it is not polled by the API (only seeded).
// @Tags            status
// @Accept          json
// @Produce         json
// @Success         200 {object} StatusResponse
// @Failure         500 {object} map[string]string "Server error"
// @Router          /status [get]
func (s *Server) GetStatus(c *gin.Context) {
	stats, err := s.DB.GetDetailedStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get system status"})
		return
	}

	// Add quality metrics from tracker
	stats.TRNGQuality = s.trngQualityTracker.GetQualityMetrics()

	// Get VirtIO metrics from VirtIO service directly (not from API database)
	virtioStats := s.getVirtIOQueueMetrics()

	// Update Prometheus metrics
	s.metrics.TRNGQueueCurrent.Set(float64(stats.TRNG.QueueCurrent))
	s.metrics.TRNGQueueCapacity.Set(float64(stats.TRNG.QueueCapacity))
	s.metrics.TRNGQueuePercentage.Set(stats.TRNG.QueuePercentage)
	s.metrics.TRNGConsumed.Set(float64(stats.TRNG.ConsumedCount))
	s.metrics.TRNGUnconsumed.Set(float64(stats.TRNG.UnconsumedCount))

	s.metrics.FortunaQueueCurrent.Set(float64(stats.Fortuna.QueueCurrent))
	s.metrics.FortunaQueueCapacity.Set(float64(stats.Fortuna.QueueCapacity))
	s.metrics.FortunaQueuePercentage.Set(stats.Fortuna.QueuePercentage)
	s.metrics.FortunaConsumed.Set(float64(stats.Fortuna.ConsumedCount))
	s.metrics.FortunaUnconsumed.Set(float64(stats.Fortuna.UnconsumedCount))

	s.metrics.VirtIOQueueCurrent.Set(float64(virtioStats.QueueCurrent))
	s.metrics.VirtIOQueueCapacity.Set(float64(virtioStats.QueueCapacity))
	s.metrics.VirtIOQueuePercentage.Set(virtioStats.QueuePercentage)
	s.metrics.VirtIOConsumed.Set(float64(virtioStats.ConsumedCount))
	s.metrics.VirtIOUnconsumed.Set(float64(virtioStats.UnconsumedCount))

	s.metrics.DatabaseSizeBytes.Set(float64(stats.Database.SizeBytes))

	// Build response with custom VirtIO stats (without polling_count)
	response := StatusResponse{
		TRNG:        stats.TRNG,
		Fortuna:     stats.Fortuna,
		Database:    stats.Database,
		TRNGQuality: stats.TRNGQuality,
		VirtIO: VirtIOStats{
			QueueCurrent:    virtioStats.QueueCurrent,
			QueueCapacity:   virtioStats.QueueCapacity,
			QueuePercentage: virtioStats.QueuePercentage,
			QueueDropped:    virtioStats.QueueDropped,
			ConsumedCount:   virtioStats.ConsumedCount,
			UnconsumedCount: virtioStats.UnconsumedCount,
			TotalGenerated:  virtioStats.TotalGenerated,
		},
	}

	c.JSON(http.StatusOK, response)
}

// HealthCheck performs a health check on the API server and its dependencies.
// @Summary Health check endpoint
// @Description Checks health of the API server and its dependencies (Controller, Fortuna, VirtIO, and Database services)
// @Tags status
// @Accept json
// @Produce json
// @Success 200 {object} HealthCheckResponse
// @Router /health [get]
func (s *Server) HealthCheck(c *gin.Context) {
	response := HealthCheckResponse{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Check database
	response.Details.Database = s.DB.HealthCheck()

	// Check API (always true if we got here)
	response.Details.API = true

	// Check controller service
	response.Details.Controller = s.checkServiceHealth(s.ControllerAddr)

	// Check Fortuna service
	response.Details.Fortuna = s.checkServiceHealth(s.FortunaAddr)

	// Check VirtIO service
	response.Details.VirtIO = s.checkServiceHealth(s.VirtioAddr)

	// Determine overall status
	if !response.Details.Database || !response.Details.Controller || !response.Details.Fortuna || !response.Details.VirtIO {
		response.Status = "degraded"
	}

	c.JSON(http.StatusOK, response)
}

// checkServiceHealth checks if a service is reachable and healthy
func (s *Server) checkServiceHealth(serviceAddr string) bool {
	// Build URL safely
	url := serviceAddr + "/health"

	// URL is constructed from validated server configuration, not user input
	resp, err := http.Get(url) // #nosec G107
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Warning: failed to close health check response body: %v", closeErr)
		}
	}()

	return resp.StatusCode == http.StatusOK
}

// MetricsHandler serves Prometheus metrics
func (s *Server) MetricsHandler(c *gin.Context) {
	promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}).ServeHTTP(c.Writer, c.Request)
}

// GetVirtIOConfig returns the current VirtIO configuration.
// @Summary Get VirtIO configuration
// @Description Get current VirtIO configuration including seeding source (TRNG, Fortuna, or both) and queue size. Queue size is queried from the VirtIO service.
// @Tags configuration
// @Accept json
// @Produce json
// @Success 200 {object} VirtIOConfig
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/v1/config/virtio [get]
func (s *Server) GetVirtIOConfig(c *gin.Context) {
	s.virtioConfigMutex.RLock()
	seedingSource := s.virtioSeedingSource
	s.virtioConfigMutex.RUnlock()

	// Get queue size from VirtIO service or database
	queueSize := 0
	if s.VirtioAddr != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		queueConfigURL := fmt.Sprintf("%s/config/queue", s.VirtioAddr)
		resp, err := client.Get(queueConfigURL)
		if err == nil {
			defer resp.Body.Close()
			var queueInfo map[string]int
			if err := json.NewDecoder(resp.Body).Decode(&queueInfo); err == nil {
				queueSize = queueInfo["queue_size"]
			}
		}
	}

	// Fallback to database if VirtIO service unavailable
	if queueSize == 0 {
		queueInfo, err := s.DB.GetQueueInfo()
		if err == nil {
			queueSize = queueInfo["virtio_queue_capacity"]
		}
	}

	c.JSON(http.StatusOK, VirtIOConfig{
		SeedingSource: seedingSource,
		QueueSize:     queueSize,
	})
}

// UpdateVirtIOConfig updates the VirtIO configuration.
// @Summary Update VirtIO configuration
// @Description Update VirtIO seeding source (TRNG, Fortuna, or both) and queue size. Queue size changes are forwarded to the VirtIO service. Seeding source changes take effect immediately.
// @Tags configuration
// @Accept json
// @Produce json
// @Param config body VirtIOConfig true "VirtIO configuration"
// @Success 200 {object} VirtIOConfig
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/v1/config/virtio [put]
func (s *Server) UpdateVirtIOConfig(c *gin.Context) {
	var config VirtIOConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := s.validate.Struct(config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update internal state
	s.virtioConfigMutex.Lock()
	oldSource := s.virtioSeedingSource
	s.virtioSeedingSource = config.SeedingSource
	s.virtioConfigMutex.Unlock()

	// Send queue size update to VirtIO service
	if s.VirtioAddr != "" && config.QueueSize > 0 {
		client := &http.Client{Timeout: 5 * time.Second}
		queueUpdateURL := fmt.Sprintf("%s/config/queue", s.VirtioAddr)
		queueUpdateBody := map[string]int{"queue_size": config.QueueSize}
		jsonBody, _ := json.Marshal(queueUpdateBody)
		req, _ := http.NewRequest("PUT", queueUpdateURL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		_, _ = client.Do(req) // Ignore errors - VirtIO service might not be available
	}

	// Update database queue size
	if err := s.DB.UpdateQueueSizes(0, 0, config.QueueSize); err != nil {
		// Log but don't fail - queue size update is best-effort
		log.Printf("[WARN] Failed to update VirtIO queue size in database: %v", err)
	}

	// If seeding source changed, notify polling manager (will be handled in polling.go)
	if oldSource != config.SeedingSource {
		// This will be handled by the polling manager when it checks the source
		log.Printf("[INFO] VirtIO seeding source changed from %s to %s", oldSource, config.SeedingSource)
	}

	c.JSON(http.StatusOK, config)
}

// SetVirtIOSeedingSource sets the VirtIO seeding source
func (s *Server) SetVirtIOSeedingSource(source string) {
	s.virtioConfigMutex.Lock()
	defer s.virtioConfigMutex.Unlock()
	s.virtioSeedingSource = source
}

// isVirtIOSeedingCircuitOpen checks if the VirtIO seeding circuit breaker is open
func (s *Server) isVirtIOSeedingCircuitOpen() bool {
	if !s.virtioSeedingCircuitOpen.Load() {
		return false
	}

	// Check if cooldown period has passed
	lastFailure := s.lastVirtIOSeedingFailure.Load()
	if lastFailure == 0 {
		return false
	}

	cooldownEnd := time.Unix(lastFailure, 0).Add(s.virtioSeedingCooldown)
	if time.Now().After(cooldownEnd) {
		// Cooldown expired, try to close circuit
		s.virtioSeedingCircuitOpen.Store(false)
		s.virtioSeedingFailures.Store(0)
		return false
	}

	return true
}
