package database

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// DataItem represents a single item in the queue
type DataItem struct {
	ID        uint64
	Data      []byte
	Timestamp time.Time
}

// QueueStats holds queue statistics using atomic operations
type QueueStats struct {
	pollingCount  atomic.Uint64
	droppedCount  atomic.Uint64
	consumedCount atomic.Uint64
	totalCount    atomic.Uint64
}

// CircularQueue implements a thread-safe circular buffer with channel semantics
type CircularQueue struct {
	items    []DataItem
	capacity int
	head     int // Read position
	tail     int // Write position
	size     int // Current number of items
	mu       sync.RWMutex
	stats    QueueStats
}

// NewCircularQueue creates a new circular queue
func NewCircularQueue(capacity int) *CircularQueue {
	return &CircularQueue{
		items:    make([]DataItem, capacity),
		capacity: capacity,
		head:     0,
		tail:     0,
		size:     0,
	}
}

// Push adds an item to the queue (FIFO: removes oldest if full)
func (q *CircularQueue) Push(item DataItem) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.size == q.capacity {
		// Queue is full, we'll overwrite the oldest item (head)
		q.stats.droppedCount.Add(1)
		q.head = (q.head + 1) % q.capacity
		q.size-- // Will be incremented back below
	}

	q.items[q.tail] = item
	q.tail = (q.tail + 1) % q.capacity
	q.size++
	q.stats.totalCount.Add(1)
}

// Get retrieves items with pagination
// If consume=true, items are removed from queue
// If consume=false, items are copied (read-only)
func (q *CircularQueue) Get(limit, offset int, consume bool) [][]byte {
	q.mu.Lock()
	defer q.mu.Unlock()

	if offset >= q.size {
		return nil
	}

	end := offset + limit
	if end > q.size {
		end = q.size
	}

	result := make([][]byte, 0, end-offset)

	if consume {
		// Consume mode: remove items from queue
		for i := 0; i < end-offset; i++ {
			if q.size == 0 {
				break
			}
			idx := (q.head + offset) % q.capacity
			result = append(result, q.items[idx].Data)
			q.stats.consumedCount.Add(1)

			// Only remove if offset is 0 (consuming from front)
			if offset == 0 {
				q.head = (q.head + 1) % q.capacity
				q.size--
			}
		}
	} else {
		// Non-consume mode: just read without removing
		for i := offset; i < end; i++ {
			idx := (q.head + i) % q.capacity
			result = append(result, q.items[idx].Data)
		}
	}

	return result
}

// Size returns current queue size
func (q *CircularQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.size
}

// Capacity returns queue capacity
func (q *CircularQueue) Capacity() int {
	return q.capacity
}

// ChannelDBHandler implements DBHandler using in-memory circular queues
type ChannelDBHandler struct {
	trngQueue     *CircularQueue
	fortunaQueue  *CircularQueue
	nextTRNGID    atomic.Uint64
	nextFortunaID atomic.Uint64
	mu            sync.RWMutex
}

// NewChannelDBHandler creates a new channel-based database handler
func NewChannelDBHandler(dbPath string, trngQueueSize, fortunaQueueSize int) (*ChannelDBHandler, error) {
	return &ChannelDBHandler{
		trngQueue:    NewCircularQueue(trngQueueSize),
		fortunaQueue: NewCircularQueue(fortunaQueueSize),
	}, nil
}

// Close is a no-op for channel handler
func (h *ChannelDBHandler) Close() error {
	return nil
}

//---------------------- TRNG Data Operations ----------------------

// StoreTRNGData stores raw TRNG data
func (h *ChannelDBHandler) StoreTRNGData(data []byte) error {
	id := h.nextTRNGID.Add(1) - 1

	item := DataItem{
		ID:        id,
		Data:      data,
		Timestamp: time.Now(),
	}

	h.trngQueue.Push(item)
	return nil
}

// GetTRNGData retrieves TRNG data with pagination and consumption tracking
func (h *ChannelDBHandler) GetTRNGData(limit, offset int, consume bool) ([][]byte, error) {
	return h.trngQueue.Get(limit, offset, consume), nil
}

//---------------------- Fortuna Data Operations ----------------------

// StoreFortunaData stores Fortuna-generated data
func (h *ChannelDBHandler) StoreFortunaData(data []byte) error {
	id := h.nextFortunaID.Add(1) - 1

	item := DataItem{
		ID:        id,
		Data:      data,
		Timestamp: time.Now(),
	}

	h.fortunaQueue.Push(item)
	return nil
}

// GetFortunaData retrieves Fortuna-generated data with pagination and consumption tracking
func (h *ChannelDBHandler) GetFortunaData(limit, offset int, consume bool) ([][]byte, error) {
	return h.fortunaQueue.Get(limit, offset, consume), nil
}

//---------------------- Enhanced Statistics Operations ----------------------

// IncrementPollingCount increments the polling counter for a data source
func (h *ChannelDBHandler) IncrementPollingCount(source string) error {
	if source == "trng" {
		h.trngQueue.stats.pollingCount.Add(1)
	} else if source == "fortuna" {
		h.fortunaQueue.stats.pollingCount.Add(1)
	}
	return nil
}

