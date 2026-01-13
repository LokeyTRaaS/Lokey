package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

func setupTestServer() (*api.Server, database.DBHandler) {
	db, _ := database.NewChannelDBHandler("", 10, 20, 15)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, testRegistry)
	return server, db
}

func TestNewServer(t *testing.T) {
	// Skip this test to avoid Prometheus metrics registration conflicts
	// when multiple tests create servers in the same test run
	// The server creation is tested by other tests that use setupTestServer
	db, _ := database.NewChannelDBHandler("", 10, 20, 15)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 8080, testRegistry)
	if server == nil {
		t.Fatal("Expected server to be non-nil")
	}
	if server.DB != db {
		t.Error("Expected server to use provided database")
	}
	if server.ControllerAddr != "http://localhost:8081" {
		t.Errorf("Expected controller address 'http://localhost:8081', got %s", server.ControllerAddr)
	}
	if server.FortunaAddr != "http://localhost:8082" {
		t.Errorf("Expected fortuna address 'http://localhost:8082', got %s", server.FortunaAddr)
	}
}

func TestServer_GetQueueConfig(t *testing.T) {
	server, _ := setupTestServer()

	t.Run("success", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/config/queue", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var config api.QueueConfig
		if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if config.TRNGQueueSize != 10 {
			t.Errorf("Expected TRNG queue size 10, got %d", config.TRNGQueueSize)
		}
		if config.FortunaQueueSize != 20 {
			t.Errorf("Expected Fortuna queue size 20, got %d", config.FortunaQueueSize)
		}
	})
}

