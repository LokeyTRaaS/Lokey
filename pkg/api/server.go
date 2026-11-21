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
	"strings"
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
	ControllerAddr string // legacy single-controller address (first endpoint)
	Controllers    []ControllerEndpoint
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
	// Per-device TRNG quality trackers
	trngDeviceTrackers map[string]*TRNGQualityTracker
	trngDeviceMu       sync.RWMutex
	// Fortuna / VirtIO quality metrics trackers
	fortunaQualityTracker *TRNGQualityTracker
	virtioQualityTracker  *TRNGQualityTracker

	// VirtIO configuration (thread-safe)
	virtioSeedingSource string
	virtioConfigMutex   sync.RWMutex

	controllerRoundRobin  uint64
	fortunaSeedBytesAcc   atomic.Uint64
	fortunaOutputBytesAcc atomic.Uint64
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

	// Labeled queue / quality / amplification metrics
	QueueCurrent         *prometheus.GaugeVec
	QueueCapacity        *prometheus.GaugeVec
	QueuePercentage      *prometheus.GaugeVec
	ConsumedTotal        *prometheus.GaugeVec
	Unconsumed           *prometheus.GaugeVec
	MonobitAverage       *prometheus.GaugeVec
	MonobitOnes          *prometheus.GaugeVec
	MonobitZeros         *prometheus.GaugeVec
	MonobitTotal         *prometheus.GaugeVec
	RepetitionFails      *prometheus.CounterVec
	RepetitionCurrent    *prometheus.GaugeVec // absolute failure count (always published)
	APTBiasTotal         *prometheus.CounterVec
	APTBiasCurrent       *prometheus.GaugeVec // absolute bias count (always published)
	APTSamplesTotal      *prometheus.CounterVec
	BytesProduced        *prometheus.CounterVec
	FortunaSeedBytes     prometheus.Counter
	FortunaOutputBytes   prometheus.Counter
	FortunaAmplification prometheus.Gauge
	FortunaReseedTotal   prometheus.Counter
	FortunaLastReseed    prometheus.Gauge
	ServiceUp            *prometheus.GaugeVec // lokey_service_up{service=...}
	ControllerUp         *prometheus.GaugeVec // lokey_controller_up{device_id=...}
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

		QueueCurrent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_queue_current",
			Help: "Current queue depth by source",
		}, []string{"source"}),
		QueueCapacity: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_queue_capacity",
			Help: "Queue capacity by source",
		}, []string{"source"}),
		QueuePercentage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_queue_percentage",
			Help: "Queue utilization percentage by source",
		}, []string{"source"}),
		ConsumedTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_consumed_total",
			Help: "Consumed items by source",
		}, []string{"source"}),
		Unconsumed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_unconsumed",
			Help: "Unconsumed items by source",
		}, []string{"source"}),
		MonobitAverage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_randomness_monobit_average",
			Help: "Monobit ones ratio by source",
		}, []string{"source", "device_id"}),
		MonobitOnes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_randomness_monobit_ones",
			Help: "Monobit ones count by source",
		}, []string{"source", "device_id"}),
		MonobitZeros: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_randomness_monobit_zeros",
			Help: "Monobit zeros count by source",
		}, []string{"source", "device_id"}),
		MonobitTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_randomness_monobit_total",
			Help: "Monobit total bits by source",
		}, []string{"source", "device_id"}),
		RepetitionFails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lokey_randomness_repetition_failures_total",
			Help: "Repetition count test failures by source (monotonic increments)",
		}, []string{"source", "device_id"}),
		RepetitionCurrent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_randomness_repetition_failures",
			Help: "Current repetition-count test failure total by source (0 = healthy)",
		}, []string{"source", "device_id"}),
		APTBiasTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lokey_randomness_apt_bias_total",
			Help: "APT bias detections by source (monotonic increments)",
		}, []string{"source", "device_id"}),
		APTBiasCurrent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_randomness_apt_bias",
			Help: "Current APT bias detection count by source (0 = healthy)",
		}, []string{"source", "device_id"}),
		APTSamplesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lokey_randomness_apt_samples_total",
			Help: "APT samples processed by source",
		}, []string{"source", "device_id"}),
		BytesProduced: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lokey_bytes_produced_total",
			Help: "Bytes produced/stored by source",
		}, []string{"source", "device_id"}),
		FortunaSeedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lokey_fortuna_seed_bytes_total",
			Help: "Total seed bytes accepted by Fortuna via API seeding",
		}),
		FortunaOutputBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lokey_fortuna_output_bytes_total",
			Help: "Total Fortuna output bytes polled by API",
		}),
		FortunaAmplification: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lokey_fortuna_amplification_ratio",
			Help: "Fortuna output/seed amplification ratio",
		}),
		FortunaReseedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lokey_fortuna_reseed_total",
			Help: "Successful Fortuna reseed operations from API",
		}),
		FortunaLastReseed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lokey_fortuna_last_reseed_timestamp",
			Help: "Unix timestamp of last successful Fortuna reseed",
		}),
		ServiceUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_service_up",
			Help: "1 if LoKey dependency is healthy, 0 otherwise (api, controller, fortuna, virtio, database)",
		}, []string{"service"}),
		ControllerUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lokey_controller_up",
			Help: "1 if a controller device is healthy, 0 otherwise",
		}, []string{"device_id"}),
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
		metrics.QueueCurrent,
		metrics.QueueCapacity,
		metrics.QueuePercentage,
		metrics.ConsumedTotal,
		metrics.Unconsumed,
		metrics.MonobitAverage,
		metrics.MonobitOnes,
		metrics.MonobitZeros,
		metrics.MonobitTotal,
		metrics.RepetitionFails,
		metrics.RepetitionCurrent,
		metrics.APTBiasTotal,
		metrics.APTBiasCurrent,
		metrics.APTSamplesTotal,
		metrics.BytesProduced,
		metrics.FortunaSeedBytes,
		metrics.FortunaOutputBytes,
		metrics.FortunaAmplification,
		metrics.FortunaReseedTotal,
		metrics.FortunaLastReseed,
		metrics.ServiceUp,
		metrics.ControllerUp,
	)

	// Read APT window size from environment (default: 512)
	aptWindowSize := 512
	if val, ok := os.LookupEnv("TRNG_APT_WINDOW_SIZE"); ok {
		if size, err := strconv.Atoi(val); err == nil && size >= 256 && size <= 2048 {
			aptWindowSize = size
		}
	}

	controllers, err := ParseControllerAddrs(os.Getenv("CONTROLLER_ADDRS"), controllerAddr)
	if err != nil {
		log.Printf("[WARN] controller address parse: %v; using fallback %s", err, controllerAddr)
		controllers = []ControllerEndpoint{{DeviceID: "default", Addr: strings.TrimRight(controllerAddr, "/")}}
	}
	primary := controllerAddr
	if len(controllers) > 0 {
		primary = controllers[0].Addr
	}

	server := &Server{
		DB:                    db,
		ControllerAddr:        primary,
		Controllers:           controllers,
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
		trngDeviceTrackers:    make(map[string]*TRNGQualityTracker),
		fortunaQualityTracker: NewTRNGQualityTracker(aptWindowSize),
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

	// Swagger UI — gin-swagger 404s bare /swagger/ (no index match); redirect in-handler.
	swaggerHandler := ginSwagger.WrapHandler(
		swaggerFiles.Handler,
		ginSwagger.URL("/swagger/doc.json"),
		ginSwagger.DefaultModelsExpandDepth(1),
	)
	s.Router.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	s.Router.GET("/swagger/*any", func(c *gin.Context) {
		if p := c.Param("any"); p == "/" || p == "" {
			c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
			return
		}
		swaggerHandler(c)
	})

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

	// Best-effort notify VirtIO service of queue size change
	s.notifyVirtIOQueueSize(config.VirtIOQueueSize)

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
					result = append(result, int8(chunk[i])) // #nosec G115 -- intentional random byte reinterpret
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
	Controllers map[string]bool          `json:"controllers,omitempty"`
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
	stats.VirtIO = virtioStats
	s.refreshQueuePrometheusMetrics()
	s.publishQuality("trng", "aggregate", s.trngQualityTracker)
	s.publishQuality("fortuna", "fortuna", s.fortunaQualityTracker)
	s.publishQuality("virtio", "virtio", s.virtioQualityTracker)

	deviceHealth := map[string]bool{}
	for _, ep := range s.controllersOrDefault() {
		deviceHealth[ep.DeviceID] = s.checkServiceHealth(ep.Addr)
	}

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
		Controllers: deviceHealth,
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

	// Check controllers (healthy if any device is up; degraded if some are down)
	controllerOK := false
	anyControllerDown := false
	for _, ep := range s.controllersOrDefault() {
		ok := s.checkServiceHealth(ep.Addr)
		if ok {
			controllerOK = true
		} else {
			anyControllerDown = true
		}
	}
	response.Details.Controller = controllerOK

	// Check Fortuna service
	response.Details.Fortuna = s.checkServiceHealth(s.FortunaAddr)

	// Check VirtIO service
	response.Details.VirtIO = s.checkServiceHealth(s.VirtioAddr)

	// Determine overall status
	if !response.Details.Database || !response.Details.Controller || !response.Details.Fortuna || !response.Details.VirtIO || anyControllerDown {
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
	// Refresh gauges on every scrape so Grafana sees continuous samples,
	// not only when /api/v1/status is hit.
	s.publishHealthMetrics()
	s.refreshQueuePrometheusMetrics()
	promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}).ServeHTTP(c.Writer, c.Request)
}

