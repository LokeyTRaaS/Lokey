package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
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

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

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
		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:99999", "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

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

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

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

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

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

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

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

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", fortunaServer.URL, "http://localhost:8083", 0, testRegistry)

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
		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", "http://localhost:99999", "http://localhost:8083", 0, testRegistry)

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

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", fortunaServer.URL, "http://localhost:8083", 0, testRegistry)

		err := server.FetchAndStoreFortunaData()
		if err == nil {
			t.Error("Expected error for non-200 status, got nil")
		}
	})
}

func TestServer_seedFortuna(t *testing.T) {
	t.Run("successful seeding", func(t *testing.T) {
		validSeed1 := make([]byte, 32)
		rand.Read(validSeed1)
		validSeed2 := make([]byte, 32)
		rand.Read(validSeed2)
		validSeed3 := make([]byte, 32)
		rand.Read(validSeed3)

		var seedRequestReceived bool
		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seed" {
				seedRequestReceived = true
				var req map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode seed request: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("{}"))
			}
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://unused", fortunaServer.URL, "http://localhost:8083", 0, testRegistry)
		server.SetConsumeConfig(api.ConsumeConfig{
			VirtIO:      true,
			FortunaSeed: api.FeedSource{Ratio: 1, Consume: false},
		})
		for _, sample := range [][]byte{validSeed1, validSeed2, validSeed3} {
			if err := db.StoreTRNGData(sample); err != nil {
				t.Fatal(err)
			}
			server.ObserveTRNGSample(sample)
		}

		if err := server.SeedFortuna(); err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !seedRequestReceived {
			t.Error("Expected seed request to be sent to Fortuna")
		}
		stats, err := db.GetDetailedStats()
		if err != nil {
			t.Fatal(err)
		}
		if stats.TRNG.QueueCurrent != 3 {
			t.Fatalf("copy reseed removed TRNG samples: current=%d", stats.TRNG.QueueCurrent)
		}
	})

	t.Run("nothing chosen is not an error", func(t *testing.T) {
		posted := false
		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			posted = true
			w.WriteHeader(http.StatusOK)
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		server := api.NewServer(db, "http://unused", fortunaServer.URL, "http://localhost:8083", 0, prometheus.NewRegistry())
		if err := server.SeedFortuna(); err != nil {
			t.Fatalf("empty share returned %v", err)
		}
		if posted {
			t.Fatal("empty share posted to Fortuna")
		}
	})

	t.Run("fortuna seeding error", func(t *testing.T) {
		validSeed := make([]byte, 32)
		rand.Read(validSeed)

		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		server := api.NewServer(db, "http://unused", fortunaServer.URL, "http://localhost:8083", 0, prometheus.NewRegistry())
		server.SetConsumeConfig(api.ConsumeConfig{
			VirtIO:      true,
			FortunaSeed: api.FeedSource{Ratio: 1, Consume: true},
		})
		if err := db.StoreTRNGData(validSeed); err != nil {
			t.Fatal(err)
		}
		server.ObserveTRNGSample(validSeed)

		if err := server.SeedFortuna(); err == nil {
			t.Fatal("expected error when fortuna seeding fails")
		}
		stats, err := db.GetDetailedStats()
		if err != nil {
			t.Fatal(err)
		}
		if stats.TRNG.QueueCurrent != 0 {
			t.Fatalf("consume reseed left the sample in the ring: current=%d", stats.TRNG.QueueCurrent)
		}
	})

	t.Run("seed validation rejects bad data", func(t *testing.T) {
		badData := bytes.Repeat([]byte{0xFF}, 32)
		posted := false
		fortunaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			posted = true
			w.WriteHeader(http.StatusOK)
		}))
		defer fortunaServer.Close()

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		server := api.NewServer(db, "http://unused", fortunaServer.URL, "http://localhost:8083", 0, prometheus.NewRegistry())
		server.SetConsumeConfig(api.ConsumeConfig{
			VirtIO:      true,
			FortunaSeed: api.FeedSource{Ratio: 1, Consume: false},
		})
		server.ObserveTRNGSample(badData)

		if err := server.SeedFortuna(); err == nil {
			t.Fatal("expected error when TRNG data validation fails")
		}
		if posted {
			t.Fatal("invalid seed was posted")
		}
	})
}

