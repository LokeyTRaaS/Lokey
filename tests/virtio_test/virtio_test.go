package virtio_test

import (
	"testing"

	"github.com/lokey/rng-service/pkg/virtio"
)

func TestVirtIORNG_GenerateRandom_Deprecated(t *testing.T) {
	// GenerateRandom() is deprecated - VirtIO should not read from device
	// It should return an error indicating it's not supported
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	data, err := v.GenerateRandom()
	if err == nil {
		t.Error("Expected GenerateRandom() to return error (deprecated method)")
	}
	if data != nil {
		t.Error("Expected nil data when GenerateRandom() returns error")
	}
}

func TestVirtIORNG_HealthCheck_QueueBased(t *testing.T) {
	// Health check should work without device access - it's queue-based
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	// Health check should return true if queue exists and has capacity
	if !v.HealthCheck() {
		t.Error("HealthCheck should return true when queue is available (device not required)")
	}

	// Health check should work even with non-existent device path
	v2, err := virtio.NewVirtIORNG("/nonexistent/device/path", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG with non-existent device: %v", err)
	}
	defer v2.Close()

	if !v2.HealthCheck() {
		t.Error("HealthCheck should return true even without device access")
	}
}

func TestVirtIORNG_Seed(t *testing.T) {
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	testData := []byte{1, 2, 3, 4, 5}
	if err := v.Seed(testData); err != nil {
		t.Fatalf("Failed to seed: %v", err)
	}

	// Retrieve the data
	data, err := v.GetData(1, 0, false)
	if err != nil {
		t.Fatalf("Failed to get data: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected to retrieve seeded data")
	}

	if len(data[0]) != len(testData) {
		t.Errorf("Expected data length %d, got %d", len(testData), len(data[0]))
	}
}

func TestVirtIORNG_UpdateQueueSize(t *testing.T) {
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	// Seed some data
	for i := 0; i < 10; i++ {
		if err := v.Seed([]byte{byte(i)}); err != nil {
			t.Fatalf("Failed to seed: %v", err)
		}
	}

	// Update queue size
	if err := v.UpdateQueueSize(500); err != nil {
		t.Fatalf("Failed to update queue size: %v", err)
	}

	if v.GetQueueSize() != 500 {
		t.Errorf("Expected queue size 500, got %d", v.GetQueueSize())
	}
}

func TestVirtIORNG_GetQueueSize(t *testing.T) {
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	if v.GetQueueSize() != 1000 {
		t.Errorf("Expected queue size 1000, got %d", v.GetQueueSize())
	}
}

func TestVirtIORNG_GetQueueCurrent(t *testing.T) {
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	if v.GetQueueCurrent() != 0 {
		t.Errorf("Expected queue current 0, got %d", v.GetQueueCurrent())
	}

	// Seed some data
	if err := v.Seed([]byte{1, 2, 3}); err != nil {
		t.Fatalf("Failed to seed: %v", err)
	}

	if v.GetQueueCurrent() != 1 {
		t.Errorf("Expected queue current 1, got %d", v.GetQueueCurrent())
	}
}

func TestVirtIORNG_GetData_Consume(t *testing.T) {
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	// Seed some data
	if err := v.Seed([]byte{1, 2, 3}); err != nil {
		t.Fatalf("Failed to seed: %v", err)
	}

	// Get data without consuming
	data1, err := v.GetData(1, 0, false)
	if err != nil {
		t.Fatalf("Failed to get data: %v", err)
	}

	if v.GetQueueCurrent() != 1 {
		t.Errorf("Expected queue current 1 after non-consuming read, got %d", v.GetQueueCurrent())
	}

	// Get data with consuming
	data2, err := v.GetData(1, 0, true)
	if err != nil {
		t.Fatalf("Failed to get data: %v", err)
	}

	if v.GetQueueCurrent() != 0 {
		t.Errorf("Expected queue current 0 after consuming read, got %d", v.GetQueueCurrent())
	}

	// Data should be the same
	if len(data1) != len(data2) || len(data1[0]) != len(data2[0]) {
		t.Error("Data should be the same before and after consuming")
	}
}

func TestVirtIORNG_ConcurrentAccess(t *testing.T) {
	v, err := virtio.NewVirtIORNG("", 1000)
	if err != nil {
		t.Fatalf("Failed to create VirtIO RNG: %v", err)
	}
	defer v.Close()

	// Concurrent seeding
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- true }()
			if err := v.Seed([]byte{byte(idx)}); err != nil {
				t.Errorf("Failed to seed in goroutine %d: %v", idx, err)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	if v.GetQueueCurrent() != 10 {
		t.Errorf("Expected queue current 10, got %d", v.GetQueueCurrent())
	}
}
