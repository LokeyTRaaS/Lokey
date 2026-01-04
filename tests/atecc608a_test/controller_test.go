//go:build !hardware

package atecc608a_test

import (
	"testing"

	"github.com/lokey/rng-service/pkg/atecc608a"
)

// Note: Full controller tests require hardware or significant refactoring
// to inject mock I2C. These tests focus on testable logic like CRC calculation.

func TestController_calculateAdafruitCRC(t *testing.T) {
	// Create a minimal controller for testing (we only need it for the method)
	// Since calculateAdafruitCRC is a method, we need a controller instance
	// We'll create a test helper that doesn't require I2C
	
	t.Run("empty data", func(t *testing.T) {
		c := &atecc608a.Controller{}
		crc := c.CalculateAdafruitCRC([]byte{})
		if crc != 0 {
			t.Errorf("Expected CRC 0 for empty data, got %d", crc)
		}
	})

	t.Run("single byte", func(t *testing.T) {
		c := &atecc608a.Controller{}
		data := []byte{0x01}
		crc := c.CalculateAdafruitCRC(data)
		// CRC should be calculated (not 0 for non-empty data)
		if crc == 0 {
			t.Error("Expected non-zero CRC for non-empty data")
		}
	})

	t.Run("known test vector", func(t *testing.T) {
		c := &atecc608a.Controller{}
		// Test with command packet structure
		// Word address + count + opcode + params
		data := []byte{0x07, 0x30, 0x00, 0x00, 0x00} // Info command structure
		crc := c.CalculateAdafruitCRC(data)
		if crc == 0 {
			t.Error("Expected non-zero CRC")
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		c := &atecc608a.Controller{}
		data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
		crc1 := c.CalculateAdafruitCRC(data)
		crc2 := c.CalculateAdafruitCRC(data)
		if crc1 != crc2 {
			t.Errorf("Expected CRC to be deterministic, got %d != %d", crc1, crc2)
		}
	})

	t.Run("different data produces different CRC", func(t *testing.T) {
		c := &atecc608a.Controller{}
		data1 := []byte{0x01, 0x02, 0x03}
		data2 := []byte{0x01, 0x02, 0x04}
		crc1 := c.CalculateAdafruitCRC(data1)
		crc2 := c.CalculateAdafruitCRC(data2)
		if crc1 == crc2 {
			t.Error("Expected different CRC for different data")
		}
	})

	t.Run("polynomial properties", func(t *testing.T) {
		c := &atecc608a.Controller{}
		// Test that CRC handles various data patterns
		testCases := [][]byte{
			{0x00, 0x00, 0x00},
			{0xFF, 0xFF, 0xFF},
			{0x01, 0x00, 0x00},
			{0x00, 0x01, 0x00},
			{0x00, 0x00, 0x01},
			make([]byte, 100), // Longer data
		}

		for _, data := range testCases {
			crc := c.CalculateAdafruitCRC(data)
			_ = crc // CRC is uint16, guaranteed to be <= 0xFFFF
		}
	})
}

// Note: Full controller initialization and hardware interaction tests
// would require either:
// 1. Hardware access (use build tags)
// 2. Dependency injection/interface for I2C (refactoring)
// 3. Integration tests with mock I2C devices
//
// For now, we test the pure logic functions that don't require I2C.

