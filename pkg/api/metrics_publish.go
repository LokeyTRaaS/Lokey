package api

import (
	"sync"

	"github.com/lokey/rng-service/pkg/database"
)

type qualitySnapshot struct {
	repetition int64
	aptBias    int64
	aptSamples int64
}

var (
	qualitySnapMu sync.Mutex
	qualitySnaps  = map[string]qualitySnapshot{}
)

func qualityKey(source, deviceID string) string {
	return source + "|" + deviceID
}

func (s *Server) publishQuality(source, deviceID string, tracker *TRNGQualityTracker) {
	if s.metrics == nil || tracker == nil {
		return
	}
	q := tracker.GetQualityMetrics()
	s.setQualityGauges(source, deviceID, q)

	key := qualityKey(source, deviceID)
	qualitySnapMu.Lock()
	prev := qualitySnaps[key]
	if s.metrics.RepetitionFails != nil {
		if delta := q.RepetitionCount.Failures - prev.repetition; delta > 0 {
			s.metrics.RepetitionFails.WithLabelValues(source, deviceID).Add(float64(delta))
		}
	}
	if s.metrics.APTBiasTotal != nil {
		if delta := q.APT.BiasCount - prev.aptBias; delta > 0 {
			s.metrics.APTBiasTotal.WithLabelValues(source, deviceID).Add(float64(delta))
		}
	}
	if s.metrics.APTSamplesTotal != nil {
		if delta := q.APT.SamplesProcessed - prev.aptSamples; delta > 0 {
			s.metrics.APTSamplesTotal.WithLabelValues(source, deviceID).Add(float64(delta))
		}
	}
	qualitySnaps[key] = qualitySnapshot{
		repetition: q.RepetitionCount.Failures,
		aptBias:    q.APT.BiasCount,
		aptSamples: q.APT.SamplesProcessed,
	}
	qualitySnapMu.Unlock()
}

func (s *Server) setQualityGauges(source, deviceID string, q database.QualityMetrics) {
	if s.metrics.MonobitAverage != nil {
		s.metrics.MonobitAverage.WithLabelValues(source, deviceID).Set(q.Monobit.Average)
	}
	if s.metrics.MonobitOnes != nil {
		s.metrics.MonobitOnes.WithLabelValues(source, deviceID).Set(float64(q.Monobit.Ones))
	}
	if s.metrics.MonobitZeros != nil {
		s.metrics.MonobitZeros.WithLabelValues(source, deviceID).Set(float64(q.Monobit.Zeros))
	}
	if s.metrics.MonobitTotal != nil {
		s.metrics.MonobitTotal.WithLabelValues(source, deviceID).Set(float64(q.Monobit.Total))
	}
	// Always publish absolute health gauges so dashboards show a flat 0 when healthy
	// (counter series only appear after the first failure/bias event).
	if s.metrics.RepetitionCurrent != nil {
		s.metrics.RepetitionCurrent.WithLabelValues(source, deviceID).Set(float64(q.RepetitionCount.Failures))
	}
	if s.metrics.APTBiasCurrent != nil {
		s.metrics.APTBiasCurrent.WithLabelValues(source, deviceID).Set(float64(q.APT.BiasCount))
	}
}

func (s *Server) updateFortunaAmplification() {
	if s.metrics == nil || s.metrics.FortunaAmplification == nil {
		return
	}
	seed := s.fortunaSeedBytesAcc.Load()
	out := s.fortunaOutputBytesAcc.Load()
	if seed == 0 {
		s.metrics.FortunaAmplification.Set(0)
		return
	}
	s.metrics.FortunaAmplification.Set(float64(out) / float64(seed))
}

func (s *Server) publishQueueMetrics(stats *database.DetailedStats) {
	if s.metrics == nil {
		return
	}
	type qp struct {
		source string
		cur    float64
		cap    float64
		pct    float64
		cons   float64
		uncons float64
	}
	rows := []qp{
		{"trng", float64(stats.TRNG.QueueCurrent), float64(stats.TRNG.QueueCapacity), stats.TRNG.QueuePercentage, float64(stats.TRNG.ConsumedCount), float64(stats.TRNG.UnconsumedCount)},
		{"fortuna", float64(stats.Fortuna.QueueCurrent), float64(stats.Fortuna.QueueCapacity), stats.Fortuna.QueuePercentage, float64(stats.Fortuna.ConsumedCount), float64(stats.Fortuna.UnconsumedCount)},
		{"virtio", float64(stats.VirtIO.QueueCurrent), float64(stats.VirtIO.QueueCapacity), stats.VirtIO.QueuePercentage, float64(stats.VirtIO.ConsumedCount), float64(stats.VirtIO.UnconsumedCount)},
	}
	for _, r := range rows {
		if s.metrics.QueueCurrent != nil {
			s.metrics.QueueCurrent.WithLabelValues(r.source).Set(r.cur)
		}
		if s.metrics.QueueCapacity != nil {
			s.metrics.QueueCapacity.WithLabelValues(r.source).Set(r.cap)
		}
		if s.metrics.QueuePercentage != nil {
			s.metrics.QueuePercentage.WithLabelValues(r.source).Set(r.pct)
		}
		if s.metrics.ConsumedTotal != nil {
			s.metrics.ConsumedTotal.WithLabelValues(r.source).Set(r.cons)
		}
		if s.metrics.Unconsumed != nil {
			s.metrics.Unconsumed.WithLabelValues(r.source).Set(r.uncons)
		}
	}
}
