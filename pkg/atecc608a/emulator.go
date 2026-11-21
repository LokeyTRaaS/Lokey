package atecc608a

import (
	"crypto/rand"
	"fmt"
	"os"
)

const emulatedRandomBytes = 32

// EmulatedController provides a crypto/rand-backed TRNG stand-in for local testing.
type EmulatedController struct {
	deviceID string
}

// NewEmulatedController creates an emulated entropy source (no I2C required).
func NewEmulatedController() *EmulatedController {
	deviceID := os.Getenv("DEVICE_ID")
	if deviceID == "" {
		deviceID = "emulated"
	}
	return &EmulatedController{deviceID: deviceID}
}

// GenerateRandom returns 32 bytes from crypto/rand.
func (e *EmulatedController) GenerateRandom() ([]byte, error) {
	buf := make([]byte, emulatedRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("emulated generate: %w", err)
	}
	return buf, nil
}

// HealthCheck always succeeds for the emulator.
func (e *EmulatedController) HealthCheck() bool {
	return true
}

// Close is a no-op for the emulator.
func (e *EmulatedController) Close() error {
	return nil
}

// Mode identifies the emulated backend.
func (e *EmulatedController) Mode() string {
	return "emulated"
}

// DeviceID returns the configured device identifier.
func (e *EmulatedController) DeviceID() string {
	return e.deviceID
}
