package database

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dgraph-io/badger/v3"
)

const (
	// Key prefixes for different data types
	prefixTRNGQueue     = "trng_queue:"    // For TRNG request queue
	prefixFortunaQueue  = "fortuna_queue:" // For Fortuna request queue
	prefixTRNGData      = "trng_data:"     // For TRNG hash data
	prefixFortunaData   = "fortuna_data:"  // For Fortuna random data
	prefixUsageStats    = "usage:"         // For usage statistics
	prefixQueueCounter  = "counter:"       // For maintaining queue positions
	prefixTRNGDataID    = "trng_id:"       // For TRNG data IDs
	prefixFortunaDataID = "fortuna_id:"    // For Fortuna data IDs
)

// BadgerDBHandler implements the database interface using BadgerDB
type BadgerDBHandler struct {
	db               *badger.DB
	trngQueueSize    int
	fortunaQueueSize int
}

// NewBadgerDBHandler creates a new BadgerDB database handler
func NewBadgerDBHandler(dbPath string, trngQueueSize, fortunaQueueSize int) (*BadgerDBHandler, error) {
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable default logger

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB: %w", err)
	}

	// Run garbage collection in the background
	go runBadgerGC(db)

	handler := &BadgerDBHandler{
		db:               db,
		trngQueueSize:    trngQueueSize,
		fortunaQueueSize: fortunaQueueSize,
	}

	// Initialize counters if they don't exist
	err = handler.initializeCounters()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize counters: %w", err)
	}

	return handler, nil
}

// initializeCounters ensures queue counters exist
func (h *BadgerDBHandler) initializeCounters() error {
	return h.db.Update(func(txn *badger.Txn) error {
		counters := []string{
			prefixQueueCounter + "trng_head",
			prefixQueueCounter + "trng_tail",
			prefixQueueCounter + "fortuna_head",
			prefixQueueCounter + "fortuna_tail",
			prefixTRNGDataID + "next",
			prefixFortunaDataID + "next",
		}

		for _, counter := range counters {
			_, err := h.getCounter(txn, counter)
			if err == badger.ErrKeyNotFound {
				err = h.setCounter(txn, counter, 0)
				if err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		return nil
	})
}

// runBadgerGC runs periodic garbage collection for BadgerDB
func runBadgerGC(db *badger.DB) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		err := db.RunValueLogGC(0.5) // Run GC if space can be reclaimed
		if err != nil && err != badger.ErrNoRewrite {
			log.Printf("Error during BadgerDB GC: %v", err)
		}
	}
}

// Close closes the database connection
func (h *BadgerDBHandler) Close() error {
	return h.db.Close()
}

// getCounter retrieves a counter value
func (h *BadgerDBHandler) getCounter(txn *badger.Txn, key string) (uint64, error) {
	item, err := txn.Get([]byte(key))
	if err != nil {
		return 0, err
	}

	var val uint64
	err = item.Value(func(v []byte) error {
		if len(v) != 8 {
			return fmt.Errorf("invalid counter format")
		}
		val = binary.BigEndian.Uint64(v)
		return nil
	})
	return val, err
}

// setCounter sets a counter value
func (h *BadgerDBHandler) setCounter(txn *badger.Txn, key string, value uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, value)
	return txn.Set([]byte(key), buf)
}

// getNextID gets the next available ID for a sequence and increments it
func (h *BadgerDBHandler) getNextID(txn *badger.Txn, counterKey string) (uint64, error) {
	id, err := h.getCounter(txn, counterKey)
	if err != nil {
		return 0, err
	}

	err = h.setCounter(txn, counterKey, id+1)
	if err != nil {
		return 0, err
	}

	return id, nil
}

//---------------------- TRNG Request Queue Operations ----------------------

