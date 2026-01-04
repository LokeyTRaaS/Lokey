package database_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

// TestDBHandlerInterface_Channel verifies database.ChannelDBHandler implements all database.DBHandler methods
func TestDBHandlerInterface_Channel(t *testing.T) {
	var _ database.DBHandler = (*database.ChannelDBHandler)(nil)
	// If this compiles, database.ChannelDBHandler implements database.DBHandler
}

// TestDBHandlerInterface_BoltDB verifies database.BoltDBHandler implements all database.DBHandler methods
func TestDBHandlerInterface_BoltDB(t *testing.T) {
	var _ database.DBHandler = (*database.BoltDBHandler)(nil)
	// If this compiles, database.BoltDBHandler implements database.DBHandler
}

func TestNewDBHandler_Default(t *testing.T) {
	// Clear any existing env var
	os.Unsetenv("DB_IMPLEMENTATION")

	tmpDir, err := os.MkdirTemp("", "db_factory_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	handler, err := database.NewDBHandler(dbPath, 10, 20)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	defer handler.Close()

	// Verify it's a database.BoltDBHandler by checking if UpdateQueueSizes works
	// (database.ChannelDBHandler returns error for UpdateQueueSizes)
	err = handler.UpdateQueueSizes(15, 25)
	if err != nil {
		t.Errorf("Expected UpdateQueueSizes to work (BoltDB), got error: %v", err)
	}
}

func TestNewDBHandler_Channel(t *testing.T) {
	// Set environment variable to use channel implementation
	os.Setenv("DB_IMPLEMENTATION", "channel")
	defer os.Unsetenv("DB_IMPLEMENTATION")

	handler, err := database.NewDBHandler("", 10, 20)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	defer handler.Close()

	// Verify it's a database.ChannelDBHandler by checking UpdateQueueSizes fails
	err = handler.UpdateQueueSizes(15, 25)
	if err == nil {
		t.Error("Expected UpdateQueueSizes to fail (database.ChannelDBHandler), got no error")
	}

	// Verify it works for other operations
	data := []byte{1, 2, 3}
	err = handler.StoreTRNGData(data)
	if err != nil {
		t.Errorf("Expected StoreTRNGData to work, got error: %v", err)
	}
}

func TestDBHandlerInterface_Methods(t *testing.T) {
	t.Run("channel handler methods", func(t *testing.T) {
		handler, _ := database.NewChannelDBHandler("", 10, 20)

		// Test all interface methods exist and are callable
		_ = handler.StoreTRNGData([]byte{1})
		_, _ = handler.GetTRNGData(1, 0, false)
		_ = handler.StoreFortunaData([]byte{2})
		_, _ = handler.GetFortunaData(1, 0, false)
		_, _ = handler.GetDetailedStats()
		_ = handler.IncrementPollingCount("trng")
		_ = handler.IncrementDroppedCount("trng")
		_, _ = handler.GetDatabaseSize()
		_ = handler.GetDatabasePath()
		_ = handler.RecordRNGUsage("trng", 100)
		_, _ = handler.GetRNGStatistics("trng", time.Now().Add(-1*time.Hour), time.Now())
		_, _ = handler.GetStats()
		_, _ = handler.GetQueueInfo()
		_ = handler.UpdateQueueSizes(15, 25)
		_ = handler.HealthCheck()
		_ = handler.Close()
	})

	t.Run("bolt handler methods", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "db_interface_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "test.db")
		handler, err := database.NewBoltDBHandler(dbPath, 10, 20)
		if err != nil {
			t.Fatalf("Failed to create handler: %v", err)
		}
		defer handler.Close()

		// Test all interface methods exist and are callable
		_ = handler.StoreTRNGData([]byte{1})
		_, _ = handler.GetTRNGData(1, 0, false)
		_ = handler.StoreFortunaData([]byte{2})
		_, _ = handler.GetFortunaData(1, 0, false)
		_, _ = handler.GetDetailedStats()
		_ = handler.IncrementPollingCount("trng")
		_ = handler.IncrementDroppedCount("trng")
		_, _ = handler.GetDatabaseSize()
		_ = handler.GetDatabasePath()
		_ = handler.RecordRNGUsage("trng", 100)
		_, _ = handler.GetRNGStatistics("trng", time.Now().Add(-1*time.Hour), time.Now())
		_, _ = handler.GetStats()
		_, _ = handler.GetQueueInfo()
		_ = handler.UpdateQueueSizes(15, 25)
		_ = handler.HealthCheck()
		_ = handler.Close()
	})
}
