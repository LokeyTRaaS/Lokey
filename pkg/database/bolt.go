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
			currentCount, _ := h.getCounter(tx, key)
			currentCount += uint64(consumedCount)
			h.setCounter(tx, key, currentCount)
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
		currentCount, _ := h.getCounter(tx, key)
		currentCount += uint64(droppedCount)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], currentCount)
		tx.Bucket(countersBucket).Put(key, buf[:])
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
			currentCount, _ := h.getCounter(tx, key)
			currentCount += uint64(consumedCount)
			h.setCounter(tx, key, currentCount)
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
		currentCount, _ := h.getCounter(tx, key)
		currentCount += uint64(droppedCount)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], currentCount)
		tx.Bucket(countersBucket).Put(key, buf[:])
	}

	return nil
}

//---------------------- Enhanced Statistics Operations ----------------------

// IncrementPollingCount increments the polling counter for a data source
func (h *BoltDBHandler) IncrementPollingCount(source string) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(countersBucket)
		key := []byte(source + "_polling_count")

		count, _ := h.getCounter(tx, key)
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

		count, _ := h.getCounter(tx, key)
		count++

		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], count)
		return b.Put(key, buf[:])
	})
}

// getCounterValue safely retrieves a counter value
func (h *BoltDBHandler) getCounterValue(tx *bolt.Tx, key string) int64 {
	count, _ := h.getCounter(tx, []byte(key))
	return int64(count)
}

// GetDetailedStats returns comprehensive system statistics
func (h *BoltDBHandler) GetDetailedStats() (*DetailedStats, error) {
	var stats DetailedStats

	err := h.db.View(func(tx *bolt.Tx) error {
		// Get basic queue info first
		queueInfo, err := h.getQueueInfoInTx(tx)
		if err != nil {
			return err
		}

		h.mu.RLock()
		trngCapacity := h.trngQueueSize
		fortunaCapacity := h.fortunaQueueSize
		h.mu.RUnlock()

		// Calculate TRNG stats
		trngCurrent := queueInfo["trng_unconsumed_count"]
		trngPercentage := float64(trngCurrent) / float64(trngCapacity) * 100
		if trngPercentage > 100 {
			trngPercentage = 100
		}

		stats.TRNG = DataSourceStats{
			PollingCount:    h.getCounterValue(tx, "trng_polling_count"),
			QueueCurrent:    trngCurrent,
			QueueCapacity:   trngCapacity,
			QueuePercentage: trngPercentage,
			QueueDropped:    h.getCounterValue(tx, "trng_dropped_count"),
			ConsumedCount:   h.getCounterValue(tx, "trng_consumed_count"),
			UnconsumedCount: trngCurrent,
			TotalGenerated:  h.getCounterValue(tx, "trng_next_id"),
		}

		// Calculate Fortuna stats
		fortunaCurrent := queueInfo["fortuna_unconsumed_count"]
		fortunaPercentage := float64(fortunaCurrent) / float64(fortunaCapacity) * 100
		if fortunaPercentage > 100 {
			fortunaPercentage = 100
		}

		stats.Fortuna = DataSourceStats{
			PollingCount:    h.getCounterValue(tx, "fortuna_polling_count"),
			QueueCurrent:    fortunaCurrent,
			QueueCapacity:   fortunaCapacity,
			QueuePercentage: fortunaPercentage,
			QueueDropped:    h.getCounterValue(tx, "fortuna_dropped_count"),
			ConsumedCount:   h.getCounterValue(tx, "fortuna_consumed_count"),
			UnconsumedCount: fortunaCurrent,
			TotalGenerated:  h.getCounterValue(tx, "fortuna_next_id"),
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Get database stats
	dbSize, _ := h.GetDatabaseSize()
	stats.Database = DatabaseStats{
		SizeBytes: dbSize,
		SizeHuman: formatBytes(dbSize),
		Path:      h.GetDatabasePath(),
	}

	return &stats, nil
}

// getQueueInfoInTx returns queue info within a transaction
func (h *BoltDBHandler) getQueueInfoInTx(tx *bolt.Tx) (map[string]int, error) {
	queueInfo := make(map[string]int)

	// Count TRNG data
	trngTotal := 0
	trngUnconsumed := 0

	trngBucket := tx.Bucket(trngDataBucket)
	cursor := trngBucket.Cursor()
	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		trngTotal++

		var data TRNGData
		if err := json.Unmarshal(v, &data); err != nil {
			return nil, fmt.Errorf("deserialize TRNG data: %w", err)
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
			return nil, fmt.Errorf("deserialize Fortuna data: %w", err)
		}

		if !data.Consumed {
			fortunaUnconsumed++
		}
	}

	queueInfo["trng_data_count"] = trngTotal
	queueInfo["trng_unconsumed_count"] = trngUnconsumed
	queueInfo["fortuna_data_count"] = fortunaTotal
	queueInfo["fortuna_unconsumed_count"] = fortunaUnconsumed

	return queueInfo, nil
}

// GetDatabaseSize returns the size of the database file in bytes
func (h *BoltDBHandler) GetDatabaseSize() (int64, error) {
	// Get the actual file size from the filesystem
	fileInfo, err := os.Stat(h.db.Path())
	if err != nil {
		return 0, fmt.Errorf("failed to get database file info: %w", err)
	}
	return fileInfo.Size(), nil
}

// GetDatabasePath returns the path to the database file
func (h *BoltDBHandler) GetDatabasePath() string {
	return h.db.Path()
}

// formatBytes formats bytes into human readable format
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
		queueInfo["trng_queue_capacity"] = trngCapacity
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
