package atecc608a

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/d2r2/go-i2c"
	goi2clogger "github.com/d2r2/go-logger"
)

const (
	DefaultI2CAddress = 0x60 // Default I2C address for ATECC608A

	// ATECC608A commands (following Adafruit implementation)
	cmdInfo   = 0x30
	cmdRandom = 0x1B
	cmdIdle   = 0x02
	cmdSleep  = 0x01
	cmdWakeup = 0x00 // Wake command
	cmdLock   = 0x17 // Lock command
	cmdConfig = 0x47 // Config command
	cmdWrite  = 0x12 // Write command

	// Timing constants (from Adafruit implementation)
	wakeupDelay    = 1 * time.Millisecond  // 1ms like Adafruit
	randomExecTime = 50 * time.Millisecond // 50ms for random command
	infoExecTime   = 1 * time.Millisecond  // 1ms for info command
	configExecTime = 35 * time.Millisecond // Config command timing
	lockExecTime   = 32 * time.Millisecond // Lock command timing
	writeExecTime  = 26 * time.Millisecond // Write command timing

	// Retry constants
	maxRetries        = 10
	initialRetryDelay = 100 * time.Millisecond
)

// LogLevel represents the logging verbosity level
type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
)

// DeviceState represents the current health state of the device
type DeviceState int

const (
	DeviceStateUnknown DeviceState = iota
	DeviceStateHealthy
	DeviceStateFailed
	DeviceStateRecovering
)

var (
	currentLogLevel = LogLevelInfo // Default to Info
)

// SetLogLevel configures the logging verbosity
func SetLogLevel(level LogLevel) {
	currentLogLevel = level
}