func TestValidateTRNGData(t *testing.T) {
	t.Run("valid seeds", func(t *testing.T) {
		validSeeds := []string{
			hex.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}),
			hex.EncodeToString([]byte{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131}),
		}
		err := api.ValidateTRNGData(validSeeds)
		if err != nil {
			t.Errorf("Expected no error for valid seeds, got %v", err)
		}
	})

	t.Run("invalid seeds - 0xFF pattern", func(t *testing.T) {
		badSeed := make([]byte, 32)
		for i := range badSeed {
			badSeed[i] = 0xFF
		}
		invalidSeeds := []string{hex.EncodeToString(badSeed)}
		err := api.ValidateTRNGData(invalidSeeds)
		if err == nil {
			t.Error("Expected error for invalid seeds with 0xFF pattern, got nil")
		}
	})

	t.Run("invalid seeds - all same bytes", func(t *testing.T) {
		badSeed := make([]byte, 32)
		for i := range badSeed {
			badSeed[i] = 0x42
		}
		invalidSeeds := []string{hex.EncodeToString(badSeed)}
		err := api.ValidateTRNGData(invalidSeeds)
		if err == nil {
			t.Error("Expected error for invalid seeds with all same bytes, got nil")
		}
	})

	t.Run("empty seeds", func(t *testing.T) {
		err := api.ValidateTRNGData([]string{})
		if err == nil {
			t.Error("Expected error for empty seeds, got nil")
		}
	})

	t.Run("invalid hex encoding", func(t *testing.T) {
		err := api.ValidateTRNGData([]string{"not-hex"})
		if err == nil {
			t.Error("Expected error for invalid hex encoding, got nil")
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

	db, _ := database.NewChannelDBHandler(10, 20, 15)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, controllerServer.URL, "http://localhost:8082", "http://localhost:8083", 0, testRegistry)

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

	db, _ := database.NewChannelDBHandler(10, 20, 15)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, "http://localhost:8081", fortunaServer.URL, "http://localhost:8083", 0, testRegistry)

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

	db, _ := database.NewChannelDBHandler(10, 20, 15)
	testRegistry := prometheus.NewRegistry()
	server := api.NewServer(db, controllerServer.URL, fortunaServer.URL, "http://localhost:8083", 0, testRegistry)

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

func TestServer_SeedVirtIOFromTRNG(t *testing.T) {
	t.Run("successful seeding", func(t *testing.T) {
		validSeed1 := make([]byte, 32)
		validSeed2 := make([]byte, 32)
		rand.Read(validSeed1)
		rand.Read(validSeed2)

		var seedRequestReceived bool
		virtioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seed" {
				seedRequestReceived = true
				var req map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode seed request: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status": "seeded", "count": 2}`))
			}
		}))
		defer virtioServer.Close()

		db, _ := database.NewChannelDBHandler(10, 20, 15)
		if err := db.StoreTRNGData(validSeed1); err != nil {
			t.Fatalf("store: %v", err)
		}
		if err := db.StoreTRNGData(validSeed2); err != nil {
			t.Fatalf("store: %v", err)
		}
		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", virtioServer.URL, 0, testRegistry)
		server.SetConsumeConfig(api.ConsumeConfig{
			VirtIO: true,
			VirtIOSources: api.VirtIOSources{
				TRNG: api.FeedSource{Ratio: 1, Consume: false},
			},
		})
		server.ObserveTRNGSample(validSeed1)
		server.ObserveTRNGSample(validSeed2)

		err := server.SeedVirtIOFromTRNG()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !seedRequestReceived {
			t.Error("Expected seed request to be sent to VirtIO")
		}
		stats, statsErr := db.GetDetailedStats()
		if statsErr != nil {
			t.Fatal(statsErr)
		}
		if stats.TRNG.QueueCurrent != 2 {
			t.Fatalf("default copy mode consumed TRNG samples: current=%d", stats.TRNG.QueueCurrent)
		}
	})
}

func TestServer_SeedVirtIOFromFortuna(t *testing.T) {
	t.Run("successful seeding", func(t *testing.T) {
		// Store some Fortuna data in database
		db, _ := database.NewChannelDBHandler(10, 20, 15)
		testData := []byte{1, 2, 3, 4, 5}
		if err := db.StoreFortunaData(testData); err != nil {
			t.Fatalf("Failed to store Fortuna data: %v", err)
		}

		var seedRequestReceived bool
		virtioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seed" {
				seedRequestReceived = true
				var req map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode seed request: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status": "seeded", "count": 1}`))
			}
		}))
		defer virtioServer.Close()

		testRegistry := prometheus.NewRegistry()
		server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", virtioServer.URL, 0, testRegistry)
		server.SetConsumeConfig(api.ConsumeConfig{
			VirtIO: true,
			VirtIOSources: api.VirtIOSources{
				Fortuna: api.FeedSource{Ratio: 1, Consume: false},
			},
		})
		server.ObserveFortunaSample(testData)

		err := server.SeedVirtIOFromFortuna()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !seedRequestReceived {
			t.Error("Expected seed request to be sent to VirtIO")
		}
	})
}
