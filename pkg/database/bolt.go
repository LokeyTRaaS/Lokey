package database

import (
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
	trngDataBucket    = []byte("trng_data")
	fortunaDataBucket = []byte("fortuna_data")
	usageStatsBucket  = []byte("usage_stats")
	countersBucket    = []byte("counters")
	configBucket      = []byte("config")
)

// BoltDBHandler implements the database interface using BoltDB
type BoltDBHandler struct {
	db               *bolt.DB
	trngQueueSize    int
	fortunaQueueSize int
	mu               sync.RWMutex // For safe concurrent access to queue sizes
}

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
			{[]byte("trng_next_id"), 0},
			{[]byte("fortuna_next_id"), 0},
			{[]byte("trng_polling_count"), 0},
			{[]byte("fortuna_polling_count"), 0},
			{[]byte("trng_dropped_count"), 0},
			{[]byte("fortuna_dropped_count"), 0},
			{[]byte("trng_consumed_count"), 0},
			{[]byte("fortuna_consumed_count"), 0},
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
		// Safe conversion - queue sizes are validated to be reasonable values
		binary.BigEndian.PutUint64(buf[:], uint64(trngQueueSize)) // #nosec G115
		if err := configB.Put([]byte("trng_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store trng queue size: %w", err)
		}

		binary.BigEndian.PutUint64(buf[:], uint64(fortunaQueueSize)) // #nosec G115
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
		return 0, nil // Return 0 instead of error for missing counters
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

//---------------------- TRNG Data Operations ----------------------

// StoreTRNGData stores raw TRNG data
func (h *BoltDBHandler) StoreTRNGData(data []byte) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		// Get next ID
		id, err := h.getNextID(tx, []byte("trng_next_id"))
		if err != nil {
			return err
		}

		// Create TRNG data object
		trngData := TRNGData{
			ID:        id,
			Data:      data,
			Timestamp: time.Now(),
			Consumed:  false,
		}

		// Serialize to JSON
		jsonData, err := json.Marshal(trngData)
		if err != nil {
			return fmt.Errorf("serialize data: %w", err)
		}

		// Store data
		b := tx.Bucket(trngDataBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, id)
		if err := b.Put(key, jsonData); err != nil {
			return fmt.Errorf("store data: %w", err)
		}

		// Maintain queue size
		return h.trimTRNGDataIfNeeded(tx)
	})
}

// GetTRNGData retrieves TRNG data with pagination and consumption tracking
func (h *BoltDBHandler) GetTRNGData(limit, offset int, consume bool) ([][]byte, error) {
	var dataSlices [][]byte
	consumedCount := int64(0)

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

		// Extract data and mark as consumed if requested
		for i := start; i < end; i++ {
			dataSlices = append(dataSlices, allItems[i].Data)

			if consume {
				allItems[i].Consumed = true
				consumedCount++
				jsonData, err := json.Marshal(allItems[i])
				if err != nil {
					return fmt.Errorf("serialize data: %w", err)
				}

				if err := b.Put(allKeys[i], jsonData); err != nil {
					return fmt.Errorf("update consumed status: %w", err)
				}
			}
		}

		// Update consumed counter
		if consume && consumedCount > 0 {
			key := []byte("trng_consumed_count")
			currentCount, err := h.getCounter(tx, key)
			if err != nil {
				return fmt.Errorf("get consumed count: %w", err)
			}
			currentCount += uint64(consumedCount) // #nosec G115
			if err := h.setCounter(tx, key, currentCount); err != nil {
				return fmt.Errorf("set consumed count: %w", err)
			}
		}

		return nil
	})

	return dataSlices, err
}

// trimTRNGDataIfNeeded maintains the TRNG data queue size and counts drops
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
		if count > maxSize {
			oldestKeys = append(oldestKeys, append([]byte{}, k...))
		}
	}

	// Delete oldest entries if we exceed the limit and count drops
	if len(oldestKeys) > 0 {
		droppedCount := len(oldestKeys)
		for _, key := range oldestKeys {
			if err := b.Delete(key); err != nil {
				return fmt.Errorf("delete oldest entry: %w", err)
			}
		}

		// Update dropped counter
		key := []byte("trng_dropped_count")
		currentCount, err := h.getCounter(tx, key)
		if err != nil {
			return fmt.Errorf("get dropped count: %w", err)
		}
		currentCount += uint64(droppedCount)
		if err := h.setCounter(tx, key, currentCount); err != nil {
			return fmt.Errorf("set dropped count: %w", err)
		}
	}

	return nil
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