// refreshQueuePrometheusMetrics updates queue/capacity gauges from live DB + VirtIO state.
func (s *Server) refreshQueuePrometheusMetrics() {
	if s.metrics == nil {
		return
	}
	stats, err := s.DB.GetDetailedStats()
	if err != nil {
		return
	}
	virtioStats := s.getVirtIOQueueMetrics()
	stats.VirtIO = virtioStats

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
	s.publishQueueMetrics(stats)
	s.updateFortunaAmplification()
}

// publishHealthMetrics refreshes dependency up/down gauges for Grafana status panels.
func (s *Server) publishHealthMetrics() {
	if s.metrics == nil || s.metrics.ServiceUp == nil {
		return
	}
	boolTo := func(ok bool) float64 {
		if ok {
			return 1
		}
		return 0
	}

	s.metrics.ServiceUp.WithLabelValues("api").Set(1)
	s.metrics.ServiceUp.WithLabelValues("database").Set(boolTo(s.DB.HealthCheck()))

	controllerOK := false
	for _, ep := range s.controllersOrDefault() {
		ok := s.checkServiceHealth(ep.Addr)
		if ok {
			controllerOK = true
		}
		if s.metrics.ControllerUp != nil {
			s.metrics.ControllerUp.WithLabelValues(ep.DeviceID).Set(boolTo(ok))
		}
	}
	s.metrics.ServiceUp.WithLabelValues("controller").Set(boolTo(controllerOK))
	s.metrics.ServiceUp.WithLabelValues("fortuna").Set(boolTo(s.checkServiceHealth(s.FortunaAddr)))
	s.metrics.ServiceUp.WithLabelValues("virtio").Set(boolTo(s.checkServiceHealth(s.VirtioAddr)))
}

