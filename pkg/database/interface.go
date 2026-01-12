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
	TRNG        DataSourceStats `json:"trng"`
	Fortuna     DataSourceStats `json:"fortuna"`
	VirtIO      DataSourceStats `json:"virtio"`
	Database    Stats           `json:"database"`
	TRNGQuality QualityMetrics  `json:"trng_quality"`
}

// QualityMetrics represents TRNG randomness quality metrics
type QualityMetrics struct {
	Monobit        MonobitMetrics        `json:"monobit"`
	RepetitionCount RepetitionCountMetrics `json:"repetition_count"`
	APT            APTMetrics            `json:"apt"`
}

// MonobitMetrics represents monobit (frequency) test results
type MonobitMetrics struct {
	Zeros   int64   `json:"zeros"`
	Ones    int64   `json:"ones"`
	Total   int64   `json:"total"`
	Average float64 `json:"average"`
}

// RepetitionCountMetrics represents repetition count test results
type RepetitionCountMetrics struct {
	Failures   int64 `json:"failures"`
	CurrentRun int   `json:"current_run"`
	LastValue  int   `json:"last_value"`
}

// APTMetrics represents adaptive proportion test results
type APTMetrics struct {
	WindowSize       int   `json:"window_size"`
	Cutoff           int   `json:"cutoff"`
	BiasCount        int64 `json:"bias_count"`
	SamplesProcessed int64 `json:"samples_processed"`
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

// VirtIOData represents VirtIO-generated random data
type VirtIOData struct {
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

	// VirtIO operations
	StoreVirtIOData(data []byte) error
	GetVirtIOData(limit, offset int, consume bool) ([][]byte, error)

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
	UpdateQueueSizes(trngSize, fortunaSize, virtioSize int) error
	HealthCheck() bool
	Close() error
}
