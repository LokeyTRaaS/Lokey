package database

// NewDBHandler creates a new database handler based on the implementation
func NewDBHandler(dbPath string, trngQueueSize, fortunaQueueSize int) (DBHandler, error) {
	// Use BoltDB implementation
	return NewBoltDBHandler(dbPath, trngQueueSize, fortunaQueueSize)
}