// GetVirtIOConfig returns the current VirtIO configuration.
// @Summary Get VirtIO configuration
// @Description Get current VirtIO configuration including seeding source (TRNG, Fortuna, or both) and queue size. Queue size is queried from the VirtIO service.
// @Tags configuration
// @Accept json
// @Produce json
// @Success 200 {object} VirtIOConfig
// @Failure 500 {object} map[string]string "Server error"
// @Router /config/virtio [get]
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
// @Router /config/virtio [put]
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

	// Best-effort notify VirtIO service of queue size change
	s.notifyVirtIOQueueSize(config.QueueSize)

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
// notifyVirtIOQueueSize best-effort pushes a queue size update to the VirtIO service.
func (s *Server) notifyVirtIOQueueSize(queueSize int) {
	if s.VirtioAddr == "" || queueSize <= 0 {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	queueUpdateURL := fmt.Sprintf("%s/config/queue", s.VirtioAddr)
	jsonBody, err := json.Marshal(map[string]int{"queue_size": queueSize})
	if err != nil {
		log.Printf("[WARN] Failed to marshal VirtIO queue update: %v", err)
		return
	}
	req, err := http.NewRequest("PUT", queueUpdateURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("[WARN] Failed to create VirtIO queue update request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[WARN] VirtIO queue update failed (service may be unavailable): %v", err)
		return
	}
	_ = resp.Body.Close()
}

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