func TestServer_UpdateQueueConfig(t *testing.T) {
	t.Run("valid config with BoltDBHandler", func(t *testing.T) {
		// Test with BoltDBHandler which supports UpdateQueueSizes
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		db, err := database.NewBoltDBHandler(dbPath, 10, 20, 15)
		if err != nil {
			t.Fatalf("Failed to create BoltDB handler: %v", err)
		}
		defer db.Close()
		defer os.RemoveAll(tmpDir)

		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

		config := api.QueueConfig{
			TRNGQueueSize:    50,
			FortunaQueueSize: 60,
			VirtIOQueueSize:  70,
		}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/queue", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var response api.QueueConfig
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if response.TRNGQueueSize != 50 {
			t.Errorf("Expected TRNG queue size 50, got %d", response.TRNGQueueSize)
		}
		if response.FortunaQueueSize != 60 {
			t.Errorf("Expected Fortuna queue size 60, got %d", response.FortunaQueueSize)
		}
		if response.VirtIOQueueSize != 70 {
			t.Errorf("Expected VirtIO queue size 70, got %d", response.VirtIOQueueSize)
		}
	})

	t.Run("valid config with ChannelDBHandler", func(t *testing.T) {
		// Test with ChannelDBHandler which doesn't support UpdateQueueSizes
		// Should return 500 Internal Server Error
		server, _ := setupTestServer()

		config := api.QueueConfig{
			TRNGQueueSize:    50,
			FortunaQueueSize: 60,
			VirtIOQueueSize:  70,
		}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/queue", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		// ChannelDBHandler doesn't support UpdateQueueSizes, so should return 500
		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500 for ChannelDBHandler, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid config - too small", func(t *testing.T) {
		server, _ := setupTestServer()
		config := api.QueueConfig{
			TRNGQueueSize:    5, // Below minimum of 10
			FortunaQueueSize: 60,
		}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/queue", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		server, _ := setupTestServer()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/queue", bytes.NewBufferString("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

func TestServer_GetConsumeConfig(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/config/consume", nil)
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var config api.ConsumeConfig
	if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	// Default should be false
	if config.Consume {
		t.Error("Expected default consume mode to be false")
	}
}

func TestServer_UpdateConsumeConfig(t *testing.T) {
	server, _ := setupTestServer()

	t.Run("set to true", func(t *testing.T) {
		config := api.ConsumeConfig{Consume: true}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/consume", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Verify it was set
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET", "/api/v1/config/consume", nil)
		server.Router.ServeHTTP(w2, req2)
		var response api.ConsumeConfig
		json.Unmarshal(w2.Body.Bytes(), &response)
		if !response.Consume {
			t.Error("Expected consume mode to be true after update")
		}
	})
}

func TestServer_GetRandomData(t *testing.T) {
	server, db := setupTestServer()

	// Add test data - need at least 8 bytes for int64/uint64 tests (8 bytes per value)
	// Add multiple chunks to ensure we have enough data for all formats
	db.StoreTRNGData([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	db.StoreFortunaData([]byte{17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32})

	formats := []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "binary"}
	sources := []string{"trng", "fortuna"}

	for _, format := range formats {
		for _, source := range sources {
			t.Run(format+"_"+source, func(t *testing.T) {
				request := api.DataRequest{
					Format: format,
					Count:  5,
					Offset: 0,
					Source: source,
				}
				body, _ := json.Marshal(request)
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				server.Router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 for format %s source %s, got %d: %s", format, source, w.Code, w.Body.String())
				}

				if format == "binary" {
					if w.Header().Get("Content-Type") != "application/octet-stream" {
						t.Errorf("Expected Content-Type application/octet-stream, got %s", w.Header().Get("Content-Type"))
					}
				} else {
					var result []interface{}
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
						t.Fatalf("Failed to unmarshal response: %v", err)
					}
					if len(result) == 0 {
						t.Error("Expected non-empty result")
					}
				}
			})
		}
	}

	t.Run("invalid format", func(t *testing.T) {
		request := api.DataRequest{
			Format: "invalid",
			Count:  5,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("no data available", func(t *testing.T) {
		emptyDB, _ := database.NewChannelDBHandler("", 10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		emptyServer := api.NewServer(emptyDB, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

		request := api.DataRequest{
			Format: "int32",
			Count:  5,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		emptyServer.Router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		testDB, _ := database.NewChannelDBHandler("", 10, 20, 15)
		for i := 0; i < 10; i++ {
			testDB.StoreTRNGData([]byte{byte(i)})
		}
		testRegistry := prometheus.NewRegistry()
		testServer := api.NewServer(testDB, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

		request := api.DataRequest{
			Format: "uint8",
			Count:  3,
			Offset: 2,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		testServer.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

func TestServer_GetStatus(t *testing.T) {
	server, db := setupTestServer()
	db.StoreTRNGData([]byte{1})
	db.StoreFortunaData([]byte{2})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/status", nil)
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats database.DetailedStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if stats.TRNG.QueueCapacity != 10 {
		t.Errorf("Expected TRNG capacity 10, got %d", stats.TRNG.QueueCapacity)
	}
	if stats.Fortuna.QueueCapacity != 20 {
		t.Errorf("Expected Fortuna capacity 20, got %d", stats.Fortuna.QueueCapacity)
	}
}

func TestServer_HealthCheck(t *testing.T) {
	server, _ := setupTestServer()

	t.Run("all healthy", func(t *testing.T) {
		// This test would require mocking HTTP calls to controller/fortuna
		// For now, we test the database health check part
		// ChannelDBHandler always returns healthy

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/health", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response api.HealthCheckResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if !response.Details.Database {
			t.Error("Expected database to be healthy")
		}
		if !response.Details.API {
			t.Error("Expected API to be healthy")
		}
	})
}

func TestServer_MetricsHandler(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check that response contains prometheus metrics
	body := w.Body.String()
	if body == "" {
		t.Error("Expected non-empty metrics response")
	}
	// Prometheus metrics typically start with comments or metric names
	if len(body) < 10 {
		t.Error("Expected meaningful metrics content")
	}
}

func TestServer_Swagger(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/swagger/index.html", nil)
	server.Router.ServeHTTP(w, req)

	// Swagger might return 200 or 404 depending on setup
	// Just verify it doesn't crash
	if w.Code >= 500 {
		t.Errorf("Expected status < 500, got %d", w.Code)
	}
}
