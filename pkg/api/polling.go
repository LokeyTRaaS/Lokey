// Package api provides HTTP API endpoints for random number generation services.
package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lokey/rng-service/pkg/fortuna"
)

// StartPolling initiates background polling of external services for random data
func (s *Server) StartPolling(ctx context.Context, trngPollInterval, fortunaPollInterval time.Duration) {
	// Start TRNG polling
	go s.PollTRNGService(ctx, trngPollInterval)

	// Start Fortuna polling
	go s.PollFortunaService(ctx, fortunaPollInterval)

	// Start Fortuna seeding with TRNG data
	go s.SeedFortunaWithTRNG(ctx, 30*time.Second)

	// Start VirtIO seeding based on configuration
	if s.VirtioAddr != "" {
		seedingSource := s.getVirtIOSeedingSource()
		seedInterval := 30 * time.Second

		switch seedingSource {
		case "trng":
			go s.SeedVirtIOWithTRNG(ctx, seedInterval)
		case "fortuna":
			go s.SeedVirtIOWithFortuna(ctx, seedInterval)
		case "both":
			go s.SeedVirtIOWithTRNG(ctx, seedInterval)
			go s.SeedVirtIOWithFortuna(ctx, seedInterval)
		}
	}

	log.Printf("Started polling services - TRNG interval: %s, Fortuna interval: %s, Fortuna seeding: every 30s",
		trngPollInterval, fortunaPollInterval)
}

// pollTRNGService periodically polls the hardware TRNG controller service for new random data
func (s *Server) PollTRNGService(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting TRNG polling from %s with interval %s", s.ControllerAddr, interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("TRNG polling stopped")
			return
		case <-ticker.C:
			if err := s.FetchAndStoreTRNGData(); err != nil {
				log.Printf("TRNG polling error: %v", err)
			}
		}
	}
}

func (s *Server) controllersOrDefault() []ControllerEndpoint {
	if len(s.Controllers) > 0 {
		return s.Controllers
	}
	return []ControllerEndpoint{{DeviceID: "default", Addr: s.ControllerAddr}}
}

func (s *Server) nextController() ControllerEndpoint {
	endpoints := s.controllersOrDefault()
	idx := atomic.AddUint64(&s.controllerRoundRobin, 1) - 1
	return endpoints[idx%uint64(len(endpoints))]
}

func (s *Server) deviceTracker(deviceID string) *TRNGQualityTracker {
	s.trngDeviceMu.Lock()
	defer s.trngDeviceMu.Unlock()
	if t, ok := s.trngDeviceTrackers[deviceID]; ok {
		return t
	}
	t := NewTRNGQualityTracker(512)
	s.trngDeviceTrackers[deviceID] = t
	return t
}

