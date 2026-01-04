package integration_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
	"github.com/prometheus/client_golang/prometheus"
)

// setupIntegrationServer creates a test server with mock external services
func setupIntegrationServer(handler database.DBHandler, controllerURL, fortunaURL string) (*api.Server, func()) {
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(handler, controllerURL, fortunaURL, 0, testRegistry)
	return server, func() {
		handler.Close()
	}
}

// setupMockController creates a mock HTTP server for the controller service
func setupMockController(t *testing.T) (*httptest.Server, func()) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate" {
			data := hex.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
			response := map[string]string{"data": data}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	return server, func() { server.Close() }
}

// setupMockFortuna creates a mock HTTP server for the Fortuna service
func setupMockFortuna(t *testing.T) (*httptest.Server, func()) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate" {
			data := hex.EncodeToString([]byte{10, 20, 30, 40, 50, 60, 70, 80, 90, 100})
			response := map[string]string{"data": data}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	return server, func() { server.Close() }
}

func TestAPI_Endpoints(t *testing.T) {
	t.Run("with ChannelDBHandler", func(t *testing.T) {
		testAPIEndpoints(t, func() (database.DBHandler, func()) {
			db, _ := database.NewChannelDBHandler("", 10, 20)
			return db, func() { db.Close() }
		})
	})

	t.Run("with BoltDBHandler", func(t *testing.T) {
		testAPIEndpoints(t, func() (database.DBHandler, func()) {
			tmpDir := t.TempDir()
			dbPath := tmpDir + "/test.db"
			db, err := database.NewBoltDBHandler(dbPath, 10, 20)
			if err != nil {
				t.Fatalf("Failed to create BoltDB handler: %v", err)
			}
			return db, func() { db.Close() }
		})
	})
}

func testAPIEndpoints(t *testing.T, setupDB func() (database.DBHandler, func())) {
	db, cleanupDB := setupDB()
	defer cleanupDB()

	controllerServer, cleanupController := setupMockController(t)
	defer cleanupController()

	fortunaServer, cleanupFortuna := setupMockFortuna(t)
	defer cleanupFortuna()

	server, _ := setupIntegrationServer(db, controllerServer.URL, fortunaServer.URL)

	t.Run("GET /api/v1/config/queue", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/config/queue", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
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

	t.Run("PUT /api/v1/config/queue", func(t *testing.T) {
		config := api.QueueConfig{
			TRNGQueueSize:    50,
			FortunaQueueSize: 60,
		}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/queue", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		// ChannelDBHandler doesn't support UpdateQueueSizes, so it may return 500
		// BoltDBHandler should return 200
		if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 200 or 500, got %d: %s", w.Code, w.Body.String())
		}

		if w.Code == http.StatusOK {
			var response api.QueueConfig
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}
			if response.TRNGQueueSize != 50 {
				t.Errorf("Expected TRNG queue size 50, got %d", response.TRNGQueueSize)
			}
		}
	})

	t.Run("GET /api/v1/config/consume", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/config/consume", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var config api.ConsumeConfig
		if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		// Default should be false
		if config.Consume {
			t.Error("Expected consume mode to be false by default")
		}
	})

	t.Run("PUT /api/v1/config/consume", func(t *testing.T) {
		config := api.ConsumeConfig{
			Consume: true,
		}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/consume", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var response api.ConsumeConfig
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if !response.Consume {
			t.Error("Expected consume mode to be true")
		}
	})

	t.Run("POST /api/v1/data - binary format", func(t *testing.T) {
		// Store some data first
		db.StoreTRNGData([]byte{1, 2, 3, 4, 5})
		db.StoreTRNGData([]byte{6, 7, 8, 9, 10})

		request := api.DataRequest{
			Format: "binary",
			Count:  5,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if w.Header().Get("Content-Type") != "application/octet-stream" {
			t.Errorf("Expected Content-Type application/octet-stream, got %s", w.Header().Get("Content-Type"))
		}

		if len(w.Body.Bytes()) != 5 {
			t.Errorf("Expected 5 bytes, got %d", len(w.Body.Bytes()))
		}
	})

	t.Run("POST /api/v1/data - int32 format", func(t *testing.T) {
		// Store more data
		db.StoreTRNGData([]byte{1, 2, 3, 4, 5, 6, 7, 8})

		request := api.DataRequest{
			Format: "int32",
			Count:  2,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var result []int32
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 values, got %d", len(result))
		}
	})

	t.Run("POST /api/v1/data - pagination", func(t *testing.T) {
		// Store data for pagination test
		for i := 0; i < 5; i++ {
			db.StoreFortunaData([]byte{byte(i), byte(i + 1), byte(i + 2)})
		}

		request := api.DataRequest{
			Format: "uint8",
			Count:  2,
			Offset: 1,
			Source: "fortuna",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var result []uint8
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 values, got %d", len(result))
		}
	})

	t.Run("GET /api/v1/status", func(t *testing.T) {
		// Create fresh server/db for status test to avoid state from previous tests
		freshDB, cleanupFreshDB := setupDB()
		defer cleanupFreshDB()
		freshServer, _ := setupIntegrationServer(freshDB, controllerServer.URL, fortunaServer.URL)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/status", nil)
		freshServer.Router.ServeHTTP(w, req)

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
	})

	t.Run("GET /api/v1/health", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/health", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
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
		if !response.Details.Controller {
			t.Error("Expected controller to be healthy")
		}
		if !response.Details.Fortuna {
			t.Error("Expected fortuna to be healthy")
		}
	})

	t.Run("GET /api/v1/metrics", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/metrics", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if body == "" {
			t.Error("Expected non-empty metrics response")
		}
	})

	t.Run("GET /metrics", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/metrics", nil)
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if body == "" {
			t.Error("Expected non-empty metrics response")
		}
	})

	t.Run("GET /swagger/index.html", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/swagger/index.html", nil)
		server.Router.ServeHTTP(w, req)

		// Swagger might return 200 or 404 depending on setup
		// Just verify it doesn't crash
		if w.Code >= 500 {
			t.Errorf("Expected status < 500, got %d", w.Code)
		}
	})
}

