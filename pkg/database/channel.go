// Package database provides database implementations for storing random number generation data.
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
	PollingCount  atomic.Uint64
	DroppedCount  atomic.Uint64
	ConsumedCount atomic.Uint64
	TotalCount    atomic.Uint64
}

// CircularQueue implements a thread-safe circular buffer with channel semantics
type CircularQueue struct {
	items         []DataItem
	capacity      int
	head          int // Read position
	tail          int // Write position
	size          int // Current number of items
	lastItemBytes int // Length of the most recently pushed item
	mu            sync.RWMutex
	Stats         QueueStats
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
		q.Stats.DroppedCount.Add(1)
		q.head = (q.head + 1) % q.capacity
		q.size-- // Will be incremented back below
	}

	q.items[q.tail] = item
	q.tail = (q.tail + 1) % q.capacity
	q.size++
	q.lastItemBytes = len(item.Data)
	q.Stats.TotalCount.Add(1)
}

// PopTail removes the most recently pushed item.
func (q *CircularQueue) PopTail() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.size == 0 {
		return nil, false
	}
	q.tail = (q.tail - 1 + q.capacity) % q.capacity
	item := q.items[q.tail]
	q.items[q.tail] = DataItem{}
	q.size--
	q.Stats.ConsumedCount.Add(1)
	return item.Data, true
}

// Get retrieves items with pagination
// If consume=true, items are removed from queue
// If consume=false, items are copied (read-only)
// Get retrieves items with pagination
// If consume=true, items are removed from queue (including offset items)
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
		// Consume mode: skip offset items, then return limit items
		// All items (offset + returned) are removed from queue

		// Skip offset items
		for i := 0; i < offset; i++ {
			if q.size == 0 {
				break
			}
			q.Stats.ConsumedCount.Add(1)
			q.head = (q.head + 1) % q.capacity
			q.size--
		}

		// Get and consume the requested items
		itemsToReturn := end - offset
		for i := 0; i < itemsToReturn; i++ {
			if q.size == 0 {
				break
			}
			// Always read from current head position
			result = append(result, q.items[q.head].Data)
			q.Stats.ConsumedCount.Add(1)

			// Move head forward and decrease size
			q.head = (q.head + 1) % q.capacity
			q.size--
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

// SampleStride copies items at logical indexes start, start+stride, start+2*stride, and so on.
// Logical index 0 is the current head. The walk stops at the end of the ring, or once the
// copied payloads reach maxBytes. maxBytes <= 0 takes the whole pass.
// nextIndex is the next logical index on this same pass after any removals.
// wrapped is true when the walk passed the end. consume removes only the returned items.
func (q *CircularQueue) SampleStride(start, stride, maxBytes int, consume bool) (items [][]byte, nextIndex int, wrapped bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if stride < 1 || start < 0 {
		return nil, 0, true
	}
	if q.size == 0 {
		// Not a finished lap. The caller keeps its cursor for when items arrive.
		return nil, start, false
	}
	if start >= q.size {
		return nil, 0, true
	}

	indexes := make([]int, 0, (q.size-start+stride-1)/stride)
	copied := 0
	next := start
	for next < q.size {
		if maxBytes > 0 && copied >= maxBytes && len(indexes) > 0 {
			break
		}
		idx := (q.head + next) % q.capacity
		buf := append([]byte(nil), q.items[idx].Data...)
		indexes = append(indexes, next)
		items = append(items, buf)
		copied += len(buf)
		next += stride
		if maxBytes > 0 && copied >= maxBytes {
			break
		}
	}
	wrapped = next >= q.size

	if consume && len(indexes) > 0 {
		q.removeLogical(indexes)
		if !wrapped {
			next -= len(indexes)
			if next < 0 {
				next = 0
			}
		}
	}
	if wrapped {
		return items, 0, true
	}
	return items, next, false
}

// removeLogical deletes the given logical indexes and closes the gaps.
// Caller holds q.mu. indexes are unique and in the range [0, size).
func (q *CircularQueue) removeLogical(indexes []int) {
	if len(indexes) == 0 || q.size == 0 {
		return
	}
	drop := make(map[int]struct{}, len(indexes))
	for _, idx := range indexes {
		if idx >= 0 && idx < q.size {
			drop[idx] = struct{}{}
		}
	}
	if len(drop) == 0 {
		return
	}
	write := 0
	for i := 0; i < q.size; i++ {
		if _, ok := drop[i]; ok {
			continue
		}
		src := (q.head + i) % q.capacity
		dst := (q.head + write) % q.capacity
		if src != dst {
			q.items[dst] = q.items[src]
		}
		write++
	}
	for i := write; i < q.size; i++ {
		q.items[(q.head+i)%q.capacity] = DataItem{}
	}
	q.Stats.ConsumedCount.Add(uint64(len(drop)))
	q.size = write
	q.tail = (q.head + q.size) % q.capacity
}

// Size returns current queue size
func (q *CircularQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.size
}

// Capacity returns queue capacity
func (q *CircularQueue) Capacity() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.capacity
}