// Log helper functions
func logDebug(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelDebug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func logInfo(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelInfo {
		log.Printf("[INFO] "+format, args...)
	}
}

func logWarn(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelWarn {
		log.Printf("[WARN] "+format, args...)
	}
}

func logError(format string, args ...interface{}) {
	if currentLogLevel >= LogLevelError {
		log.Printf("[ERROR] "+format, args...)
	}
}

// TLS configuration template based on Adafruit implementation
var CFG_TLS = []byte{
	0x01, 0x23, 0x00, 0x00, 0x00, 0x00, 0x50, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x71, 0x00,
	0xC0, 0x00, 0x55, 0x00, 0x83, 0x20, 0x87, 0x20, 0x87, 0x20, 0x87, 0x2F, 0x87, 0x2F, 0x8F, 0x8F,
	0x9F, 0x8F, 0xAF, 0x8F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0x00, 0x00, 0x55, 0x55, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x33, 0x00, 0x33, 0x00,
}

// Controller represents the ATECC608A device controller
type Controller struct {
	i2c              *i2c.I2C
	LastError        error
	mutex            sync.Mutex
	busNumber        int
	state            DeviceState
	autoConfig       bool
	recoveryInFlight bool
	recoveryMutex    sync.Mutex
}

// NewController creates a new ATECC608A controller
func NewController(busNumber int) (*Controller, error) {
	// Set log level from environment
	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr != "" {
		switch logLevelStr {
		case "DEBUG":
			SetLogLevel(LogLevelDebug)
			if err := goi2clogger.ChangePackageLogLevel("i2c", goi2clogger.DebugLevel); err != nil {
				logInfo("Failed to set i2c logger level: %v", err)
			}
		case "INFO":
			SetLogLevel(LogLevelInfo)
			if err := goi2clogger.ChangePackageLogLevel("i2c", goi2clogger.InfoLevel); err != nil {
				logInfo("Failed to set i2c logger level: %v", err)
			}
		case "WARN":
			SetLogLevel(LogLevelWarn)
			if err := goi2clogger.ChangePackageLogLevel("i2c", goi2clogger.WarnLevel); err != nil {
				logInfo("Failed to set i2c logger level: %v", err)
			}
		case "ERROR":
			SetLogLevel(LogLevelError)
			if err := goi2clogger.ChangePackageLogLevel("i2c", goi2clogger.ErrorLevel); err != nil {
				logInfo("Failed to set i2c logger level: %v", err)
			}
		default:
			SetLogLevel(LogLevelInfo)
			if err := goi2clogger.ChangePackageLogLevel("i2c", goi2clogger.InfoLevel); err != nil {
				logInfo("Failed to set i2c logger level: %v", err)
			}
		}
	} else {
		// Disable i2c library logging by default in production
		if err := goi2clogger.ChangePackageLogLevel("i2c", goi2clogger.FatalLevel); err != nil {
			logInfo("Failed to set i2c logger level: %v", err)
		}
	}

	// Optionally disable i2c logging output completely for production
	var logOutput *os.File
	if logLevelStr == "ERROR" || logLevelStr == "WARN" {
		logOutput = os.Stderr
		log.SetOutput(io.Discard)
	}

	logDebug("Container I2C Debug - UID: %d, GID: %d", os.Getuid(), os.Getgid())

	// Check if I2C device exists
	if _, err := os.Stat("/dev/i2c-1"); os.IsNotExist(err) {
		if logOutput != nil {
			log.SetOutput(logOutput)
		}
		return nil, fmt.Errorf("I2C device /dev/i2c-1 does not exist in container")
	}

	// Check I2C device permissions
	info, err := os.Stat("/dev/i2c-1")
	if err == nil {
		logDebug("I2C device permissions: %s", info.Mode())
	}

	logInfo("Initializing I2C connection to 0x%02x on bus %d", DefaultI2CAddress, busNumber)
	i2c, err := i2c.NewI2C(DefaultI2CAddress, busNumber)
	if err != nil {
		if logOutput != nil {
			log.SetOutput(logOutput)
		}
		return nil, fmt.Errorf("failed to initialize I2C: %w", err)
	}

	// Restore log output after i2c init
	if logOutput != nil {
		log.SetOutput(logOutput)
	}

	controller := &Controller{
		i2c:              i2c,
		LastError:        nil,
		busNumber:        busNumber,
		state:            DeviceStateUnknown,
		autoConfig:       true, // Enable auto-configuration by default
		recoveryInFlight: false,
	}

	// Read environment variable to check if auto-config is disabled
	if val, ok := os.LookupEnv("DISABLE_AUTO_CONFIG"); ok && val == "true" {
		controller.autoConfig = false
		logInfo("Auto-configuration disabled by environment variable")
	}

	// Initialize the device
	if err := controller.initialize(); err != nil {
		logError("Device initialization failed: %v", err)
		controller.setState(DeviceStateFailed)
		// Start recovery in background
		go controller.startRecovery()
		// Don't return error - let the controller exist but in failed state
	} else {
		controller.setState(DeviceStateHealthy)
		logWarn("ATECC608A device initialized successfully")
	}

	return controller, nil
}

// setState changes the device state and logs only on state changes
func (c *Controller) setState(newState DeviceState) {
	c.mutex.Lock()
	oldState := c.state
	c.state = newState
	c.mutex.Unlock()

	if oldState != newState {
		switch newState {
		case DeviceStateHealthy:
			logWarn("ATECC608A device state changed to HEALTHY")
		case DeviceStateFailed:
			logError("ATECC608A device state changed to FAILED")
		case DeviceStateRecovering:
			logWarn("ATECC608A device state changed to RECOVERING")
		case DeviceStateUnknown:
			logWarn("ATECC608A device state changed to UNKNOWN")
		}
	}
}

// getState returns the current device state
func (c *Controller) getState() DeviceState {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.state
}

// startRecovery initiates the recovery process with exponential backoff
func (c *Controller) startRecovery() {
	c.recoveryMutex.Lock()
	if c.recoveryInFlight {
		c.recoveryMutex.Unlock()
		return
	}
	c.recoveryInFlight = true
	c.recoveryMutex.Unlock()

	c.setState(DeviceStateRecovering)

	delay := initialRetryDelay
	for attempt := 1; attempt <= maxRetries; attempt++ {
		logWarn("ATECC608A recovery attempt %d/%d (delay: %v)", attempt, maxRetries, delay)
		time.Sleep(delay)

		// Try to reinitialize
		if err := c.reinitialize(); err != nil {
			logWarn("ATECC608A recovery attempt %d/%d failed: %v", attempt, maxRetries, err)
			delay *= 2 // Exponential backoff
		} else {
			logWarn("ATECC608A recovery successful on attempt %d/%d", attempt, maxRetries)
			c.setState(DeviceStateHealthy)
			c.recoveryMutex.Lock()
			c.recoveryInFlight = false
			c.recoveryMutex.Unlock()
			return
		}
	}

	// All retries exhausted
	logError("ATECC608A recovery failed after %d attempts. Exiting.", maxRetries)
	c.setState(DeviceStateFailed)
	c.recoveryMutex.Lock()
	c.recoveryInFlight = false
	c.recoveryMutex.Unlock()

	// Exit the process
	os.Exit(1)
}

// reinitialize attempts to reinitialize the device
func (c *Controller) reinitialize() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Close existing connection if any
	if c.i2c != nil {
		c.sleep()
		_ = c.i2c.Close()
	}

	// Recreate I2C connection
	i2c, err := i2c.NewI2C(DefaultI2CAddress, c.busNumber)
	if err != nil {
		return fmt.Errorf("failed to reinitialize I2C: %w", err)
	}

	c.i2c = i2c

	// Initialize the device
	return c.initializeUnlocked()
}

