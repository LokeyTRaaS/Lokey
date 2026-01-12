package database_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

func setupBoltDBTest(t *testing.T) (*database.BoltDBHandler, string, func()) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	handler, err := database.NewBoltDBHandler(dbPath, 10, 20, 15)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create BoltDB handler: %v", err)
	}

	cleanup := func() {
		handler.Close()
		os.RemoveAll(tmpDir)
	}

	return handler, dbPath, cleanup
}

func TestNewBoltDBHandler(t *testing.T) {
	t.Run("create with file path", func(t *testing.T) {
		handler, _, cleanup := setupBoltDBTest(t)
		defer cleanup()

		if handler == nil {
			t.Fatal("Expected handler to be non-nil")
		}
		if handler.TRNGQueueSize != 10 {
			t.Errorf("Expected TRNG queue size 10, got %d", handler.TRNGQueueSize)
		}
		if handler.FortunaQueueSize != 20 {
			t.Errorf("Expected Fortuna queue size 20, got %d", handler.FortunaQueueSize)
		}
	})

	t.Run("create with directory path", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bolt_test_dir_*")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		handler, err := database.NewBoltDBHandler(tmpDir, 10, 20, 15)
		if err != nil {
			t.Fatalf("Failed to create handler with directory: %v", err)
		}
		defer handler.Close()

		expectedPath := filepath.Join(tmpDir, "database.db")
		if handler.GetDatabasePath() != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, handler.GetDatabasePath())
		}
	})
}

func TestBoltDBHandler_StoreTRNGData(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	data := []byte{1, 2, 3, 4}
	err := handler.StoreTRNGData(data)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify data was stored
	result, err := handler.GetTRNGData(1, 0, false)
	if err != nil {
		t.Fatalf("Expected no error reading data, got %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}
	if string(result[0]) != string(data) {
		t.Errorf("Expected data %v, got %v", data, result[0])
	}
}

func TestBoltDBHandler_GetTRNGData(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	// Store multiple items
	for i := 0; i < 5; i++ {
		data := []byte{byte(i)}
		if err := handler.StoreTRNGData(data); err != nil {
			t.Fatalf("Failed to store data: %v", err)
		}
	}

	t.Run("non-consume mode", func(t *testing.T) {
		result, err := handler.GetTRNGData(2, 0, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}

		// Verify items are still there
		result2, err := handler.GetTRNGData(2, 0, false)
		if err != nil {
			t.Fatalf("Expected no error on second read, got %v", err)
		}
		if len(result2) != 2 {
			t.Errorf("Expected 2 items on second read (non-consume), got %d", len(result2))
		}
	})

	t.Run("consume mode", func(t *testing.T) {
		handler2, _, cleanup2 := setupBoltDBTest(t)
		defer cleanup2()

		for i := 0; i < 3; i++ {
			data := []byte{byte(i + 10)}
			handler2.StoreTRNGData(data)
		}

		result, err := handler2.GetTRNGData(2, 0, true)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}

		// Verify items were consumed
		result2, err := handler2.GetTRNGData(10, 0, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(result2) != 1 {
			t.Errorf("Expected 1 remaining item after consume, got %d", len(result2))
		}
	})

	t.Run("pagination with offset", func(t *testing.T) {
		handler3, _, cleanup3 := setupBoltDBTest(t)
		defer cleanup3()

		for i := 0; i < 5; i++ {
			data := []byte{byte(i + 20)}
			handler3.StoreTRNGData(data)
		}

		result, err := handler3.GetTRNGData(2, 1, false)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 items with offset 1, got %d", len(result))
		}
		if result[0][0] != 21 {
			t.Errorf("Expected first item to be 21, got %d", result[0][0])
		}
	})

	t.Run("queue trimming", func(t *testing.T) {
		handler4, _, cleanup4 := setupBoltDBTest(t)
		defer cleanup4()

		// Create handler with small queue size
		handler4.TRNGQueueSize = 3

		// Store more items than queue size
		for i := 0; i < 5; i++ {
			data := []byte{byte(i)}
			handler4.StoreTRNGData(data)
		}

		// Queue should be trimmed to size 3
		stats, err := handler4.GetDetailedStats()
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if stats.TRNG.QueueCurrent > 3 {
			t.Errorf("Expected queue current <= 3, got %d", stats.TRNG.QueueCurrent)
		}
		if stats.TRNG.QueueDropped == 0 {
			t.Error("Expected dropped count > 0, got 0")
		}
	})
}

func TestBoltDBHandler_StoreFortunaData(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	data := []byte{10, 20, 30}
	err := handler.StoreFortunaData(data)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	result, err := handler.GetFortunaData(1, 0, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result))
	}
}

