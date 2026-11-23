package api

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

const feedRatioScale = 1000000

// FeedSource is one draw from a stored ring.
// Ratio is the fraction of newly stored items that are given to the consumer.
// 0.1 means 10 percent. 0 turns the draw off. 1 takes every new item.
// Consume removes each chosen item from the source ring. False leaves it there.
type FeedSource struct {
	Ratio   float64 `json:"ratio" validate:"gte=0,lte=1"`
	Consume bool    `json:"consume"`
}

// VirtIOSources is the pair of draws that fill the VirtIO ring.
// Either ratio may be zero. Both above zero feeds VirtIO from both rings.
type VirtIOSources struct {
	TRNG    FeedSource `json:"trng"`
	Fortuna FeedSource `json:"fortuna"`
}

// ratioFeed selects a deterministic share of arriving items and holds them
// until the next seed tick.
type ratioFeed struct {
	mu      sync.Mutex
	ratio   float64
	scaled  uint64
	consume bool
	credit  uint64
	pending [][]byte
}

func (f *ratioFeed) configure(src FeedSource) {
	scaled := int(math.Round(src.Ratio * feedRatioScale))
	if scaled < 0 {
		scaled = 0
	}
	if scaled > feedRatioScale {
		scaled = feedRatioScale
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	next := uint64(scaled)
	if f.scaled != next {
		f.credit = 0
	}
	f.scaled = next
	f.ratio = float64(scaled) / feedRatioScale
	f.consume = src.Consume
}

func (f *ratioFeed) snapshot() FeedSource {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FeedSource{Ratio: f.ratio, Consume: f.consume}
}

// observe counts one new source item. remove is true when this item was chosen
// and the feed is in consume mode, so the caller deletes it from the source ring.
func (f *ratioFeed) observe(sample []byte) (remove bool) {
	if len(sample) == 0 {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scaled == 0 {
		return false
	}
	f.credit += f.scaled
	if f.credit < feedRatioScale {
		return false
	}
	f.credit -= feedRatioScale
	f.pending = append(f.pending, append([]byte(nil), sample...))
	return f.consume
}

func (f *ratioFeed) drain() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil
	}
	out := f.pending
	f.pending = nil
	return out
}

func (f *ratioFeed) restore(batch [][]byte) {
	if len(batch) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(batch, f.pending...)
}

func mergeFeed(dst *FeedSource, raw json.RawMessage) error {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return fmt.Errorf("invalid feed")
	}
	var incoming FeedSource
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return fmt.Errorf("invalid feed")
	}
	if _, ok := present["ratio"]; ok {
		if incoming.Ratio < 0 || incoming.Ratio > 1 {
			return fmt.Errorf("ratio must be between 0 and 1")
		}
		dst.Ratio = incoming.Ratio
	}
	if _, ok := present["consume"]; ok {
		dst.Consume = incoming.Consume
	}
	return nil
}

func mergeVirtIOSources(dst *VirtIOSources, raw json.RawMessage) error {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return fmt.Errorf("invalid virtio_sources")
	}
	if rawTRNG, ok := present["trng"]; ok {
		if err := mergeFeed(&dst.TRNG, rawTRNG); err != nil {
			return err
		}
	}
	if rawFortuna, ok := present["fortuna"]; ok {
		if err := mergeFeed(&dst.Fortuna, rawFortuna); err != nil {
			return err
		}
	}
	return nil
}

func ratiosForSeedingSource(source string, current VirtIOSources) VirtIOSources {
	switch source {
	case "fortuna":
		current.TRNG.Ratio = 0
		if current.Fortuna.Ratio <= 0 {
			current.Fortuna.Ratio = 0.1
		}
	case "both":
		if current.TRNG.Ratio <= 0 {
			current.TRNG.Ratio = 0.1
		}
		if current.Fortuna.Ratio <= 0 {
			current.Fortuna.Ratio = 0.1
		}
	default:
		current.Fortuna.Ratio = 0
		if current.TRNG.Ratio <= 0 {
			current.TRNG.Ratio = 0.1
		}
	}
	return current
}

// SampleRatio is the fraction of one ring included in a mixed data read.
// Ratio 0 leaves the ring out. 0.1 reads every 10th item. The value must be
// 0 or the reciprocal of an integer, so the stride lands on item boundaries.
type SampleRatio struct {
	Ratio float64 `json:"ratio"`
}

// DataSample is the pair of ratios for POST /api/v1/data with source mixed.
// The ratios are independent. 0.1 and 0.1 returns 10 percent of each ring.
type DataSample struct {
	TRNG    SampleRatio `json:"trng"`
	Fortuna SampleRatio `json:"fortuna"`
}

// StrideForRatio converts a mix ratio into an integer stride.
// 0 leaves the ring out. 0.1 is stride 10. A ratio that is not 1/n is rejected.
func StrideForRatio(ratio float64) (stride int, snapped float64, err error) {
	if ratio == 0 {
		return 0, 0, nil
	}
	if ratio < 0 || ratio > 1 {
		return 0, 0, fmt.Errorf("ratio must be between 0 and 1")
	}
	scaled := int(math.Round(ratio * feedRatioScale))
	if scaled <= 0 || scaled > feedRatioScale {
		return 0, 0, fmt.Errorf("ratio must be between 0 and 1")
	}
	if feedRatioScale%scaled != 0 {
		return 0, 0, fmt.Errorf("ratio must be the reciprocal of an integer stride")
	}
	return feedRatioScale / scaled, float64(scaled) / feedRatioScale, nil
}

func mergeSampleRatio(dst *SampleRatio, raw json.RawMessage) error {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return fmt.Errorf("invalid sample ratio")
	}
	var incoming SampleRatio
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return fmt.Errorf("invalid sample ratio")
	}
	if _, ok := present["ratio"]; ok {
		_, snapped, err := StrideForRatio(incoming.Ratio)
		if err != nil {
			return err
		}
		dst.Ratio = snapped
	}
	return nil
}

func mergeDataSample(dst *DataSample, raw json.RawMessage) error {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return fmt.Errorf("invalid data_sample")
	}
	if rawTRNG, ok := present["trng"]; ok {
		if err := mergeSampleRatio(&dst.TRNG, rawTRNG); err != nil {
			return err
		}
	}
	if rawFortuna, ok := present["fortuna"]; ok {
		if err := mergeSampleRatio(&dst.Fortuna, rawFortuna); err != nil {
			return err
		}
	}
	return nil
}

func seedingSourceFromRatios(trngRatio, fortunaRatio float64) string {
	switch {
	case trngRatio > 0 && fortunaRatio > 0:
		return "both"
	case fortunaRatio > 0:
		return "fortuna"
	default:
		return "trng"
	}
}
