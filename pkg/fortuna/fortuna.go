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

// Generator implements the Fortuna algorithm for random number generation
type Generator struct {
	key        []byte
	counter    uint64
	blockSize  int
	cipher     cipher.Block
	mutex      sync.Mutex
	lastReseed time.Time
	pools      [32][]byte // 32 entropy pools
	isHealthy  bool
}

// NewGenerator creates a new Fortuna generator
func NewGenerator(seed []byte) (*Generator, error) {
	if len(seed) < 32 {
		return nil, fmt.Errorf("seed must be at least 32 bytes long")
	}

	// Initialize with zero key
	initKey := make([]byte, 32)
	copy(initKey, seed)

	// Create AES cipher
	cipher, err := aes.NewCipher(initKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	g := &Generator{
		key:        initKey,
		counter:    0,
		blockSize:  cipher.BlockSize(),
		cipher:     cipher,
		mutex:      sync.Mutex{},
		lastReseed: time.Now(),
		isHealthy:  true,
	}

	// Initialize pools
	for i := 0; i < 32; i++ {
		g.pools[i] = make([]byte, 0, 64)
	}

	return g, nil
}

// AddRandomEvent adds random data to the entropy pools
func (g *Generator) AddRandomEvent(source byte, value []byte) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	poolIndex := source % 32

	// Add to the selected pool
	g.pools[poolIndex] = append(g.pools[poolIndex], value...)

	// Trim pool if it grows too large
	if len(g.pools[poolIndex]) > 1024 {
		g.pools[poolIndex] = g.pools[poolIndex][len(g.pools[poolIndex])-1024:]
	}
}

// Reseed reseeds the generator using available entropy
func (g *Generator) Reseed(seeds [][]byte) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Create a hash of all seeds
	h := sha256.New()
	h.Write(g.key) // Include current key

	for _, seed := range seeds {
		h.Write(seed)
	}

	// Update the key
	newKey := h.Sum(nil)

	// Create new cipher with updated key
	cipher, err := aes.NewCipher(newKey)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	g.key = newKey
	g.cipher = cipher
	g.counter++ // Increment counter on reseed
	g.lastReseed = time.Now()

	return nil
}

// GenerateRandomData generates random data of the specified length
func (g *Generator) GenerateRandomData(length int) ([]byte, error) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Check if we have a valid cipher
	if g.cipher == nil {
		return nil, fmt.Errorf("generator not properly initialized")
	}

	// Generate random data
	result := make([]byte, length)
	blocks := (length + g.blockSize - 1) / g.blockSize
	temp := make([]byte, g.blockSize)

	for i := 0; i < blocks; i++ {
		// Convert counter to bytes
		ctrBytes := make([]byte, 16) // AES block size
		binary.BigEndian.PutUint64(ctrBytes[8:], g.counter)

		// Encrypt counter to generate random block
		g.cipher.Encrypt(temp, ctrBytes)

		// Copy to result
		copySize := g.blockSize
		if i == blocks-1 && length%g.blockSize != 0 {
			copySize = length % g.blockSize
		}

		copy(result[i*g.blockSize:], temp[:copySize])

		// Increment counter
		g.counter++
	}

	return result, nil
}

// ReseedFromPools reseeds using available entropy pools
func (g *Generator) ReseedFromPools() error {
	// Determine which pools to use based on the reseed count
	reseedCount := g.counter
	var poolsToUse [][]byte

	for i := 0; i < 32; i++ {
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

	// Only reseed if we have entropy
	if len(poolsToUse) > 0 {
		return g.Reseed(poolsToUse)
	}

	return nil
}

// AmplifyRandomData takes a seed and generates a larger random output
func (g *Generator) AmplifyRandomData(seed []byte, outputLength int) ([]byte, error) {
	// Add the seed to pools
	seedLen := len(seed)
	for i := 0; i < seedLen; i += 32 {
		end := i + 32
		if end > seedLen {
			end = seedLen
		}
		g.AddRandomEvent(byte(i%32), seed[i:end])
	}

	// Reseed from pools
	err := g.ReseedFromPools()
	if err != nil {
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

	return g.isHealthy
}