// GetFortunaData retrieves Fortuna-generated data with pagination and consumption tracking
func (h *BoltDBHandler) GetFortunaData(limit, offset int, consume bool) ([][]byte, error) {
	var dataSlices [][]byte
	consumedCount := int64(0)

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
				consumedCount++
				jsonData, err := json.Marshal(allItems[i])
				if err != nil {
					return fmt.Errorf("serialize data: %w", err)
				}

				if err := b.Put(allKeys[i], jsonData); err != nil {
					return fmt.Errorf("update consumed status: %w", err)
				}
			}
		}

		// Update consumed counter
		if consume && consumedCount > 0 {
			key := []byte("fortuna_consumed_count")
			currentCount, err := h.getCounter(tx, key)
			if err != nil {
				return fmt.Errorf("get consumed count: %w", err)
			}
			currentCount += uint64(consumedCount) // #nosec G115
			if err := h.setCounter(tx, key, currentCount); err != nil {
				return fmt.Errorf("set consumed count: %w", err)
			}
		}

		return nil
	})

	return dataSlices, err
}

// trimFortunaDataIfNeeded maintains the Fortuna data queue size and counts drops
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
		if count > maxSize {
			oldestKeys = append(oldestKeys, append([]byte{}, k...))
		}
	}

	// Delete oldest entries if we exceed the limit and count drops
	if len(oldestKeys) > 0 {
		droppedCount := len(oldestKeys)
		for _, key := range oldestKeys {
			if err := b.Delete(key); err != nil {
				return fmt.Errorf("delete oldest entry: %w", err)
			}
		}

		// Update dropped counter
		key := []byte("fortuna_dropped_count")
		currentCount, err := h.getCounter(tx, key)
		if err != nil {
			return fmt.Errorf("get dropped count: %w", err)
		}
		currentCount += uint64(droppedCount)

		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], currentCount)
		if err := tx.Bucket(countersBucket).Put(key, buf[:]); err != nil {
			return fmt.Errorf("update dropped count: %w", err)
		}
	}

	return nil
}

//---------------------- Enhanced Statistics Operations ----------------------

// IncrementPollingCount increments the polling counter for a data source
func (h *BoltDBHandler) IncrementPollingCount(source string) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(countersBucket)
		key := []byte(source + "_polling_count")

		count, err := h.getCounter(tx, key)
		if err != nil {
			return fmt.Errorf("get polling count: %w", err)
		}
		count++

		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], count)
		return b.Put(key, buf[:])
	})
}

// IncrementDroppedCount increments the dropped counter for a data source
func (h *BoltDBHandler) IncrementDroppedCount(source string) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(countersBucket)
		key := []byte(source + "_dropped_count")

		count, err := h.getCounter(tx, key)
		if err != nil {
			return fmt.Errorf("get dropped count: %w", err)
		}
		count++

		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], count)
		return b.Put(key, buf[:])
	})
}

// getCounterValue safely retrieves a counter value
func (h *BoltDBHandler) getCounterValue(tx *bolt.Tx, key string) int64 {
	count, err := h.getCounter(tx, []byte(key))
	if err != nil {
		log.Printf("Warning: failed to get counter %s: %v", key, err)
		return 0
	}
	// Safe conversion - counter values are tracking reasonable metrics
	return int64(count) // #nosec G115
}

