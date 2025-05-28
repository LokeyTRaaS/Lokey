package atecc608a

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/d2r2/go-i2c"
)

const (
	Atecc608Address = 0x60 // ATECC608A I2C address
	RandomCommand   = 0x1B // Random command opcode
	ShaCommand      = 0x47 // SHA command opcode
	WakeCommand     = 0x00 // Wake command
	WakeParameter   = 0x11 // Wake parameter
)

type Controller struct {
	i2c         *i2c.I2C
	initialWake bool
	LastError   error
}

// NewController creates a new ATECC608A controller
func NewController(i2cBusNumber int) (*Controller, error) {
	i2c, err := i2c.NewI2C(Atecc608Address, i2cBusNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C bus: %w", err)
	}

	controller := &Controller{
		i2c:         i2c,
		initialWake: false,
		LastError:   nil,
	}

	// Wake up the device on initialization
	err = controller.WakeUp()
	if err != nil {
		i2c.Close()
		return nil, fmt.Errorf("failed to wake up ATECC608A: %w", err)
	}

	controller.initialWake = true
	return controller, nil
}

// WakeUp wakes up the ATECC608A device
func (c *Controller) WakeUp() error {
	err := c.writeCommand(WakeCommand, []byte{WakeParameter})
	if err != nil {
		c.LastError = err
		return fmt.Errorf("wake command failed: %w", err)
	}

	// Wait for device to wake up
	time.Sleep(10 * time.Millisecond)
	return nil
}

// writeCommand sends a command to the ATECC608A
func (c *Controller) writeCommand(command byte, data []byte) error {
	buf := append([]byte{command}, data...)
	_, err := c.i2c.WriteBytes(buf)
	if err != nil {
		c.LastError = err
		return err
	}
	return nil
}

// readResponse reads a response from the ATECC608A
func (c *Controller) readResponse(length int) ([]byte, error) {
	data, err := c.i2c.ReadBytes([]byte{byte(length)})
	if err != nil {
		c.LastError = err
		return nil, err
	}
	return data, nil
}

// GenerateRandomBytes generates random bytes using the ATECC608A's TRNG
func (c *Controller) GenerateRandomBytes() ([]byte, error) {
	// Ensure device is awake
	if !c.initialWake {
		err := c.WakeUp()
		if err != nil {
			return nil, fmt.Errorf("failed to wake up device: %w", err)
		}
	}

	// Send Random command
	err := c.writeCommand(0x03, []byte{RandomCommand})
	if err != nil {
		return nil, fmt.Errorf("failed to send Random command: %w", err)
	}

	// Wait for processing
	time.Sleep(5 * time.Millisecond)

	// Read 32-byte random number
	randomData, err := c.readResponse(32)
	if err != nil {
		return nil, fmt.Errorf("failed to read random data: %w", err)
	}

	if len(randomData) != 32 {
		return nil, errors.New("invalid random data length")
	}

	return randomData, nil
}

// GenerateHashFromRandom generates a SHA-256 hash of random data using the device's hardware
func (c *Controller) GenerateHashFromRandom() ([]byte, error) {
	// Generate random data first
	randomData, err := c.GenerateRandomBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random data: %w", err)
	}

	// Start SHA computation
	err = c.writeCommand(0x03, []byte{ShaCommand})
	if err != nil {
		return nil, fmt.Errorf("failed to send SHA command: %w", err)
	}

	// Wait for processing
	time.Sleep(5 * time.Millisecond)

	// Send Random data to SHA command
	err = c.writeCommand(0x04, randomData)
	if err != nil {
		return nil, fmt.Errorf("failed to send data to SHA command: %w", err)
	}

	// Wait for processing
	time.Sleep(10 * time.Millisecond)

	// Read SHA digest
	shaDigest, err := c.readResponse(32)
	if err != nil {
		return nil, fmt.Errorf("failed to read SHA digest: %w", err)
	}

	if len(shaDigest) != 32 {
		return nil, errors.New("invalid SHA digest length")
	}

	return shaDigest, nil
}

// Close closes the connection to the ATECC608A
func (c *Controller) Close() error {
	return c.i2c.Close()
}

// HealthCheck checks if the ATECC608A device is responsive
func (c *Controller) HealthCheck() bool {
	// Try to wake up the device
	err := c.WakeUp()
	if err != nil {
		log.Printf("ATECC608A health check failed: %v", err)
		return false
	}

	// Try to generate random data as a test
	_, err = c.GenerateRandomBytes()
	if err != nil {
		log.Printf("ATECC608A random generation failed: %v", err)
		return false
	}

	return true
}