// fetchAndStoreTRNGData fetches and stores TRNG data from controllers (round-robin).
func (s *Server) FetchAndStoreTRNGData() error {
	ep := s.nextController()
	resp, err := s.httpClient.Get(ep.Addr + "/generate")
	if err != nil {
		return fmt.Errorf("error connecting to TRNG controller %s (%s): %w", ep.DeviceID, ep.Addr, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing TRNG response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TRNG controller %s returned status %d", ep.DeviceID, resp.StatusCode)
	}

	var result struct {
		Data string `json:"data"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
		return fmt.Errorf("error parsing TRNG response: %w", decodeErr)
	}

	dataBytes, err := hex.DecodeString(result.Data)
	if err != nil {
		return fmt.Errorf("error decoding data from controller: %w", err)
	}

	s.trngQualityTracker.ProcessData(dataBytes)
	s.deviceTracker(ep.DeviceID).ProcessData(dataBytes)
	s.publishQuality("trng", ep.DeviceID, s.deviceTracker(ep.DeviceID))
	if s.metrics != nil && s.metrics.BytesProduced != nil {
		s.metrics.BytesProduced.WithLabelValues("trng", ep.DeviceID).Add(float64(len(dataBytes)))
	}

	if err := s.DB.StoreTRNGData(dataBytes); err != nil {
		return fmt.Errorf("error storing TRNG data: %w", err)
	}
	if err := s.DB.IncrementPollingCount("trng"); err != nil {
		log.Printf("Warning: failed to increment TRNG polling count: %v", err)
	}
	return nil
}

// pollFortunaService periodically polls the Fortuna PRNG service for new random data
func (s *Server) PollFortunaService(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting Fortuna polling from %s with interval %s", s.FortunaAddr, interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Fortuna polling stopped")
			return
		case <-ticker.C:
			if err := s.FetchAndStoreFortunaData(); err != nil {
				log.Printf("Fortuna polling error: %v", err)
			}
		}
	}
}

// fetchAndStoreFortunaData fetches and stores Fortuna data from the service
func (s *Server) FetchAndStoreFortunaData() error {
	// Attempt to fetch data from the Fortuna service using the correct endpoint
	// Specify size=256 for a reasonable amount of random data
	resp, err := s.httpClient.Get(s.FortunaAddr + "/generate?size=256")
	if err != nil {
		return fmt.Errorf("error connecting to Fortuna service: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing Fortuna response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fortuna service returned status %d", resp.StatusCode)
	}

	// Parse response which contains a data field with hex-encoded random data
	var result struct {
		Data string `json:"data"`
		Size int    `json:"size"`
	}

	if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
		return fmt.Errorf("error parsing Fortuna response: %w", decodeErr)
	}

	// Decode the hex-encoded data
	randomData, err := hex.DecodeString(result.Data)
	if err != nil {
		return fmt.Errorf("error decoding data from Fortuna: %w", err)
	}

	s.fortunaQualityTracker.ProcessData(randomData)
	s.publishQuality("fortuna", "fortuna", s.fortunaQualityTracker)
	if s.metrics != nil {
		if s.metrics.BytesProduced != nil {
			s.metrics.BytesProduced.WithLabelValues("fortuna", "fortuna").Add(float64(len(randomData)))
		}
		if s.metrics.FortunaOutputBytes != nil {
			s.metrics.FortunaOutputBytes.Add(float64(len(randomData)))
		}
		s.fortunaOutputBytesAcc.Add(uint64(len(randomData)))
		s.updateFortunaAmplification()
	}

	// Store the data in database
	if err := s.DB.StoreFortunaData(randomData); err != nil {
		return fmt.Errorf("error storing Fortuna data: %w", err)
	}

	// Increment polling count only on successful storage
	if err := s.DB.IncrementPollingCount("fortuna"); err != nil {
		log.Printf("Warning: failed to increment Fortuna polling count: %v", err)
	}

	return nil
}

// seedFortunaWithTRNG periodically seeds Fortuna generator with hardware TRNG data
func (s *Server) SeedFortunaWithTRNG(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting Fortuna seeding with TRNG data every %s", interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Fortuna seeding stopped")
			return
		case <-ticker.C:
			// Check circuit breaker
			if s.isSeedingCircuitOpen() {
				log.Printf("Fortuna seeding circuit breaker is OPEN, skipping attempt")
				continue
			}

			if err := s.SeedFortuna(); err != nil {
				s.seedingFailures.Add(1)
				s.lastSeedingFailure.Store(time.Now().Unix())
				log.Printf("Fortuna seeding error: %v (failures: %d)", err, s.seedingFailures.Load())

				// Open circuit breaker after 5 consecutive failures
				if s.seedingFailures.Load() >= 5 {
					s.seedingCircuitOpen.Store(true)
					log.Printf("Fortuna seeding circuit breaker opened after %d failures", s.seedingFailures.Load())
				}
			} else {
				// Reset failure count on success
				s.seedingFailures.Store(0)
				s.seedingCircuitOpen.Store(false)
			}
		}
	}
}

// isSeedingCircuitOpen checks if seeding circuit breaker is open
func (s *Server) isSeedingCircuitOpen() bool {
	if !s.seedingCircuitOpen.Load() {
		return false
	}

	// Check if cooldown period has expired (default: 5 minutes)
	lastFailure := s.lastSeedingFailure.Load()
	if lastFailure > 0 {
		elapsed := time.Since(time.Unix(lastFailure, 0))
		if elapsed >= s.seedingCooldown {
			// Transition to half-open for testing
			s.seedingCircuitOpen.Store(false)
			log.Printf("Fortuna seeding circuit breaker cooldown expired, resuming attempts")
			return false
		}
	}
	return true
}

// ValidateTRNGData validates that TRNG seed data has sufficient quality
func ValidateTRNGData(seeds []string) error {
	if len(seeds) == 0 {
		return fmt.Errorf("no seeds provided")
	}

	for i, seedHex := range seeds {
		seed, err := hex.DecodeString(seedHex)
		if err != nil {
			return fmt.Errorf("seed %d: invalid hex encoding: %w", i, err)
		}

		// Use Fortuna's validation function
		if !fortuna.ValidateSeedQuality(seed) {
			return fmt.Errorf("seed %d: low quality detected (repeating pattern or low entropy)", i)
		}
	}

	return nil
}

// seedFortuna fetches TRNG data from all healthy controllers and seeds Fortuna.
func (s *Server) SeedFortuna() error {
	var seeds []string
	var seedBytes int
	for _, ep := range s.controllersOrDefault() {
		resp, err := s.httpClient.Get(ep.Addr + "/generate?count=5")
		if err != nil {
			log.Printf("seeding: controller %s unreachable: %v", ep.DeviceID, err)
			continue
		}
		var result struct {
			Data interface{} `json:"data"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			log.Printf("seeding: controller %s bad response", ep.DeviceID)
			continue
		}
		switch v := result.Data.(type) {
		case string:
			seeds = append(seeds, v)
			if b, err := hex.DecodeString(v); err == nil {
				seedBytes += len(b)
			}
		case []interface{}:
			for _, item := range v {
				if sHex, ok := item.(string); ok {
					seeds = append(seeds, sHex)
					if b, err := hex.DecodeString(sHex); err == nil {
						seedBytes += len(b)
					}
				}
			}
		case []string:
			seeds = append(seeds, v...)
			for _, sHex := range v {
				if b, err := hex.DecodeString(sHex); err == nil {
					seedBytes += len(b)
				}
			}
		}
	}

	if len(seeds) == 0 {
		return fmt.Errorf("no TRNG data received for seeding from any controller")
	}
	if err := ValidateTRNGData(seeds); err != nil {
		return fmt.Errorf("TRNG data validation failed: %w", err)
	}

	seedRequest := struct {
		Seeds []string `json:"seeds"`
	}{Seeds: seeds}
	seedData, err := json.Marshal(seedRequest)
	if err != nil {
		return fmt.Errorf("error marshaling seed request: %w", err)
	}

	seedResp, err := s.httpClient.Post(s.FortunaAddr+"/seed", "application/json", bytes.NewBuffer(seedData))
	if err != nil {
		return fmt.Errorf("error seeding Fortuna: %w", err)
	}
	defer func() {
		if closeErr := seedResp.Body.Close(); closeErr != nil {
			log.Printf("Error closing seed response body: %v", closeErr)
		}
	}()
	if seedResp.StatusCode != http.StatusOK {
		return fmt.Errorf("fortuna seeding failed with status: %d", seedResp.StatusCode)
	}

	if s.metrics != nil {
		if s.metrics.FortunaSeedBytes != nil {
			s.metrics.FortunaSeedBytes.Add(float64(seedBytes))
		}
		s.fortunaSeedBytesAcc.Add(uint64(seedBytes))
		if s.metrics.FortunaReseedTotal != nil {
			s.metrics.FortunaReseedTotal.Inc()
		}
		if s.metrics.FortunaLastReseed != nil {
			s.metrics.FortunaLastReseed.Set(float64(time.Now().Unix()))
		}
		s.updateFortunaAmplification()
	}

	log.Printf("Successfully seeded Fortuna with %d TRNG samples from %d controllers", len(seeds), len(s.controllersOrDefault()))
	return nil
}

