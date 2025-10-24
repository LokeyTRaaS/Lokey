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
)

// StartPolling initiates background polling of external services for random data
func (s *Server) StartPolling(ctx context.Context, trngPollInterval, fortunaPollInterval time.Duration) {
	// Start TRNG polling
	go s.pollTRNGService(ctx, trngPollInterval)

	// Start Fortuna polling
	go s.pollFortunaService(ctx, fortunaPollInterval)

	// Start Fortuna seeding with TRNG data
	go s.seedFortunaWithTRNG(ctx, 30*time.Second)

	log.Printf("Started polling services - TRNG interval: %s, Fortuna interval: %s, Fortuna seeding: every 30s",
		trngPollInterval, fortunaPollInterval)
}

// pollTRNGService periodically polls the hardware TRNG controller service for new random data
func (s *Server) pollTRNGService(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting TRNG polling from %s with interval %s", s.controllerAddr, interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("TRNG polling stopped")
			return
		case <-ticker.C:
			if err := s.fetchAndStoreTRNGData(); err != nil {
				log.Printf("TRNG polling error: %v", err)
			}
		}
	}
}

// fetchAndStoreTRNGData fetches and stores TRNG data from the controller
func (s *Server) fetchAndStoreTRNGData() error {
	// Attempt to fetch data from the controller service
	resp, err := http.Get(s.controllerAddr + "/generate")
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

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("error parsing TRNG response: %w", err)
	}

	// Decode the hex-encoded data
	dataBytes, err := hex.DecodeString(result.Data)
	if err != nil {
		return fmt.Errorf("error decoding data from controller: %w", err)
	}

	// Store the data in database
	if err := s.db.StoreTRNGData(dataBytes); err != nil {
		return fmt.Errorf("error storing TRNG data: %w", err)
	}

	// Increment polling count only on successful storage
	if err := s.db.IncrementPollingCount("trng"); err != nil {
		log.Printf("Warning: failed to increment TRNG polling count: %v", err)
	}

	return nil
}

// pollFortunaService periodically polls the Fortuna PRNG service for new random data
func (s *Server) pollFortunaService(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting Fortuna polling from %s with interval %s", s.fortunaAddr, interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Fortuna polling stopped")
			return
		case <-ticker.C:
			if err := s.fetchAndStoreFortunaData(); err != nil {
				log.Printf("Fortuna polling error: %v", err)
			}
		}
	}
}

// fetchAndStoreFortunaData fetches and stores Fortuna data from the service
func (s *Server) fetchAndStoreFortunaData() error {
	// Attempt to fetch data from the Fortuna service using the correct endpoint
	// Specify size=256 for a reasonable amount of random data
	resp, err := http.Get(s.fortunaAddr + "/generate?size=256")
	if err != nil {
		return fmt.Errorf("error connecting to Fortuna service: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Error closing Fortuna response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Fortuna service returned status %d", resp.StatusCode)
	}

	// Parse response which contains a data field with hex-encoded random data
	var result struct {
		Data string `json:"data"`
		Size int    `json:"size"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("error parsing Fortuna response: %w", err)
	}

	// Decode the hex-encoded data
	randomData, err := hex.DecodeString(result.Data)
	if err != nil {
		return fmt.Errorf("error decoding data from Fortuna: %w", err)
	}

	// Store the data in database
	if err := s.db.StoreFortunaData(randomData); err != nil {
		return fmt.Errorf("error storing Fortuna data: %w", err)
	}

	// Increment polling count only on successful storage
	if err := s.db.IncrementPollingCount("fortuna"); err != nil {
		log.Printf("Warning: failed to increment Fortuna polling count: %v", err)
	}

	return nil
}

// seedFortunaWithTRNG periodically seeds Fortuna generator with hardware TRNG data
func (s *Server) seedFortunaWithTRNG(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting Fortuna seeding with TRNG data every %s", interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Fortuna seeding stopped")
			return
		case <-ticker.C:
			if err := s.seedFortuna(); err != nil {
				log.Printf("Fortuna seeding error: %v", err)
			}
		}
	}
}

// seedFortuna fetches TRNG data and seeds the Fortuna generator
func (s *Server) seedFortuna() error {
	// Fetch multiple TRNG samples from controller for good entropy
	resp, err := http.Get(s.controllerAddr + "/generate?count=5")
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

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("error parsing controller response for seeding: %w", err)
	}

	if len(result.Data) == 0 {
		return fmt.Errorf("no TRNG data received for seeding")
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
		s.fortunaAddr+"/seed",
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
		return fmt.Errorf("Fortuna seeding failed with status: %d", seedResp.StatusCode)
	}

	log.Printf("Successfully seeded Fortuna with %d TRNG samples", len(result.Data))
	return nil
}