// initialize configures the device for random number generation
func (c *Controller) initialize() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.initializeUnlocked()
}

// initializeUnlocked performs initialization without acquiring the mutex
func (c *Controller) initializeUnlocked() error {
	// Wake up the device first
	c.wakeup()

	// Check device info
	logInfo("Checking device information...")
	if err := c.sendCommand(cmdInfo, 0x00, 0x0000, nil); err != nil {
		return fmt.Errorf("failed to send info command: %w", err)
	}

	infoResponse, err := c.getResponse(4, infoExecTime)
	if err != nil {
		return fmt.Errorf("failed to get info response: %w", err)
	}

	if len(infoResponse) < 4 {
		return fmt.Errorf("info response too short: %d bytes", len(infoResponse))
	}

	logDebug("Device info: %x", infoResponse)

	// Check if device is locked by reading config zone lock bytes
	isLocked, err := c.isDeviceLockedUnlocked()
	if err != nil {
		logWarn("Could not determine lock status: %v", err)
	}

	// Try to read the device's current configuration
	logInfo("Reading device configuration...")
	if err := c.sendCommand(cmdConfig, 0x02, 0x0000, nil); err != nil {
		return fmt.Errorf("failed to send config read command: %w", err)
	}

	configResponse, err := c.getResponse(32, configExecTime)
	if err != nil {
		logWarn("Failed to read configuration: %v", err)
		// Continue anyway, we'll try to configure
	} else {
		logDebug("Current configuration: %x", configResponse)
	}

	// If device is not locked and auto-config is enabled, try to configure it
	if !isLocked && c.autoConfig {
		logInfo("Device not locked. Auto-configuration is enabled.")
		if err := c.configureDeviceUnlocked(); err != nil {
			logWarn("Failed to auto-configure device: %v", err)
			logInfo("The device will continue to operate without being locked")
		} else {
			logInfo("Device successfully configured and locked!")
		}
	} else if !isLocked {
		logInfo("Device not locked but auto-configuration is disabled.")
	}

	// Put device in idle
	c.idle()

	return nil
}

// isDeviceLockedUnlocked checks lock status without acquiring mutex
func (c *Controller) isDeviceLockedUnlocked() (bool, error) {
	// Read the lock bytes from config zone (address 0x15)
	if err := c.sendCommand(cmdConfig, 0x02, 0x0015, nil); err != nil {
		return false, fmt.Errorf("failed to send config read command: %w", err)
	}

	response, err := c.getResponse(4, configExecTime)
	if err != nil {
		return false, fmt.Errorf("failed to get lock status: %w", err)
	}

	// Check lock bytes (0x00 = locked)
	return response[2] == 0x00 && response[3] == 0x00, nil
}

// configureDeviceUnlocked configures without acquiring mutex
func (c *Controller) configureDeviceUnlocked() error {
	logInfo("Starting device configuration...")

	// Ask for user confirmation to protect against accidental locking
	logWarn("About to configure and lock the ATECC608A device.")
	logWarn("This operation is IRREVERSIBLE and will permanently configure the device.")
	logWarn("If you want to proceed, set FORCE_CONFIG=true in environment variables.")

	// Check for force configuration environment variable
	if val, ok := os.LookupEnv("FORCE_CONFIG"); !ok || val != "true" {
		logWarn("Configuration aborted. FORCE_CONFIG environment variable not set to 'true'.")
		return fmt.Errorf("configuration aborted due to missing confirmation")
	}

	logInfo("FORCE_CONFIG is set. Proceeding with device configuration...")

	// Write configuration (skipping first 16 bytes which are read-only)
	for i := 16; i < 128; i += 4 {
		// Skip certain addresses that can't be written
		if i == 84 {
			continue
		}

		// Get the 4-byte block to write
		blockData := make([]byte, 4)
		end := i + 4
		if end > len(CFG_TLS) {
			end = len(CFG_TLS)
		}
		copy(blockData, CFG_TLS[i:end])

		// Write the block - safe conversion as i/4 is always < 32
		addr := uint16(i / 4) // #nosec G115 - i is bounded 16-128, i/4 always fits in uint16
		logDebug("Writing block at address %d: %x", i, blockData)
		if err := c.sendCommand(cmdWrite, 0x00, addr, blockData); err != nil {
			return fmt.Errorf("failed to write configuration block: %w", err)
		}

		// Wait for write to complete
		time.Sleep(writeExecTime)

		// Check status
		status, err := c.getResponse(1, 1*time.Millisecond)
		if err != nil {
			return fmt.Errorf("failed to get write status: %w", err)
		}

		if len(status) > 0 && status[0] != 0x00 {
			return fmt.Errorf("write failed with status: %x", status[0])
		}
	}

	// Lock the configuration zone
	logInfo("Configuration written. Now locking the configuration zone...")

	// Send lock command for config zone (0x00)
	if err := c.sendCommand(cmdLock, 0x80, 0x0000, nil); err != nil {
		return fmt.Errorf("failed to send lock command: %w", err)
	}

	// Wait for lock to complete
	time.Sleep(lockExecTime)

	// Check status
	status, err := c.getResponse(1, 1*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to get lock status: %w", err)
	}

	if len(status) > 0 && status[0] != 0x00 {
		return fmt.Errorf("lock failed with status: %x", status[0])
	}

	logInfo("Device configuration zone successfully locked!")
	return nil
}

