package integration_test

import (
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

func TestChannelDatabase(t *testing.T) {
	db, err := database.NewChannelDBHandler(10, 20, 15)
	if err != nil {
		t.Fatalf("Failed to create channel handler: %v", err)
	}
	defer db.Close()

	t.Run("Store and retrieve TRNG data", func(t *testing.T) {
		testData := []byte{1, 2, 3, 4, 5}
		if err := db.StoreTRNGData(testData); err != nil {
			t.Fatalf("store TRNG: %v", err)
		}
		result, err := db.GetTRNGData(1, 0, false)
		if err != nil {
			t.Fatalf("get TRNG: %v", err)
		}
		if len(result) != 1 || string(result[0]) != string(testData) {
			t.Fatalf("unexpected TRNG result: %#v", result)
		}
	})

	t.Run("Store and retrieve Fortuna data", func(t *testing.T) {
		testData := []byte{10, 20, 30, 40, 50}
		if err := db.StoreFortunaData(testData); err != nil {
			t.Fatalf("store Fortuna: %v", err)
		}
		result, err := db.GetFortunaData(1, 0, false)
		if err != nil {
			t.Fatalf("get Fortuna: %v", err)
		}
		if len(result) != 1 || string(result[0]) != string(testData) {
			t.Fatalf("unexpected Fortuna result: %#v", result)
		}
	})

	t.Run("Queue info and stats", func(t *testing.T) {
		info, err := db.GetQueueInfo()
		if err != nil {
			t.Fatalf("GetQueueInfo: %v", err)
		}
		if info["trng_queue_capacity"] != 10 || info["fortuna_queue_capacity"] != 20 {
			t.Fatalf("unexpected capacities: %#v", info)
		}
		stats, err := db.GetDetailedStats()
		if err != nil {
			t.Fatalf("GetDetailedStats: %v", err)
		}
		if stats.Database.Path != "memory://channels" {
			t.Errorf("expected memory://channels, got %s", stats.Database.Path)
		}
	})

	t.Run("Update queue sizes", func(t *testing.T) {
		if err := db.UpdateQueueSizes(15, 25, 20); err != nil {
			t.Fatalf("UpdateQueueSizes: %v", err)
		}
		info, _ := db.GetQueueInfo()
		if info["trng_queue_capacity"] != 15 || info["fortuna_queue_capacity"] != 25 || info["virtio_queue_capacity"] != 20 {
			t.Fatalf("unexpected capacities after resize: %#v", info)
		}
	})

	t.Run("Health and stats helpers", func(t *testing.T) {
		if !db.HealthCheck() {
			t.Fatal("expected healthy")
		}
		if _, err := db.GetDatabaseSize(); err != nil {
			t.Fatalf("GetDatabaseSize: %v", err)
		}
		if err := db.RecordRNGUsage("trng", 100); err != nil {
			t.Fatalf("RecordRNGUsage: %v", err)
		}
		stats, err := db.GetRNGStatistics("trng", time.Now().Add(-time.Hour), time.Now())
		if err != nil {
			t.Fatalf("GetRNGStatistics: %v", err)
		}
		if len(stats) != 0 {
			t.Errorf("expected empty historical stats, got %d", len(stats))
		}
	})

	t.Run("Consume mode", func(t *testing.T) {
		fresh, _ := database.NewChannelDBHandler(10, 20, 15)
		defer fresh.Close()
		_ = fresh.StoreTRNGData([]byte{9})
		before, _ := fresh.GetDetailedStats()
		result, err := fresh.GetTRNGData(1, 0, true)
		if err != nil || len(result) != 1 {
			t.Fatalf("consume get: %v %#v", err, result)
		}
		after, _ := fresh.GetDetailedStats()
		if after.TRNG.QueueCurrent != before.TRNG.QueueCurrent-1 {
			t.Errorf("expected queue to shrink after consume, before=%d after=%d",
				before.TRNG.QueueCurrent, after.TRNG.QueueCurrent)
		}
	})
}
