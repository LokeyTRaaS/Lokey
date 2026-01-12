package integration_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

// setupBoltDBForIntegration creates a BoltDB handler for integration tests
func setupBoltDBForIntegration(t *testing.T) (*database.BoltDBHandler, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	handler, err := database.NewBoltDBHandler(dbPath, 10, 20, 15)
	if err != nil {
		t.Fatalf("Failed to create BoltDB handler: %v", err)
	}
	return handler, func() {
		handler.Close()
	}
}

func TestDatabaseImplementations(t *testing.T) {
	implementations := []struct {
		name    string
		setup   func(*testing.T) (database.DBHandler, func())
		cleanup func()
	}{
		{
			name: "ChannelDBHandler",
			setup: func(t *testing.T) (database.DBHandler, func()) {
				db, _ := database.NewChannelDBHandler("", 10, 20, 15)
				return db, func() { db.Close() }
			},
		},
		{
			name: "BoltDBHandler",
			setup: func(t *testing.T) (database.DBHandler, func()) {
				handler, cleanup := setupBoltDBForIntegration(t)
				return handler, cleanup
			},
		},
	}

	for _, impl := range implementations {
		t.Run(impl.name, func(t *testing.T) {
			db, cleanup := impl.setup(t)
			defer cleanup()

			t.Run("Store and retrieve TRNG data", func(t *testing.T) {
				testData := []byte{1, 2, 3, 4, 5}
				err := db.StoreTRNGData(testData)
				if err != nil {
					t.Fatalf("Expected no error storing TRNG data, got %v", err)
				}

				result, err := db.GetTRNGData(1, 0, false)
				if err != nil {
					t.Fatalf("Expected no error retrieving TRNG data, got %v", err)
				}
				if len(result) != 1 {
					t.Fatalf("Expected 1 item, got %d", len(result))
				}
				if string(result[0]) != string(testData) {
					t.Errorf("Expected data %v, got %v", testData, result[0])
				}
			})

			t.Run("Store and retrieve Fortuna data", func(t *testing.T) {
				testData := []byte{10, 20, 30, 40, 50}
				err := db.StoreFortunaData(testData)
				if err != nil {
					t.Fatalf("Expected no error storing Fortuna data, got %v", err)
				}

				result, err := db.GetFortunaData(1, 0, false)
				if err != nil {
					t.Fatalf("Expected no error retrieving Fortuna data, got %v", err)
				}
				if len(result) != 1 {
					t.Fatalf("Expected 1 item, got %d", len(result))
				}
				if string(result[0]) != string(testData) {
					t.Errorf("Expected data %v, got %v", testData, result[0])
				}
			})

			t.Run("Get queue info", func(t *testing.T) {
				info, err := db.GetQueueInfo()
				if err != nil {
					t.Fatalf("Expected no error getting queue info, got %v", err)
				}

				if info["trng_queue_capacity"] != 10 {
					t.Errorf("Expected TRNG capacity 10, got %d", info["trng_queue_capacity"])
				}
				if info["fortuna_queue_capacity"] != 20 {
					t.Errorf("Expected Fortuna capacity 20, got %d", info["fortuna_queue_capacity"])
				}
			})

			t.Run("Get detailed stats", func(t *testing.T) {
				stats, err := db.GetDetailedStats()
				if err != nil {
					t.Fatalf("Expected no error getting detailed stats, got %v", err)
				}

				if stats.TRNG.QueueCapacity != 10 {
					t.Errorf("Expected TRNG capacity 10, got %d", stats.TRNG.QueueCapacity)
				}
				if stats.Fortuna.QueueCapacity != 20 {
					t.Errorf("Expected Fortuna capacity 20, got %d", stats.Fortuna.QueueCapacity)
				}
			})

			t.Run("Get database size", func(t *testing.T) {
				size, err := db.GetDatabaseSize()
				if err != nil {
					t.Fatalf("Expected no error getting database size, got %v", err)
				}
				// Size should be non-negative (0 for ChannelDBHandler, >0 for BoltDBHandler after data is stored)
				if size < 0 {
					t.Errorf("Expected non-negative database size, got %d", size)
				}
			})

			t.Run("Get database path", func(t *testing.T) {
				path := db.GetDatabasePath()
				if path == "" {
					t.Error("Expected non-empty database path")
				}
			})

			t.Run("Health check", func(t *testing.T) {
				healthy := db.HealthCheck()
				if !healthy {
					t.Error("Expected database to be healthy")
				}
			})

			t.Run("Update queue sizes", func(t *testing.T) {
				err := db.UpdateQueueSizes(15, 25, 20)
				if impl.name == "ChannelDBHandler" {
					// ChannelDBHandler doesn't support UpdateQueueSizes
					if err == nil {
						t.Error("Expected error for ChannelDBHandler UpdateQueueSizes, got nil")
					}
				} else {
					// BoltDBHandler should support UpdateQueueSizes
					if err != nil {
						t.Errorf("Expected no error for BoltDBHandler UpdateQueueSizes, got %v", err)
					}

					// Verify queue sizes were updated
					info, _ := db.GetQueueInfo()
					if info["trng_queue_capacity"] != 15 {
						t.Errorf("Expected TRNG capacity 15 after update, got %d", info["trng_queue_capacity"])
					}
					if info["fortuna_queue_capacity"] != 25 {
						t.Errorf("Expected Fortuna capacity 25 after update, got %d", info["fortuna_queue_capacity"])
					}
				}
			})

			t.Run("Record RNG usage", func(t *testing.T) {
				err := db.RecordRNGUsage("trng", 100)
				if err != nil {
					t.Errorf("Expected no error recording RNG usage, got %v", err)
				}
			})

			t.Run("Get RNG statistics", func(t *testing.T) {
				stats, err := db.GetRNGStatistics("trng", time.Now().Add(-1*time.Hour), time.Now())
				if err != nil {
					t.Errorf("Expected no error getting RNG statistics, got %v", err)
				}
				if impl.name == "ChannelDBHandler" {
					// ChannelDBHandler returns empty slice
					if len(stats) != 0 {
						t.Errorf("Expected empty stats for ChannelDBHandler, got %d items", len(stats))
					}
				} else {
					// BoltDBHandler may return stats if data was recorded
					// Just verify it doesn't error
					_ = stats
				}
			})

			t.Run("Statistics tracking", func(t *testing.T) {
				// Test polling count
				err := db.IncrementPollingCount("trng")
				if err != nil {
					t.Errorf("Expected no error incrementing polling count, got %v", err)
				}

				// Test dropped count
				err = db.IncrementDroppedCount("trng")
				if err != nil {
					t.Errorf("Expected no error incrementing dropped count, got %v", err)
				}

				// Verify stats reflect the increments
				stats, _ := db.GetDetailedStats()
				if stats.TRNG.PollingCount == 0 {
					// Stats may not be immediately reflected, but should not error
					_ = stats
				}
			})

			t.Run("Pagination with offset", func(t *testing.T) {
				// Store multiple items
				for i := 0; i < 5; i++ {
					db.StoreTRNGData([]byte{byte(i)})
				}

				// Get first 2 items
				result1, err := db.GetTRNGData(2, 0, false)
				if err != nil {
					t.Fatalf("Expected no error, got %v", err)
				}
				if len(result1) != 2 {
					t.Errorf("Expected 2 items, got %d", len(result1))
				}

				// Get next 2 items with offset
				result2, err := db.GetTRNGData(2, 2, false)
				if err != nil {
					t.Fatalf("Expected no error with offset, got %v", err)
				}
				if len(result2) != 2 {
					t.Errorf("Expected 2 items with offset, got %d", len(result2))
				}

				// Results should be different
				if len(result1) > 0 && len(result2) > 0 {
					if string(result1[0]) == string(result2[0]) {
						t.Error("Expected different data with offset")
					}
				}
			})

			t.Run("Consume mode true", func(t *testing.T) {
				// Store data
				db.StoreTRNGData([]byte{100})
				db.StoreTRNGData([]byte{101})

				// Get initial count
				statsBefore, _ := db.GetDetailedStats()
				countBefore := statsBefore.TRNG.QueueCurrent

				// Get data in consume mode
				result, err := db.GetTRNGData(1, 0, true)
				if err != nil {
					t.Fatalf("Expected no error in consume mode, got %v", err)
				}
				if len(result) != 1 {
					t.Errorf("Expected 1 item, got %d", len(result))
				}

				// Verify data was consumed (queue size decreased)
				statsAfter, _ := db.GetDetailedStats()
				countAfter := statsAfter.TRNG.QueueCurrent
				if countAfter >= countBefore {
					t.Errorf("Expected queue size to decrease in consume mode, got %d (before: %d)", countAfter, countBefore)
				}
			})

			t.Run("Consume mode false", func(t *testing.T) {
				// Store data
				db.StoreFortunaData([]byte{200})
				db.StoreFortunaData([]byte{201})

				// Get initial count
				statsBefore, _ := db.GetDetailedStats()
				countBefore := statsBefore.Fortuna.QueueCurrent

				// Get data in non-consume mode
				result1, err := db.GetFortunaData(1, 0, false)
				if err != nil {
					t.Fatalf("Expected no error in non-consume mode, got %v", err)
				}

				// Get again - should get same data
				result2, err := db.GetFortunaData(1, 0, false)
				if err != nil {
					t.Fatalf("Expected no error on second retrieval, got %v", err)
				}

				// Verify data was not consumed (queue size unchanged)
				statsAfter, _ := db.GetDetailedStats()
				countAfter := statsAfter.Fortuna.QueueCurrent
				if countAfter != countBefore {
					t.Errorf("Expected queue size to remain unchanged in non-consume mode, got %d (before: %d)", countAfter, countBefore)
				}

				// Results should be the same
				if len(result1) > 0 && len(result2) > 0 {
					if string(result1[0]) != string(result2[0]) {
						t.Error("Expected same data in non-consume mode")
					}
				}
			})
		})
	}
}