// EnqueueTRNGRequest adds a new TRNG request to the queue
func (h *BadgerDBHandler) EnqueueTRNGRequest(request Request) error {
	return h.db.Update(func(txn *badger.Txn) error {
		// Check queue size before adding new request
		headCounter, err := h.getCounter(txn, prefixQueueCounter+"trng_head")
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(txn, prefixQueueCounter+"trng_tail")
		if err != nil {
			return err
		}

		// Calculate current queue size
		queueSize := int(headCounter - tailCounter)
		if queueSize >= h.trngQueueSize {
			return fmt.Errorf("TRNG queue is full")
		}

		// Set request status and timestamps
		request.Status = "pending"
		request.CreatedAt = time.Now()
		request.UpdatedAt = request.CreatedAt
		request.Source = "trng"

		// Serialize request
		requestData, err := json.Marshal(request)
		if err != nil {
			return err
		}

		// Create key with current head counter
		key := fmt.Sprintf("%s%d", prefixTRNGQueue, headCounter)

		// Store request
		err = txn.Set([]byte(key), requestData)
		if err != nil {
			return err
		}

		// Increment head counter
		return h.setCounter(txn, prefixQueueCounter+"trng_head", headCounter+1)
	})
}

// DequeueTRNGRequest retrieves and removes the next TRNG request from the queue
func (h *BadgerDBHandler) DequeueTRNGRequest() (Request, error) {
	var request Request

	err := h.db.Update(func(txn *badger.Txn) error {
		headCounter, err := h.getCounter(txn, prefixQueueCounter+"trng_head")
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(txn, prefixQueueCounter+"trng_tail")
		if err != nil {
			return err
		}

		// Check if queue is empty
		if headCounter == tailCounter {
			return fmt.Errorf("TRNG queue is empty")
		}

		// Get request from tail
		key := fmt.Sprintf("%s%d", prefixTRNGQueue, tailCounter)
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		// Deserialize request
		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &request)
		})
		if err != nil {
			return err
		}

		// Delete processed request
		err = txn.Delete([]byte(key))
		if err != nil {
			return err
		}

		// Increment tail counter
		return h.setCounter(txn, prefixQueueCounter+"trng_tail", tailCounter+1)
	})

	return request, err
}

//---------------------- Fortuna Request Queue Operations ----------------------

// EnqueueFortunaRequest adds a new Fortuna request to the queue
func (h *BadgerDBHandler) EnqueueFortunaRequest(request Request) error {
	return h.db.Update(func(txn *badger.Txn) error {
		// Check queue size before adding new request
		headCounter, err := h.getCounter(txn, prefixQueueCounter+"fortuna_head")
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(txn, prefixQueueCounter+"fortuna_tail")
		if err != nil {
			return err
		}

		// Calculate current queue size
		queueSize := int(headCounter - tailCounter)
		if queueSize >= h.fortunaQueueSize {
			return fmt.Errorf("Fortuna queue is full")
		}

		// Set request status and timestamps
		request.Status = "pending"
		request.CreatedAt = time.Now()
		request.UpdatedAt = request.CreatedAt
		request.Source = "fortuna"

		// Serialize request
		requestData, err := json.Marshal(request)
		if err != nil {
			return err
		}

		// Create key with current head counter
		key := fmt.Sprintf("%s%d", prefixFortunaQueue, headCounter)

		// Store request
		err = txn.Set([]byte(key), requestData)
		if err != nil {
			return err
		}

		// Increment head counter
		return h.setCounter(txn, prefixQueueCounter+"fortuna_head", headCounter+1)
	})
}

// DequeueFortunaRequest retrieves and removes the next Fortuna request from the queue
func (h *BadgerDBHandler) DequeueFortunaRequest() (Request, error) {
	var request Request

	err := h.db.Update(func(txn *badger.Txn) error {
		headCounter, err := h.getCounter(txn, prefixQueueCounter+"fortuna_head")
		if err != nil {
			return err
		}

		tailCounter, err := h.getCounter(txn, prefixQueueCounter+"fortuna_tail")
		if err != nil {
			return err
		}

		// Check if queue is empty
		if headCounter == tailCounter {
			return fmt.Errorf("Fortuna queue is empty")
		}

		// Get request from tail
		key := fmt.Sprintf("%s%d", prefixFortunaQueue, tailCounter)
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		// Deserialize request
		err = item.Value(func(v []byte) error {
			return json.Unmarshal(v, &request)
		})
		if err != nil {
			return err
		}

		// Delete processed request
		err = txn.Delete([]byte(key))
		if err != nil {
			return err
		}

		// Increment tail counter
		return h.setCounter(txn, prefixQueueCounter+"fortuna_tail", tailCounter+1)
	})

	return request, err
}

