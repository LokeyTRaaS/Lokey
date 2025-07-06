package database

import "time"

// Request represents a random number generation request
type Request struct {
	ID        string    `json:"id"`
	Size      int       `json:"size"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Result    []byte    `json:"result,omitempty"`
	Source    string    `json:"source"`
}

// UsageStat represents usage statistics
type UsageStat struct {
	Source    string    `json:"source"`
	BytesUsed int64     `json:"bytes_used"`
	Requests  int64     `json:"requests"`
	Timestamp time.Time `json:"timestamp"`
}

// TRNGData represents true random number generator data
type TRNGData struct {
	ID        uint64    `json:"id"`
	Hash      []byte    `json:"hash"`
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
	// Queue management
	EnqueueTRNGRequest(request Request) error
	DequeueTRNGRequest() (Request, error)
	EnqueueFortunaRequest(request Request) error
	DequeueFortunaRequest() (Request, error)

	// TRNG operations
	StoreTRNGHash(hash []byte) error
	GetTRNGHashes(limit, offset int, consume bool) ([][]byte, error)

	// Fortuna operations
	StoreFortunaData(data []byte) error
	GetFortunaData(limit, offset int, consume bool) ([][]byte, error)

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