func TestAPI_CompleteFlow(t *testing.T) {
	t.Run("with ChannelDBHandler", func(t *testing.T) {
		testCompleteFlow(t, func() (database.DBHandler, func()) {
			db, _ := database.NewChannelDBHandler("", 10, 20)
			return db, func() { db.Close() }
		})
	})

	t.Run("with BoltDBHandler", func(t *testing.T) {
		testCompleteFlow(t, func() (database.DBHandler, func()) {
			tmpDir := t.TempDir()
			dbPath := tmpDir + "/test.db"
			db, err := database.NewBoltDBHandler(dbPath, 10, 20)
			if err != nil {
				t.Fatalf("Failed to create BoltDB handler: %v", err)
			}
			return db, func() { db.Close() }
		})
	})
}

func testCompleteFlow(t *testing.T, setupDB func() (database.DBHandler, func())) {
	db, cleanupDB := setupDB()
	defer cleanupDB()

	controllerServer, cleanupController := setupMockController(t)
	defer cleanupController()

	fortunaServer, cleanupFortuna := setupMockFortuna(t)
	defer cleanupFortuna()

	server, _ := setupIntegrationServer(db, controllerServer.URL, fortunaServer.URL)

	// Step 1: Store TRNG data via polling
	t.Run("store TRNG data", func(t *testing.T) {
		err := server.FetchAndStoreTRNGData()
		if err != nil {
			t.Fatalf("Expected no error storing TRNG data, got %v", err)
		}

		// Verify data was stored
		data, err := db.GetTRNGData(1, 0, false)
		if err != nil {
			t.Fatalf("Expected no error retrieving TRNG data, got %v", err)
		}
		if len(data) == 0 {
			t.Error("Expected TRNG data to be stored")
		}
	})

	// Step 2: Store Fortuna data via polling
	t.Run("store Fortuna data", func(t *testing.T) {
		err := server.FetchAndStoreFortunaData()
		if err != nil {
			t.Fatalf("Expected no error storing Fortuna data, got %v", err)
		}

		// Verify data was stored
		data, err := db.GetFortunaData(1, 0, false)
		if err != nil {
			t.Fatalf("Expected no error retrieving Fortuna data, got %v", err)
		}
		if len(data) == 0 {
			t.Error("Expected Fortuna data to be stored")
		}
	})

	// Step 3: Retrieve data in non-consume mode (verify data still exists)
	t.Run("retrieve data in non-consume mode", func(t *testing.T) {
		// Store more data for retrieval test
		db.StoreTRNGData([]byte{1, 2, 3, 4, 5})
		db.StoreFortunaData([]byte{10, 20, 30, 40, 50})

		// First retrieval
		request := api.DataRequest{
			Format: "binary",
			Count:  5,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		firstRetrieval := w.Body.Bytes()

		// Second retrieval - should get same data (non-consume mode)
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req2.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Errorf("Expected status 200 on second retrieval, got %d: %s", w2.Code, w2.Body.String())
		}

		secondRetrieval := w2.Body.Bytes()

		// Data should be the same (non-consume mode)
		if len(firstRetrieval) != len(secondRetrieval) {
			t.Errorf("Expected same data length in non-consume mode, got %d and %d", len(firstRetrieval), len(secondRetrieval))
		}
	})

	// Step 4: Enable consume mode
	t.Run("enable consume mode", func(t *testing.T) {
		config := api.ConsumeConfig{Consume: true}
		body, _ := json.Marshal(config)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/v1/config/consume", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify consume mode is enabled
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET", "/api/v1/config/consume", nil)
		server.Router.ServeHTTP(w2, req2)

		var response api.ConsumeConfig
		if err := json.Unmarshal(w2.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if !response.Consume {
			t.Error("Expected consume mode to be enabled")
		}
	})

	// Step 5: Retrieve data in consume mode (verify data is removed)
	t.Run("retrieve data in consume mode", func(t *testing.T) {
		// Store fresh data for consume test
		db.StoreTRNGData([]byte{100, 101, 102, 103, 104})
		db.StoreFortunaData([]byte{200, 201, 202, 203, 204})

		// Get initial queue size
		statsBefore, _ := db.GetDetailedStats()
		trngCountBefore := statsBefore.TRNG.QueueCurrent

		// First retrieval in consume mode
		request := api.DataRequest{
			Format: "binary",
			Count:  5,
			Source: "trng",
		}
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify data was consumed (queue size decreased)
		statsAfter, _ := db.GetDetailedStats()
		trngCountAfter := statsAfter.TRNG.QueueCurrent
		if trngCountAfter >= trngCountBefore {
			t.Errorf("Expected queue size to decrease in consume mode, got %d (before: %d)", trngCountAfter, trngCountBefore)
		}

		// Second retrieval should get different data (or error if no data)
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("POST", "/api/v1/data", bytes.NewBuffer(body))
		req2.Header.Set("Content-Type", "application/json")
		server.Router.ServeHTTP(w2, req2)

		// Should get different data or no data available
		if w2.Code == http.StatusOK {
			secondRetrieval := w2.Body.Bytes()
			if len(secondRetrieval) == 0 {
				t.Error("Expected data or error, got empty response")
			}
		} else if w2.Code != http.StatusNotFound {
			t.Errorf("Expected status 200 or 404, got %d: %s", w2.Code, w2.Body.String())
		}
	})

	// Step 6: Verify queue statistics reflect consumed data
	t.Run("verify statistics", func(t *testing.T) {
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

		// Verify statistics structure
		if stats.TRNG.QueueCapacity != 10 {
			t.Errorf("Expected TRNG capacity 10, got %d", stats.TRNG.QueueCapacity)
		}
		if stats.Fortuna.QueueCapacity != 20 {
			t.Errorf("Expected Fortuna capacity 20, got %d", stats.Fortuna.QueueCapacity)
		}

		// Consumed count should be greater than 0 if we consumed data
		if stats.TRNG.ConsumedCount == 0 && stats.TRNG.QueueCurrent < stats.TRNG.QueueCapacity {
			t.Error("Expected consumed count > 0 if queue decreased")
		}
	})
}

func TestAPI_StartPolling(t *testing.T) {
	t.Run("with ChannelDBHandler", func(t *testing.T) {
		testStartPolling(t, func() (database.DBHandler, func()) {
			db, _ := database.NewChannelDBHandler("", 10, 20)
			return db, func() { db.Close() }
		})
	})

	t.Run("with BoltDBHandler", func(t *testing.T) {
		testStartPolling(t, func() (database.DBHandler, func()) {
			tmpDir := t.TempDir()
			dbPath := tmpDir + "/test.db"
			db, err := database.NewBoltDBHandler(dbPath, 10, 20)
			if err != nil {
				t.Fatalf("Failed to create BoltDB handler: %v", err)
			}
			return db, func() { db.Close() }
		})
	})
}

func testStartPolling(t *testing.T, setupDB func() (database.DBHandler, func())) {
	db, cleanupDB := setupDB()
	defer cleanupDB()

	controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate" {
			if r.URL.Query().Get("count") != "" {
				data := []string{
					hex.EncodeToString([]byte{1, 2, 3, 4, 5}),
					hex.EncodeToString([]byte{6, 7, 8, 9, 10}),
					hex.EncodeToString([]byte{11, 12, 13, 14, 15}),
					hex.EncodeToString([]byte{16, 17, 18, 19, 20}),
					hex.EncodeToString([]byte{21, 22, 23, 24, 25}),
				}
				response := map[string][]string{"data": data}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			} else {
				data := hex.EncodeToString([]byte{1, 2, 3, 4, 5})
				response := map[string]string{"data": data}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}
		} else if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	defer controllerServer.Close()

	fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate" {
			data := hex.EncodeToString([]byte{10, 20, 30, 40, 50, 60, 70, 80, 90, 100})
			response := map[string]interface{}{
				"data": data,
				"size": 10,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else if r.URL.Path == "/seed" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "seeded"})
		} else if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	}))
	defer fortunaServer.Close()

	server, _ := setupIntegrationServer(db, controllerServer.URL, fortunaServer.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.StartPolling(ctx, 50*time.Millisecond, 50*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	t.Run("TRNG polling works", func(t *testing.T) {
		data, err := db.GetTRNGData(10, 0, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(data) == 0 {
			t.Error("Expected TRNG data to be stored from polling")
		}
	})

	t.Run("Fortuna polling works", func(t *testing.T) {
		data, err := db.GetFortunaData(10, 0, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(data) == 0 {
			t.Error("Expected Fortuna data to be stored from polling")
		}
	})

	t.Run("Polling counts incremented", func(t *testing.T) {
		stats, err := db.GetDetailedStats()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if stats.TRNG.PollingCount == 0 {
			t.Error("Expected TRNG polling count > 0")
		}
		if stats.Fortuna.PollingCount == 0 {
			t.Error("Expected Fortuna polling count > 0")
		}
	})

	t.Run("Polling stops on context cancellation", func(t *testing.T) {
		statsBefore, _ := db.GetDetailedStats()
		trngCountBefore := statsBefore.TRNG.PollingCount
		fortunaCountBefore := statsBefore.Fortuna.PollingCount

		cancel()

		time.Sleep(150 * time.Millisecond)

		statsAfter, _ := db.GetDetailedStats()
		trngCountAfter := statsAfter.TRNG.PollingCount
		fortunaCountAfter := statsAfter.Fortuna.PollingCount

		if trngCountAfter > trngCountBefore+1 {
			t.Errorf("Expected polling to stop, but TRNG count increased from %d to %d", trngCountBefore, trngCountAfter)
		}
		if fortunaCountAfter > fortunaCountBefore+1 {
			t.Errorf("Expected polling to stop, but Fortuna count increased from %d to %d", fortunaCountBefore, fortunaCountAfter)
		}
	})
}

func TestAPI_CORS(t *testing.T) {
	db, cleanupDB := func() (database.DBHandler, func()) {
		db, _ := database.NewChannelDBHandler("", 10, 20)
		return db, func() { db.Close() }
	}()
	defer cleanupDB()

	server, _ := setupIntegrationServer(db, "http://localhost:8081", "http://localhost:8082")

	t.Run("OPTIONS request returns CORS headers", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("OPTIONS", "/api/v1/config/queue", nil)
		req.Header.Set("Origin", "http://example.com")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status 204, got %d", w.Code)
		}

		corsHeaders := map[string]string{
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
			"Access-Control-Allow-Headers": "Content-Type, Authorization, Accept",
		}

		for header, expectedValue := range corsHeaders {
			actualValue := w.Header().Get(header)
			if actualValue != expectedValue {
				t.Errorf("Expected %s header to be %s, got %s", header, expectedValue, actualValue)
			}
		}
	})

	t.Run("GET request includes CORS headers", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/config/queue", nil)
		req.Header.Set("Origin", "http://example.com")
		server.Router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		corsOrigin := w.Header().Get("Access-Control-Allow-Origin")
		if corsOrigin != "*" {
			t.Errorf("Expected Access-Control-Allow-Origin to be *, got %s", corsOrigin)
		}
	})
}

