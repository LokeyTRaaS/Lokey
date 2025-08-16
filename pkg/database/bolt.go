package database

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	// Bucket names
	trngQueueBucket    = []byte("trng_queue")
	fortunaQueueBucket = []byte("fortuna_queue")
	trngDataBucket     = []byte("trng_data")
	fortunaDataBucket  = []byte("fortuna_data")
	usageStatsBucket   = []byte("usage_stats")
	countersBucket     = []byte("counters")
	configBucket       = []byte("config")
)

// BoltDBHandler implements the database interface using BoltDB
type BoltDBHandler struct {
	db               *bolt.DB
	trngQueueSize    int
	fortunaQueueSize int
	mu               sync.RWMutex // For safe concurrent access to queue sizes
}

// NewBoltDBHandler creates a new BoltDB handler
// NewBoltDBHandler creates a new BoltDB handler
func NewBoltDBHandler(dbPath string, trngQueueSize, fortunaQueueSize int) (*BoltDBHandler, error) {
	// Check if the path is a directory and append default filename if needed
	fileInfo, err := os.Stat(dbPath)
	if err == nil && fileInfo.IsDir() {
		// It's a directory, append a default database filename
		dbPath = filepath.Join(dbPath, "database.db")
		log.Printf("Path is a directory, using file: %s", dbPath)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open BoltDB with minimal settings suitable for Raspberry Pi
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{
		Timeout:      1 * time.Second,
		NoGrowSync:   false,
		FreelistType: bolt.FreelistMapType,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to open BoltDB: %w", err)
	}

	// Initialize buckets
	err = db.Update(func(tx *bolt.Tx) error {
		buckets := [][]byte{
			trngQueueBucket,
			fortunaQueueBucket,
			trngDataBucket,
			fortunaDataBucket,
			usageStatsBucket,
			countersBucket,
			configBucket,
		}
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return fmt.Errorf("create bucket %s: %w", bucket, err)
			}
		}

		// Initialize counters if they don't exist
		counters := []struct {
			key   []byte
			value uint64
		}{
			{[]byte("trng_head"), 0},
			{[]byte("trng_tail"), 0},
			{[]byte("fortuna_head"), 0},
			{[]byte("fortuna_tail"), 0},
			{[]byte("trng_next_id"), 0},
			{[]byte("fortuna_next_id"), 0},
		}

		b := tx.Bucket(countersBucket)
		for _, counter := range counters {
			if b.Get(counter.key) == nil {
				var buf [8]byte
				binary.BigEndian.PutUint64(buf[:], counter.value)
				if err := b.Put(counter.key, buf[:]); err != nil {
					return fmt.Errorf("initialize counter %s: %w", counter.key, err)
				}
			}
		}

		// Store queue sizes in config
		configB := tx.Bucket(configBucket)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(trngQueueSize))
		if err := configB.Put([]byte("trng_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store trng queue size: %w", err)
		}

		binary.BigEndian.PutUint64(buf[:], uint64(fortunaQueueSize))
		if err := configB.Put([]byte("fortuna_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store fortuna queue size: %w", err)
		}

		return nil
	})

	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return &BoltDBHandler{
		db:               db,
		trngQueueSize:    trngQueueSize,
		fortunaQueueSize: fortunaQueueSize,
	}, nil
}

// Close closes the database connection
func (h *BoltDBHandler) Close() error {
	return h.db.Close()
}

// Helper functions for counters

// getCounter retrieves a counter value from the database
func (h *BoltDBHandler) getCounter(tx *bolt.Tx, key []byte) (uint64, error) {
	b := tx.Bucket(countersBucket)
	value := b.Get(key)
	if value == nil {
		return 0, fmt.Errorf("counter %s not found", key)
	}
	return binary.BigEndian.Uint64(value), nil
}

// setCounter updates a counter value in the database
func (h *BoltDBHandler) setCounter(tx *bolt.Tx, key []byte, value uint64) error {
	b := tx.Bucket(countersBucket)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], value)
	return b.Put(key, buf[:])
}

// getNextID gets the next available ID for a sequence and increments it
func (h *BoltDBHandler) getNextID(tx *bolt.Tx, counterKey []byte) (uint64, error) {
	id, err := h.getCounter(tx, counterKey)
	if err != nil {
		return 0, err
	}

	err = h.setCounter(tx, counterKey, id+1)
	if err != nil {
		return 0, err
	}

	return id, nil
}

