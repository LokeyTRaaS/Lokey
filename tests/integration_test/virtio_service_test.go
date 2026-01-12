package integration_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lokey/rng-service/pkg/virtio"
)

// VirtIOServiceTestHelper wraps the VirtIO service for testing
// This mirrors the structure from cmd/virtio/main.go
type VirtIOServiceTestHelper struct {
	device *virtio.VirtIORNG
	router *gin.Engine
	pipe   *virtio.NamedPipe
}

// QueueConfig represents the queue configuration request/response
type QueueConfig struct {
	QueueSize int `json:"queue_size" binding:"required"`
}

func setupVirtIOServiceForTest(t *testing.T, queueSize int) (*VirtIOServiceTestHelper, func()) {
	// Use an empty device path
	device, err := virtio.NewVirtIORNG("", queueSize)
	if err != nil {
		t.Fatalf("Failed to create VirtIO device: %v", err)
	}

	// Create named pipe (use temp directory for testing)
	tmpDir := t.TempDir()
	pipePath := tmpDir + "/virtio-rng"
	pipe, err := virtio.NewNamedPipe(pipePath)
	if err != nil {
		t.Fatalf("Failed to create named pipe: %v", err)
	}

	// Set Gin to test mode to reduce logging
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	service := &VirtIOServiceTestHelper{
		device: device,
		router: router,
		pipe:   pipe,
	}

	// Setup routes
	service.setupRoutes()

	cleanup := func() {
		pipe.Stop()
		device.Close()
	}

	return service, cleanup
}

func (v *VirtIOServiceTestHelper) setupRoutes() {
	v.router.GET("/health", v.healthCheckHandler)
	v.router.GET("/info", v.infoHandler)
	v.router.GET("/generate", v.generateHandler)
	v.router.GET("/stream", v.streamHandler)
	v.router.POST("/seed", v.seedHandler)
	v.router.GET("/config/queue", v.getQueueConfigHandler)
	v.router.PUT("/config/queue", v.updateQueueConfigHandler)
}

func (v *VirtIOServiceTestHelper) healthCheckHandler(ctx *gin.Context) {
	healthy := v.device.HealthCheck()
	if healthy {
		ctx.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": "2024-01-01T00:00:00Z", // Fixed for testing
		})
	} else {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    "unhealthy",
			"timestamp": "2024-01-01T00:00:00Z",
		})
	}
}

func (v *VirtIOServiceTestHelper) infoHandler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status":    "running",
		"pipe_path": v.pipe.Path(),
	})
}

func (v *VirtIOServiceTestHelper) streamHandler(ctx *gin.Context) {
	// Simplified streaming handler for testing
	// Get optional query parameters
	chunkSizeStr := ctx.DefaultQuery("chunk_size", "1024")
	chunkSize, err := strconv.Atoi(chunkSizeStr)
	if err != nil || chunkSize < 1 || chunkSize > 65536 {
		chunkSize = 1024
	}

	// Set headers for streaming
	ctx.Header("Content-Type", "application/octet-stream")
	ctx.Header("Transfer-Encoding", "chunked")
	ctx.Writer.WriteHeader(http.StatusOK)
	ctx.Writer.Flush()

	// Stream a few chunks for testing
	for i := 0; i < 3; i++ {
		data, _, err := v.device.PullBytes(chunkSize)
		if err != nil {
			return
		}
		if _, err := ctx.Writer.Write(data); err != nil {
			return
		}
		ctx.Writer.Flush()
	}
}

func (v *VirtIOServiceTestHelper) generateHandler(ctx *gin.Context) {
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
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate random data: queue may be empty"})
			return
		}

		// Ensure we have exactly 32 bytes (pad if needed, though this shouldn't happen)
		if len(randomData) < 32 {
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

func (v *VirtIOServiceTestHelper) seedHandler(ctx *gin.Context) {
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

	seededCount := 0
	for _, seedHex := range request.Seeds {
		seed, err := hex.DecodeString(seedHex)
		if err != nil {
			continue
		}

		if err := v.device.Seed(seed); err != nil {
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

func (v *VirtIOServiceTestHelper) getQueueConfigHandler(ctx *gin.Context) {
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

func (v *VirtIOServiceTestHelper) updateQueueConfigHandler(ctx *gin.Context) {
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
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update queue size"})
		return
	}

	queueCurrent := v.device.GetQueueCurrent()

	ctx.JSON(http.StatusOK, gin.H{
		"queue_size":    config.QueueSize,
		"queue_current": queueCurrent,
	})
}

func TestVirtIOService_HealthEndpoint(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 200 or 503, got %d", w.Code)
	}
}

func TestVirtIOService_InfoEndpoint(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/info", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["status"] != "running" {
		t.Errorf("Expected status 'running', got '%s'", result["status"])
	}
}

func TestVirtIOService_GenerateEndpoint(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	// Seed some data first to ensure we have data available
	testData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	service.device.Seed(testData)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/generate?count=1", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Data) == 0 {
		t.Error("Expected non-empty data")
	}

	// Verify it's valid hex
	_, err := hex.DecodeString(result.Data)
	if err != nil {
		t.Errorf("Data is not valid hex: %v", err)
	}
}

func TestVirtIOService_GenerateMultiple(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	// Seed multiple items to ensure we have enough data
	for i := 0; i < 10; i++ {
		testData := make([]byte, 32)
		for j := 0; j < 32; j++ {
			testData[j] = byte(i*32 + j)
		}
		service.device.Seed(testData)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/generate?count=5", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var result struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Data) != 5 {
		t.Errorf("Expected 5 data items, got %d", len(result.Data))
	}
}

func TestVirtIOService_SeedEndpoint(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	// Generate some test data
	testSeeds := []string{
		hex.EncodeToString([]byte{1, 2, 3, 4, 5}),
		hex.EncodeToString([]byte{6, 7, 8, 9, 10}),
	}

	seedRequest := map[string][]string{
		"seeds": testSeeds,
	}

	jsonData, err := json.Marshal(seedRequest)
	if err != nil {
		t.Fatalf("Failed to marshal seed request: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/seed", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var result struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Status != "seeded" {
		t.Errorf("Expected status 'seeded', got '%s'", result.Status)
	}

	if result.Count != 2 {
		t.Errorf("Expected count 2, got %d", result.Count)
	}
}

func TestVirtIOService_GetQueueConfig(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/config/queue", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var result struct {
		QueueSize      int   `json:"queue_size"`
		QueueCurrent   int   `json:"queue_current"`
		TotalGenerated int64 `json:"total_generated"`
		ConsumedCount  int64 `json:"consumed_count"`
		QueueDropped   int64 `json:"queue_dropped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.QueueSize <= 0 {
		t.Error("Expected positive queue size")
	}
	
	// Verify new metrics fields are present (may be 0 initially)
	if result.TotalGenerated < 0 {
		t.Error("total_generated should be non-negative")
	}
	if result.ConsumedCount < 0 {
		t.Error("consumed_count should be non-negative")
	}
	if result.QueueDropped < 0 {
		t.Error("queue_dropped should be non-negative")
	}
}

func TestVirtIOService_UpdateQueueConfig(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	updateRequest := map[string]int{
		"queue_size": 100000,
	}

	jsonData, err := json.Marshal(updateRequest)
	if err != nil {
		t.Fatalf("Failed to marshal update request: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/config/queue", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var result struct {
		QueueSize    int `json:"queue_size"`
		QueueCurrent int `json:"queue_current"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.QueueSize != 100000 {
		t.Errorf("Expected queue size 100000, got %d", result.QueueSize)
	}
}
