package atecc608a

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/d2r2/go-i2c"
)

const (
	DefaultI2CAddress = 0x60 // Default I2C address for ATECC608A

	// ATECC608A commands
	cmdWakeUp = 0x01
	cmdSleep  = 0x02
	cmdRandom = 0x1B
	cmdInfo   = 0x30

	// Response codes
	respSuccess        = 0x00
	respChecksumError  = 0x01
	respParseError     = 0x03
	respExecutionError = 0x0F

	// Timing constants
	wakeupDelay    = 1500 * time.Microsecond
	executionDelay = 50 * time.Millisecond
)

// Controller represents the ATECC608A device controller
type Controller struct {
	i2c         *i2c.I2C
	initialWake bool
	LastError   error
	mutex       sync.Mutex
}

// NewController creates a new ATECC608A controller
func NewController(busNumber int) (*Controller, error) {
	// Initialize I2C device
	i2c, err := i2c.NewI2C(DefaultI2CAddress, busNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize I2C: %w", err)
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
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Send wakeup command
	_, err := c.i2c.WriteBytes([]byte{0x00})
	if err != nil {
		return fmt.Errorf("failed to send wakeup command: %w", err)
	}

	// Wait for device to wake up
	time.Sleep(wakeupDelay)

	return nil
}

// Sleep puts the ATECC608A device into low-power mode
func (c *Controller) Sleep() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Send sleep command
	_, err := c.i2c.WriteBytes([]byte{cmdSleep})
	if err != nil {
		return fmt.Errorf("failed to send sleep command: %w", err)
	}

	return nil
}

// GenerateRandom generates random bytes from the ATECC608A
func (c *Controller) GenerateRandom() ([]byte, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Ensure device is awake
	err := c.WakeUp()
	if err != nil {
		return nil, err
	}

	// Send random command (implementation simplified for example)
	_, err = c.i2c.WriteBytes([]byte{cmdRandom, 0x00, 0x00})
	if err != nil {
		return nil, fmt.Errorf("failed to send random command: %w", err)
	}

	// Wait for execution
	time.Sleep(executionDelay)

	// Read response (32 bytes of random data + status byte)
	buf := make([]byte, 33)
	_, err = c.i2c.ReadBytes(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read random data: %w", err)
	}

	// Check status code (first byte)
	if buf[0] != respSuccess {
		return nil, fmt.Errorf("device returned error code: %d", buf[0])
	}

	// Return the random data (excluding status byte)
	return buf[1:33], nil
}

// GenerateHashFromRandom generates a SHA-256 hash from random data
func (c *Controller) GenerateHashFromRandom() ([]byte, error) {
	// Get random data from the device
	randomData, err := c.GenerateRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random data: %w", err)
	}

	// Hash the random data using SHA-256
	hash := sha256.Sum256(randomData)

	return hash[:], nil
}

// Close closes the I2C connection
func (c *Controller) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Put device to sleep before closing
	err := c.Sleep()
	if err != nil {
		return fmt.Errorf("failed to put device to sleep: %w", err)
	}

	return c.i2c.Close()
}

// HealthCheck verifies the device is responsive
func (c *Controller) HealthCheck() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Try to wake up the device
	err := c.WakeUp()
	if err != nil {
		c.LastError = err
		return false
	}

	// Send info command to check if device is responsive
	_, err = c.i2c.WriteBytes([]byte{cmdInfo, 0x00})
	if err != nil {
		c.LastError = err
		return false
	}

	// Wait for execution
	time.Sleep(executionDelay)

	// Read response
	buf := make([]byte, 4)
	_, err = c.i2c.ReadBytes(buf)
	if err != nil {
		c.LastError = err
		return false
	}

	// Check status code
	if buf[0] != respSuccess {
		c.LastError = fmt.Errorf("device returned error code: %d", buf[0])
		return false
	}

	return true
}

// The helper functions below were removed as they are not currently used
// They can be reimplemented when needed
