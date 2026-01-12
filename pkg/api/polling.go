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

// fetchAndStoreTRNGData fetches and stores TRNG data from the controller
func (s *Server) FetchAndStoreTRNGData() error {
	// Attempt to fetch data from the controller service
	resp, err := http.Get(s.ControllerAddr + "/generate")
	if err != nil {
		return fmt.Errorf("error connecting to TRNG controller: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing TRNG response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TRNG controller returned status %d", resp.StatusCode)
	}

	// Read and parse response body which contains a data field
	var result struct {
		Data string `json:"data"`
	}

	if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
		return fmt.Errorf("error parsing TRNG response: %w", decodeErr)
	}

	// Decode the hex-encoded data
	dataBytes, err := hex.DecodeString(result.Data)
	if err != nil {
		return fmt.Errorf("error decoding data from controller: %w", err)
	}

	// Calculate quality metrics BEFORE storing
	s.trngQualityTracker.ProcessData(dataBytes)

	// Store the data in database
	if err := s.DB.StoreTRNGData(dataBytes); err != nil {
		return fmt.Errorf("error storing TRNG data: %w", err)
	}

	// Increment polling count only on successful storage
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
	resp, err := http.Get(s.FortunaAddr + "/generate?size=256")
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

// seedFortuna fetches TRNG data and seeds the Fortuna generator
func (s *Server) SeedFortuna() error {
	// Fetch multiple TRNG samples from controller for good entropy
	resp, err := http.Get(s.ControllerAddr + "/generate?count=5")
	if err != nil {
		return fmt.Errorf("error fetching TRNG data for seeding: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing controller response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller returned status %d for seeding", resp.StatusCode)
	}

	// Parse controller response (returns array when count > 1)
	var result struct {
		Data []string `json:"data"`
	}

	if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
		return fmt.Errorf("error parsing controller response for seeding: %w", decodeErr)
	}

	if len(result.Data) == 0 {
		return fmt.Errorf("no TRNG data received for seeding")
	}

	// NEW: Validate TRNG data quality before sending to Fortuna
	if err := ValidateTRNGData(result.Data); err != nil {
		return fmt.Errorf("TRNG data validation failed: %w", err)
	}

	// Send seeds to Fortuna's /seed endpoint
	seedRequest := struct {
		Seeds []string `json:"seeds"`
	}{
		Seeds: result.Data,
	}

	seedData, err := json.Marshal(seedRequest)
	if err != nil {
		return fmt.Errorf("error marshaling seed request: %w", err)
	}

	seedResp, err := http.Post(
		s.FortunaAddr+"/seed",
		"application/json",
		bytes.NewBuffer(seedData),
	)
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

	log.Printf("Successfully seeded Fortuna with %d TRNG samples", len(result.Data))
	return nil
}

// SeedVirtIOWithTRNG seeds VirtIO with TRNG data
func (s *Server) SeedVirtIOWithTRNG(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting VirtIO seeding with TRNG data every %s", interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("VirtIO seeding with TRNG stopped")
			return
		case <-ticker.C:
			// Check circuit breaker
			if s.isVirtIOSeedingCircuitOpen() {
				log.Printf("VirtIO seeding circuit breaker is OPEN, skipping attempt")
				continue
			}

			if err := s.SeedVirtIOFromTRNG(); err != nil {
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

// SeedVirtIOFromTRNG fetches TRNG data and sends it to VirtIO's /seed endpoint
func (s *Server) SeedVirtIOFromTRNG() error {
	// Fetch TRNG data from controller (5 samples for seeding)
	resp, err := http.Get(s.ControllerAddr + "/generate?count=5")
	if err != nil {
		return fmt.Errorf("error fetching TRNG data for seeding: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing controller response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller returned status %d for seeding", resp.StatusCode)
	}

	// Parse controller response (returns array when count > 1)
	var result struct {
		Data []string `json:"data"`
	}

	if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
		return fmt.Errorf("error parsing controller response for seeding: %w", decodeErr)
	}

	if len(result.Data) == 0 {
		return fmt.Errorf("no TRNG data received for seeding")
	}

	// Validate TRNG data quality before sending to VirtIO
	if err := ValidateTRNGData(result.Data); err != nil {
		return fmt.Errorf("TRNG data validation failed: %w", err)
	}

	// Send seeds to VirtIO's /seed endpoint
	seedRequest := struct {
		Seeds []string `json:"seeds"`
	}{
		Seeds: result.Data,
	}

	seedData, err := json.Marshal(seedRequest)
	if err != nil {
		return fmt.Errorf("error marshaling seed request: %w", err)
	}

	seedResp, err := http.Post(
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

	log.Printf("Successfully seeded VirtIO with %d TRNG samples", len(result.Data))
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

	seedResp, err := http.Post(
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
