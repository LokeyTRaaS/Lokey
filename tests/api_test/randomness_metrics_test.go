package api_test

import (
	"crypto/rand"
	"testing"

	"github.com/lokey/rng-service/pkg/api"
)

func TestTRNGQualityTracker_Monobit(t *testing.T) {
	tracker := api.NewTRNGQualityTracker(512)

	t.Run("all zeros", func(t *testing.T) {
		tracker2 := api.NewTRNGQualityTracker(512)
		data := make([]byte, 100)
		// All zeros
		for i := range data {
			data[i] = 0x00
		}
		tracker2.ProcessData(data)

		metrics := tracker2.GetQualityMetrics()
		if metrics.Monobit.Zeros != 800 { // 100 bytes * 8 bits
			t.Errorf("Expected 800 zeros, got %d", metrics.Monobit.Zeros)
		}
		if metrics.Monobit.Ones != 0 {
			t.Errorf("Expected 0 ones, got %d", metrics.Monobit.Ones)
		}
		if metrics.Monobit.Average != 0.0 {
			t.Errorf("Expected average 0.0, got %f", metrics.Monobit.Average)
		}
	})

	t.Run("all ones", func(t *testing.T) {
		tracker2 := api.NewTRNGQualityTracker(512)
		data := make([]byte, 100)
		// All ones
		for i := range data {
			data[i] = 0xFF
		}
		tracker2.ProcessData(data)

		metrics := tracker2.GetQualityMetrics()
		if metrics.Monobit.Zeros != 0 {
			t.Errorf("Expected 0 zeros, got %d", metrics.Monobit.Zeros)
		}
		if metrics.Monobit.Ones != 800 { // 100 bytes * 8 bits
			t.Errorf("Expected 800 ones, got %d", metrics.Monobit.Ones)
		}
		if metrics.Monobit.Average != 1.0 {
			t.Errorf("Expected average 1.0, got %f", metrics.Monobit.Average)
		}
	})

	t.Run("balanced data", func(t *testing.T) {
		tracker2 := api.NewTRNGQualityTracker(512)
		// Create balanced data (alternating pattern)
		data := make([]byte, 100)
		for i := range data {
			data[i] = 0xAA // 10101010 - 4 ones, 4 zeros per byte
		}
		tracker2.ProcessData(data)

		metrics := tracker2.GetQualityMetrics()
		if metrics.Monobit.Zeros != 400 {
			t.Errorf("Expected 400 zeros, got %d", metrics.Monobit.Zeros)
		}
		if metrics.Monobit.Ones != 400 {
			t.Errorf("Expected 400 ones, got %d", metrics.Monobit.Ones)
		}
		if metrics.Monobit.Average != 0.5 {
			t.Errorf("Expected average 0.5, got %f", metrics.Monobit.Average)
		}
	})

	t.Run("random data", func(t *testing.T) {
		data := make([]byte, 1000)
		rand.Read(data)
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		// For random data, average should be close to 0.5
		if metrics.Monobit.Average < 0.45 || metrics.Monobit.Average > 0.55 {
			t.Errorf("Expected average close to 0.5 for random data, got %f", metrics.Monobit.Average)
		}
		if metrics.Monobit.Total != 8000 { // 1000 bytes * 8 bits
			t.Errorf("Expected total 8000 bits, got %d", metrics.Monobit.Total)
		}
		if metrics.Monobit.Zeros+metrics.Monobit.Ones != metrics.Monobit.Total {
			t.Errorf("Zeros + Ones should equal Total: %d + %d != %d",
				metrics.Monobit.Zeros, metrics.Monobit.Ones, metrics.Monobit.Total)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		tracker2 := api.NewTRNGQualityTracker(512)
		tracker2.ProcessData([]byte{})

		metrics := tracker2.GetQualityMetrics()
		if metrics.Monobit.Total != 0 {
			t.Errorf("Expected total 0 for empty data, got %d", metrics.Monobit.Total)
		}
	})
}

func TestTRNGQualityTracker_RepetitionCount(t *testing.T) {
	t.Run("no repetitions", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		data := make([]byte, 100)
		for i := range data {
			data[i] = byte(i % 256)
		}
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		if metrics.RepetitionCount.Failures != 0 {
			t.Errorf("Expected 0 failures for no repetitions, got %d", metrics.RepetitionCount.Failures)
		}
	})

	t.Run("repetition below threshold", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		// Create 30 repetitions (below cutoff of 35)
		data := make([]byte, 30)
		for i := range data {
			data[i] = 0x42
		}
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		if metrics.RepetitionCount.Failures != 0 {
			t.Errorf("Expected 0 failures for repetitions below threshold, got %d", metrics.RepetitionCount.Failures)
		}
		if metrics.RepetitionCount.CurrentRun != 30 {
			t.Errorf("Expected current run 30, got %d", metrics.RepetitionCount.CurrentRun)
		}
	})

	t.Run("repetition above threshold", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		// Create 40 repetitions (above cutoff of 35)
		data := make([]byte, 40)
		for i := range data {
			data[i] = 0x42
		}
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		if metrics.RepetitionCount.Failures < 1 {
			t.Errorf("Expected at least 1 failure for repetitions above threshold, got %d", metrics.RepetitionCount.Failures)
		}
	})

	t.Run("multiple repetition sequences", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		// Create multiple sequences of 40 repetitions
		data := make([]byte, 200)
		for i := 0; i < 200; i += 50 {
			for j := 0; j < 40 && i+j < 200; j++ {
				data[i+j] = byte(i / 50) // Different value for each sequence
			}
			if i+40 < 200 {
				data[i+40] = 0xFF // Break sequence
			}
		}
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		// Should detect multiple failures
		if metrics.RepetitionCount.Failures < 3 {
			t.Errorf("Expected at least 3 failures for multiple repetition sequences, got %d", metrics.RepetitionCount.Failures)
		}
	})
}