func TestBoltDBHandler_GetFortunaData(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		data := []byte{byte(i + 30)}
		handler.StoreFortunaData(data)
	}

	result, err := handler.GetFortunaData(2, 0, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(result) != 2 {
		t.Errorf("Expected 2 items, got %d", len(result))
	}

	// Test consume mode
	handler2, _, cleanup2 := setupBoltDBTest(t)
	defer cleanup2()

	for i := 0; i < 2; i++ {
		data := []byte{byte(i + 40)}
		handler2.StoreFortunaData(data)
	}

	result2, err := handler2.GetFortunaData(1, 0, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(result2) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result2))
	}

	// Verify consumed
	result3, err := handler2.GetFortunaData(10, 0, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(result3) != 1 {
		t.Errorf("Expected 1 remaining item, got %d", len(result3))
	}
}

func TestBoltDBHandler_IncrementPollingCount(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	err := handler.IncrementPollingCount("trng")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	stats, err := handler.GetDetailedStats()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if stats.TRNG.PollingCount != 1 {
		t.Errorf("Expected polling count 1, got %d", stats.TRNG.PollingCount)
	}

	err = handler.IncrementPollingCount("fortuna")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	stats2, err := handler.GetDetailedStats()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if stats2.Fortuna.PollingCount != 1 {
		t.Errorf("Expected polling count 1, got %d", stats2.Fortuna.PollingCount)
	}
}

func TestBoltDBHandler_IncrementDroppedCount(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	err := handler.IncrementDroppedCount("trng")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	stats, err := handler.GetDetailedStats()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if stats.TRNG.QueueDropped != 1 {
		t.Errorf("Expected dropped count 1, got %d", stats.TRNG.QueueDropped)
	}
}

func TestBoltDBHandler_GetDetailedStats(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	// Store some data
	handler.StoreTRNGData([]byte{1, 2, 3})
	handler.StoreFortunaData([]byte{4, 5, 6})
	handler.IncrementPollingCount("trng")
	handler.IncrementPollingCount("fortuna")

	stats, err := handler.GetDetailedStats()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if stats.TRNG.QueueCapacity != 10 {
		t.Errorf("Expected TRNG capacity 10, got %d", stats.TRNG.QueueCapacity)
	}
	if stats.TRNG.QueueCurrent != 1 {
		t.Errorf("Expected TRNG current 1, got %d", stats.TRNG.QueueCurrent)
	}
	if stats.TRNG.PollingCount != 1 {
		t.Errorf("Expected TRNG polling count 1, got %d", stats.TRNG.PollingCount)
	}
	if stats.TRNG.TotalGenerated != 1 {
		t.Errorf("Expected TRNG total generated 1, got %d", stats.TRNG.TotalGenerated)
	}

	if stats.Fortuna.QueueCapacity != 20 {
		t.Errorf("Expected Fortuna capacity 20, got %d", stats.Fortuna.QueueCapacity)
	}
	if stats.Fortuna.QueueCurrent != 1 {
		t.Errorf("Expected Fortuna current 1, got %d", stats.Fortuna.QueueCurrent)
	}

	if stats.Database.SizeBytes <= 0 {
		t.Errorf("Expected database size > 0, got %d", stats.Database.SizeBytes)
	}
	if stats.Database.Path == "" {
		t.Error("Expected database path to be non-empty")
	}
}

func TestBoltDBHandler_GetQueueInfo(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	handler.StoreTRNGData([]byte{1})
	handler.StoreFortunaData([]byte{2})

	info, err := handler.GetQueueInfo()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if info["trng_queue_capacity"] != 10 {
		t.Errorf("Expected TRNG capacity 10, got %d", info["trng_queue_capacity"])
	}
	if info["trng_queue_current"] != 1 {
		t.Errorf("Expected TRNG current 1, got %d", info["trng_queue_current"])
	}
	if info["fortuna_queue_capacity"] != 20 {
		t.Errorf("Expected Fortuna capacity 20, got %d", info["fortuna_queue_capacity"])
	}
	if info["fortuna_queue_current"] != 1 {
		t.Errorf("Expected Fortuna current 1, got %d", info["fortuna_queue_current"])
	}
}

