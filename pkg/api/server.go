package api

import (
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	_ "github.com/lokey/rng-service/pkg/api/docs" // Import generated swagger docs
	"github.com/lokey/rng-service/pkg/database"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Server struct {
	db             database.DBHandler
	controllerAddr string
	fortunaAddr    string
	port           int
	router         *gin.Engine
	validate       *validator.Validate
	metrics        *Metrics
}

// QueueConfig represents the queue configuration
type QueueConfig struct {
	TRNGQueueSize    int `json:"trng_queue_size" validate:"required,min=10,max=10000"`
	FortunaQueueSize int `json:"fortuna_queue_size" validate:"required,min=10,max=10000"`
}

// ConsumptionConfig represents the consumption behavior configuration
type ConsumptionConfig struct {
	DeleteOnConsumption bool `json:"delete_on_consumption"`
}

// DataRequest represents a request for random data
type DataRequest struct {
	Format string `json:"format" validate:"required,oneof=int8 int16 int32 int64 uint8 uint16 uint32 uint64 binary"`
	Count  int    `json:"limit" validate:"required,min=1,max=100000"`
	Offset int    `json:"offset" validate:"min=0"`
	Source string `json:"source" validate:"required,oneof=trng fortuna"`
}

// HealthCheckResponse represents the health check response
type HealthCheckResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Details   struct {
		API        bool `json:"api"`
		Controller bool `json:"controller"`
		Fortuna    bool `json:"fortuna"`
		Database   bool `json:"database"`
	} `json:"details"`
}

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

	DatabaseSizeBytes prometheus.Gauge
}

// NewServer creates a new API server
func NewServer(db database.DBHandler, controllerAddr, fortunaAddr string, port int) *Server {
	router := gin.Default()
	validate := validator.New()

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

		DatabaseSizeBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "database_size_bytes",
			Help: "Size of database in bytes",
		}),
	}

	// Register metrics
	prometheus.MustRegister(
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
		metrics.DatabaseSizeBytes,
	)

	server := &Server{
		db:             db,
		controllerAddr: controllerAddr,
		fortunaAddr:    fortunaAddr,
		port:           port,
		router:         router,
		validate:       validate,
		metrics:        metrics,
	}
	// Setup routes
	server.setupRoutes()

	return server
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Add CORS middleware
	s.router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Swagger docs
	s.router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Prometheus metrics endpoint
	s.router.GET("/metrics", s.MetricsHandler)
	s.router.GET("api/v1/metrics", s.MetricsHandler) // For backwards compatibility

	// API v1 group
	api := s.router.Group("/api/v1")
	{
		// Configuration endpoints
		api.GET("/config/queue", s.GetQueueConfig)
		api.PUT("/config/queue", s.UpdateQueueConfig)
		api.GET("/config/consumption", s.GetConsumptionConfig)
		api.PUT("/config/consumption", s.UpdateConsumptionConfig)

		// Data retrieval endpoints
		api.POST("/data", s.GetRandomData)

		// Status endpoints
		api.GET("/status", s.GetStatus)
		api.GET("/health", s.HealthCheck)
	}
}

// Run starts the API server
func (s *Server) Run() error {
	return s.router.Run(fmt.Sprintf(":%d", s.port))
}

// @Summary         Get queue configuration
// @Description     Get current queue size configuration for TRNG and Fortuna data
// @Tags            configuration
// @Accept          json
// @Produce         json
// @Success         200 {object} QueueConfig
// @Failure         500 {object} map[string]string "Database error"
// @Router          /config/queue [get]
func (s *Server) GetQueueConfig(c *gin.Context) {
	// Check if database is initialized properly
	if !s.db.HealthCheck() {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database is not healthy or not properly initialized",
		})
		return
	}

	// Try to get queue info
	queueInfo, err := s.db.GetQueueInfo()
	if err != nil {
		// Log the error
		log.Printf("Error getting queue info: %v", err)

		// Return default values instead of failing
		c.JSON(http.StatusOK, QueueConfig{
			TRNGQueueSize:    100, // Default value
			FortunaQueueSize: 100, // Default value
		})
		return
	}

	// Safely extract values
	var trngSize, fortunaSize int

	if trngVal, ok := queueInfo["trng_queue_capacity"]; ok {
		trngSize = trngVal
	} else {
		trngSize = 100 // Default value
	}

	if fortunaVal, ok := queueInfo["fortuna_queue_capacity"]; ok {
		fortunaSize = fortunaVal
	} else {
		fortunaSize = 100 // Default value
	}

	// Return the configuration
	c.JSON(http.StatusOK, QueueConfig{
		TRNGQueueSize:    trngSize,
		FortunaQueueSize: fortunaSize,
	})
}

// @Summary Update queue configuration
// @Description Update queue size configuration for TRNG and Fortuna data
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

	// Validate config
	if err := s.validate.Struct(config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update queue sizes
	err := s.db.UpdateQueueSizes(config.TRNGQueueSize, config.FortunaQueueSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update queue configuration"})
		return
	}

	c.JSON(http.StatusOK, config)
}

// @Summary Get consumption configuration
// @Description Get current consumption behavior configuration
// @Tags configuration
// @Accept json
// @Produce json
// @Success 200 {object} ConsumptionConfig
// @Router /config/consumption [get]
func (s *Server) GetConsumptionConfig(c *gin.Context) {
	// For now, this is hardcoded since it's stored in memory
	// In a real implementation, this would be stored in a configuration store
	config := ConsumptionConfig{
		DeleteOnConsumption: true,
	}

	c.JSON(http.StatusOK, config)
}