//---------------------- TRNG Request Queue Operations ----------------------

// EnqueueTRNGRequest adds a new TRNG request to the queue
func (h *BoltDBHandler) EnqueueTRNGRequest(request Request) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		// Get queue counters
		headCounter, err := h.getCounter(tx, []byte("trng_head"))
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(tx, []byte("trng_tail"))
		if err != nil {
			return err
		}

		// Check queue size
		h.mu.RLock()
		queueSize := int(headCounter - tailCounter)
		maxSize := h.trngQueueSize
		h.mu.RUnlock()

		if queueSize >= maxSize {
			return fmt.Errorf("TRNG queue is full")
		}

		// Set request fields
		request.Status = "pending"
		request.CreatedAt = time.Now()
		request.UpdatedAt = request.CreatedAt
		request.Source = "trng"

		// Serialize request to JSON
		data, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("serialize request: %w", err)
		}

		// Store in queue
		b := tx.Bucket(trngQueueBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, headCounter)
		if err := b.Put(key, data); err != nil {
			return fmt.Errorf("store request: %w", err)
		}

		// Update head counter
		return h.setCounter(tx, []byte("trng_head"), headCounter+1)
	})
}

// DequeueTRNGRequest retrieves and removes the next TRNG request from the queue
func (h *BoltDBHandler) DequeueTRNGRequest() (Request, error) {
	var request Request

	err := h.db.Update(func(tx *bolt.Tx) error {
		// Get queue counters
		headCounter, err := h.getCounter(tx, []byte("trng_head"))
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(tx, []byte("trng_tail"))
		if err != nil {
			return err
		}

		// Check if queue is empty
		if headCounter == tailCounter {
			return fmt.Errorf("TRNG queue is empty")
		}

		// Get request from tail
		b := tx.Bucket(trngQueueBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, tailCounter)

		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("request not found")
		}

		// Deserialize request
		if err := json.Unmarshal(data, &request); err != nil {
			return fmt.Errorf("deserialize request: %w", err)
		}

		// Delete request from queue
		if err := b.Delete(key); err != nil {
			return fmt.Errorf("delete request: %w", err)
		}

		// Update tail counter
		return h.setCounter(tx, []byte("trng_tail"), tailCounter+1)
	})

	return request, err
}

//---------------------- Fortuna Request Queue Operations ----------------------

// EnqueueFortunaRequest adds a new Fortuna request to the queue
func (h *BoltDBHandler) EnqueueFortunaRequest(request Request) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		// Get queue counters
		headCounter, err := h.getCounter(tx, []byte("fortuna_head"))
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(tx, []byte("fortuna_tail"))
		if err != nil {
			return err
		}

		// Check queue size
		h.mu.RLock()
		queueSize := int(headCounter - tailCounter)
		maxSize := h.fortunaQueueSize
		h.mu.RUnlock()

		if queueSize >= maxSize {
			return fmt.Errorf("Fortuna queue is full")
		}

		// Set request fields
		request.Status = "pending"
		request.CreatedAt = time.Now()
		request.UpdatedAt = request.CreatedAt
		request.Source = "fortuna"

		// Serialize request to JSON
		data, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("serialize request: %w", err)
		}

		// Store in queue
		b := tx.Bucket(fortunaQueueBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, headCounter)
		if err := b.Put(key, data); err != nil {
			return fmt.Errorf("store request: %w", err)
		}

		// Update head counter
		return h.setCounter(tx, []byte("fortuna_head"), headCounter+1)
	})
}

// DequeueFortunaRequest retrieves and removes the next Fortuna request from the queue
func (h *BoltDBHandler) DequeueFortunaRequest() (Request, error) {
	var request Request

	err := h.db.Update(func(tx *bolt.Tx) error {
		// Get queue counters
		headCounter, err := h.getCounter(tx, []byte("fortuna_head"))
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(tx, []byte("fortuna_tail"))
		if err != nil {
			return err
		}

		// Check if queue is empty
		if headCounter == tailCounter {
			return fmt.Errorf("Fortuna queue is empty")
		}

		// Get request from tail
		b := tx.Bucket(fortunaQueueBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, tailCounter)

		data := b.Get(key)
		if data == nil {
			return fmt.Errorf("request not found")
		}

		// Deserialize request
		if err := json.Unmarshal(data, &request); err != nil {
			return fmt.Errorf("deserialize request: %w", err)
		}

		// Delete request from queue
		if err := b.Delete(key); err != nil {
			return fmt.Errorf("delete request: %w", err)
		}

		// Update tail counter
		return h.setCounter(tx, []byte("fortuna_tail"), tailCounter+1)
	})

	return request, err
}

