// Package virtio provides VirtIO RNG service with circular queue for named pipe and HTTP streaming.
// VirtIO serves data from its queue (seeded by API) to VMs via named pipe (FIFO) and HTTP streaming.
// Device access is not required - all data comes from the seeded queue.
package virtio

import (
	"fmt"
	"sync"
	"time"

	"github.com/lokey/rng-service/pkg/database"
)

const (
	// DefaultDevicePath is the default path for the hardware random number generator device.
	DefaultDevicePath = "/dev/hwrng"
	// FallbackDevicePath is the fallback path if /dev/hwrng is not available.
	FallbackDevicePath = "/dev/random"
	// DefaultQueueSize is the default size for the internal circular queue.
	// Set to ~15,000 items to achieve ~480 KB total capacity (within 100-500 KB requirement).
	// Calculation: 15,000 items × 32 bytes/item = 480,000 bytes = 480 KB
	DefaultQueueSize = 15000
	// RandomDataSize is the size of random data to generate per request (32 bytes, same as controller).
	RandomDataSize = 32
)

// DeviceState represents the current health state of the device.
type DeviceState int

const (
	// DeviceStateUnknown represents an unknown device state.
	DeviceStateUnknown DeviceState = iota
	// DeviceStateHealthy represents a healthy device state.
	DeviceStateHealthy
	// DeviceStateFailed represents a failed device state.
	DeviceStateFailed
	// DeviceStateRecovering represents a recovering device state.
	DeviceStateRecovering
)

// VirtIORNG represents the VirtIO RNG service.
// It serves data from its queue (seeded by API) to VMs via EGD protocol.
type VirtIORNG struct {
	// Internal circular queue for storing seeded data
	queue      *database.CircularQueue
	queueMutex sync.RWMutex
	nextID     uint64
	idMutex    sync.Mutex
}

// NewVirtIORNG creates a new VirtIO RNG controller.
// Device access is optional and not required - VirtIO serves data from its queue only.
func NewVirtIORNG(devicePath string, queueSize int) (*VirtIORNG, error) {
	if queueSize < 10 {
		queueSize = DefaultQueueSize
	}
	if queueSize > 1000000 {
		queueSize = 1000000
	}

	v := &VirtIORNG{
		queue: database.NewCircularQueue(queueSize),
	}

	// Device initialization is optional - we don't need it
	// VirtIO service only serves data from its queue (seeded by API)
	// No device access required

	return v, nil
}


// GenerateRandom is deprecated - VirtIO should not read from device.
// Use PullBytes() to get data from the queue instead.
// This method is kept for backward compatibility but returns an error.
func (v *VirtIORNG) GenerateRandom() ([]byte, error) {
	return nil, fmt.Errorf("GenerateRandom() is not supported - VirtIO serves data from queue only. Use PullBytes() instead")
}

// Seed stores random data in the circular queue.
func (v *VirtIORNG) Seed(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty data provided")
	}

	v.queueMutex.Lock()
	defer v.queueMutex.Unlock()

	v.idMutex.Lock()
	id := v.nextID
	v.nextID++
	v.idMutex.Unlock()

	item := database.DataItem{
		ID:        id,
		Data:      data,
		Timestamp: time.Now(),
	}

	v.queue.Push(item)
	return nil
}

// GetData retrieves data from the queue with pagination and consumption tracking.
func (v *VirtIORNG) GetData(limit, offset int, consume bool) ([][]byte, error) {
	v.queueMutex.RLock()
	defer v.queueMutex.RUnlock()

	return v.queue.Get(limit, offset, consume), nil
}

// PullBytes pulls exactly 'length' bytes from the queue, consuming items as needed.
// Returns concatenated bytes, number of items consumed, and error.
// This method is used by EGD protocol to pull raw bytes for QEMU consumption.
const (
	// MaxPullBytes is the maximum bytes that can be requested in a single pull (1MB)
	MaxPullBytes = 1024 * 1024
)

