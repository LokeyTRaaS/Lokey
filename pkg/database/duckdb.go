package database

import (
	"database/sql"
	"fmt"
	"log"
	"sync"

	_ "github.com/marcboeker/go-duckdb"
)

type DuckDBHandler struct {
	db               *sql.DB
	trngQueueSize    int
	fortunaQueueSize int
	mutex            sync.Mutex
}

// NewDuckDBHandler creates a new DuckDB database handler
func NewDuckDBHandler(dbPath string, trngQueueSize, fortunaQueueSize int) (*DuckDBHandler, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB: %w", err)
	}

	handler := &DuckDBHandler{
		db:               db,
		trngQueueSize:    trngQueueSize,
		fortunaQueueSize: fortunaQueueSize,
		mutex:            sync.Mutex{},
	}

	err = handler.setupTables()
	if err != nil {
		return nil, err
	}

	return handler, nil
}

// setupTables creates necessary tables if they don't exist
func (d *DuckDBHandler) setupTables() error {
	// Create TRNG data table
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS trng_data (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hash BLOB NOT NULL,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			consumed BOOLEAN DEFAULT FALSE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create trng_data table: %w", err)
	}

	// Create Fortuna data table
	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS fortuna_data (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			data BLOB NOT NULL,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			consumed BOOLEAN DEFAULT FALSE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create fortuna_data table: %w", err)
	}

	return nil
}

// StoreTRNGHash stores a new TRNG hash and maintains queue size
func (d *DuckDBHandler) StoreTRNGHash(hash []byte) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Insert new hash
	_, err = tx.Exec("INSERT INTO trng_data (hash) VALUES (?)", hash)
	if err != nil {
		return fmt.Errorf("failed to insert TRNG hash: %w", err)
	}

	// Maintain queue size by removing oldest entries
	_, err = tx.Exec(`
		DELETE FROM trng_data
		WHERE id IN (
			SELECT id FROM trng_data
			ORDER BY timestamp ASC
			LIMIT (SELECT MAX(0, COUNT(*) - ?) FROM trng_data)
		)
	`, d.trngQueueSize)
	if err != nil {
		return fmt.Errorf("failed to maintain TRNG queue size: %w", err)
	}

	return tx.Commit()
}

// StoreFortunaData stores Fortuna-generated data and maintains queue size
func (d *DuckDBHandler) StoreFortunaData(data []byte) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Insert new data
	_, err = tx.Exec("INSERT INTO fortuna_data (data) VALUES (?)", data)
	if err != nil {
		return fmt.Errorf("failed to insert Fortuna data: %w", err)
	}

	// Maintain queue size
	_, err = tx.Exec(`
		DELETE FROM fortuna_data
		WHERE id IN (
			SELECT id FROM fortuna_data
			ORDER BY timestamp ASC
			LIMIT (SELECT MAX(0, COUNT(*) - ?) FROM fortuna_data)
		)
	`, d.fortunaQueueSize)
	if err != nil {
		return fmt.Errorf("failed to maintain Fortuna queue size: %w", err)
	}

	return tx.Commit()
}

