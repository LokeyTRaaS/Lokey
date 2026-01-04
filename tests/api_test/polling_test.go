package api_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

func TestServer_fetchAndStoreTRNGData(t *testing.T) {
	t.Run("successful fetch and store", func(t *testing.T) {
		// Create mock HTTP server for controller
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/generate" {
				t.Errorf("Expected path /generate, got %s", r.URL.Path)
			}
			data := hex.EncodeToString([]byte{1, 2, 3, 4, 5})
			response := map[string]string{"data": data}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer controllerServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", 0, testRegistry)

		err := server.FetchAndStoreTRNGData()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Verify data was stored
		data, err := db.GetTRNGData(1, 0, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(data) == 0 {
			t.Error("Expected data to be stored")
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:99999", "http://localhost:8082", 0, testRegistry)

		err := server.FetchAndStoreTRNGData()
		if err == nil {
			t.Error("Expected error for invalid address, got nil")
		}
		if err != nil && err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("non-200 status", func(t *testing.T) {
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer controllerServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", 0, testRegistry)

		err := server.FetchAndStoreTRNGData()
		if err == nil {
			t.Error("Expected error for non-200 status, got nil")
		}
		if err != nil && err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
		}))
		defer controllerServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", 0, testRegistry)

		err := server.FetchAndStoreTRNGData()
		if err == nil {
			t.Error("Expected error for invalid JSON, got nil")
		}
		if err != nil && err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("invalid hex data", func(t *testing.T) {
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]string{"data": "invalid hex data"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer controllerServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", 0, testRegistry)

		err := server.FetchAndStoreTRNGData()
		if err == nil {
			t.Error("Expected error for invalid hex data, got nil")
		}
		if err != nil && err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})
}

func TestServer_fetchAndStoreFortunaData(t *testing.T) {
	t.Run("successful fetch and store", func(t *testing.T) {
		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/generate" {
				t.Errorf("Expected path /generate, got %s", r.URL.Path)
			}
			data := hex.EncodeToString([]byte{10, 20, 30, 40})
			response := map[string]interface{}{
				"data": data,
				"size": 4,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", fortunaServer.URL, 0, testRegistry)

		err := server.FetchAndStoreFortunaData()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Verify data was stored
		data, err := db.GetFortunaData(1, 0, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(data) == 0 {
			t.Error("Expected data to be stored")
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", "http://localhost:99999", 0, testRegistry)

		err := server.FetchAndStoreFortunaData()
		if err == nil {
			t.Error("Expected error for invalid address, got nil")
		}
	})

	t.Run("non-200 status", func(t *testing.T) {
		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", fortunaServer.URL, 0, testRegistry)

		err := server.FetchAndStoreFortunaData()
		if err == nil {
			t.Error("Expected error for non-200 status, got nil")
		}
	})
}

func TestServer_seedFortuna(t *testing.T) {
	t.Run("successful seeding", func(t *testing.T) {
		var seedRequestReceived bool
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/generate" {
				// Return multiple TRNG samples
				data := []string{
					hex.EncodeToString([]byte{1, 2, 3}),
					hex.EncodeToString([]byte{4, 5, 6}),
					hex.EncodeToString([]byte{7, 8, 9}),
				}
				response := map[string]interface{}{"data": data}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}
		}))

		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seed" {
				seedRequestReceived = true
				var req map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode seed request: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("{}"))
			}
		}))

		defer controllerServer.Close()
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, fortunaServer.URL, 0, testRegistry)

		err := server.SeedFortuna()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !seedRequestReceived {
			t.Error("Expected seed request to be sent to Fortuna")
		}
	})

	t.Run("controller error", func(t *testing.T) {
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer controllerServer.Close()

		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, fortunaServer.URL, 0, testRegistry)

		err := server.SeedFortuna()
		if err == nil {
			t.Error("Expected error when controller returns error, got nil")
		}
		if err != nil && err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("fortuna seeding error", func(t *testing.T) {
		controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/generate" {
				data := []string{hex.EncodeToString([]byte{1, 2, 3})}
				response := map[string]interface{}{"data": data}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer controllerServer.Close()

		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seed" {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler("", 10, 20)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, fortunaServer.URL, 0, testRegistry)

		err := server.SeedFortuna()
		if err == nil {
			t.Error("Expected error when fortuna seeding fails, got nil")
		}
		if err != nil && err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})
}

func TestServer_pollTRNGService_Cancellation(t *testing.T) {
	controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := hex.EncodeToString([]byte{1, 2, 3})
		response := map[string]string{"data": data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer controllerServer.Close()

	db, _ := database.NewChannelDBHandler("", 10, 20)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", 0, testRegistry)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should return quickly
	done := make(chan bool)
	go func() {
		server.PollTRNGService(ctx, 100*time.Millisecond)
		done <- true
	}()

	select {
	case <-done:
		// Success - function returned after cancellation
	case <-time.After(1 * time.Second):
		t.Error("Expected pollTRNGService to return after cancellation")
	}
}

func TestServer_pollFortunaService_Cancellation(t *testing.T) {
	fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := hex.EncodeToString([]byte{1, 2, 3})
		response := map[string]interface{}{"data": data, "size": 3}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer fortunaServer.Close()

	db, _ := database.NewChannelDBHandler("", 10, 20)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, "http://localhost:8081", fortunaServer.URL, 0, testRegistry)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan bool)
	go func() {
		server.PollFortunaService(ctx, 100*time.Millisecond)
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Expected pollFortunaService to return after cancellation")
	}
}

func TestServer_seedFortunaWithTRNG_Cancellation(t *testing.T) {
	controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := []string{hex.EncodeToString([]byte{1})}
		response := map[string]interface{}{"data": data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer controllerServer.Close()
	defer fortunaServer.Close()

	db, _ := database.NewChannelDBHandler("", 10, 20)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, controllerServer.URL, fortunaServer.URL, 0, testRegistry)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan bool)
	go func() {
		server.SeedFortunaWithTRNG(ctx, 100*time.Millisecond)
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Expected seedFortunaWithTRNG to return after cancellation")
	}
}