//---------------------- TRNG Data Operations ----------------------

// StoreTRNGHash stores a new TRNG hash
func (h *BadgerDBHandler) StoreTRNGHash(hash []byte) error {
	return h.db.Update(func(txn *badger.Txn) error {
		// Get next ID
		id, err := h.getNextID(txn, prefixTRNGDataID+"next")
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

		// Serialize data
		dataBytes, err := json.Marshal(data)
		if err != nil {
			return err
		}

		// Store data
		key := fmt.Sprintf("%s%d", prefixTRNGData, id)
		err = txn.Set([]byte(key), dataBytes)
		if err != nil {
			return err
		}

		// Maintain queue size
		return h.trimTRNGDataIfNeeded(txn)
	})
}

// trimTRNGDataIfNeeded maintains the TRNG data queue size
func (h *BadgerDBHandler) trimTRNGDataIfNeeded(txn *badger.Txn) error {
	// Count total TRNG data items
	total := 0
	oldestKeys := []string{}

	opts := badger.DefaultIteratorOptions
	// We don't need values here, just count keys
	opts.PrefetchValues = false
	it := txn.NewIterator(opts)
	defer it.Close()

	prefix := []byte(prefixTRNGData)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		total++
		if total <= h.trngQueueSize {
			oldestKeys = append(oldestKeys, string(it.Item().Key()))
		}
	}

	// If we have more items than allowed, delete the oldest ones
	if total > h.trngQueueSize {
		excess := total - h.trngQueueSize
		for i := 0; i < excess && i < len(oldestKeys); i++ {
			err := txn.Delete([]byte(oldestKeys[i]))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// GetTRNGHashes retrieves TRNG hashes with pagination and optional consumption
func (h *BadgerDBHandler) GetTRNGHashes(limit, offset int, consume bool) ([][]byte, error) {
	var hashes [][]byte

	err := h.db.Update(func(txn *badger.Txn) error {
		// Get all TRNG data keys
		var allKeys []string
		prefix := []byte(prefixTRNGData)

		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()

			// Check if item is consumed
			var data TRNGData
			err := item.Value(func(v []byte) error {
				return json.Unmarshal(v, &data)
			})
			if err != nil {
				return err
			}

			if !data.Consumed {
				allKeys = append(allKeys, string(item.Key()))
			}
		}

		// Apply pagination
		start := offset
		end := offset + limit
		if start >= len(allKeys) {
			return nil
		}
		if end > len(allKeys) {
			end = len(allKeys)
		}

		// Get data for selected keys
		for i := start; i < end; i++ {
			item, err := txn.Get([]byte(allKeys[i]))
			if err != nil {
				return err
			}

			var data TRNGData
			err = item.Value(func(v []byte) error {
				return json.Unmarshal(v, &data)
			})
			if err != nil {
				return err
			}

			hashes = append(hashes, data.Hash)

			// Mark as consumed if requested
			if consume {
				data.Consumed = true
				dataBytes, err := json.Marshal(data)
				if err != nil {
					return err
				}

				err = txn.Set([]byte(allKeys[i]), dataBytes)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	return hashes, err
}

//---------------------- Fortuna Data Operations ----------------------

// StoreFortunaData stores Fortuna-generated data
func (h *BadgerDBHandler) StoreFortunaData(data []byte) error {
	return h.db.Update(func(txn *badger.Txn) error {
		// Get next ID
		id, err := h.getNextID(txn, prefixFortunaDataID+"next")
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

		// Serialize data
		dataBytes, err := json.Marshal(fortunaData)
		if err != nil {
			return err
		}

		// Store data
		key := fmt.Sprintf("%s%d", prefixFortunaData, id)
		err = txn.Set([]byte(key), dataBytes)
		if err != nil {
			return err
		}

		// Maintain queue size
		return h.trimFortunaDataIfNeeded(txn)
	})
}

// trimFortunaDataIfNeeded maintains the Fortuna data queue size
func (h *BadgerDBHandler) trimFortunaDataIfNeeded(txn *badger.Txn) error {
	// Count total Fortuna data items
	total := 0
	oldestKeys := []string{}

	opts := badger.DefaultIteratorOptions
	// We don't need values here, just count keys
	opts.PrefetchValues = false
	it := txn.NewIterator(opts)
	defer it.Close()

	prefix := []byte(prefixFortunaData)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		total++
		if total <= h.fortunaQueueSize {
			oldestKeys = append(oldestKeys, string(it.Item().Key()))
		}
	}

	// If we have more items than allowed, delete the oldest ones
	if total > h.fortunaQueueSize {
		excess := total - h.fortunaQueueSize
		for i := 0; i < excess && i < len(oldestKeys); i++ {
			err := txn.Delete([]byte(oldestKeys[i]))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// GetFortunaData retrieves Fortuna-generated data with pagination and optional consumption
func (h *BadgerDBHandler) GetFortunaData(limit, offset int, consume bool) ([][]byte, error) {
	var dataSlices [][]byte

	err := h.db.Update(func(txn *badger.Txn) error {
		// Get all Fortuna data keys
		var allKeys []string
		prefix := []byte(prefixFortunaData)

		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()

			// Check if item is consumed
			var data FortunaData
			err := item.Value(func(v []byte) error {
				return json.Unmarshal(v, &data)
			})
			if err != nil {
				return err
			}

			if !data.Consumed {
				allKeys = append(allKeys, string(item.Key()))
			}
		}

		// Apply pagination
		start := offset
		end := offset + limit
		if start >= len(allKeys) {
			return nil
		}
		if end > len(allKeys) {
			end = len(allKeys)
		}

		// Get data for selected keys
		for i := start; i < end; i++ {
			item, err := txn.Get([]byte(allKeys[i]))
			if err != nil {
				return err
			}

			var data FortunaData
			err = item.Value(func(v []byte) error {
				return json.Unmarshal(v, &data)
			})
			if err != nil {
				return err
			}

			dataSlices = append(dataSlices, data.Data)

			// Mark as consumed if requested
			if consume {
				data.Consumed = true
				dataBytes, err := json.Marshal(data)
				if err != nil {
					return err
				}

				err = txn.Set([]byte(allKeys[i]), dataBytes)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	return dataSlices, err
}

//---------------------- Statistics Operations ----------------------

// RecordRNGUsage records RNG usage statistics
func (h *BadgerDBHandler) RecordRNGUsage(source string, bytesUsed int64) error {
	return h.db.Update(func(txn *badger.Txn) error {
		now := time.Now()
		timestamp := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())

		// Create a key that includes source and hourly timestamp
		key := fmt.Sprintf("%s%s:%d", prefixUsageStats, source, timestamp.Unix())

		// Try to get existing stats for this hour
		item, err := txn.Get([]byte(key))

		var stat UsageStat
		if err == nil {
			// Found existing stats, update them
			err = item.Value(func(v []byte) error {
				return json.Unmarshal(v, &stat)
			})
			if err != nil {
				return err
			}

			stat.BytesUsed += bytesUsed
			stat.Requests++
		} else if err == badger.ErrKeyNotFound {
			// No existing stats, create new ones
			stat = UsageStat{
				Source:    source,
				BytesUsed: bytesUsed,
				Requests:  1,
				Timestamp: timestamp,
			}
		} else {
			return err
		}

		// Store updated stats
		statData, err := json.Marshal(stat)
		if err != nil {
			return err
		}

		return txn.Set([]byte(key), statData)
	})
}

// GetRNGStatistics retrieves RNG usage statistics for a specific time range
func (h *BadgerDBHandler) GetRNGStatistics(source string, start, end time.Time) ([]UsageStat, error) {
	var stats []UsageStat

	err := h.db.View(func(txn *badger.Txn) error {
		// We'll iterate through all usage stats and filter by time range
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte(prefixUsageStats + source + ":")

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()

			var stat UsageStat
			err := item.Value(func(v []byte) error {
				return json.Unmarshal(v, &stat)
			})
			if err != nil {
				return err
			}

			// Check if stat is within the requested time range
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
func (h *BadgerDBHandler) GetQueueInfo() (map[string]int, error) {
	queueInfo := make(map[string]int)

	err := h.db.View(func(txn *badger.Txn) error {
		// Get TRNG queue counters
		trngHead, err := h.getCounter(txn, prefixQueueCounter+"trng_head")
		if err != nil {
			return err
		}

		trngTail, err := h.getCounter(txn, prefixQueueCounter+"trng_tail")
		if err != nil {
			return err
		}

		// Get Fortuna queue counters
		fortunaHead, err := h.getCounter(txn, prefixQueueCounter+"fortuna_head")
		if err != nil {
			return err
		}

		fortunaTail, err := h.getCounter(txn, prefixQueueCounter+"fortuna_tail")
		if err != nil {
			return err
		}

		// Count total TRNG data items
		trngDataCount := 0
		trngUnconsumedCount := 0

		prefix := []byte(prefixTRNGData)
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			trngDataCount++

			// Check if consumed
			var data TRNGData
			err := it.Item().Value(func(v []byte) error {
				return json.Unmarshal(v, &data)
			})
			if err != nil {
				return err
			}

			if !data.Consumed {
				trngUnconsumedCount++
			}
		}

		// Count total Fortuna data items
		fortunaDataCount := 0
		fortunaUnconsumedCount := 0

		prefix = []byte(prefixFortunaData)
		// Create a new iterator for Fortuna data
		it = txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			fortunaDataCount++

			// Check if consumed
			var data FortunaData
			err := it.Item().Value(func(v []byte) error {
				return json.Unmarshal(v, &data)
			})
			if err != nil {
				return err
			}

			if !data.Consumed {
				fortunaUnconsumedCount++
			}
		}

		queueInfo["trng_queue_size"] = int(trngHead - trngTail)
		queueInfo["trng_queue_capacity"] = h.trngQueueSize
		queueInfo["fortuna_queue_size"] = int(fortunaHead - fortunaTail)
		queueInfo["fortuna_queue_capacity"] = h.fortunaQueueSize
		queueInfo["trng_data_count"] = trngDataCount
		queueInfo["trng_unconsumed_count"] = trngUnconsumedCount
		queueInfo["fortuna_data_count"] = fortunaDataCount
		queueInfo["fortuna_unconsumed_count"] = fortunaUnconsumedCount

		return nil
	})

	return queueInfo, err
}

// GetStats returns statistics about the database
func (h *BadgerDBHandler) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get queue info
	queueInfo, err := h.GetQueueInfo()
	if err != nil {
		return nil, err
	}

	// Format stats in the same way as the DuckDB implementation
	stats["trng_total"] = queueInfo["trng_data_count"]
	stats["trng_unconsumed"] = queueInfo["trng_unconsumed_count"]
	stats["trng_queue_full"] = queueInfo["trng_data_count"] >= h.trngQueueSize
	stats["fortuna_total"] = queueInfo["fortuna_data_count"]
	stats["fortuna_unconsumed"] = queueInfo["fortuna_unconsumed_count"]
	stats["fortuna_queue_full"] = queueInfo["fortuna_data_count"] >= h.fortunaQueueSize

	return stats, nil
}

// UpdateQueueSizes updates the queue size configuration
func (h *BadgerDBHandler) UpdateQueueSizes(trngSize, fortunaSize int) error {
	h.trngQueueSize = trngSize
	h.fortunaQueueSize = fortunaSize
	return nil
}

// HealthCheck checks if the database is accessible
func (h *BadgerDBHandler) HealthCheck() bool {
	err := h.db.View(func(txn *badger.Txn) error {
		// Try to read a counter as a simple health check
		_, err := h.getCounter(txn, prefixQueueCounter+"trng_head")
		return err
	})

	if err != nil {
		log.Printf("Database health check failed: %v", err)
		return false
	}

	return true
}