//---------------------- TRNG Data Operations ----------------------

// StoreTRNGHash stores a new TRNG hash
func (h *BoltDBHandler) StoreTRNGHash(hash []byte) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		// Get next ID
		id, err := h.getNextID(tx, []byte("trng_next_id"))
		if err != nil {
			return err
		}

		// Create TRNG data object
		data := TRNGData{
			ID:        id,
			Hash:      hash,
			Timestamp: time.Now(),
			Consumed:  false,
		}

		// Serialize to JSON
		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("serialize data: %w", err)
		}

		// Store data
		b := tx.Bucket(trngDataBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)
		if err := b.Put(key, jsonData); err != nil {
			return fmt.Errorf("store hash: %w", err)
		}

		// Maintain queue size
		return h.trimTRNGDataIfNeeded(tx)
	})
}

// trimTRNGDataIfNeeded maintains the TRNG data queue size
func (h *BoltDBHandler) trimTRNGDataIfNeeded(tx *bolt.Tx) error {
	b := tx.Bucket(trngDataBucket)

	// Count total items and collect oldest keys
	var count int
	var oldestKeys [][]byte

	h.mu.RLock()
	maxSize := h.trngQueueSize
	h.mu.RUnlock()

	cursor := b.Cursor()
	for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
		count++
		if len(oldestKeys) < count-maxSize {
			oldestKeys = append(oldestKeys, append([]byte{}, k...))
		}
	}

	// Delete oldest entries if we exceed the limit
	if count > maxSize {
		for _, key := range oldestKeys {
			if err := b.Delete(key); err != nil {
				return fmt.Errorf("delete oldest entry: %w", err)
			}
		}
	}

	return nil
}

// GetTRNGHashes retrieves TRNG hashes with pagination and optional consumption
func (h *BoltDBHandler) GetTRNGHashes(limit, offset int, consume bool) ([][]byte, error) {
	var hashes [][]byte

	err := h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(trngDataBucket)

		// Collect all unconsumed items
		var allKeys [][]byte
		var allItems []TRNGData

		cursor := b.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var data TRNGData
			if err := json.Unmarshal(v, &data); err != nil {
				return fmt.Errorf("deserialize data: %w", err)
			}

			if !data.Consumed {
				allKeys = append(allKeys, append([]byte{}, k...))
				allItems = append(allItems, data)
			}
		}

		// Apply pagination
		start := offset
		end := offset + limit
		if start >= len(allItems) {
			return nil // No data available at this offset
		}
		if end > len(allItems) {
			end = len(allItems)
		}

		// Extract hashes and mark as consumed if requested
		for i := start; i < end; i++ {
			hashes = append(hashes, allItems[i].Hash)

			if consume {
				allItems[i].Consumed = true
				jsonData, err := json.Marshal(allItems[i])
				if err != nil {
					return fmt.Errorf("serialize data: %w", err)
				}

				if err := b.Put(allKeys[i], jsonData); err != nil {
					return fmt.Errorf("update consumed status: %w", err)
				}
			}
		}

		return nil
	})

	return hashes, err
}

//---------------------- Fortuna Data Operations ----------------------

// StoreFortunaData stores Fortuna-generated data
func (h *BoltDBHandler) StoreFortunaData(data []byte) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		// Get next ID
		id, err := h.getNextID(tx, []byte("fortuna_next_id"))
		if err != nil {
			return err
		}

		// Create Fortuna data object
		fortunaData := FortunaData{
			ID:        id,
			Data:      data,
			Timestamp: time.Now(),
			Consumed:  false,
		}

		// Serialize to JSON
		jsonData, err := json.Marshal(fortunaData)
		if err != nil {
			return fmt.Errorf("serialize data: %w", err)
		}

		// Store data
		b := tx.Bucket(fortunaDataBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)
		if err := b.Put(key, jsonData); err != nil {
			return fmt.Errorf("store data: %w", err)
		}

		// Maintain queue size
		return h.trimFortunaDataIfNeeded(tx)
	})
}