// GetDetailedStats returns comprehensive system statistics
func (h *BoltDBHandler) GetDetailedStats() (*DetailedStats, error) {
	stats := &DetailedStats{}

	err := h.db.View(func(tx *bolt.Tx) error {
		// TRNG stats
		stats.TRNG.PollingCount = h.getCounterValue(tx, "trng_polling_count")
		stats.TRNG.QueueDropped = h.getCounterValue(tx, "trng_dropped_count")
		stats.TRNG.ConsumedCount = h.getCounterValue(tx, "trng_consumed_count")

		// Count TRNG items
		trngBucket := tx.Bucket(trngDataBucket)
		trngCursor := trngBucket.Cursor()
		trngTotal := 0
		trngUnconsumed := 0
		for k, v := trngCursor.First(); k != nil; k, v = trngCursor.Next() {
			trngTotal++
			var data TRNGData
			if err := json.Unmarshal(v, &data); err == nil && !data.Consumed {
				trngUnconsumed++
			}
		}

		h.mu.RLock()
		stats.TRNG.QueueCapacity = h.trngQueueSize
		h.mu.RUnlock()

		stats.TRNG.QueueCurrent = trngUnconsumed
		if stats.TRNG.QueueCapacity > 0 {
			stats.TRNG.QueuePercentage = float64(trngUnconsumed) / float64(stats.TRNG.QueueCapacity) * 100
		}
		stats.TRNG.UnconsumedCount = trngUnconsumed
		stats.TRNG.TotalGenerated = int64(trngTotal)

		// Fortuna stats
		stats.Fortuna.PollingCount = h.getCounterValue(tx, "fortuna_polling_count")
		stats.Fortuna.QueueDropped = h.getCounterValue(tx, "fortuna_dropped_count")
		stats.Fortuna.ConsumedCount = h.getCounterValue(tx, "fortuna_consumed_count")

		// Count Fortuna items
		fortunaBucket := tx.Bucket(fortunaDataBucket)
		fortunaCursor := fortunaBucket.Cursor()
		fortunaTotal := 0
		fortunaUnconsumed := 0
		for k, v := fortunaCursor.First(); k != nil; k, v = fortunaCursor.Next() {
			fortunaTotal++
			var data FortunaData
			if err := json.Unmarshal(v, &data); err == nil && !data.Consumed {
				fortunaUnconsumed++
			}
		}

		h.mu.RLock()
		stats.Fortuna.QueueCapacity = h.fortunaQueueSize
		h.mu.RUnlock()

		stats.Fortuna.QueueCurrent = fortunaUnconsumed
		if stats.Fortuna.QueueCapacity > 0 {
			stats.Fortuna.QueuePercentage = float64(fortunaUnconsumed) / float64(stats.Fortuna.QueueCapacity) * 100
		}
		stats.Fortuna.UnconsumedCount = fortunaUnconsumed
		stats.Fortuna.TotalGenerated = int64(fortunaTotal)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get detailed stats: %w", err)
	}

	// Database stats
	dbSize, err := h.GetDatabaseSize()
	if err != nil {
		log.Printf("Warning: failed to get database size: %v", err)
		dbSize = 0
	}
	stats.Database.SizeBytes = dbSize
	stats.Database.SizeHuman = formatBytes(dbSize)
	stats.Database.Path = h.GetDatabasePath()

	return stats, nil
}

// GetDatabaseSize returns the size of the database file in bytes
func (h *BoltDBHandler) GetDatabaseSize() (int64, error) {
	var size int64
	err := h.db.View(func(tx *bolt.Tx) error {
		size = tx.Size()
		return nil
	})
	return size, err
}

// GetDatabasePath returns the path to the database file
func (h *BoltDBHandler) GetDatabasePath() string {
	return h.db.Path()
}

// formatBytes formats bytes into human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

//---------------------- Statistics Operations ----------------------

// RecordRNGUsage records usage statistics for RNG data
func (h *BoltDBHandler) RecordRNGUsage(source string, bytesUsed int64) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageStatsBucket)

		stat := UsageStat{
			Source:    source,
			BytesUsed: bytesUsed,
			Requests:  1,
			Timestamp: time.Now(),
		}

		jsonData, err := json.Marshal(stat)
		if err != nil {
			return fmt.Errorf("serialize usage stat: %w", err)
		}

		// Use timestamp as key
		key := []byte(fmt.Sprintf("%s_%d", source, time.Now().UnixNano()))
		return b.Put(key, jsonData)
	})
}

