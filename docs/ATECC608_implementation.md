# ATECC608A Controller: Technical Implementation Guide

## Overview

The `controller.go` file implements a Go interface to the ATECC608A cryptographic chip for harvesting true random numbers. This implementation provides hardware-based true random number generation (TRNG) for the Lokey service, following the reference implementation from Adafruit's CircuitPython library.

## Call Flow Diagram

```
┌────────────────────────────────────────────────────────────────────┐
│                       Controller Service                           │
└───────────────────────────────┬────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     atecc608a.NewController()                       │
│                                                                     │
│  ┌─────────────────────┐    ┌───────────────────┐                   │
│  │  I2C Initialization │───▶│ Device Detection  │                   │
│  └─────────────────────┘    └─────────┬─────────┘                   │
│                                       │                             │
│                                       ▼                             │
│  ┌─────────────────────┐    ┌───────────────────┐                   │
│  │ Initialization      │◀───│ Device Wake-up    │                   │
│  └──────────┬──────────┘    └───────────────────┘                   │
│             │                                                       │
│             ▼                                                       │
│  ┌────────────────────────────────────────────┐                     │
│  │            Device Info Check               │                     │
│  └──────────┬─────────────────────────────────┘                     │
│             │                                                       │
│             ▼                                                       │
│  ┌────────────────────────────────────────────┐                     │
│  │            Check Lock Status               │───┐                 │
│  └──────────┬─────────────────────────────────┘   │                 │
│             │                                     │                 │
│             │  (if not locked)                    │                 │
│             ▼                                     │ (if locked)     │
│  ┌────────────────────────────────────────────┐   │                 │
│  │     Device Configuration (One-time)        │   │                 │
│  │ ┌─────────────────────┐ ┌───────────────┐  │   │                 │
│  │ │ Write Config Blocks │ │ Lock Config   │  │   │                 │
│  │ └─────────────────────┘ └───────────────┘  │   │                 │
│  └────────────────────────────────────────────┘   │                 │
│                             │                     │                 │
│                             ▼                     ▼                 │
│  ┌────────────────────────────────────────────────────────┐         │
│  │               Device Ready for Operation               │         │
│  └────────────────────────────────────────────────────────┘         │
└──────────────────────────────────────────────────────────-──────────┘
                                │
                                ▼
┌────────────────────────────────────────────────────────────────────┐
│                     Random Number Generation                        │
│                                                                     │
│  ┌─────────────────────┐     ┌───────────────────┐                  │
│  │  Device Wake-up     │────▶│ Send Random Cmd   │                  │
│  └─────────────────────┘     └────────┬──────────┘                  │
│                                       │                             │
│                                       ▼                             │
│  ┌─────────────────────┐     ┌───────────────────┐                  │
│  │ Process Response    │◀────│ Wait for Response │                  │
│  └──────────┬──────────┘     └───────────────────┘                  │
│             │                                                       │
│             │ (if response valid)                                   │
│             ▼                                                       │
│  ┌────────────────────────────────────────────┐                     │
│  │    Skip Status Byte & Extract Random Data  │                     │
│  └──────────┬─────────────────────────────────┘                     │
│             │                                                       │
│             ▼                          ┌─────────────────────────┐  │
│  ┌────────────────────┐  (on failure)  │ Fallback: Time-based    │  │
│  │ Return Random Data │◀───────────────│ Random Generation       │  │
│  └────────────────────┘                └─────────────────────────┘  │
└───────────────────────────────────────────────────────────-─────────┘
                                │
                                ▼
┌────────────────────────────────────────────────────────────────────┐
│                        SHA-256 Hashing                             │
│                                                                    │
│  ┌─────────────────────┐     ┌───────────────────┐                 │
│  │  Hash Random Data   │────▶│ Store in Database │                 │
│  └─────────────────────┘     └───────────────────┘                 │
└────────────────────────────────────────────────────────────────────┘
```


## Hardware Communication

### I2C Protocol Implementation

The controller communicates with the ATECC608A chip using the I2C protocol with these specific characteristics:

- **Default I2C Address**: `0x60`
- **Bus Configuration**: Dynamically configurable through environment variables
- **Wake-Sleep Cycle**: Implements precise timing for device power management:
    - Wake command uses a 1ms delay (`wakeupDelay`)
    - Random commands require 23ms execution time (`randomExecTime`)
    - Configuration operations use specific timing constants

### Command Structure