// GetTRNGHashes retrieves TRNG hashes with pagination and optional consumption
func (d *DuckDBHandler) GetTRNGHashes(limit, offset int, consume bool) ([][]byte, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	query := `
		SELECT id, hash
		FROM trng_data
		WHERE consumed = FALSE
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query TRNG hashes: %w", err)
	}
	defer rows.Close()

	var hashes [][]byte
	var ids []int

	for rows.Next() {
		var id int
		var hash []byte
		err = rows.Scan(&id, &hash)
		if err != nil {
			return nil, fmt.Errorf("failed to scan TRNG hash: %w", err)
		}

		hashes = append(hashes, hash)
		ids = append(ids, id)
	}

	if consume && len(ids) > 0 {
		// Mark hashes as consumed
		tx, err := d.db.Begin()
		if err != nil {
			return nil, fmt.Errorf("failed to begin transaction: %w", err)
		}

		for _, id := range ids {
			_, err = tx.Exec("UPDATE trng_data SET consumed = TRUE WHERE id = ?", id)
			if err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to mark TRNG hash as consumed: %w", err)
			}
		}

		err = tx.Commit()
		if err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	return hashes, nil
}

// GetFortunaData retrieves Fortuna-generated data with pagination and optional consumption
func (d *DuckDBHandler) GetFortunaData(limit, offset int, consume bool) ([][]byte, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	query := `
		SELECT id, data
		FROM fortuna_data
		WHERE consumed = FALSE
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query Fortuna data: %w", err)
	}
	defer rows.Close()

	var dataSlices [][]byte
	var ids []int

	for rows.Next() {
		var id int
		var data []byte
		err = rows.Scan(&id, &data)
		if err != nil {
			return nil, fmt.Errorf("failed to scan Fortuna data: %w", err)
		}

		dataSlices = append(dataSlices, data)
		ids = append(ids, id)
	}

	if consume && len(ids) > 0 {
		// Mark data as consumed
		tx, err := d.db.Begin()
		if err != nil {
			return nil, fmt.Errorf("failed to begin transaction: %w", err)
		}

		for _, id := range ids {
			_, err = tx.Exec("UPDATE fortuna_data SET consumed = TRUE WHERE id = ?", id)
			if err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to mark Fortuna data as consumed: %w", err)
			}
		}

		err = tx.Commit()
		if err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	return dataSlices, nil
}

// GetStats returns statistics about the database
func (d *DuckDBHandler) GetStats() (map[string]interface{}, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	stats := make(map[string]interface{})

	// Get TRNG queue stats
	var trngCount, trngUnconsumedCount int
	err := d.db.QueryRow("SELECT COUNT(*) FROM trng_data").Scan(&trngCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get TRNG count: %w", err)
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM trng_data WHERE consumed = FALSE").Scan(&trngUnconsumedCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get unconsumed TRNG count: %w", err)
	}

	// Get Fortuna queue stats
	var fortunaCount, fortunaUnconsumedCount int
	err = d.db.QueryRow("SELECT COUNT(*) FROM fortuna_data").Scan(&fortunaCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get Fortuna count: %w", err)
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM fortuna_data WHERE consumed = FALSE").Scan(&fortunaUnconsumedCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get unconsumed Fortuna count: %w", err)
	}

	stats["trng_total"] = trngCount
	stats["trng_unconsumed"] = trngUnconsumedCount
	stats["trng_queue_full"] = trngCount >= d.trngQueueSize
	stats["fortuna_total"] = fortunaCount
	stats["fortuna_unconsumed"] = fortunaUnconsumedCount
	stats["fortuna_queue_full"] = fortunaCount >= d.fortunaQueueSize

	return stats, nil
}

// Close closes the database connection
func (d *DuckDBHandler) Close() error {
	return d.db.Close()
}

// UpdateQueueSizes updates the queue sizes for TRNG and Fortuna data
func (d *DuckDBHandler) UpdateQueueSizes(trngQueueSize, fortunaQueueSize int) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.trngQueueSize = trngQueueSize
	d.fortunaQueueSize = fortunaQueueSize

	// Trim queues if they exceed new sizes
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Trim TRNG queue
	_, err = tx.Exec(`
		DELETE FROM trng_data
		WHERE id IN (
			SELECT id FROM trng_data
			ORDER BY timestamp ASC
			LIMIT (SELECT MAX(0, COUNT(*) - ?) FROM trng_data)
		)
	`, trngQueueSize)
	if err != nil {
		return fmt.Errorf("failed to trim TRNG queue: %w", err)
	}

	// Trim Fortuna queue
	_, err = tx.Exec(`
		DELETE FROM fortuna_data
		WHERE id IN (
			SELECT id FROM fortuna_data
			ORDER BY timestamp ASC
			LIMIT (SELECT MAX(0, COUNT(*) - ?) FROM fortuna_data)
		)
	`, fortunaQueueSize)
	if err != nil {
		return fmt.Errorf("failed to trim Fortuna queue: %w", err)
	}

	return tx.Commit()
}

// HealthCheck checks if the database is accessible
func (d *DuckDBHandler) HealthCheck() bool {
	err := d.db.Ping()
	if err != nil {
		log.Printf("Database health check failed: %v", err)
		return false
	}
	return true
}