// GetRNGStatistics retrieves usage statistics for a specific source within a time range
func (h *BoltDBHandler) GetRNGStatistics(source string, start, end time.Time) ([]UsageStat, error) {
	var stats []UsageStat

	err := h.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(usageStatsBucket)
		cursor := b.Cursor()

		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var stat UsageStat
			if err := json.Unmarshal(v, &stat); err != nil {
				continue
			}

			if stat.Source == source && stat.Timestamp.After(start) && stat.Timestamp.Before(end) {
				stats = append(stats, stat)
			}
		}

		return nil
	})

	return stats, err
}

// GetStats returns general statistics about the database
func (h *BoltDBHandler) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	err := h.db.View(func(tx *bolt.Tx) error {
		// Count TRNG data
		trngBucket := tx.Bucket(trngDataBucket)
		trngCount := 0
		trngCursor := trngBucket.Cursor()
		for k, _ := trngCursor.First(); k != nil; k, _ = trngCursor.Next() {
			trngCount++
		}
		stats["trng_count"] = trngCount

		// Count Fortuna data
		fortunaBucket := tx.Bucket(fortunaDataBucket)
		fortunaCount := 0
		fortunaCursor := fortunaBucket.Cursor()
		for k, _ := fortunaCursor.First(); k != nil; k, _ = fortunaCursor.Next() {
			fortunaCount++
		}
		stats["fortuna_count"] = fortunaCount

		// Get database size
		stats["db_size"] = tx.Size()

		return nil
	})

	return stats, err
}

//---------------------- Queue Management ----------------------

// GetQueueInfo returns information about the current queue sizes
func (h *BoltDBHandler) GetQueueInfo() (map[string]int, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	info := map[string]int{
		"trng_queue_capacity":    h.trngQueueSize,
		"fortuna_queue_capacity": h.fortunaQueueSize,
	}

	// Get current queue usage
	err := h.db.View(func(tx *bolt.Tx) error {
		// Count TRNG unconsumed
		trngBucket := tx.Bucket(trngDataBucket)
		trngCursor := trngBucket.Cursor()
		trngUnconsumed := 0
		for k, v := trngCursor.First(); k != nil; k, v = trngCursor.Next() {
			var data TRNGData
			if err := json.Unmarshal(v, &data); err == nil && !data.Consumed {
				trngUnconsumed++
			}
		}
		info["trng_queue_current"] = trngUnconsumed

		// Count Fortuna unconsumed
		fortunaBucket := tx.Bucket(fortunaDataBucket)
		fortunaCursor := fortunaBucket.Cursor()
		fortunaUnconsumed := 0
		for k, v := fortunaCursor.First(); k != nil; k, v = fortunaCursor.Next() {
			var data FortunaData
			if err := json.Unmarshal(v, &data); err == nil && !data.Consumed {
				fortunaUnconsumed++
			}
		}
		info["fortuna_queue_current"] = fortunaUnconsumed

		return nil
	})

	return info, err
}

// UpdateQueueSizes updates the queue size configuration
func (h *BoltDBHandler) UpdateQueueSizes(trngSize, fortunaSize int) error {
	h.mu.Lock()
	h.trngQueueSize = trngSize
	h.fortunaQueueSize = fortunaSize
	h.mu.Unlock()

	return h.db.Update(func(tx *bolt.Tx) error {
		configB := tx.Bucket(configBucket)
		var buf [8]byte

		// Safe conversion - queue sizes are validated configuration values
		binary.BigEndian.PutUint64(buf[:], uint64(trngSize)) // #nosec G115
		if err := configB.Put([]byte("trng_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store trng queue size: %w", err)
		}

		binary.BigEndian.PutUint64(buf[:], uint64(fortunaSize)) // #nosec G115
		if err := configB.Put([]byte("fortuna_queue_size"), buf[:]); err != nil {
			return fmt.Errorf("store fortuna queue size: %w", err)
		}

		return nil
	})
}

//---------------------- Health Check ----------------------

// HealthCheck performs a basic health check on the database
func (h *BoltDBHandler) HealthCheck() bool {
	err := h.db.View(func(tx *bolt.Tx) error {
		return nil
	})
	return err == nil
}