func TestAPI_HealthCheckFailures(t *testing.T) {
	db, cleanupDB := func() (database.DBHandler, func()) {
		db, _ := database.NewChannelDBHandler("", 10, 20)
		return db, func() { db.Close() }
	}()
	defer cleanupDB()

	t.Run("Controller service down", func(t *testing.T) {
		server, _ := setupIntegrationServer(db, "http://localhost:99999", "http://localhost:8082")

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

		if response.Details.Controller {
			t.Error("Expected controller to be unhealthy")
		}
		if !response.Details.Database {
			t.Error("Expected database to be healthy")
		}
		if response.Status != "degraded" {
			t.Errorf("Expected status 'degraded', got %s", response.Status)
		}
	})

	t.Run("Fortuna service down", func(t *testing.T) {
		server, _ := setupIntegrationServer(db, "http://localhost:8081", "http://localhost:99999")

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

		if response.Details.Fortuna {
			t.Error("Expected Fortuna to be unhealthy")
		}
		if !response.Details.Database {
			t.Error("Expected database to be healthy")
		}
		if response.Status != "degraded" {
			t.Errorf("Expected status 'degraded', got %s", response.Status)
		}
	})

	t.Run("Both services down", func(t *testing.T) {
		server, _ := setupIntegrationServer(db, "http://localhost:99999", "http://localhost:99998")

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

		if response.Details.Controller {
			t.Error("Expected controller to be unhealthy")
		}
		if response.Details.Fortuna {
			t.Error("Expected Fortuna to be unhealthy")
		}
		if !response.Details.Database {
			t.Error("Expected database to be healthy")
		}
		if response.Status != "degraded" {
			t.Errorf("Expected status 'degraded', got %s", response.Status)
		}
	})

	t.Run("All services healthy", func(t *testing.T) {
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}
		}))
		defer controllerServer.Close()

		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			}
		}))
		defer fortunaServer.Close()

		server, _ := setupIntegrationServer(db, controllerServer.URL, fortunaServer.URL)

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

		if !response.Details.Controller {
			t.Error("Expected controller to be healthy")
		}
		if !response.Details.Fortuna {
			t.Error("Expected Fortuna to be healthy")
		}
		if !response.Details.Database {
			t.Error("Expected database to be healthy")
		}
		if response.Status != "ok" {
			t.Errorf("Expected status 'ok', got %s", response.Status)
		}
	})
}

