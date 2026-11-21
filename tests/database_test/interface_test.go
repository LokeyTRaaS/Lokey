package database_test

import (
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

// TestDBHandlerInterface_Channel verifies database.ChannelDBHandler implements all database.DBHandler methods
func TestDBHandlerInterface_Channel(t *testing.T) {
	var _ database.DBHandler = (*database.ChannelDBHandler)(nil)
}

func TestNewDBHandler(t *testing.T) {
	handler, err := database.NewDBHandler(10, 20, 15)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	defer handler.Close()

	if got := handler.GetDatabasePath(); got != "memory://channels" {
		t.Errorf("Expected memory://channels, got %s", got)
	}

	if err := handler.UpdateQueueSizes(15, 25, 20); err != nil {
		t.Errorf("Expected UpdateQueueSizes to succeed, got %v", err)
	}

	data := []byte{1, 2, 3}
	if err := handler.StoreTRNGData(data); err != nil {
		t.Errorf("Expected StoreTRNGData to work, got error: %v", err)
	}
}

func TestDBHandlerInterface_Methods(t *testing.T) {
	handler, err := database.NewChannelDBHandler(10, 20, 15)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

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
	_ = handler.UpdateQueueSizes(15, 25, 20)
	_ = handler.HealthCheck()
}