// SeedVirtIOWithTRNG seeds VirtIO with TRNG data
func (s *Server) SeedVirtIOWithTRNG(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting VirtIO seeding with TRNG data every %s", interval)

	seedOnce := func() {
		if s.isVirtIOSeedingCircuitOpen() {
			log.Printf("VirtIO seeding circuit breaker is OPEN, skipping attempt")
			return
		}
		if err := s.SeedVirtIOFromTRNG(); err != nil {
			s.virtioSeedingFailures.Add(1)
			s.lastVirtIOSeedingFailure.Store(time.Now().Unix())
			log.Printf("VirtIO seeding error: %v (failures: %d)", err, s.virtioSeedingFailures.Load())
			if s.virtioSeedingFailures.Load() >= 5 {
				s.virtioSeedingCircuitOpen.Store(true)
				log.Printf("VirtIO seeding circuit breaker opened after %d failures", s.virtioSeedingFailures.Load())
			}
			return
		}
		s.virtioSeedingFailures.Store(0)
	}

	// Seed immediately so /virtio endpoints are usable without waiting for the first tick.
	seedOnce()

	for {
		select {
		case <-ctx.Done():
			log.Printf("VirtIO seeding with TRNG stopped")
			return
		case <-ticker.C:
			seedOnce()
		}
	}
}