// wakeup follows Adafruit's approach - always wake before operations
func (c *Controller) wakeup() {
	// Adafruit approach: try general call but ignore errors
	wakeupI2C, err := i2c.NewI2C(0x00, c.busNumber)
	if err == nil {
		// Try to send wakeup, error is expected and can be ignored per Adafruit implementation
		if _, err := wakeupI2C.WriteBytes([]byte{0x00}); err != nil {
			logInfo("Wakeup signal error (expected): %v", err)
		}
		if err := wakeupI2C.Close(); err != nil {
			logInfo("Wakeup I2C close error: %v", err)
		}
	}
	// Always wait after wakeup attempt
	time.Sleep(wakeupDelay)
}

// idle puts device in idle mode (following Adafruit)
func (c *Controller) idle() {
	if _, err := c.i2c.WriteBytes([]byte{cmdIdle}); err != nil {
		logError("I2C idle command failed: %v", err)
	}
	time.Sleep(wakeupDelay)
}

// sleep puts device in sleep mode (following Adafruit)
func (c *Controller) sleep() {
	if _, err := c.i2c.WriteBytes([]byte{cmdSleep}); err != nil {
		logError("I2C sleep command failed: %v", err)
	}
	time.Sleep(wakeupDelay)
}

// sendCommand builds and sends a command packet following Adafruit's structure
func (c *Controller) sendCommand(opcode byte, param1 byte, param2 uint16, data []byte) error {
	// Build command packet like Adafruit
	commandPacket := make([]byte, 8+len(data))

	// Word address
	commandPacket[0] = 0x03
	// Count (total packet length - 1)
	commandPacket[1] = byte(len(commandPacket) - 1)
	// Opcode
	commandPacket[2] = opcode
	// Param1
	commandPacket[3] = param1
	// Param2 (16-bit, little endian)
	commandPacket[4] = byte(param2 & 0xFF)
	commandPacket[5] = byte(param2 >> 8)

	// Data
	copy(commandPacket[6:], data)

	// Calculate CRC on everything except word address and CRC itself
	crc := c.calculateAdafruitCRC(commandPacket[1 : len(commandPacket)-2])
	commandPacket[len(commandPacket)-2] = byte(crc & 0xFF)
	commandPacket[len(commandPacket)-1] = byte(crc >> 8)

	// Always wake up before sending command
	c.wakeup()

	logDebug("Sending command packet: %x", commandPacket)

	// Send command
	_, err := c.i2c.WriteBytes(commandPacket)
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	time.Sleep(wakeupDelay)
	return nil
}

// getResponse reads response with retries (following Adafruit)
func (c *Controller) getResponse(expectedLength int, execTime time.Duration) ([]byte, error) {
	// Wait for command execution
	time.Sleep(execTime)

	// Try to read response with retries
	response := make([]byte, expectedLength+3) // +3 for length byte and 2 CRC bytes
	var err error

	logDebug("Attempting to read %d byte response", len(response))

	for retry := 0; retry < 20; retry++ {
		_, err = c.i2c.ReadBytes(response)
		if err == nil {
			logDebug("Read successful on retry %d: %x", retry, response)
			break
		}
		logDebug("Retry %d failed: %v", retry, err)
		time.Sleep(wakeupDelay)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read response after retries: %w", err)
	}

	// Return data portion (skip length byte and CRC)
	return response[1 : len(response)-2], nil
}