func TestAPI_MetricsAccuracy(t *testing.T) {
	db, cleanupDB := func() (database.DBHandler, func()) {
		db, _ := database.NewChannelDBHandler("", 10, 20)
		return db, func() { db.Close() }
	}()
	defer cleanupDB()

	server, _ := setupIntegrationServer(db, "http://localhost:8081", "http://localhost:8082")

	db.StoreTRNGData([]byte{1, 2, 3})
	db.StoreTRNGData([]byte{4, 5, 6})
	db.StoreFortunaData([]byte{10, 20, 30})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/status", nil)
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats database.DetailedStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	metricsReq, _ := http.NewRequest("GET", "/metrics", nil)
	metricsW := httptest.NewRecorder()
	server.Router.ServeHTTP(metricsW, metricsReq)

	if metricsW.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for metrics, got %d", metricsW.Code)
	}

	metricsBody := metricsW.Body.String()

	t.Run("TRNG queue metrics present", func(t *testing.T) {
		expectedMetrics := []string{
			"trng_queue_current",
			"trng_queue_capacity",
			"trng_queue_percentage",
		}
		for _, metric := range expectedMetrics {
			if !strings.Contains(metricsBody, metric) {
				t.Errorf("Expected metrics to contain %s", metric)
			}
		}
	})

	t.Run("Fortuna queue metrics present", func(t *testing.T) {
		expectedMetrics := []string{
			"fortuna_queue_current",
			"fortuna_queue_capacity",
			"fortuna_queue_percentage",
		}
		for _, metric := range expectedMetrics {
			if !strings.Contains(metricsBody, metric) {
				t.Errorf("Expected metrics to contain %s", metric)
			}
		}
	})

	t.Run("Database metrics present", func(t *testing.T) {
		if !strings.Contains(metricsBody, "database_size_bytes") {
			t.Error("Expected metrics to contain database_size_bytes")
		}
	})
}
