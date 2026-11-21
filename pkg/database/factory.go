package database

// NewDBHandler creates the in-memory channel-backed database handler.
func NewDBHandler(trngQueueSize, fortunaQueueSize, virtioQueueSize int) (DBHandler, error) {
	return NewChannelDBHandler(trngQueueSize, fortunaQueueSize, virtioQueueSize)
}
