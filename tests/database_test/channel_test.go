package database_test

import (
	"testing"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

func TestCircularQueue_Push(t *testing.T) {
	t.Run("basic push", func(t *testing.T) {
		q := database.NewCircularQueue(5)
		item := database.DataItem{
			ID:        1,
			Data:      []byte{1, 2, 3},
			Timestamp: time.Now(),
		}
		q.Push(item)
		if q.Size() != 1 {
			t.Errorf("Expected size 1, got %d", q.Size())
		}
	})

	t.Run("capacity limits", func(t *testing.T) {
		q := database.NewCircularQueue(3)
		for i := 0; i < 5; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		if q.Size() != 3 {
			t.Errorf("Expected size 3 (capacity), got %d", q.Size())
		}
		if q.Capacity() != 3 {
			t.Errorf("Expected capacity 3, got %d", q.Capacity())
		}
	})

	t.Run("overflow behavior", func(t *testing.T) {
		q := database.NewCircularQueue(2)
		// Push items until queue is full
		for i := 0; i < 2; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		initialDropped := q.Stats.DroppedCount.Load()
		// Push one more to trigger overflow
		item := database.DataItem{
			ID:        10,
			Data:      []byte{10},
			Timestamp: time.Now(),
		}
		q.Push(item)
		if q.Stats.DroppedCount.Load() != initialDropped+1 {
			t.Errorf("Expected dropped count to increment, got %d", q.Stats.DroppedCount.Load())
		}
		if q.Size() != 2 {
			t.Errorf("Expected size to remain at capacity 2, got %d", q.Size())
		}
	})
}

func TestCircularQueue_Get(t *testing.T) {
	t.Run("read-only mode", func(t *testing.T) {
		q := database.NewCircularQueue(5)
		for i := 0; i < 3; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		sizeBefore := q.Size()
		result := q.Get(2, 0, false)
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}
		if q.Size() != sizeBefore {
			t.Errorf("Expected size to remain %d in read-only mode, got %d", sizeBefore, q.Size())
		}
	})

	t.Run("consume mode", func(t *testing.T) {
		q := database.NewCircularQueue(5)
		for i := 0; i < 3; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		result := q.Get(2, 0, true)
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}
		if q.Size() != 1 {
			t.Errorf("Expected size 1 after consuming 2 items, got %d", q.Size())
		}
		if q.Stats.ConsumedCount.Load() != 2 {
			t.Errorf("Expected consumed count 2, got %d", q.Stats.ConsumedCount.Load())
		}
	})

	t.Run("pagination with offset", func(t *testing.T) {
		q := database.NewCircularQueue(5)
		for i := 0; i < 4; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		result := q.Get(2, 1, false)
		if len(result) != 2 {
			t.Errorf("Expected 2 items with offset 1, got %d", len(result))
		}
		if result[0][0] != 1 {
			t.Errorf("Expected first item to be 1, got %d", result[0][0])
		}
	})

	t.Run("consume with offset", func(t *testing.T) {
		q := database.NewCircularQueue(5)
		for i := 0; i < 4; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		initialConsumed := q.Stats.ConsumedCount.Load()
		result := q.Get(2, 1, true)
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}
		// Should consume offset (1) + limit (2) = 3 items total
		if q.Stats.ConsumedCount.Load() != initialConsumed+3 {
			t.Errorf("Expected consumed count to increase by 3, got %d", q.Stats.ConsumedCount.Load())
		}
		if q.Size() != 1 {
			t.Errorf("Expected size 1 after consuming 3 items, got %d", q.Size())
		}
	})

	t.Run("offset exceeds size", func(t *testing.T) {
		q := database.NewCircularQueue(5)
		for i := 0; i < 2; i++ {
			item := database.DataItem{
				ID:        uint64(i),
				Data:      []byte{byte(i)},
				Timestamp: time.Now(),
			}
			q.Push(item)
		}
		result := q.Get(2, 5, false)
		if result != nil {
			t.Errorf("Expected nil when offset exceeds size, got %v", result)
		}
	})
}

