package api

import "sync"

// sampleCursor remembers where a mixed read stopped on one ring.
// phase is the residue class of the current lap. next is the next logical
// index on that lap. After a lap passes the end, phase advances by one so
// the following lap does not read the same slots.
type sampleCursor struct {
	mu     sync.Mutex
	ratio  float64
	stride int
	phase  int
	next   int
}

func (c *sampleCursor) configure(ratio float64) error {
	stride, snapped, err := StrideForRatio(ratio)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stride != stride {
		c.phase = 0
		c.next = 0
	}
	c.stride = stride
	c.ratio = snapped
	return nil
}

func (c *sampleCursor) snapshot() SampleRatio {
	c.mu.Lock()
	defer c.mu.Unlock()
	return SampleRatio{Ratio: c.ratio}
}

func (c *sampleCursor) strideNow() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stride
}

type strideFunc func(start, stride, maxBytes int, consume bool) (items [][]byte, nextIndex int, wrapped bool, err error)

// take copies the next slice of this lap, stopping at maxBytes or at the end of the ring.
// A finished lap advances phase by one and the next lap starts at that phase.
func (c *sampleCursor) take(sample strideFunc, maxBytes int, consume bool) ([][]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stride == 0 || maxBytes <= 0 {
		return nil, false, nil
	}
	items, next, wrapped, err := sample(c.next, c.stride, maxBytes, consume)
	if err != nil {
		return nil, false, err
	}
	if wrapped {
		c.phase = (c.phase + 1) % c.stride
		c.next = c.phase
		return items, true, nil
	}
	c.next = next
	return items, false, nil
}

// collectMixed walks each configured ring by its stride and concatenates the
// selections, TRNG then Fortuna. Each ring advances at most one full cover.
func (s *Server) collectMixed(maxBytes int) ([][]byte, error) {
	trngStride := s.dataSampleTRNG.strideNow()
	fortunaStride := s.dataSampleFortuna.strideNow()
	if trngStride == 0 && fortunaStride == 0 {
		return nil, nil
	}

	var trngItems [][]byte
	var fortunaItems [][]byte
	got := 0
	trngLaps := 0
	fortunaLaps := 0
	trngStop := false
	fortunaStop := false

	for got < maxBytes {
		progressed := false
		if trngStride > 0 && !trngStop && trngLaps < trngStride && got < maxBytes {
			items, wrapped, err := s.dataSampleTRNG.take(s.DB.SampleTRNGStride, maxBytes-got, s.consumeTRNG.Load())
			if err != nil {
				return nil, err
			}
			if len(items) > 0 {
				trngItems = append(trngItems, items...)
				for _, item := range items {
					got += len(item)
				}
				progressed = true
			}
			if wrapped {
				trngLaps++
			} else {
				trngStop = true
			}
		}
		if fortunaStride > 0 && !fortunaStop && fortunaLaps < fortunaStride && got < maxBytes {
			items, wrapped, err := s.dataSampleFortuna.take(s.DB.SampleFortunaStride, maxBytes-got, s.consumeFortuna.Load())
			if err != nil {
				return nil, err
			}
			if len(items) > 0 {
				fortunaItems = append(fortunaItems, items...)
				for _, item := range items {
					got += len(item)
				}
				progressed = true
			}
			if wrapped {
				fortunaLaps++
			} else {
				fortunaStop = true
			}
		}
		if !progressed {
			break
		}
	}
	if len(trngItems) == 0 && len(fortunaItems) == 0 {
		return nil, nil
	}
	return append(trngItems, fortunaItems...), nil
}