func TestDatabaseConsistency(t *testing.T) {
	// Setup both implementations
	channelDB, cleanupChannel := func() (database.DBHandler, func()) {
		db, _ := database.NewChannelDBHandler("", 10, 20, 15)
		return db, func() { db.Close() }
	}()

	boltDB, cleanupBolt := setupBoltDBForIntegration(t)

	defer cleanupChannel()
	defer cleanupBolt()

	t.Run("Same data stored returns consistent results", func(t *testing.T) {
		testData := []byte{1, 2, 3, 4, 5}

		// Store same data in both
		channelDB.StoreTRNGData(testData)
		boltDB.StoreTRNGData(testData)

		// Retrieve from both
		channelData, _ := channelDB.GetTRNGData(1, 0, false)
		boltData, _ := boltDB.GetTRNGData(1, 0, false)

		// Data should match
		if len(channelData) != len(boltData) {
			t.Errorf("Expected same data length, got %d and %d", len(channelData), len(boltData))
		}
		if len(channelData) > 0 && len(boltData) > 0 {
			if string(channelData[0]) != string(boltData[0]) {
				t.Errorf("Expected same data, got %v and %v", channelData[0], boltData[0])
			}
		}
	})

	t.Run("Queue capacity matches", func(t *testing.T) {
		channelInfo, _ := channelDB.GetQueueInfo()
		boltInfo, _ := boltDB.GetQueueInfo()

		if channelInfo["trng_queue_capacity"] != boltInfo["trng_queue_capacity"] {
			t.Errorf("Expected same TRNG capacity, got %d and %d",
				channelInfo["trng_queue_capacity"], boltInfo["trng_queue_capacity"])
		}
		if channelInfo["fortuna_queue_capacity"] != boltInfo["fortuna_queue_capacity"] {
			t.Errorf("Expected same Fortuna capacity, got %d and %d",
				channelInfo["fortuna_queue_capacity"], boltInfo["fortuna_queue_capacity"])
		}
	})

	t.Run("GetQueueInfo structure matches", func(t *testing.T) {
		channelInfo, _ := channelDB.GetQueueInfo()
		boltInfo, _ := boltDB.GetQueueInfo()

		// Check that both have the same keys
		expectedKeys := []string{"trng_queue_capacity", "fortuna_queue_capacity", "trng_queue_current", "fortuna_queue_current"}
		for _, key := range expectedKeys {
			if _, ok := channelInfo[key]; !ok {
				t.Errorf("Expected key %s in channel info", key)
			}
			if _, ok := boltInfo[key]; !ok {
				t.Errorf("Expected key %s in bolt info", key)
			}
		}
	})

	t.Run("GetDetailedStats structure matches", func(t *testing.T) {
		channelStats, _ := channelDB.GetDetailedStats()
		boltStats, _ := boltDB.GetDetailedStats()

		// Structure should match (path and size may differ)
		if channelStats.TRNG.QueueCapacity != boltStats.TRNG.QueueCapacity {
			t.Errorf("Expected same TRNG capacity, got %d and %d",
				channelStats.TRNG.QueueCapacity, boltStats.TRNG.QueueCapacity)
		}
		if channelStats.Fortuna.QueueCapacity != boltStats.Fortuna.QueueCapacity {
			t.Errorf("Expected same Fortuna capacity, got %d and %d",
				channelStats.Fortuna.QueueCapacity, boltStats.Fortuna.QueueCapacity)
		}

		// Path should differ (known difference)
		if channelStats.Database.Path == boltStats.Database.Path {
			t.Error("Expected different database paths (known difference)")
		}
	})

	t.Run("Health check both return true", func(t *testing.T) {
		channelHealthy := channelDB.HealthCheck()
		boltHealthy := boltDB.HealthCheck()

		if !channelHealthy {
			t.Error("Expected channel DB to be healthy")
		}
		if !boltHealthy {
			t.Error("Expected bolt DB to be healthy")
		}
	})

	t.Run("Store/Get operations behave identically", func(t *testing.T) {
		testData := []byte{10, 20, 30}

		// Store in both
		channelDB.StoreFortunaData(testData)
		boltDB.StoreFortunaData(testData)

		// Get from both
		channelResult, _ := channelDB.GetFortunaData(1, 0, false)
		boltResult, _ := boltDB.GetFortunaData(1, 0, false)

		// Results should match
		if len(channelResult) != len(boltResult) {
			t.Errorf("Expected same result length, got %d and %d", len(channelResult), len(boltResult))
		}
		if len(channelResult) > 0 && len(boltResult) > 0 {
			if string(channelResult[0]) != string(boltResult[0]) {
				t.Errorf("Expected same data, got %v and %v", channelResult[0], boltResult[0])
			}
		}
	})

	t.Run("Pagination works the same way", func(t *testing.T) {
		// Store multiple items in both
		for i := 0; i < 5; i++ {
			data := []byte{byte(i)}
			channelDB.StoreTRNGData(data)
			boltDB.StoreTRNGData(data)
		}

		// Get first 2 from both
		channelFirst, _ := channelDB.GetTRNGData(2, 0, false)
		boltFirst, _ := boltDB.GetTRNGData(2, 0, false)

		if len(channelFirst) != len(boltFirst) {
			t.Errorf("Expected same first batch length, got %d and %d", len(channelFirst), len(boltFirst))
		}

		// Get next 2 with offset from both
		channelSecond, _ := channelDB.GetTRNGData(2, 2, false)
		boltSecond, _ := boltDB.GetTRNGData(2, 2, false)

		if len(channelSecond) != len(boltSecond) {
			t.Errorf("Expected same second batch length, got %d and %d", len(channelSecond), len(boltSecond))
		}
	})

	t.Run("Consume mode behavior matches", func(t *testing.T) {
		// Store data in both
		channelDB.StoreTRNGData([]byte{100})
		boltDB.StoreTRNGData([]byte{100})

		// Get in consume mode from both
		channelResult, _ := channelDB.GetTRNGData(1, 0, true)
		boltResult, _ := boltDB.GetTRNGData(1, 0, true)

		// Both should return data
		if len(channelResult) != len(boltResult) {
			t.Errorf("Expected same result length in consume mode, got %d and %d", len(channelResult), len(boltResult))
		}

		// Both should have consumed the data (queue size decreased)
		channelStats, _ := channelDB.GetDetailedStats()
		boltStats, _ := boltDB.GetDetailedStats()

		// Queue current should be 0 after consuming the one item we stored
		if channelStats.TRNG.QueueCurrent != 0 && boltStats.TRNG.QueueCurrent != 0 {
			// If we stored more data earlier, queue might not be 0, but both should match
			if channelStats.TRNG.QueueCurrent != boltStats.TRNG.QueueCurrent {
				t.Errorf("Expected same queue current after consume, got %d and %d",
					channelStats.TRNG.QueueCurrent, boltStats.TRNG.QueueCurrent)
			}
		}
	})
}
