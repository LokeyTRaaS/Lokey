package api

import (
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	_ "github.com/lokey/rng-service/pkg/api/docs"
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

	// API v1 group
	api := s.router.Group("/api/v1")
	{
		// Configuration endpoints
		api.GET("/config/queue", s.GetQueueConfig)
		api.PUT("/config/queue", s.UpdateQueueConfig)

		// Data retrieval endpoints
		api.POST("/data", s.GetRandomData)

		// Status endpoints
		api.GET("/status", s.GetStatus)
		api.GET("/health", s.HealthCheck)
		api.GET("/metrics", s.MetricsHandler)
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
	if !s.db.HealthCheck() {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database is not healthy",
		})
		return
	}

	queueInfo, err := s.db.GetQueueInfo()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get queue info: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, QueueConfig{
		TRNGQueueSize:    queueInfo["trng_queue_capacity"],
		FortunaQueueSize: queueInfo["fortuna_queue_capacity"],
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

	if err := s.validate.Struct(config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.db.UpdateQueueSizes(config.TRNGQueueSize, config.FortunaQueueSize); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update queue configuration"})
		return
	}

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

	// Always consume data (delete-on-read)
	consumeData := true

	// Calculate chunks needed
	bytesPerValue := getBytesPerValue(request.Format)
	estimatedBytesNeeded := request.Count * bytesPerValue
	estimatedChunksNeeded := (estimatedBytesNeeded / 31) + 5

	// Retrieve data
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

	s.metrics.DatabaseSizeBytes.Set(float64(stats.Database.SizeBytes))

	c.JSON(http.StatusOK, stats)
}

// @Summary Health check endpoint
// @Description Checks health of the API server and its dependencies
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
	response.Details.Database = s.db.HealthCheck()

	// Check API (always true if we got here)
	response.Details.API = true

	// Check controller service
	response.Details.Controller = s.checkServiceHealth(s.controllerAddr)

	// Check Fortuna service
	response.Details.Fortuna = s.checkServiceHealth(s.fortunaAddr)

	// Determine overall status
	if !response.Details.Database || !response.Details.Controller || !response.Details.Fortuna {
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
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}
