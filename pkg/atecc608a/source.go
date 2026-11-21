package atecc608a

// EntropySource abstracts hardware and emulated TRNG backends.
type EntropySource interface {
	GenerateRandom() ([]byte, error)
	HealthCheck() bool
	Close() error
	Mode() string
	DeviceID() string
}