// trimFortunaDataIfNeeded maintains the Fortuna data queue size
func (h *BoltDBHandler) trimFortunaDataIfNeeded(tx *bolt.Tx) error {
	b := tx.Bucket(fortunaDataBucket)

	// Count total items and collect oldest keys
	var count int
	var oldestKeys [][]byte

	h.mu.RLock()
	maxSize := h.fortunaQueueSize
	h.mu.RUnlock()

	cursor := b.Cursor()
	for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
		count++
		if len(oldestKeys) < count-maxSize {
			oldestKeys = append(oldestKeys, append([]byte{}, k...))
		}
	}

	// Delete oldest entries if we exceed the limit
	if count > maxSize {
		for _, key := range oldestKeys {
			if err := b.Delete(key); err != nil {
				return fmt.Errorf("delete oldest entry: %w", err)
			}
		}
	}

	return nil
}

// GetFortunaData retrieves Fortuna-generated data with pagination and optional consumption
func (h *BoltDBHandler) GetFortunaData(limit, offset int, consume bool) ([][]byte, error) {
	var dataSlices [][]byte

	err := h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(fortunaDataBucket)

		// Collect all unconsumed items
		var allKeys [][]byte
		var allItems []FortunaData

		cursor := b.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var data FortunaData
			if err := json.Unmarshal(v, &data); err != nil {
				return fmt.Errorf("deserialize data: %w", err)
			}

			if !data.Consumed {
				allKeys = append(allKeys, append([]byte{}, k...))
				allItems = append(allItems, data)
			}
		}

		// Apply pagination
		start := offset
		end := offset + limit
		if start >= len(allItems) {
			return nil // No data available at this offset
		}
		if end > len(allItems) {
			end = len(allItems)
		}

		// Extract data and mark as consumed if requested
		for i := start; i < end; i++ {
			dataSlices = append(dataSlices, allItems[i].Data)

			if consume {
				allItems[i].Consumed = true
				jsonData, err := json.Marshal(allItems[i])
				if err != nil {
					return fmt.Errorf("serialize data: %w", err)
				}

				if err := b.Put(allKeys[i], jsonData); err != nil {
					return fmt.Errorf("update consumed status: %w", err)
				}
			}
		}

		return nil
	})

	return dataSlices, err
}

//---------------------- Statistics Operations ----------------------

// RecordRNGUsage records RNG usage statistics
func (h *BoltDBHandler) RecordRNGUsage(source string, bytesUsed int64) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageStatsBucket)

		// Create hourly timestamp bucket key
		now := time.Now()
		timestamp := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
		bucketKey := fmt.Sprintf("%s:%d", source, timestamp.Unix())

		// Check for existing stats
		var stat UsageStat
		data := b.Get([]byte(bucketKey))

		if data != nil {
			// Update existing stat
			if err := json.Unmarshal(data, &stat); err != nil {
				return fmt.Errorf("deserialize usage stat: %w", err)
			}

			stat.BytesUsed += bytesUsed
			stat.Requests++
		} else {
			// Create new stat
			stat = UsageStat{
				Source:    source,
				BytesUsed: bytesUsed,
				Requests:  1,
				Timestamp: timestamp,
			}
		}

		// Serialize and store
		jsonData, err := json.Marshal(stat)
		if err != nil {
			return fmt.Errorf("serialize usage stat: %w", err)
		}

		return b.Put([]byte(bucketKey), jsonData)
	})
}

// GetRNGStatistics retrieves RNG usage statistics for a specific time range
func (h *BoltDBHandler) GetRNGStatistics(source string, start, end time.Time) ([]UsageStat, error) {
	var stats []UsageStat

	err := h.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageStatsBucket)

		// Source prefix for keys
		prefix := []byte(source + ":")

		cursor := b.Cursor()
		for k, v := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = cursor.Next() {
			var stat UsageStat
			if err := json.Unmarshal(v, &stat); err != nil {
				return fmt.Errorf("deserialize usage stat: %w", err)
			}

			// Filter by time range
			if (stat.Timestamp.Equal(start) || stat.Timestamp.After(start)) &&
				(stat.Timestamp.Equal(end) || stat.Timestamp.Before(end)) {
				stats = append(stats, stat)
			}
		}

		return nil
	})

	return stats, err
}

//---------------------- Utility Operations ----------------------