Commands to the ATECC608A follow a strictly defined packet format:

```
[Word Address (0x03)][Count][Opcode][Param1][Param2 (2 bytes)][Data][CRC (2 bytes)]
```


The implementation precisely follows Adafruit's approach to packet construction, including:
- **CRC Calculation**: Implements the exact CRC polynomial (`0x8005`) used by Adafruit
- **Parameter Formatting**: Little-endian encoding for multi-byte parameters
- **Response Handling**: Processes responses with status byte evaluation

## Command/Response Sequence Diagram

```
┌───────────┐                      ┌───────────┐
│ Go Code   │                      │ ATECC608A │
└─────┬─────┘                      └─────┬─────┘
      │                                  │
      │        Wake-up Sequence          │
      │ ─────────────────────────────────>
      │                                  │
      │          Info Command            │
      │ ─────────────────────────────────>
      │                                  │
      │          Info Response           │
      │ <─────────────────────────────────
      │                                  │
      │        Check Lock Status         │
      │ ─────────────────────────────────>
      │                                  │
      │         Lock Status Response     │
      │ <─────────────────────────────────
      │                                  │
      │    Configuration (if unlocked)   │
      │ ─────────────────────────────────>
      │                                  │
      │      Configuration Response      │
      │ <─────────────────────────────────
      │                                  │
      │          Lock Command            │
      │ ─────────────────────────────────>
      │                                  │
      │          Lock Response           │
      │ <─────────────────────────────────
      │                                  │
      │          Random Command          │
      │ ─────────────────────────────────>
      │                                  │
      │     Random Data (32 bytes)       │
      │ <─────────────────────────────────
      │                                  │
      │           Idle Command           │
      │ ─────────────────────────────────>
      │                                  │
┌─────┴─────┐                      ┌─────┴─────┐
│ Go Code   │                      │ ATECC608A │
└───────────┘                      └───────────┘
```


## One-Time Device Configuration

A critical aspect of this implementation is the one-time configuration process for the ATECC608A chip:

### Configuration Lock Process

The ATECC608A must be configured before first use, with these important characteristics:
1. **One-Time Operation**: Once the configuration is locked, it **CANNOT be changed**
2. **Safety Mechanisms**: Requires explicit `FORCE_CONFIG=true` environment variable
3. **Configuration Template**: Uses the `CFG_TLS` byte array template (128 bytes) based on Adafruit's implementation
4. **Lock Detection**: Checks if device is already locked before attempting configuration

### Configuration Steps

The configuration procedure:
1. Checks if the device is already locked by reading lock bytes
2. If unlocked and auto-configuration is enabled:
    - Writes the TLS configuration in 4-byte blocks (skipping read-only sections)
    - Sends the lock command (`0x17`) with specific parameters
    - Waits for lock confirmation
    - Validates the lock was successful

## Data Flow in Random Number Generation

```
┌──────────────────────────────────────────────────────────────────┐
│                    TRNG Generation Process                       │
└──────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌──────────────────────────────────────────────────────────────────┐
│                   Hardware TRNG (primary path)                   │
│                                                                  │
│  ┌────────────────┐     ┌────────────────┐    ┌────────────────┐ │
│  │ Send Random    │────▶│ Read 32 bytes  │───▶│ Extract Data   │ │
│  │ Command (0x1B) │     │ from Device    │    │ (Skip Status)  │ │
│  └────────────────┘     └────────────────┘    └───────┬────────┘ │
│                                                       │          │
└───────────────────────────────────────────────────────┼──────────┘
                                                        │
                                                        │
┌───────────────────────────────────────────────────────┼──────────┐
│                 Time-based TRNG (fallback)            │          │
│                                                       │          │
│  ┌────────────────┐     ┌────────────────┐    ┌───────▼────────┐ │
│  │ Get Nanosecond │────▶│ Combine with   │───▶│ Format as      │ │
│  │ Timestamp      │     │ PID and Unix   │    │ 32-byte Array  │ │
│  └────────────────┘     │ Timestamp      │    └───────┬────────┘ │
│                         └────────────────┘            │          │
└───────────────────────────────────────────────────────┼──────────┘
                                                        │
                                                        ▼
┌───────────────────────────────────────────────────────────────────┐
│                       SHA-256 Hashing                             │
│                                                                   │
│  ┌────────────────┐                                               │
│  │ Apply SHA-256  │                                               │
│  │ to Random Data │                                               │
│  └───────┬────────┘                                               │
│          │                                                        │
│          ▼                                                        │
│  ┌────────────────┐     ┌────────────────┐    ┌────────────────┐  │
│  │ 32-byte        │────▶│ Store in       │───▶│ Make Available │  │
│  │ Cryptographic  │     │ BadgerDB Queue │    │ via API        │  │
│  │ Hash           │     └────────────────┘    └────────────────┘  │
│  └────────────────┘                                               │
└───────────────────────────────────────────────────────────────────┘
```


