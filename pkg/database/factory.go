package database

import "os"

// NewDBHandler creates a new database handler based on the implementation
func NewDBHandler(dbPath string, trngQueueSize, fortunaQueueSize int) (DBHandler, error) {
	// Check environment variable to choose implementation
	if impl, ok := os.LookupEnv("DB_IMPLEMENTATION"); ok && impl == "channel" {
		// Use channel-based implementation
		return NewChannelDBHandler(dbPath, trngQueueSize, fortunaQueueSize)
	}

	// Default: Use BoltDB implementation
	return NewBoltDBHandler(dbPath, trngQueueSize, fortunaQueueSize)
}