// calculateAdafruitCRC implements Adafruit's exact CRC calculation
func (c *Controller) calculateAdafruitCRC(data []byte) uint16 {
	if len(data) == 0 {
		return 0
	}

	polynom := uint16(0x8005)
	crc := uint16(0x0000)

	for _, b := range data {
		for shift := 0; shift < 8; shift++ {
			dataBit := uint16((b >> shift) & 1)
			crcBit := (crc >> 15) & 1
			crc <<= 1
			crc &= 0xFFFF
			if dataBit != crcBit {
				crc ^= polynom
				crc &= 0xFFFF
			}
		}
	}
	return crc & 0xFFFF
}

// GenerateRandom generates random bytes - ONLY from ATECC608A, no fallback
func (c *Controller) GenerateRandom() ([]byte, error) {
	// Check device state first - fail fast if not healthy
	if c.getState() != DeviceStateHealthy {
		return nil, fmt.Errorf("ATECC608A device not healthy (state: %d)", c.getState())
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Send random command (opcode 0x1B, param1 0x00, param2 0x0000)
	err := c.sendCommand(cmdRandom, 0x00, 0x0000, nil)
	if err != nil {
		c.LastError = fmt.Errorf("failed to send random command: %w", err)
		// Trigger recovery
		go c.handleDeviceFailure()
		return nil, c.LastError
	}

	// Get response (32 bytes of random data)
	response, err := c.getResponse(32, randomExecTime)
	if err != nil {
		c.LastError = fmt.Errorf("failed to get random response: %w", err)
		// Trigger recovery
		go c.handleDeviceFailure()
		return nil, c.LastError
	}

	logDebug("Random response length: %d bytes", len(response))

	if len(response) > 1 {
		// Skip the status byte and use the rest as random data
		randomData := response[1:]

		// VALIDATION: Check if data is actually random (not error patterns)
		if isRepeatingPattern(randomData) {
			c.idle()
			c.LastError = fmt.Errorf("ATECC608A returned invalid/repeating pattern - hardware failure detected")
			// Trigger recovery
			go c.handleDeviceFailure()
			return nil, c.LastError
		}

		c.idle()
		return randomData, nil
	}

	// Response too short
	c.idle()
	c.LastError = fmt.Errorf("ATECC608A response too short: %d bytes", len(response))
	// Trigger recovery
	go c.handleDeviceFailure()
	return nil, c.LastError
}

// handleDeviceFailure marks device as failed and starts recovery
func (c *Controller) handleDeviceFailure() {
	if c.getState() == DeviceStateHealthy {
		c.setState(DeviceStateFailed)
		c.startRecovery()
	}
}

// isRepeatingPattern checks if data has suspicious repeating patterns
func isRepeatingPattern(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	// Check if all bytes are the same (common error pattern)
	allSame := true
	firstByte := data[0]
	for _, b := range data {
		if b != firstByte {
			allSame = false
			break
		}
	}
	if allSame {
		logError("Hardware failure: all bytes identical (0x%02x)", firstByte)
		return true
	}

	// Check for 0xFF repeating (common I2C error - bus not responding)
	ffCount := 0
	for _, b := range data {
		if b == 0xFF {
			ffCount++
		}
	}
	if float64(ffCount)/float64(len(data)) > 0.9 {
		logError("Hardware failure: 0xFF pattern detected (%d/%d bytes)", ffCount, len(data))
		return true
	}

	return false
}

// Close closes the I2C connection
func (c *Controller) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Put device to sleep before closing
	c.sleep()

	return c.i2c.Close()
}

// HealthCheck verifies the device is responsive
func (c *Controller) HealthCheck() bool {
	// Check state first
	if c.getState() != DeviceStateHealthy {
		return false
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Wake up the device
	c.wakeup()

	// Send info command
	err := c.sendCommand(cmdInfo, 0x00, 0x0000, nil)
	if err != nil {
		c.LastError = fmt.Errorf("health check failed (send): %w", err)
		// Trigger recovery
		go c.handleDeviceFailure()
		return false
	}

	// Try to get response
	_, err = c.getResponse(4, infoExecTime)
	if err != nil {
		c.LastError = fmt.Errorf("health check failed (receive): %w", err)
		// Trigger recovery
		go c.handleDeviceFailure()
		return false
	}

	// Put device back to idle
	c.idle()

	return true
}

// SetAutoConfig enables or disables auto-configuration
func (c *Controller) SetAutoConfig(enabled bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.autoConfig = enabled
}

// GetState returns the current device state (for API exposure)
func (c *Controller) GetState() DeviceState {
	return c.getState()
}

// IsHealthy returns true if device is in healthy state
func (c *Controller) IsHealthy() bool {
	return c.getState() == DeviceStateHealthy
}