## Random Number Generation

### TRNG Implementation

The random number generation uses a multi-step process:
1. **Wake Cycle**: Always wakes the device before operations
2. **Random Command**: Sends command `0x1B` with specific parameters
3. **Response Processing**: Processes 32-byte response, skipping the status byte
4. **Hashing**: Applies SHA-256 to the raw random data for additional security
5. **Fallback**: Uses time-based entropy when hardware access fails

### Hardware Specifics

The implementation handles ATECC608A hardware quirks:
- Status byte is logged but not treated as an error condition
- Specific timing delays between commands (based on Adafruit's implementation)
- Response verification with retries (up to 20 attempts)
- Response format handling with correct byte offsetting

## System Integration

```
┌────────────────────────────────────────────────────────────────────┐
│                       Lokey System Architecture                    │
└────────────────────────────────────────────────────────────────────┘
                                │
        ┌─────────────────┬─────┴──────┬─────────────────┐
        │                 │            │                 │
        ▼                 ▼            ▼                 ▼
┌───────────────┐  ┌────────────┐  ┌────────┐  ┌─────────────────┐
│ HTTP API      │  │ Controller │  │Fortuna │  │ Database        │
│ Endpoints     │  │ Service    │  │Service │  │ (BadgerDB)      │
└───────┬───────┘  └─────┬──────┘  └────┬───┘  └────────┬────────┘
        │                │              │               │
        │                │              │               │
        │          ┌─────┴──────┐       │               │
        │          │ atecc608a  │       │               │
        │          │ Controller │       │               │
        │          └─────┬──────┘       │               │
        │                │              │               │
        │                ▼              │               │
        │          ┌────────────┐       │               │
        │          │ I2C        │       │               │
        │          │ Interface  │       │               │
        │          └─────┬──────┘       │               │
        │                │              │               │
        │                ▼              │               │
        │          ┌────────────┐       │               │
        │          │ ATECC608A  │       │               │
        │          │ Chip       │       │               │
        │          └────────────┘       │               │
        │                               │               │
        └───────────────┬───────────────┘               │
                        │                               │
                        └───────────────────────────────┘
```


## Concurrency and Error Handling

### Thread Safety

All device operations are protected:
- **Mutex Implementation**: Prevents concurrent device access
- **Context-Based Cancellation**: Allows graceful shutdown
- **Retry Logic**: Built-in recovery for intermittent communication issues

### Error Recovery

Multiple layers of error handling:
1. **I2C Communication Errors**: Captured and logged with detailed diagnostics
2. **Command Response Errors**: Status codes are processed appropriately
3. **Device Initialization Failures**: Graceful degradation to fallback mode
4. **Logging**: Comprehensive logs for troubleshooting

## Integration with Lokey Service

The controller integrates with the broader system:
1. **Background Hash Generation**: Runs a ticker-based generator at configurable intervals
2. **API Endpoints**:
    - `/health`: Device health verification
    - `/info`: Runtime statistics
    - `/generate`: On-demand random number generation
3. **Database Storage**: Generated random data is stored in BadgerDB for consumption by other services

## Environment Configuration

The implementation is highly configurable through environment variables:
- `I2C_BUS_NUMBER`: I2C bus for device communication (default: 1)
- `DISABLE_AUTO_CONFIG`: Disables automatic configuration attempts
- `FORCE_CONFIG`: Required to enable the irreversible configuration process
- `HASH_INTERVAL_MS`: Milliseconds between hash generation (default: 1000)
- `TRNG_QUEUE_SIZE`: Maximum queue size for stored random numbers (default: 100)

---

This implementation provides a reliable interface to the ATECC608A for true random number generation, carefully balancing hardware capabilities with operational resilience. By following Adafruit's reference implementation while adding Go-specific enhancements and robust error handling, it delivers a production-ready service for cryptographic applications requiring true randomness.

Adafruit's implementation is available at: https://github.com/adafruit/Adafruit_CircuitPython_ATECC608A 