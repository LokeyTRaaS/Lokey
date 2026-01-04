// Package fortuna implements the Fortuna cryptographically secure random number generator.
package fortuna

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

const (
	// MinimumSeedLength is the minimum required seed length in bytes
	MinimumSeedLength = 32
	// MaxPoolSize is the maximum size of an entropy pool
	MaxPoolSize = 1024
	// NumberOfPools is the number of entropy pools
	NumberOfPools = 32
)

// Generator implements the Fortuna algorithm for random number generation
type Generator struct {
	key        []byte
	Counter    uint64
	BlockSize  int
	cipher     cipher.Block
	mutex      sync.Mutex
	lastReseed time.Time
	pools      [NumberOfPools][]byte
	IsHealthy  bool
}

// NewGenerator creates a new Fortuna generator
func NewGenerator(seed []byte) (*Generator, error) {
	if len(seed) < MinimumSeedLength {
		return nil, fmt.Errorf("seed must be at least %d bytes long, got %d", MinimumSeedLength, len(seed))
	}

	// Initialize with zero key
	initKey := make([]byte, MinimumSeedLength)
	copy(initKey, seed)

	// Create AES cipher
	aesCipher, err := aes.NewCipher(initKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	g := &Generator{
		key:        initKey,
		Counter:    0,
		BlockSize:   aesCipher.BlockSize(),
		cipher:     aesCipher,
		mutex:      sync.Mutex{},
		lastReseed: time.Now(),
		IsHealthy:   true,
	}

	// Initialize pools
	for i := 0; i < NumberOfPools; i++ {
		g.pools[i] = make([]byte, 0, 64)
	}

	return g, nil
}

// AddRandomEvent adds random data to the entropy pools
func (g *Generator) AddRandomEvent(source byte, value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("cannot add empty random event")
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()

	poolIndex := source % NumberOfPools

	// Add to the selected pool
	g.pools[poolIndex] = append(g.pools[poolIndex], value...)

	// Trim pool if it grows too large
	if len(g.pools[poolIndex]) > MaxPoolSize {
		g.pools[poolIndex] = g.pools[poolIndex][len(g.pools[poolIndex])-MaxPoolSize:]
	}

	return nil
}

// Reseed reseeds the generator using available entropy
func (g *Generator) Reseed(seeds [][]byte) error {
	if len(seeds) == 0 {
		return fmt.Errorf("cannot reseed with empty seed list")
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Create a hash of all seeds
	h := sha256.New()
	h.Write(g.key) // Include current key

	for _, seed := range seeds {
		if len(seed) > 0 {
			h.Write(seed)
		}
	}

	// Update the key
	newKey := h.Sum(nil)

	// Create new cipher with updated key
	aesCipher, err := aes.NewCipher(newKey)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	g.key = newKey
	g.cipher = aesCipher
	g.Counter++ // Increment counter on reseed
	g.lastReseed = time.Now()

	return nil
}

// GenerateRandomData generates random data of the specified length
func (g *Generator) GenerateRandomData(length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be positive, got %d", length)
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Check if we have a valid cipher
	if g.cipher == nil {
		return nil, fmt.Errorf("generator not properly initialized")
	}

	// Generate random data
	result := make([]byte, length)
	blocks := (length + g.BlockSize - 1) / g.BlockSize
	temp := make([]byte, g.BlockSize)

	for i := 0; i < blocks; i++ {
		// Convert counter to bytes
		ctrBytes := make([]byte, 16) // AES block size
		binary.BigEndian.PutUint64(ctrBytes[8:], g.Counter)

		// Encrypt counter to generate random block
		g.cipher.Encrypt(temp, ctrBytes)

		// Copy to result
		copySize := g.BlockSize
		if i == blocks-1 && length%g.BlockSize != 0 {
			copySize = length % g.BlockSize
		}

		copy(result[i*g.BlockSize:], temp[:copySize])

		// Increment counter
		g.Counter++
	}

	return result, nil
}

// ReseedFromPools reseeds using available entropy pools
func (g *Generator) ReseedFromPools() error {
	g.mutex.Lock()
	// Determine which pools to use based on the reseed count
	reseedCount := g.Counter
	var poolsToUse [][]byte

	for i := 0; i < NumberOfPools; i++ {
		// Use a pool if its corresponding bit in reseedCount is 1
		if (reseedCount & (1 << i)) != 0 {
			if len(g.pools[i]) > 0 {
				// Create a copy of the pool
				poolCopy := make([]byte, len(g.pools[i]))
				copy(poolCopy, g.pools[i])
				poolsToUse = append(poolsToUse, poolCopy)

				// Clear the used pool
				g.pools[i] = g.pools[i][:0]
			}
		}
	}
	g.mutex.Unlock()

	// Only reseed if we have entropy
	if len(poolsToUse) > 0 {
		return g.Reseed(poolsToUse)
	}

	return fmt.Errorf("no entropy available in pools for reseeding")
}

// AmplifyRandomData takes a seed and generates a larger random output
func (g *Generator) AmplifyRandomData(seed []byte, outputLength int) ([]byte, error) {
	if len(seed) == 0 {
		return nil, fmt.Errorf("seed cannot be empty")
	}

	if outputLength <= 0 {
		return nil, fmt.Errorf("output length must be positive, got %d", outputLength)
	}

	// Add the seed to pools
	seedLen := len(seed)
	for i := 0; i < seedLen; i += 32 {
		end := i + 32
		if end > seedLen {
			end = seedLen
		}
		if err := g.AddRandomEvent(byte(i%NumberOfPools), seed[i:end]); err != nil {
			return nil, fmt.Errorf("failed to add random event: %w", err)
		}
	}

	// Reseed from pools
	if err := g.ReseedFromPools(); err != nil {
		return nil, fmt.Errorf("failed to reseed from pools: %w", err)
	}

	// Generate the requested amount of random data
	return g.GenerateRandomData(outputLength)
}

// HealthCheck returns whether the generator is healthy
func (g *Generator) HealthCheck() bool {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Check time since last reseed
	timeSinceReseed := time.Since(g.lastReseed)
	if timeSinceReseed > 24*time.Hour {
		// Haven't been reseeded in a day, might be a problem
		return false
	}

	return g.IsHealthy
}

// GetLastReseedTime returns the time of the last reseed operation
func (g *Generator) GetLastReseedTime() time.Time {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return g.lastReseed
}

// GetCounter returns the current counter value
func (g *Generator) GetCounter() uint64 {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return g.Counter
}