func TestTRNGQualityTracker_APT(t *testing.T) {
	t.Run("default window size", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		metrics := tracker.GetQualityMetrics()
		if metrics.APT.WindowSize != 512 {
			t.Errorf("Expected window size 512, got %d", metrics.APT.WindowSize)
		}
		if metrics.APT.Cutoff < 250 || metrics.APT.Cutoff > 300 {
			t.Errorf("Expected cutoff around 290 for window 512, got %d", metrics.APT.Cutoff)
		}
	})

	t.Run("custom window size", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(1024)
		metrics := tracker.GetQualityMetrics()
		if metrics.APT.WindowSize != 1024 {
			t.Errorf("Expected window size 1024, got %d", metrics.APT.WindowSize)
		}
	})

	t.Run("window size clamping", func(t *testing.T) {
		// Test minimum
		tracker1 := api.NewTRNGQualityTracker(100)
		metrics1 := tracker1.GetQualityMetrics()
		if metrics1.APT.WindowSize != 256 {
			t.Errorf("Expected window size clamped to 256, got %d", metrics1.APT.WindowSize)
		}

		// Test maximum
		tracker2 := api.NewTRNGQualityTracker(5000)
		metrics2 := tracker2.GetQualityMetrics()
		if metrics2.APT.WindowSize != 2048 {
			t.Errorf("Expected window size clamped to 2048, got %d", metrics2.APT.WindowSize)
		}
	})

	t.Run("random data - no bias", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		data := make([]byte, 1000)
		rand.Read(data)
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		if metrics.APT.SamplesProcessed != 1000 {
			t.Errorf("Expected 1000 samples processed, got %d", metrics.APT.SamplesProcessed)
		}
		// For random data, bias count should be low or zero
		if metrics.APT.BiasCount > 10 {
			t.Errorf("Expected low bias count for random data, got %d", metrics.APT.BiasCount)
		}
	})

	t.Run("biased data - all same value", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		// Fill window with same value
		data := make([]byte, 600) // More than window size
		for i := range data {
			data[i] = 0x42
		}
		tracker.ProcessData(data)

		metrics := tracker.GetQualityMetrics()
		// Should detect bias after window fills
		if metrics.APT.BiasCount == 0 && metrics.APT.SamplesProcessed > int64(metrics.APT.WindowSize) {
			t.Errorf("Expected bias detection for all-same data, got bias_count %d", metrics.APT.BiasCount)
		}
	})

	t.Run("samples processed counter", func(t *testing.T) {
		tracker := api.NewTRNGQualityTracker(512)
		data1 := make([]byte, 100)
		rand.Read(data1)
		tracker.ProcessData(data1)

		data2 := make([]byte, 200)
		rand.Read(data2)
		tracker.ProcessData(data2)

		metrics := tracker.GetQualityMetrics()
		if metrics.APT.SamplesProcessed != 300 {
			t.Errorf("Expected 300 samples processed, got %d", metrics.APT.SamplesProcessed)
		}
	})
}

func TestTRNGQualityTracker_ConcurrentAccess(t *testing.T) {
	tracker := api.NewTRNGQualityTracker(512)

	// Process data concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			data := make([]byte, 100)
			rand.Read(data)
			tracker.ProcessData(data)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	metrics := tracker.GetQualityMetrics()
	// Should have processed 10 * 100 = 1000 bytes
	if metrics.Monobit.Total != 8000 { // 1000 bytes * 8 bits
		t.Errorf("Expected total 8000 bits from concurrent processing, got %d", metrics.Monobit.Total)
	}
	if metrics.APT.SamplesProcessed != 1000 {
		t.Errorf("Expected 1000 samples processed from concurrent processing, got %d", metrics.APT.SamplesProcessed)
	}
}

func TestTRNGQualityTracker_GetQualityMetrics(t *testing.T) {
	tracker := api.NewTRNGQualityTracker(512)
	data := make([]byte, 50)
	rand.Read(data)
	tracker.ProcessData(data)

	metrics := tracker.GetQualityMetrics()

	// Verify all fields are populated
	if metrics.Monobit.Total == 0 {
		t.Error("Monobit total should be non-zero")
	}
	if metrics.APT.WindowSize == 0 {
		t.Error("APT window size should be non-zero")
	}
	if metrics.APT.Cutoff == 0 {
		t.Error("APT cutoff should be non-zero")
	}

	// Verify consistency
	if metrics.Monobit.Zeros+metrics.Monobit.Ones != metrics.Monobit.Total {
		t.Errorf("Monobit counters inconsistent: %d + %d != %d",
			metrics.Monobit.Zeros, metrics.Monobit.Ones, metrics.Monobit.Total)
	}
}