func TestBoltDBHandler_UpdateQueueSizes(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	err := handler.UpdateQueueSizes(50, 60, 55)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	info, err := handler.GetQueueInfo()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if info["trng_queue_capacity"] != 50 {
		t.Errorf("Expected TRNG capacity 50, got %d", info["trng_queue_capacity"])
	}
	if info["fortuna_queue_capacity"] != 60 {
		t.Errorf("Expected Fortuna capacity 60, got %d", info["fortuna_queue_capacity"])
	}
}

func TestBoltDBHandler_RecordRNGUsage(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	err := handler.RecordRNGUsage("trng", 100)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	start := time.Now().Add(-1 * time.Hour)
	end := time.Now().Add(1 * time.Hour)
	stats, err := handler.GetRNGStatistics("trng", start, end)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("Expected 1 usage stat, got %d", len(stats))
	}
	if stats[0].BytesUsed != 100 {
		t.Errorf("Expected bytes used 100, got %d", stats[0].BytesUsed)
	}
	if stats[0].Source != "trng" {
		t.Errorf("Expected source 'trng', got %s", stats[0].Source)
	}
}

func TestBoltDBHandler_GetRNGStatistics(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	// Record some usage
	handler.RecordRNGUsage("trng", 100)
	handler.RecordRNGUsage("trng", 200)
	handler.RecordRNGUsage("fortuna", 150)

	start := time.Now().Add(-1 * time.Hour)
	end := time.Now().Add(1 * time.Hour)

	stats, err := handler.GetRNGStatistics("trng", start, end)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("Expected 2 TRNG stats, got %d", len(stats))
	}

	stats2, err := handler.GetRNGStatistics("fortuna", start, end)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(stats2) != 1 {
		t.Errorf("Expected 1 Fortuna stat, got %d", len(stats2))
	}

	// Test time range filtering
	pastStart := time.Now().Add(-2 * time.Hour)
	pastEnd := time.Now().Add(-1 * time.Hour)
	stats3, err := handler.GetRNGStatistics("trng", pastStart, pastEnd)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(stats3) != 0 {
		t.Errorf("Expected 0 stats in past range, got %d", len(stats3))
	}
}

func TestBoltDBHandler_GetStats(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	handler.StoreTRNGData([]byte{1})
	handler.StoreFortunaData([]byte{2})

	stats, err := handler.GetStats()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if trngCount, ok := stats["trng_count"].(int); !ok || trngCount != 1 {
		t.Errorf("Expected trng_count 1, got %d", trngCount)
	}
	if fortunaCount, ok := stats["fortuna_count"].(int); !ok || fortunaCount != 1 {
		t.Errorf("Expected fortuna_count 1, got %d", fortunaCount)
	}
	if dbSize, ok := stats["db_size"].(int64); !ok || dbSize <= 0 {
		t.Errorf("Expected db_size > 0, got %d", dbSize)
	}
}

func TestBoltDBHandler_HealthCheck(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	if !handler.HealthCheck() {
		t.Error("Expected HealthCheck to return true, got false")
	}
}

func TestBoltDBHandler_Close(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	// Don't call cleanup here, test Close explicitly

	err := handler.Close()
	if err != nil {
		t.Errorf("Expected no error from Close, got %v", err)
	}

	// Verify handler is closed
	if handler.HealthCheck() {
		t.Error("Expected HealthCheck to return false after Close, got true")
	}

	cleanup() // Clean up temp directory
}

func TestBoltDBHandler_GetDatabaseSize(t *testing.T) {
	handler, _, cleanup := setupBoltDBTest(t)
	defer cleanup()

	size, err := handler.GetDatabaseSize()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if size <= 0 {
		t.Errorf("Expected database size > 0, got %d", size)
	}

	// Store some data - BoltDB uses pages so size might not increase immediately
	// if data fits in existing pages, but size should be at least the initial size
	handler.StoreTRNGData([]byte{1, 2, 3, 4, 5})
	size2, err := handler.GetDatabaseSize()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if size2 < size {
		t.Errorf("Expected size to be >= initial size after storing data, got %d < %d", size2, size)
	}
}

func TestBoltDBHandler_GetDatabasePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bolt_test_path_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	handler, err := database.NewBoltDBHandler(dbPath, 10, 20, 15)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	path := handler.GetDatabasePath()
	if path != dbPath {
		t.Errorf("Expected path %s, got %s", dbPath, path)
	}
}