func TestCircularQueue_Size_Capacity(t *testing.T) {
	q := database.NewCircularQueue(10)
	if q.Capacity() != 10 {
		t.Errorf("Expected capacity 10, got %d", q.Capacity())
	}
	if q.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", q.Size())
	}

	for i := 0; i < 5; i++ {
		item := database.DataItem{
			ID:        uint64(i),
			Data:      []byte{byte(i)},
			Timestamp: time.Now(),
		}
		q.Push(item)
	}

	if q.Size() != 5 {
		t.Errorf("Expected size 5, got %d", q.Size())
	}
	if q.Capacity() != 10 {
		t.Errorf("Expected capacity 10, got %d", q.Capacity())
	}
}

func TestCircularQueue_Concurrency(t *testing.T) {
	q := database.NewCircularQueue(100)
	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				item := database.DataItem{
					ID:        uint64(id*10 + j),
					Data:      []byte{byte(id), byte(j)},
					Timestamp: time.Now(),
				}
				q.Push(item)
			}
			done <- true
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				q.Get(1, 0, false)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Queue should still be in valid state
	if q.Size() < 0 || q.Size() > q.Capacity() {
		t.Errorf("Invalid queue state: size=%d, capacity=%d", q.Size(), q.Capacity())
	}
}

func TestNewChannelDBHandler(t *testing.T) {
	handler, err := database.NewChannelDBHandler("", 10, 20, 15)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if handler == nil {
		t.Fatal("Expected handler to be non-nil")
	}
	if handler.TRNGQueue.Capacity() != 10 {
		t.Errorf("Expected TRNG queue capacity 10, got %d", handler.TRNGQueue.Capacity())
	}
	if handler.FortunaQueue.Capacity() != 20 {
		t.Errorf("Expected Fortuna queue capacity 20, got %d", handler.FortunaQueue.Capacity())
	}
}

func TestChannelDBHandler_StoreTRNGData(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	data := []byte{1, 2, 3, 4}
	err := handler.StoreTRNGData(data)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if handler.TRNGQueue.Size() != 1 {
		t.Errorf("Expected queue size 1, got %d", handler.TRNGQueue.Size())
	}
}

func TestChannelDBHandler_GetTRNGData(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	data := []byte{1, 2, 3}
	handler.StoreTRNGData(data)

	t.Run("non-consume mode", func(t *testing.T) {
		result, err := handler.GetTRNGData(1, 0, false)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Errorf("Expected 1 item, got %d", len(result))
		}
		if handler.TRNGQueue.Size() != 1 {
			t.Errorf("Expected size to remain 1 in non-consume mode, got %d", handler.TRNGQueue.Size())
		}
	})

	t.Run("consume mode", func(t *testing.T) {
		handler2, _ := database.NewChannelDBHandler("", 10, 10, 15)
		handler2.StoreTRNGData([]byte{5, 6, 7})
		result, err := handler2.GetTRNGData(1, 0, true)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if len(result) != 1 {
			t.Errorf("Expected 1 item, got %d", len(result))
		}
		if handler2.TRNGQueue.Size() != 0 {
			t.Errorf("Expected size 0 after consume, got %d", handler2.TRNGQueue.Size())
		}
	})
}

func TestChannelDBHandler_StoreFortunaData(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	data := []byte{10, 20, 30}
	err := handler.StoreFortunaData(data)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if handler.FortunaQueue.Size() != 1 {
		t.Errorf("Expected queue size 1, got %d", handler.FortunaQueue.Size())
	}
}

func TestChannelDBHandler_GetFortunaData(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	data := []byte{10, 20, 30}
	handler.StoreFortunaData(data)

	result, err := handler.GetFortunaData(1, 0, false)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(result) != 1 {
		t.Errorf("Expected 1 item, got %d", len(result))
	}
}

