package database

import "time"

// UsageStat represents usage statistics
type UsageStat struct {
	Source    string    `json:"source"`
	BytesUsed int64     `json:"bytes_used"`
	Requests  int64     `json:"requests"`
	Timestamp time.Time `json:"timestamp"`
}

// DetailedStats represents comprehensive system statistics
type DetailedStats struct {
	TRNG     DataSourceStats `json:"trng"`
	Fortuna  DataSourceStats `json:"fortuna"`
	Database Stats           `json:"database"`
}

// DataSourceStats represents statistics for a specific data source
type DataSourceStats struct {
	PollingCount    int64   `json:"polling_count"`    // How many times data was polled/retrieved
	QueueCurrent    int     `json:"queue_current"`    // Current items in queue
	QueueCapacity   int     `json:"queue_capacity"`   // Maximum queue size
	QueuePercentage float64 `json:"queue_percentage"` // Percentage of queue filled
	QueueDropped    int64   `json:"queue_dropped"`    // Items dropped when queue was full
	ConsumedCount   int64   `json:"consumed_count"`   // Total items consumed
	UnconsumedCount int     `json:"unconsumed_count"` // Current unconsumed items
	TotalGenerated  int64   `json:"total_generated"`  // Total items ever generated
}

// Stats represents database-related statistics.
type Stats struct {
	SizeBytes int64  `json:"size_bytes"`
	SizeHuman string `json:"size_human"`
	Path      string `json:"path"`
}

// TRNGData represents true random number generator data
type TRNGData struct {
	ID        uint64    `json:"id"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Consumed  bool      `json:"consumed"`
}

// FortunaData represents Fortuna-generated random data
type FortunaData struct {
	ID        uint64    `json:"id"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Consumed  bool      `json:"consumed"`
}

// DBHandler defines the interface for database operations
type DBHandler interface {
	// TRNG operations
	StoreTRNGData(data []byte) error
	GetTRNGData(limit, offset int, consume bool) ([][]byte, error)

	// Fortuna operations
	StoreFortunaData(data []byte) error
	GetFortunaData(limit, offset int, consume bool) ([][]byte, error)

	// Enhanced statistics
	GetDetailedStats() (*DetailedStats, error)
	IncrementPollingCount(source string) error
	IncrementDroppedCount(source string) error
	GetDatabaseSize() (int64, error)
	GetDatabasePath() string

	// Statistics
	RecordRNGUsage(source string, bytesUsed int64) error
	GetRNGStatistics(source string, start, end time.Time) ([]UsageStat, error)
	GetStats() (map[string]interface{}, error)

	// Health and utility methods
	GetQueueInfo() (map[string]int, error)
	UpdateQueueSizes(trngSize, fortunaSize int) error
	HealthCheck() bool
	Close() error
}
