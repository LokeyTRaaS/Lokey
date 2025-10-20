package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
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
			// Attempt to fetch data from the controller service
			resp, err := http.Get(s.controllerAddr + "/generate")
			if err != nil {
				log.Printf("Error connecting to TRNG controller: %v", err)
				continue
			}

			if resp.StatusCode != http.StatusOK {
				log.Printf("TRNG controller returned non-OK status: %d", resp.StatusCode)
				resp.Body.Close()
				continue
			}

			// Read and parse response body which contains a data field
			var result struct {
				Data string `json:"data"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				log.Printf("Error parsing TRNG response: %v", err)
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			// Decode the hex-encoded data
			dataBytes, err := hex.DecodeString(result.Data)
			if err != nil {
				log.Printf("Error decoding data from controller: %v", err)
				continue
			}

			// Store the data in database
			if err := s.db.StoreTRNGData(dataBytes); err != nil {
				log.Printf("Error storing TRNG data: %v", err)
			} else {
				// Increment polling count only on successful storage
				s.db.IncrementPollingCount("trng")
			}
		}
	}
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
			// Attempt to fetch data from the Fortuna service using the correct endpoint
			// Specify size=256 for a reasonable amount of random data
			resp, err := http.Get(s.fortunaAddr + "/generate?size=256")
			if err != nil {
				log.Printf("Error connecting to Fortuna service: %v", err)
				continue
			}

			if resp.StatusCode != http.StatusOK {
				log.Printf("Fortuna service returned non-OK status: %d", resp.StatusCode)
				resp.Body.Close()
				continue
			}

			// Parse response which contains a data field with hex-encoded random data
			var result struct {
				Data string `json:"data"`
				Size int    `json:"size"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				log.Printf("Error parsing Fortuna response: %v", err)
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			// Decode the hex-encoded data
			randomData, err := hex.DecodeString(result.Data)
			if err != nil {
				log.Printf("Error decoding data from Fortuna: %v", err)
				continue
			}

			// Store the data in database
			if err := s.db.StoreFortunaData(randomData); err != nil {
				log.Printf("Error storing Fortuna data: %v", err)
			} else {
				// Increment polling count only on successful storage
				s.db.IncrementPollingCount("fortuna")
			}
		}
	}
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
			// Fetch multiple TRNG samples from controller for good entropy
			resp, err := http.Get(s.controllerAddr + "/generate?count=5")
			if err != nil {
				log.Printf("Error fetching TRNG data for Fortuna seeding: %v", err)
				continue
			}

			if resp.StatusCode != http.StatusOK {
				log.Printf("Controller returned non-OK status for seeding: %d", resp.StatusCode)
				resp.Body.Close()
				continue
			}

			// Parse controller response (returns array when count > 1)
			var result struct {
				Data []string `json:"data"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				log.Printf("Error parsing controller response for seeding: %v", err)
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			if len(result.Data) == 0 {
				log.Printf("No TRNG data received for seeding")
				continue
			}

			// Send seeds to Fortuna's /seed endpoint
			seedRequest := struct {
				Seeds []string `json:"seeds"`
			}{
				Seeds: result.Data,
			}

			seedData, err := json.Marshal(seedRequest)
			if err != nil {
				log.Printf("Error marshaling seed request: %v", err)
				continue
			}

			seedResp, err := http.Post(
				s.fortunaAddr+"/seed",
				"application/json",
				bytes.NewBuffer(seedData),
			)
			if err != nil {
				log.Printf("Error seeding Fortuna: %v", err)
				continue
			}

			if seedResp.StatusCode != http.StatusOK {
				log.Printf("Fortuna seeding failed with status: %d", seedResp.StatusCode)
			} else {
				log.Printf("Successfully seeded Fortuna with %d TRNG samples", len(result.Data))
			}
			seedResp.Body.Close()
		}
	}
}