func TestChannelDBHandler_IncrementPollingCount(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)

	err := handler.IncrementPollingCount("trng")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if handler.TRNGQueue.Stats.PollingCount.Load() != 1 {
		t.Errorf("Expected polling count 1, got %d", handler.TRNGQueue.Stats.PollingCount.Load())
	}

	err = handler.IncrementPollingCount("fortuna")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if handler.FortunaQueue.Stats.PollingCount.Load() != 1 {
		t.Errorf("Expected polling count 1, got %d", handler.FortunaQueue.Stats.PollingCount.Load())
	}
}

func TestChannelDBHandler_IncrementDroppedCount(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)

	err := handler.IncrementDroppedCount("trng")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if handler.TRNGQueue.Stats.DroppedCount.Load() != 1 {
		t.Errorf("Expected dropped count 1, got %d", handler.TRNGQueue.Stats.DroppedCount.Load())
	}

	err = handler.IncrementDroppedCount("fortuna")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if handler.FortunaQueue.Stats.DroppedCount.Load() != 1 {
		t.Errorf("Expected dropped count 1, got %d", handler.FortunaQueue.Stats.DroppedCount.Load())
	}
}

func TestChannelDBHandler_GetDetailedStats(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 20, 15)
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

	if stats.Fortuna.QueueCapacity != 20 {
		t.Errorf("Expected Fortuna capacity 20, got %d", stats.Fortuna.QueueCapacity)
	}
	if stats.Fortuna.QueueCurrent != 1 {
		t.Errorf("Expected Fortuna current 1, got %d", stats.Fortuna.QueueCurrent)
	}

	if stats.Database.Path != "memory://channels" {
		t.Errorf("Expected path 'memory://channels', got %s", stats.Database.Path)
	}
}

func TestChannelDBHandler_GetQueueInfo(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 15, 25, 20)
	handler.StoreTRNGData([]byte{1})
	handler.StoreFortunaData([]byte{2})

	info, err := handler.GetQueueInfo()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if info["trng_queue_capacity"] != 15 {
		t.Errorf("Expected TRNG capacity 15, got %d", info["trng_queue_capacity"])
	}
	if info["trng_queue_current"] != 1 {
		t.Errorf("Expected TRNG current 1, got %d", info["trng_queue_current"])
	}
	if info["fortuna_queue_capacity"] != 25 {
		t.Errorf("Expected Fortuna capacity 25, got %d", info["fortuna_queue_capacity"])
	}
	if info["fortuna_queue_current"] != 1 {
		t.Errorf("Expected Fortuna current 1, got %d", info["fortuna_queue_current"])
	}
}

func TestChannelDBHandler_UpdateQueueSizes(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	err := handler.UpdateQueueSizes(20, 30, 25)
	if err == nil {
		t.Error("Expected error for UpdateQueueSizes (not supported), got nil")
	}
}

func TestChannelDBHandler_HealthCheck(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	if !handler.HealthCheck() {
		t.Error("Expected HealthCheck to return true, got false")
	}
}

func TestChannelDBHandler_Close(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	err := handler.Close()
	if err != nil {
		t.Errorf("Expected no error from Close, got %v", err)
	}
}

func TestChannelDBHandler_RecordRNGUsage(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	err := handler.RecordRNGUsage("trng", 100)
	if err != nil {
		t.Errorf("Expected no error (no-op), got %v", err)
	}
}

func TestChannelDBHandler_GetRNGStatistics(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
	stats, err := handler.GetRNGStatistics("trng", time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("Expected empty stats (no-op implementation), got %d items", len(stats))
	}
}

func TestChannelDBHandler_GetStats(t *testing.T) {
	handler, _ := database.NewChannelDBHandler("", 10, 10, 15)
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
	if dbSize, ok := stats["db_size"].(int64); !ok || dbSize != 0 {
		t.Errorf("Expected db_size 0 (in-memory), got %d", dbSize)
	}
}