// IncrementDroppedCount increments the dropped counter for a data source
func (h *ChannelDBHandler) IncrementDroppedCount(source string) error {
	if source == "trng" {
		h.trngQueue.stats.droppedCount.Add(1)
	} else if source == "fortuna" {
		h.fortunaQueue.stats.droppedCount.Add(1)
	}
	return nil
}

// GetDetailedStats returns comprehensive system statistics
func (h *ChannelDBHandler) GetDetailedStats() (*DetailedStats, error) {
	stats := &DetailedStats{}

	// TRNG stats
	trngSize := h.trngQueue.Size()
	trngCapacity := h.trngQueue.Capacity()

	stats.TRNG.PollingCount = int64(h.trngQueue.stats.pollingCount.Load())
	stats.TRNG.QueueDropped = int64(h.trngQueue.stats.droppedCount.Load())
	stats.TRNG.ConsumedCount = int64(h.trngQueue.stats.consumedCount.Load())
	stats.TRNG.QueueCurrent = trngSize
	stats.TRNG.QueueCapacity = trngCapacity
	stats.TRNG.UnconsumedCount = trngSize
	stats.TRNG.TotalGenerated = int64(h.trngQueue.stats.totalCount.Load())

	if trngCapacity > 0 {
		stats.TRNG.QueuePercentage = float64(trngSize) / float64(trngCapacity) * 100
	}

	// Fortuna stats
	fortunaSize := h.fortunaQueue.Size()
	fortunaCapacity := h.fortunaQueue.Capacity()

	stats.Fortuna.PollingCount = int64(h.fortunaQueue.stats.pollingCount.Load())
	stats.Fortuna.QueueDropped = int64(h.fortunaQueue.stats.droppedCount.Load())
	stats.Fortuna.ConsumedCount = int64(h.fortunaQueue.stats.consumedCount.Load())
	stats.Fortuna.QueueCurrent = fortunaSize
	stats.Fortuna.QueueCapacity = fortunaCapacity
	stats.Fortuna.UnconsumedCount = fortunaSize
	stats.Fortuna.TotalGenerated = int64(h.fortunaQueue.stats.totalCount.Load())

	if fortunaCapacity > 0 {
		stats.Fortuna.QueuePercentage = float64(fortunaSize) / float64(fortunaCapacity) * 100
	}

	// Database stats (in-memory, so no file)
	stats.Database.SizeBytes = 0
	stats.Database.SizeHuman = "0 B (in-memory)"
	stats.Database.Path = "memory://channels"

	return stats, nil
}

// GetDatabaseSize returns 0 for in-memory implementation
func (h *ChannelDBHandler) GetDatabaseSize() (int64, error) {
	// Calculate approximate memory usage
	trngSize := h.trngQueue.Size()
	fortunaSize := h.fortunaQueue.Size()

	// Approximate: 32 bytes data + 24 bytes overhead per item
	approxSize := int64((trngSize + fortunaSize) * 56)

	return approxSize, nil
}

// GetDatabasePath returns a virtual path for in-memory storage
func (h *ChannelDBHandler) GetDatabasePath() string {
	return "memory://channels"
}

//---------------------- Statistics Operations ----------------------

// RecordRNGUsage is a no-op for channel implementation (no historical stats)
func (h *ChannelDBHandler) RecordRNGUsage(source string, bytesUsed int64) error {
	// No-op: we don't store historical usage stats in channel implementation
	return nil
}

// GetRNGStatistics returns empty for channel implementation (no historical stats)
func (h *ChannelDBHandler) GetRNGStatistics(source string, start, end time.Time) ([]UsageStat, error) {
	// Return empty slice: channel implementation doesn't support historical queries
	return []UsageStat{}, nil
}

// GetStats returns general statistics about the queues
func (h *ChannelDBHandler) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	stats["trng_count"] = h.trngQueue.Size()
	stats["fortuna_count"] = h.fortunaQueue.Size()
	stats["db_size"] = int64(0)

	return stats, nil
}

//---------------------- Queue Management ----------------------

// GetQueueInfo returns information about the current queue sizes
func (h *ChannelDBHandler) GetQueueInfo() (map[string]int, error) {
	info := map[string]int{
		"trng_queue_capacity":    h.trngQueue.Capacity(),
		"fortuna_queue_capacity": h.fortunaQueue.Capacity(),
		"trng_queue_current":     h.trngQueue.Size(),
		"fortuna_queue_current":  h.fortunaQueue.Size(),
	}

	return info, nil
}

// UpdateQueueSizes is not supported for channel implementation
func (h *ChannelDBHandler) UpdateQueueSizes(trngSize, fortunaSize int) error {
	return fmt.Errorf("dynamic queue resizing not supported in channel implementation")
}

//---------------------- Health Check ----------------------

// HealthCheck always returns true for in-memory implementation
func (h *ChannelDBHandler) HealthCheck() bool {
	return true
}