// @Summary Update consumption configuration
// @Description Update consumption behavior configuration
// @Tags configuration
// @Accept json
// @Produce json
// @Param config body ConsumptionConfig true "Consumption configuration"
// @Success 200 {object} ConsumptionConfig
// @Failure 400 {object} map[string]string "Invalid request"
// @Router /config/consumption [put]
func (s *Server) UpdateConsumptionConfig(c *gin.Context) {
	var config ConsumptionConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// In a real implementation, this would update a configuration store
	// For now, we just return the received configuration

	c.JSON(http.StatusOK, config)
}

// @Summary Get random data
// @Description Retrieve random data in various formats with pagination
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
// In GetRandomData method:
func (s *Server) GetRandomData(c *gin.Context) {
	var request DataRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate request
	if err := s.validate.Struct(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get consumption configuration
	var consumeData bool = true // Default to true

	// Calculate how many database chunks we might need
	// We'll fetch more chunks than needed to ensure we have enough data
	bytesPerValue := getBytesPerValue(request.Format)
	estimatedBytesNeeded := request.Count * bytesPerValue
	estimatedChunksNeeded := (estimatedBytesNeeded / 31) + 5 // Assume ~31 bytes per chunk, add buffer

	// Retrieve data based on source
	var rawData [][]byte
	var err error
	if request.Source == "trng" {
		rawData, err = s.db.GetTRNGData(estimatedChunksNeeded, request.Offset, consumeData)
	} else {
		rawData, err = s.db.GetFortunaData(estimatedChunksNeeded, request.Offset, consumeData)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve data"})
		return
	}

	if len(rawData) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No data available"})
		return
	}

	// Process data based on requested format
	switch request.Format {
	case "binary":
		// For binary format, return the requested count of bytes
		binaryData := make([]byte, 0)
		bytesNeeded := request.Count

		for _, data := range rawData {
			if len(binaryData) >= bytesNeeded {
				break
			}
			remaining := bytesNeeded - len(binaryData)
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
		response := convertToIntFormat(rawData, request.Count, 1, true)
		c.JSON(http.StatusOK, response)

	case "uint8":
		response := convertToIntFormat(rawData, request.Count, 1, false)
		c.JSON(http.StatusOK, response)

	case "int16":
		response := convertToIntFormat(rawData, request.Count, 2, true)
		c.JSON(http.StatusOK, response)

	case "uint16":
		response := convertToIntFormat(rawData, request.Count, 2, false)
		c.JSON(http.StatusOK, response)

	case "int32":
		response := convertToIntFormat(rawData, request.Count, 4, true)
		c.JSON(http.StatusOK, response)

	case "uint32":
		response := convertToIntFormat(rawData, request.Count, 4, false)
		c.JSON(http.StatusOK, response)

	case "int64":
		response := convertToIntFormat(rawData, request.Count, 8, true)
		c.JSON(http.StatusOK, response)

	case "uint64":
		response := convertToIntFormat(rawData, request.Count, 8, false)
		c.JSON(http.StatusOK, response)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported format"})
	}
}

// Helper function to calculate bytes needed per value
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
		// Process each value (bytesPerValue at a time)
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
					result = append(result, int16(binary.BigEndian.Uint16(chunk[i:i+2])))
				} else {
					result = append(result, binary.BigEndian.Uint16(chunk[i:i+2]))
				}
			case 4:
				if signed {
					result = append(result, int32(binary.BigEndian.Uint32(chunk[i:i+4])))
				} else {
					result = append(result, binary.BigEndian.Uint32(chunk[i:i+4]))
				}
			case 8:
				if signed {
					result = append(result, int64(binary.BigEndian.Uint64(chunk[i:i+8])))
				} else {
					result = append(result, binary.BigEndian.Uint64(chunk[i:i+8]))
				}
			}
			valuesGenerated++
		}

		// Break if we've generated enough values
		if valuesGenerated >= maxCount {
			break
		}
	}

	return result
}

// @Summary         Get system status
// @Description     Get detailed status of TRNG and Fortuna systems with comprehensive metrics
// @Tags            status
// @Accept          json
// @Produce         json
// @Success         200 {object} database.DetailedStats
// @Failure         500 {object} map[string]string "Server error"
// @Router          /status [get]
func (s *Server) GetStatus(c *gin.Context) {
	stats, err := s.db.GetDetailedStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get system status"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// @Summary         Check system health
// @Description     Check health of all system components
// @Tags            status
// @Accept          json
// @Produce         json
// @Success         200 {object} HealthCheckResponse
// @Router          /health [get]
func (s *Server) HealthCheck(c *gin.Context) {
	// Check database health
	dbHealthy := s.db.HealthCheck()

	// Check controller health (simplified for example)
	controllerHealthy := checkServiceHealth(s.controllerAddr + "/health")

	// Check fortuna service health (simplified for example)
	fortunaHealthy := checkServiceHealth(s.fortunaAddr + "/health")

	// Determine overall status
	overallStatus := "healthy"
	if !dbHealthy || !controllerHealthy || !fortunaHealthy {
		overallStatus = "unhealthy"
	}

	response := HealthCheckResponse{
		Status:    overallStatus,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	response.Details.API = true // API is running if we're handling this request
	response.Details.Controller = controllerHealthy
	response.Details.Fortuna = fortunaHealthy
	response.Details.Database = dbHealthy

	c.JSON(http.StatusOK, response)
}

// @Summary Get Prometheus metrics (root)
// @Description Returns all Prometheus metrics at root and /api/v1. The 2 endpoints are equivalent are there for compatibility reasons.
// @Tags metrics
// @Tags metrics
// @Produce text/plain
// @Success 200 {string} string "Prometheus metrics"
// @Router /metrics [get]
func (s *Server) MetricsHandler(c *gin.Context) {
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}

// checkServiceHealth checks if a service is healthy by making an HTTP request
func checkServiceHealth(url string) bool {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Health check failed for %s: %v", url, err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