// GetQueueInfo returns information about the current queue status
func (h *BoltDBHandler) GetQueueInfo() (map[string]int, error) {
	queueInfo := make(map[string]int)

	err := h.db.View(func(tx *bolt.Tx) error {
		// Get queue counters
		trngHead, err := h.getCounter(tx, []byte("trng_head"))
		if err != nil {
			return err
		}

		trngTail, err := h.getCounter(tx, []byte("trng_tail"))
		if err != nil {
			return err
		}

		fortunaHead, err := h.getCounter(tx, []byte("fortuna_head"))
		if err != nil {
			return err
		}

		fortunaTail, err := h.getCounter(tx, []byte("fortuna_tail"))
		if err != nil {
			return err
		}

		// Count TRNG data
		trngTotal := 0
		trngUnconsumed := 0

		trngBucket := tx.Bucket(trngDataBucket)
		cursor := trngBucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			trngTotal++

			var data TRNGData
			if err := json.Unmarshal(v, &data); err != nil {
				return fmt.Errorf("deserialize TRNG data: %w", err)
			}

			if !data.Consumed {
				trngUnconsumed++
			}
		}

		// Count Fortuna data
		fortunaTotal := 0
		fortunaUnconsumed := 0

		fortunaBucket := tx.Bucket(fortunaDataBucket)
		cursor = fortunaBucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			fortunaTotal++

			var data FortunaData
			if err := json.Unmarshal(v, &data); err != nil {
				return fmt.Errorf("deserialize Fortuna data: %w", err)
			}

			if !data.Consumed {
				fortunaUnconsumed++
			}
		}

		// Read current queue capacities
		h.mu.RLock()
		trngCapacity := h.trngQueueSize
		fortunaCapacity := h.fortunaQueueSize
		h.mu.RUnlock()

		// Populate result
		queueInfo["trng_queue_size"] = int(trngHead - trngTail)
		queueInfo["trng_queue_capacity"] = trngCapacity
		queueInfo["fortuna_queue_size"] = int(fortunaHead - fortunaTail)
		queueInfo["fortuna_queue_capacity"] = fortunaCapacity
		queueInfo["trng_data_count"] = trngTotal
		queueInfo["trng_unconsumed_count"] = trngUnconsumed
		queueInfo["fortuna_data_count"] = fortunaTotal
		queueInfo["fortuna_unconsumed_count"] = fortunaUnconsumed

		return nil
	})

	return queueInfo, err
}

// GetStats returns statistics about the database
func (h *BoltDBHandler) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get queue info
	queueInfo, err := h.GetQueueInfo()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	trngQueueSize := h.trngQueueSize
	fortunaQueueSize := h.fortunaQueueSize
	h.mu.RUnlock()

	// Format stats
	stats["trng_total"] = queueInfo["trng_data_count"]
	stats["trng_unconsumed"] = queueInfo["trng_unconsumed_count"]
	stats["trng_queue_full"] = queueInfo["trng_data_count"] >= trngQueueSize
	stats["fortuna_total"] = queueInfo["fortuna_data_count"]
	stats["fortuna_unconsumed"] = queueInfo["fortuna_unconsumed_count"]
	stats["fortuna_queue_full"] = queueInfo["fortuna_data_count"] >= fortunaQueueSize

	return stats, nil
}

// UpdateQueueSizes updates the queue size configuration
func (h *BoltDBHandler) UpdateQueueSizes(trngSize, fortunaSize int) error {
	h.mu.Lock()
	h.trngQueueSize = trngSize
	h.fortunaQueueSize = fortunaSize
	h.mu.Unlock()

	// Also persist the settings
	return h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(configBucket)

		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(trngSize))
		if err := b.Put([]byte("trng_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store trng queue size: %w", err)
		}

		binary.BigEndian.PutUint64(buf[:], uint64(fortunaSize))
		if err := b.Put([]byte("fortuna_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store fortuna queue size: %w", err)
		}

		return nil
	})
}

// HealthCheck checks if the database is accessible
func (h *BoltDBHandler) HealthCheck() bool {
	err := h.db.View(func(tx *bolt.Tx) error {
		// Try to access counters bucket as a basic health check
		b := tx.Bucket(countersBucket)
		if b == nil {
			return fmt.Errorf("counters bucket missing")
		}
		return nil
	})

	if err != nil {
		log.Printf("Database health check failed: %v", err)
		return false
	}

	return true
}
