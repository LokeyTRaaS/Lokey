// Package api provides randomness quality metrics for TRNG data.
package api

import (
	"math"
	"sync"

	"github.com/lokey/rng-service/pkg/database"
)

// TRNGQualityTracker tracks randomness quality metrics for TRNG data
// using three NIST statistical tests: Monobit, Repetition Count, and Adaptive Proportion Test
type TRNGQualityTracker struct {
	mu              sync.RWMutex
	monobitState    MonobitState
	repetitionState RepetitionState
	aptState        APTState
}

// MonobitState tracks the frequency test state
type MonobitState struct {
	zeros   int64
	ones    int64
	total   int64
	average float64
}

// RepetitionState tracks the repetition count test state
type RepetitionState struct {
	recentSamples   []byte
	maxWindow       int
	repetitionCount int64
	lastValue       byte
	currentRun      int
	cutoff          int
}

// APTState tracks the adaptive proportion test state
type APTState struct {
	window          []byte
	windowSize      int
	cutoff          int
	biasCount       int64
	samplesProcessed int64
	writeIndex      int
}


// NewTRNGQualityTracker creates a new TRNG quality tracker
// aptWindowSize is the window size for the Adaptive Proportion Test (default: 512)
func NewTRNGQualityTracker(aptWindowSize int) *TRNGQualityTracker {
	if aptWindowSize < 256 {
		aptWindowSize = 256
	}
	if aptWindowSize > 2048 {
		aptWindowSize = 2048
	}

	// Calculate APT cutoff: floor(0.5 * window_size) + 3 * sqrt(window_size * 0.25)
	aptCutoff := int(math.Floor(0.5*float64(aptWindowSize)) + 3*math.Sqrt(float64(aptWindowSize)*0.25))

	// Repetition Count Test cutoff: typically 35 for 8-bit values
	repetitionCutoff := 35

	return &TRNGQualityTracker{
		monobitState: MonobitState{},
		repetitionState: RepetitionState{
			recentSamples: make([]byte, 0, repetitionCutoff+5),
			maxWindow:     repetitionCutoff + 5,
			cutoff:        repetitionCutoff,
		},
		aptState: APTState{
			window:     make([]byte, aptWindowSize),
			windowSize: aptWindowSize,
			cutoff:     aptCutoff,
		},
	}
}

// ProcessData processes incoming TRNG data and updates all quality metrics
func (t *TRNGQualityTracker) ProcessData(data []byte) {
	if len(data) == 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Process each byte for all tests
	for _, b := range data {
		t.processMonobit(b)
		t.processRepetitionCount(b)
		t.processAPT(b)
	}
}

// processMonobit updates monobit (frequency) test statistics
func (t *TRNGQualityTracker) processMonobit(b byte) {
	// Count bits in the byte
	for i := 0; i < 8; i++ {
		t.monobitState.total++
		if (b>>i)&1 == 1 {
			t.monobitState.ones++
		} else {
			t.monobitState.zeros++
		}
	}

	// Calculate average on-demand
	if t.monobitState.total > 0 {
		t.monobitState.average = float64(t.monobitState.ones) / float64(t.monobitState.total)
	}
}

// processRepetitionCount updates repetition count test statistics
func (t *TRNGQualityTracker) processRepetitionCount(b byte) {
	// Check if this is a continuation of the previous value
	if b == t.repetitionState.lastValue {
		t.repetitionState.currentRun++
	} else {
		// Value changed, reset run
		t.repetitionState.currentRun = 1
		t.repetitionState.lastValue = b
	}

	// Add to recent samples window (for potential future use)
	if len(t.repetitionState.recentSamples) >= t.repetitionState.maxWindow {
		// Remove oldest
		t.repetitionState.recentSamples = t.repetitionState.recentSamples[1:]
	}
	t.repetitionState.recentSamples = append(t.repetitionState.recentSamples, b)

	// Check if current run exceeds cutoff
	if t.repetitionState.currentRun > t.repetitionState.cutoff {
		t.repetitionState.repetitionCount++
		// Reset to prevent counting the same run multiple times
		t.repetitionState.currentRun = 1
	}
}

// processAPT updates adaptive proportion test statistics
func (t *TRNGQualityTracker) processAPT(b byte) {
	t.aptState.samplesProcessed++

	// If window is not full, just add the value
	if t.aptState.samplesProcessed <= int64(t.aptState.windowSize) {
		t.aptState.window[t.aptState.writeIndex] = b
		t.aptState.writeIndex = (t.aptState.writeIndex + 1) % t.aptState.windowSize
		return
	}

	// Window is full, check for bias
	// Count occurrences of the new value in the current window
	count := 0
	for i := 0; i < t.aptState.windowSize; i++ {
		if t.aptState.window[i] == b {
			count++
		}
	}

	// If count exceeds cutoff, increment bias count
	if count > t.aptState.cutoff {
		t.aptState.biasCount++
	}

	// Add new value to window (overwrites oldest)
	t.aptState.window[t.aptState.writeIndex] = b
	t.aptState.writeIndex = (t.aptState.writeIndex + 1) % t.aptState.windowSize
}

// GetQualityMetrics returns the current quality metrics
func (t *TRNGQualityTracker) GetQualityMetrics() database.QualityMetrics {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return database.QualityMetrics{
		Monobit: database.MonobitMetrics{
			Zeros:   t.monobitState.zeros,
			Ones:    t.monobitState.ones,
			Total:   t.monobitState.total,
			Average: t.monobitState.average,
		},
		RepetitionCount: database.RepetitionCountMetrics{
			Failures:   t.repetitionState.repetitionCount,
			CurrentRun: t.repetitionState.currentRun,
			LastValue:  int(t.repetitionState.lastValue),
		},
		APT: database.APTMetrics{
			WindowSize:       t.aptState.windowSize,
			Cutoff:           t.aptState.cutoff,
			BiasCount:        t.aptState.biasCount,
			SamplesProcessed: t.aptState.samplesProcessed,
		},
	}
}