// SeedVirtIOFromTRNG fetches TRNG data and sends it to VirtIO's /seed endpoint
func (s *Server) SeedVirtIOFromTRNG() error {
	var seeds []string
	for _, ep := range s.controllersOrDefault() {
		resp, err := s.httpClient.Get(ep.Addr + "/generate?count=5")
		if err != nil {
			log.Printf("virtio seeding: controller %s unreachable: %v", ep.DeviceID, err)
			continue
		}
		var result struct {
			Data interface{} `json:"data"`
		}
		if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
			_ = resp.Body.Close()
			log.Printf("virtio seeding: controller %s decode error: %v", ep.DeviceID, decodeErr)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("virtio seeding: controller %s status %d", ep.DeviceID, resp.StatusCode)
			continue
		}
		switch data := result.Data.(type) {
		case []interface{}:
			for _, item := range data {
				if s, ok := item.(string); ok {
					seeds = append(seeds, s)
				}
			}
		case string:
			seeds = append(seeds, data)
		}
	}

	if len(seeds) == 0 {
		return fmt.Errorf("no TRNG data received for seeding")
	}

	// Validate TRNG data quality before sending to VirtIO
	if err := ValidateTRNGData(seeds); err != nil {
		return fmt.Errorf("TRNG data validation failed: %w", err)
	}

	// Send seeds to VirtIO's /seed endpoint
	seedRequest := struct {
		Seeds []string `json:"seeds"`
	}{
		Seeds: seeds,
	}

	seedData, err := json.Marshal(seedRequest)
	if err != nil {
		return fmt.Errorf("error marshaling seed request: %w", err)
	}

	seedResp, err := s.httpClient.Post(
		s.VirtioAddr+"/seed",
		"application/json",
		bytes.NewBuffer(seedData),
	)
	if err != nil {
		return fmt.Errorf("error seeding VirtIO: %w", err)
	}
	defer func() {
		if closeErr := seedResp.Body.Close(); closeErr != nil {
			log.Printf("Error closing seed response body: %v", closeErr)
		}
	}()

	if seedResp.StatusCode != http.StatusOK {
		return fmt.Errorf("virtio seeding failed with status: %d", seedResp.StatusCode)
	}

	for _, seedHex := range seeds {
		if b, err := hex.DecodeString(seedHex); err == nil {
			s.virtioQualityTracker.ProcessData(b)
		}
	}
	s.publishQuality("virtio", "virtio", s.virtioQualityTracker)

	log.Printf("Successfully seeded VirtIO with %d TRNG samples from %d controllers", len(seeds), len(s.controllersOrDefault()))
	return nil
}

// SeedVirtIOWithFortuna seeds VirtIO with Fortuna data
func (s *Server) SeedVirtIOWithFortuna(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting VirtIO seeding with Fortuna data every %s", interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("VirtIO seeding with Fortuna stopped")
			return
		case <-ticker.C:
			// Check circuit breaker
			if s.isVirtIOSeedingCircuitOpen() {
				log.Printf("VirtIO seeding circuit breaker is OPEN, skipping attempt")
				continue
			}

			if err := s.SeedVirtIOFromFortuna(); err != nil {
				s.virtioSeedingFailures.Add(1)
				s.lastVirtIOSeedingFailure.Store(time.Now().Unix())
				log.Printf("VirtIO seeding error: %v (failures: %d)", err, s.virtioSeedingFailures.Load())

				// Open circuit breaker after 5 consecutive failures
				if s.virtioSeedingFailures.Load() >= 5 {
					s.virtioSeedingCircuitOpen.Store(true)
					log.Printf("VirtIO seeding circuit breaker opened after %d failures", s.virtioSeedingFailures.Load())
				}
			} else {
				// Reset failure count on success
				s.virtioSeedingFailures.Store(0)
			}
		}
	}
}

// SeedVirtIOFromFortuna fetches Fortuna data and sends it to VirtIO's /seed endpoint
func (s *Server) SeedVirtIOFromFortuna() error {
	// Fetch Fortuna data from database (unconsumed items, 5 samples)
	fortunaData, err := s.DB.GetFortunaData(5, 0, false)
	if err != nil {
		return fmt.Errorf("error fetching Fortuna data for seeding: %w", err)
	}

	if len(fortunaData) == 0 {
		return fmt.Errorf("no Fortuna data available for seeding")
	}

	// Convert to hex strings for seeding
	seeds := make([]string, 0, len(fortunaData))
	for _, data := range fortunaData {
		seeds = append(seeds, hex.EncodeToString(data))
	}

	// Send seeds to VirtIO's /seed endpoint
	seedRequest := struct {
		Seeds []string `json:"seeds"`
	}{
		Seeds: seeds,
	}

	seedData, err := json.Marshal(seedRequest)
	if err != nil {
		return fmt.Errorf("error marshaling seed request: %w", err)
	}

	seedResp, err := s.httpClient.Post(
		s.VirtioAddr+"/seed",
		"application/json",
		bytes.NewBuffer(seedData),
	)
	if err != nil {
		return fmt.Errorf("error seeding VirtIO: %w", err)
	}
	defer func() {
		if closeErr := seedResp.Body.Close(); closeErr != nil {
			log.Printf("Error closing seed response body: %v", closeErr)
		}
	}()

	if seedResp.StatusCode != http.StatusOK {
		return fmt.Errorf("virtio seeding failed with status: %d", seedResp.StatusCode)
	}

	log.Printf("Successfully seeded VirtIO with %d Fortuna samples", len(seeds))
	return nil
}

// getVirtIOSeedingSource safely reads the current VirtIO seeding source
func (s *Server) getVirtIOSeedingSource() string {
	s.virtioConfigMutex.RLock()
	defer s.virtioConfigMutex.RUnlock()
	return s.virtioSeedingSource
}