// ByteStats reports payload bytes currently stored and the item size used for capacity.
// Item size is the common length when every stored item matches, otherwise the last pushed length.
func (q *CircularQueue) ByteStats() (currentBytes, itemBytes int) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	uniform := q.size > 0
	firstLen := 0
	for i := 0; i < q.size; i++ {
		n := len(q.items[(q.head+i)%q.capacity].Data)
		currentBytes += n
		if i == 0 {
			firstLen = n
			continue
		}
		if n != firstLen {
			uniform = false
		}
	}
	switch {
	case uniform && q.size > 0:
		itemBytes = firstLen
	default:
		itemBytes = q.lastItemBytes
	}
	return currentBytes, itemBytes
}

// Resize changes capacity, preserving newest items when shrinking.
func (q *CircularQueue) Resize(newCap int) error {
	if newCap < 1 {
		return fmt.Errorf("queue capacity must be at least 1")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if newCap == q.capacity {
		return nil
	}

	start := 0
	count := q.size
	if count > newCap {
		start = count - newCap
		count = newCap
	}
	next := make([]DataItem, newCap)
	for i := 0; i < count; i++ {
		next[i] = q.items[(q.head+start+i)%q.capacity]
	}
	q.items = next
	q.capacity = newCap
	q.head = 0
	q.tail = count % newCap
	q.size = count
	return nil
}

// ChannelDBHandler implements DBHandler using in-memory circular queues
type ChannelDBHandler struct {
	TRNGQueue     *CircularQueue
	FortunaQueue  *CircularQueue
	VirtIOQueue   *CircularQueue
	nextTRNGID    atomic.Uint64
	nextFortunaID atomic.Uint64
	nextVirtIOID  atomic.Uint64
	// Note: No mutex needed here - CircularQueue handles its own synchronization
	// and atomic fields are self-synchronized
}

// NewChannelDBHandler creates a new channel-based database handler
func NewChannelDBHandler(trngQueueSize, fortunaQueueSize, virtioQueueSize int) (*ChannelDBHandler, error) {
	return &ChannelDBHandler{
		TRNGQueue:    NewCircularQueue(trngQueueSize),
		FortunaQueue: NewCircularQueue(fortunaQueueSize),
		VirtIOQueue:  NewCircularQueue(virtioQueueSize),
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

	h.TRNGQueue.Push(item)
	return nil
}

// GetTRNGData retrieves TRNG data with pagination and consumption tracking
func (h *ChannelDBHandler) GetTRNGData(limit, offset int, consume bool) ([][]byte, error) {
	return h.TRNGQueue.Get(limit, offset, consume), nil
}

// SampleTRNGStride walks the TRNG ring by stride from start.
func (h *ChannelDBHandler) SampleTRNGStride(start, stride, maxBytes int, consume bool) ([][]byte, int, bool, error) {
	items, next, wrapped := h.TRNGQueue.SampleStride(start, stride, maxBytes, consume)
	return items, next, wrapped, nil
}

// PopNewestTRNG removes the sample most recently stored in the TRNG ring.
func (h *ChannelDBHandler) PopNewestTRNG() ([]byte, bool) {
	return h.TRNGQueue.PopTail()
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

	h.FortunaQueue.Push(item)
	return nil
}

// GetFortunaData retrieves Fortuna-generated data with pagination and consumption tracking
func (h *ChannelDBHandler) GetFortunaData(limit, offset int, consume bool) ([][]byte, error) {
	return h.FortunaQueue.Get(limit, offset, consume), nil
}

// SampleFortunaStride walks the Fortuna ring by stride from start.
func (h *ChannelDBHandler) SampleFortunaStride(start, stride, maxBytes int, consume bool) ([][]byte, int, bool, error) {
	items, next, wrapped := h.FortunaQueue.SampleStride(start, stride, maxBytes, consume)
	return items, next, wrapped, nil
}

// PopNewestFortuna removes the sample most recently stored in the Fortuna ring.
func (h *ChannelDBHandler) PopNewestFortuna() ([]byte, bool) {
	return h.FortunaQueue.PopTail()
}

//---------------------- VirtIO Data Operations ----------------------

// StoreVirtIOData stores VirtIO-generated data
func (h *ChannelDBHandler) StoreVirtIOData(data []byte) error {
	id := h.nextVirtIOID.Add(1) - 1

	item := DataItem{
		ID:        id,
		Data:      data,
		Timestamp: time.Now(),
	}

	h.VirtIOQueue.Push(item)
	return nil
}

// GetVirtIOData retrieves VirtIO-generated data with pagination and consumption tracking
func (h *ChannelDBHandler) GetVirtIOData(limit, offset int, consume bool) ([][]byte, error) {
	return h.VirtIOQueue.Get(limit, offset, consume), nil
}

//---------------------- Enhanced Statistics Operations ----------------------

// IncrementPollingCount increments the polling counter for a data source
func (h *ChannelDBHandler) IncrementPollingCount(source string) error {
	switch source {
	case "trng":
		h.TRNGQueue.Stats.PollingCount.Add(1)
	case "fortuna":
		h.FortunaQueue.Stats.PollingCount.Add(1)
	case "virtio":
		h.VirtIOQueue.Stats.PollingCount.Add(1)
	}
	return nil
}

// IncrementDroppedCount increments the dropped counter for a data source
func (h *ChannelDBHandler) IncrementDroppedCount(source string) error {
	switch source {
	case "trng":
		h.TRNGQueue.Stats.DroppedCount.Add(1)
	case "fortuna":
		h.FortunaQueue.Stats.DroppedCount.Add(1)
	case "virtio":
		h.VirtIOQueue.Stats.DroppedCount.Add(1)
	}
	return nil
}

func fillQueueStats(dst *DataSourceStats, q *CircularQueue) {
	currentBytes, itemBytes := q.ByteStats()
	size := q.Size()
	capacity := q.Capacity()

	dst.PollingCount = int64(q.Stats.PollingCount.Load())   // #nosec G115
	dst.QueueDropped = int64(q.Stats.DroppedCount.Load())   // #nosec G115
	dst.ConsumedCount = int64(q.Stats.ConsumedCount.Load()) // #nosec G115
	dst.QueueCurrent = size
	dst.QueueCapacity = capacity
	dst.UnconsumedCount = size
	dst.TotalGenerated = int64(q.Stats.TotalCount.Load()) // #nosec G115
	dst.QueueCurrentBytes = currentBytes
	dst.QueueItemBytes = itemBytes
	if itemBytes > 0 {
		dst.QueueCapacityBytes = capacity * itemBytes
	}
	if capacity > 0 {
		dst.QueuePercentage = float64(size) / float64(capacity) * 100
	}
}

// GetDetailedStats returns comprehensive system statistics
func (h *ChannelDBHandler) GetDetailedStats() (*DetailedStats, error) {
	stats := &DetailedStats{}
	fillQueueStats(&stats.TRNG, h.TRNGQueue)
	fillQueueStats(&stats.Fortuna, h.FortunaQueue)
	fillQueueStats(&stats.VirtIO, h.VirtIOQueue)

	buffered := int64(stats.TRNG.QueueCurrentBytes + stats.Fortuna.QueueCurrentBytes + stats.VirtIO.QueueCurrentBytes)
	stats.Database.SizeBytes = buffered
	stats.Database.SizeHuman = FormatBytes(buffered) + " (in-memory)"
	stats.Database.Path = "memory://channels"
	stats.TRNGQuality = QualityMetrics{}

	return stats, nil
}

// FormatBytes renders a byte count with a binary unit.
func FormatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KiB", "MiB", "GiB"}
	value := float64(n)
	unit := units[0]
	for i := 0; i < len(units)-1 && value >= 1024; i++ {
		value /= 1024
		unit = units[i+1]
	}
	if unit == "B" {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

// GetDatabaseSize returns the sum of entropy payload bytes currently stored.
func (h *ChannelDBHandler) GetDatabaseSize() (int64, error) {
	trngBytes, _ := h.TRNGQueue.ByteStats()
	fortunaBytes, _ := h.FortunaQueue.ByteStats()
	virtioBytes, _ := h.VirtIOQueue.ByteStats()
	return int64(trngBytes + fortunaBytes + virtioBytes), nil
}

// GetDatabasePath returns a virtual path for in-memory storage
func (h *ChannelDBHandler) GetDatabasePath() string {
	return "memory://channels"
}

//---------------------- Statistics Operations ----------------------

// RecordRNGUsage is a no-op for channel implementation (no historical stats)
func (h *ChannelDBHandler) RecordRNGUsage(_ string, _ int64) error {
	// No-op: we don't store historical usage stats in channel implementation
	return nil
}

// GetRNGStatistics returns empty for channel implementation (no historical stats)
func (h *ChannelDBHandler) GetRNGStatistics(_ string, _, _ time.Time) ([]UsageStat, error) {
	// Return empty slice: channel implementation doesn't support historical queries
	return []UsageStat{}, nil
}

// GetStats returns general statistics about the queues
func (h *ChannelDBHandler) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	stats["trng_count"] = h.TRNGQueue.Size()
	stats["fortuna_count"] = h.FortunaQueue.Size()
	stats["virtio_count"] = h.VirtIOQueue.Size()
	size, _ := h.GetDatabaseSize()
	stats["db_size"] = size

	return stats, nil
}

//---------------------- Queue Management ----------------------

// GetQueueInfo returns information about the current queue sizes
func (h *ChannelDBHandler) GetQueueInfo() (map[string]int, error) {
	info := map[string]int{
		"trng_queue_capacity":    h.TRNGQueue.Capacity(),
		"fortuna_queue_capacity": h.FortunaQueue.Capacity(),
		"virtio_queue_capacity":  h.VirtIOQueue.Capacity(),
		"trng_queue_current":     h.TRNGQueue.Size(),
		"fortuna_queue_current":  h.FortunaQueue.Size(),
		"virtio_queue_current":   h.VirtIOQueue.Size(),
	}

	return info, nil
}

// UpdateQueueSizes resizes in-memory queues (0 leaves a queue unchanged).
func (h *ChannelDBHandler) UpdateQueueSizes(trngSize, fortunaSize, virtioSize int) error {
	if trngSize > 0 {
		if err := h.TRNGQueue.Resize(trngSize); err != nil {
			return err
		}
	}
	if fortunaSize > 0 {
		if err := h.FortunaQueue.Resize(fortunaSize); err != nil {
			return err
		}
	}
	if virtioSize > 0 {
		if err := h.VirtIOQueue.Resize(virtioSize); err != nil {
			return err
		}
	}
	return nil
}

//---------------------- Health Check ----------------------

// HealthCheck always returns true for in-memory implementation
func (h *ChannelDBHandler) HealthCheck() bool {
	return true
}