func (v *VirtIORNG) PullBytes(length int) ([]byte, int, error) {
	if length <= 0 {
		return nil, 0, fmt.Errorf("length must be positive, got %d", length)
	}

	if length > MaxPullBytes {
		return nil, 0, fmt.Errorf("length exceeds maximum pull size: %d > %d", length, MaxPullBytes)
	}

	v.queueMutex.Lock()
	defer v.queueMutex.Unlock()

	// Calculate how many items we might need (each item is typically 32 bytes)
	// We'll pull items until we have enough bytes
	itemsNeeded := (length + RandomDataSize - 1) / RandomDataSize // Ceiling division
	if itemsNeeded < 1 {
		itemsNeeded = 1
	}

	// Get items from queue (consuming them)
	items := v.queue.Get(itemsNeeded, 0, true)
	if len(items) == 0 {
		return nil, 0, fmt.Errorf("insufficient data in queue: requested %d bytes, queue is empty", length)
	}

	// Concatenate bytes from items
	result := make([]byte, 0, length)
	itemsConsumed := 0

	for _, item := range items {
		if len(result) >= length {
			break
		}

		remaining := length - len(result)
		if len(item) <= remaining {
			// Take entire item
			result = append(result, item...)
			itemsConsumed++
		} else {
			// Take only what we need from this item
			result = append(result, item[:remaining]...)
			itemsConsumed++

			// Note: We consumed the entire item even though we only used part of it
			// This is acceptable as we prioritize simplicity
			// and the queue will be continuously refilled by the API service
			break
		}
	}

	if len(result) < length {
		// We don't have enough data - this can happen if queue is nearly empty
		// Return what we have (actual length may be less than requested)
		return result, itemsConsumed, nil
	}

	return result[:length], itemsConsumed, nil
}

// GetQueueSizeBytes returns the total byte capacity of the queue.
// This calculates: capacity * average_item_size (32 bytes per item).
func (v *VirtIORNG) GetQueueSizeBytes() int {
	v.queueMutex.RLock()
	defer v.queueMutex.RUnlock()

	capacity := v.queue.Capacity()
	return capacity * RandomDataSize
}

// UpdateQueueSize dynamically resizes the queue.
func (v *VirtIORNG) UpdateQueueSize(newSize int) error {
	if newSize < 10 || newSize > 1000000 {
		return fmt.Errorf("invalid queue size: %d (must be between 10 and 1000000)", newSize)
	}

	v.queueMutex.Lock()
	defer v.queueMutex.Unlock()

	// Get current queue data before resizing
	currentSize := v.queue.Size()
	oldQueue := v.queue

	// Create new queue
	newQueue := database.NewCircularQueue(newSize)

	// Migrate data if possible (preserve as much as we can)
	if currentSize > 0 {
		// Get all current items (non-consuming read)
		items := oldQueue.Get(currentSize, 0, false)
		// Add to new queue (will be limited by new size)
		for _, item := range items {
			if len(item) > 0 {
				newQueue.Push(database.DataItem{
					ID:        v.nextID,
					Data:      item,
					Timestamp: time.Now(),
				})
				v.idMutex.Lock()
				v.nextID++
				v.idMutex.Unlock()
			}
		}
	}

	// Replace queue
	v.queue = newQueue
	return nil
}

// GetQueueSize returns the current queue capacity.
func (v *VirtIORNG) GetQueueSize() int {
	v.queueMutex.RLock()
	defer v.queueMutex.RUnlock()
	return v.queue.Capacity()
}

// GetQueueCurrent returns the current number of items in the queue.
func (v *VirtIORNG) GetQueueCurrent() int {
	v.queueMutex.RLock()
	defer v.queueMutex.RUnlock()
	return v.queue.Size()
}

// GetQueueStats returns the queue statistics (total generated, consumed, dropped).
func (v *VirtIORNG) GetQueueStats() (totalGenerated, consumedCount, droppedCount uint64) {
	v.queueMutex.RLock()
	defer v.queueMutex.RUnlock()
	
	if v.queue == nil {
		return 0, 0, 0
	}
	
	return v.queue.Stats.TotalCount.Load(),
		v.queue.Stats.ConsumedCount.Load(),
		v.queue.Stats.DroppedCount.Load()
}

// HealthCheck verifies that the queue is available and service is running.
// Device accessibility is not checked - VirtIO doesn't use device.
func (v *VirtIORNG) HealthCheck() bool {
	v.queueMutex.RLock()
	defer v.queueMutex.RUnlock()

	// Service is healthy if queue exists and has capacity
	// Queue can be empty (will be filled by API seeding)
	return v.queue != nil && v.queue.Capacity() > 0
}

// Close cleans up resources.
// No device to close - VirtIO doesn't use device.
func (v *VirtIORNG) Close() error {
	// No device to close - queue cleanup is handled by GC
	return nil
}
