# Architecture

Technical architecture and implementation details of the LoKey system.

## Table of Contents

- [System Overview](#system-overview)
- [Component Architecture](#component-architecture)
- [Data Flow](#data-flow)
- [Controller Service](#controller-service)
- [Fortuna Service](#fortuna-service)
- [API Service](#api-service)
- [Database Design](#database-design)
- [Communication Patterns](#communication-patterns)
- [Security Considerations](#security-considerations)

## System Overview

LoKey is a microservices-based true random number generation system designed for high availability and throughput. The architecture separates concerns into three independent services that communicate via HTTP REST APIs.

### Design Principles

1. **Separation of Concerns** - Each service has a single, well-defined responsibility
2. **Stateless Services** - Controller and Fortuna services maintain no persistent state
3. **Centralized Storage** - API service manages all data persistence
4. **Fault Tolerance** - Services can restart independently without data loss
5. **Horizontal Scalability** - Fortuna services can be replicated for higher throughput

### High-Level Architecture
```

┌─────────────────────────────────────────────────────────────────┐
│                         Client Applications                      │
└────────────────────────────┬────────────────────────────────────┘
│
│ HTTP REST API
│
▼
┌─────────────────────────────────────────────────────────────────┐
│                          API Service                             │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  • REST API Endpoints                                     │   │
│  │  • Request Validation                                     │   │
│  │  • Data Format Conversion                                 │   │
│  │  • Queue Management                                       │   │
│  │  • Statistics & Metrics                                   │   │
│  └──────────────────────────────────────────────────────────┘   │
│                             │                                    │
│  ┌──────────────────────────┼───────────────────────────────┐   │
│  │         BoltDB Database  │                               │   │
│  │  • TRNG Data Queue      │                               │   │
│  │  • Fortuna Data Queue   │                               │   │
│  │  • Configuration        │                               │   │
│  │  • Counters & Stats     │                               │   │
│  └──────────────────────────┼───────────────────────────────┘   │
└─────────────────────────────┼────────────────────────────────────┘
│
┌───────────────────┴───────────────────┐
│                                       │
▼                                       ▼
┌─────────────────────┐               ┌─────────────────────┐
│ Controller Service  │               │  Fortuna Service    │
│                     │               │                     │
│ ┌─────────────────┐ │               │ ┌─────────────────┐ │
│ │  I2C Interface  │ │               │ │ Fortuna CSPRNG  │ │
│ │  ATECC608A Ops  │ │               │ │ Entropy Pools   │ │
│ │  Random Gen     │ │               │ │ Amplification   │ │
│ │  Health Check   │ │               │ │ Reseeding       │ │
│ └─────────────────┘ │               │ └─────────────────┘ │
│         │           │               │                     │
│         ▼           │               │                     │
│ ┌─────────────────┐ │               │                     │
│ │   ATECC608A     │ │               │                     │
│ │  Hardware Chip  │ │               │                     │
│ └─────────────────┘ │               │                     │
└─────────────────────┘               └─────────────────────┘
Hardware TRNG                       Cryptographic PRNG
(~10 samples/sec)                   (High throughput)
```
## Component Architecture

### Controller Service (Port 8081)

**Purpose:** Interface with ATECC608A hardware for true random number generation.

**Responsibilities:**
- Initialize and configure ATECC608A chip
- Generate hardware random numbers on demand
- Provide health status
- Handle I2C communication

**Technology:**
- Go 1.24
- `github.com/d2r2/go-i2c` for I2C communication
- Gin web framework
- Stateless design

**Key Features:**
- One-time device configuration (irreversible)
- Fallback to time-based entropy if hardware unavailable
- Automatic wake/sleep cycles for power efficiency
- CRC validation for data integrity

### Fortuna Service (Port 8082)

**Purpose:** Cryptographic amplification of random data using the Fortuna algorithm.

**Responsibilities:**
- Accept seed data from TRNG
- Generate high-throughput random data
- Maintain entropy pools
- Periodic reseeding

**Technology:**
- Go 1.24
- AES-256 cipher for generation
- SHA-256 for pool hashing
- Gin web framework
- Stateless design

**Key Features:**
- 32 entropy pools for catastrophic reseeding resistance
- Cryptographically secure output
- High throughput generation (1000s/sec)
- Automatic pool rotation

### API Service (Port 8080)

**Purpose:** Unified REST API, data storage, and queue management.

**Responsibilities:**
- Accept client requests
- Poll controller and fortuna services
- Store random data in queues
- Serve data in multiple formats
- Track statistics and metrics
- Health monitoring

**Technology:**
- Go 1.24
- BoltDB for persistence
- Gin web framework
- Prometheus metrics
- Swagger/OpenAPI documentation

**Key Features:**
- Multiple data formats (int8-64, uint8-64, binary)
- Configurable queue sizes
- Consumption tracking
- Delete-on-read behavior
- Comprehensive statistics

## Data Flow

### TRNG Data Flow
```

┌──────────────┐
│ ATECC608A    │
│ Hardware     │
└──────┬───────┘
│ 1. Generate 32 bytes
│    true random data
▼
┌──────────────┐
│ Controller   │
│ Service      │ 2. Expose via /generate endpoint
└──────┬───────┘
│
│ 3. API polls periodically
│    (TRNG_POLL_INTERVAL_MS)
▼
┌──────────────┐
│ API Service  │
│              │ 4. Store in TRNG queue
│ ┌──────────┐ │    (max: TRNG_QUEUE_SIZE)
│ │ BoltDB   │ │
│ │ TRNG     │ │ 5. Mark as unconsumed
│ │ Queue    │ │
│ └──────────┘ │
└──────┬───────┘
│
│ 6. Client requests data
▼
┌──────────────┐
│ Client       │ 7. Returns data in requested format
│ Application  │    (int8, uint64, binary, etc.)
└──────────────┘    Marks as consumed (deleted)
```
### Fortuna Data Flow
```

┌──────────────┐
│ Controller   │
│ Service      │ 1. API fetches TRNG data
└──────┬───────┘
│
│ 2. API sends seeds to Fortuna
▼
┌──────────────┐
│ Fortuna      │
│ Service      │ 3. Add to entropy pools
│              │
│ ┌──────────┐ │ 4. Reseed generator
│ │ 32 Pools │ │
│ │ AES-256  │ │ 5. Generate amplified data
│ └──────────┘ │
└──────┬───────┘
│
│ 6. API polls /generate
│    (FORTUNA_POLL_INTERVAL_MS)
▼
┌──────────────┐
│ API Service  │
│              │ 7. Store in Fortuna queue
│ ┌──────────┐ │    (max: FORTUNA_QUEUE_SIZE)
│ │ BoltDB   │ │
│ │ Fortuna  │ │ 8. Mark as unconsumed
│ │ Queue    │ │
│ └──────────┘ │
└──────┬───────┘
│
│ 9. Client requests data
▼
┌──────────────┐
│ Client       │ 10. Returns high-throughput data
│ Application  │     Marks as consumed (deleted)
└──────────────┘
```
### Fortuna Seeding Flow
```

Every 30 seconds:

┌──────────────┐
│ API Service  │
│              │ 1. Fetch 5 TRNG samples
│              │    from Controller
└──────┬───────┘
│
│ 2. POST to /seed endpoint
▼
┌──────────────┐
│ Fortuna      │
│ Service      │ 3. Distribute seeds across pools
│              │
│ ┌──────────┐ │ 4. Hash pools with SHA-256
│ │ Pools    │ │
│ │ 0-31     │ │ 5. Reseed AES generator
│ └──────────┘ │
│              │ 6. Increment counter
└──────────────┘    (determines next pools to use)
```
## Controller Service

### ATECC608A Implementation

The controller implements precise I2C communication with the ATECC608A cryptographic chip, following Adafruit's reference implementation.

**Hardware Communication:**
```

┌─────────────────────────────────────────────────────────────┐
│                    Command Sequence                          │
└─────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────┐
│ Wake Device   │ • Send to I2C address 0x00
│ (1ms delay)   │ • Wait 1ms for device ready
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ Send Command  │ • Build packet: [0x03][Count][Opcode][Param1]
│               │                 [Param2][Data][CRC16]
└───────┬───────┘ • CRC with polynomial 0x8005
        │
        ▼
┌───────────────┐
│ Wait for      │ • Command-specific delay:
│ Execution     │   - Random: 23ms
└───────┬───────┘   - Info: 1ms
        │           - Config: 35ms
        ▼
┌───────────────┐
│ Read Response │ • Retry up to 20 times
│               │ • Parse: [Length][Status][Data][CRC16]
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ Process Data  │ • Skip status byte
│               │ • Extract random data (32 bytes)
└───────┬───────┘ • Apply SHA-256 hash
        │
        ▼
┌───────────────┐
│ Return Hash   │ • Return 32-byte hash
│               │ • Put device to idle
└───────────────┘
```
**Device Configuration:**

The ATECC608A must be configured once before use. This is an **irreversible operation**.

Configuration process (**one-time only**)
1. Check if device is locked
2. If unlocked and FORCE_CONFIG=true:
   - Write TLS configuration template (128 bytes)
   - Write in 4-byte blocks to addresses 16-127
   - Skip read-only sections
3. Send lock command (0x17)
4. Verify lock status
5. Device is now permanently configured


**Safety Mechanisms:**
- Requires `FORCE_CONFIG=true` environment variable
- Logs all operations for audit trail
- Checks lock status before attempting configuration
- Fallback to time-based entropy if hardware fails

### Endpoints

**GET /health**
- Returns device health status
- Tests I2C communication
- Validates device responsiveness

**GET /info**
- Returns service information
- Device status
- Configuration state

**GET /generate?count=N**
- Generates N random values (1-100)
- Returns hex-encoded hashes
- Each hash is 32 bytes (256 bits)

## Fortuna Service

### Algorithm Implementation

LoKey implements the Fortuna cryptographically secure pseudo-random number generator designed by Bruce Schneier and Niels Ferguson.

**Core Components:**

```
┌─────────────────────────────────────────────────────────────┐
│                    Fortuna Generator                        │
└─────────────────────────────────────────────────────────────┘
│
├──► AES-256 Cipher (key: 256 bits)
│
├──► Counter (64-bit, increments per block)
│
└──► 32 Entropy Pools
│
├──► Pool 0  (used every reseed)
├──► Pool 1  (used every 2nd reseed)
├──► Pool 2  (used every 4th reseed)
├──► Pool 3  (used every 8th reseed)
│    ...
└──► Pool 31 (used every 2^31 reseeds)
```


**Generation Process:**

```textmate
// Pseudo-code
func GenerateRandomData(length int) []byte {
    result := make([]byte, length)
    blocks := (length + blockSize - 1) / blockSize
    
    for i := 0; i < blocks; i++ {
        // Encrypt counter value
        counterBytes := encodeCounter(counter)
        block := aesEncrypt(key, counterBytes)
        
        // Copy to result
        copy(result[i*blockSize:], block)
        
        // Increment counter
        counter++
    }
    
    return result
}
```


**Reseeding Logic:**

```textmate
// Pseudo-code
func ReseedFromPools() {
    reseedCount := counter
    poolsToUse := [][]byte{}
    
    for i := 0; i < 32; i++ {
        // Use pool if bit i is set in reseedCount
        if (reseedCount & (1 << i)) != 0 {
            if len(pools[i]) > 0 {
                poolsToUse = append(poolsToUse, pools[i])
                pools[i] = pools[i][:0]  // Clear pool
            }
        }
    }
    
    // Hash all pool data with current key
    newKey := sha256(key || poolsToUse...)
    
    // Update cipher with new key
    cipher = aes.NewCipher(newKey)
    counter++
}
```


**Catastrophic Reseeding:**

The pool selection algorithm ensures that even if an attacker compromises the state:
- Pool 0 is used every reseed (fast recovery)
- Pool 31 is used every 2^31 reseeds (long-term security)
- After 32 reseeds with fresh entropy, attacker's knowledge is worthless

### Endpoints

**GET /health**
- Returns generator health
- Checks last reseed time
- Validates entropy state

**GET /info**
- Returns service information
- Last reseed timestamp
- Generation counter

**GET /generate?size=N**
- Generates N bytes (1-1048576)
- Returns hex-encoded data
- High throughput

**POST /seed**
- Accepts array of hex-encoded seeds
- Distributes to entropy pools
- Triggers reseed operation

**POST /amplify**
- Accepts seed + desired output size
- Adds seed to pools
- Returns amplified data

## API Service

### Request Processing

```
Client Request
     │
     ▼
┌─────────────────┐
│ Gin Router      │
│ (HTTP Handler)  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Validation      │ • Check format (int8-64, uint8-64, binary)
│                 │ • Validate limit (1-100000)
└────────┬────────┘ • Verify source (trng/fortuna)
         │
         ▼
┌─────────────────┐
│ Calculate       │ • Bytes needed = count × bytesPerValue
│ Requirements    │ • Chunks needed = bytes / 31 + buffer
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Database Query  │ • GetTRNGData() or GetFortunaData()
│                 │ • Mark as consumed
└────────┬────────┘ • Increment counters
         │
         ▼
┌─────────────────┐
│ Format          │ • Convert raw bytes to requested format
│ Conversion      │ • Apply big-endian byte order
└────────┬────────┘ • Handle signed/unsigned
         │
         ▼
┌─────────────────┐
│ Response        │ • JSON array for numeric types
│                 │ • Binary stream for binary format
└─────────────────┘ • Set appropriate Content-Type
```


### Polling Mechanism

The API service uses background goroutines to continuously poll the controller and fortuna services:

```textmate
// Simplified polling logic
func pollTRNGService(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Fetch from controller
            data := fetchFromController()
            
            // Store in database
            db.StoreTRNGData(data)
            
            // Increment counter
            db.IncrementPollingCount("trng")
        }
    }
}
```


**Polling Configuration:**
- `TRNG_POLL_INTERVAL_MS`: How often to fetch TRNG data (default: 1000ms)
- `FORTUNA_POLL_INTERVAL_MS`: How often to fetch Fortuna data (default: 5000ms)
- Fortuna seeding: Every 30 seconds with 5 TRNG samples

### Queue Management

```
Data arrives from polling
        │
        ▼
┌─────────────────┐
│ Add to Queue    │ • Assign sequential ID
│                 │ • Set timestamp
└────────┬────────┘ • Mark as unconsumed
         │
         ▼
    Queue Full?
         │
    ┌────┴────┐
   Yes        No
    │          │
    ▼          ▼
┌────────┐ ┌────────┐
│ Drop   │ │ Store  │
│ Oldest │ │ New    │
└───┬────┘ └───┬────┘
    │          │
    │ Increment│
    │ dropped  │
    │ counter  │
    └────┬─────┘
         │
         ▼
    Queue Ready
```


**Queue Characteristics:**
- FIFO (First In, First Out)
- Configurable capacity
- Automatic trimming when full
- Consumption tracking
- Metrics for drops and utilization

## Database Design

### BoltDB Schema

LoKey uses BoltDB, an embedded key-value store with ACID guarantees.

**Buckets:**

```
lokey.db
├── trng_data          # TRNG random data
│   ├── [id: uint64] → TRNGData JSON
│   ├── [id: uint64] → TRNGData JSON
│   └── ...
│
├── fortuna_data       # Fortuna random data
│   ├── [id: uint64] → FortunaData JSON
│   ├── [id: uint64] → FortunaData JSON
│   └── ...
│
├── counters           # System counters
│   ├── trng_next_id → uint64
│   ├── fortuna_next_id → uint64
│   ├── trng_polling_count → uint64
│   ├── fortuna_polling_count → uint64
│   ├── trng_dropped_count → uint64
│   ├── fortuna_dropped_count → uint64
│   ├── trng_consumed_count → uint64
│   └── fortuna_consumed_count → uint64
│
├── config             # Configuration
│   ├── trng_queue_size → uint64
│   └── fortuna_queue_size → uint64
│
└── usage_stats        # Usage statistics
    ├── [source_timestamp] → UsageStat JSON
    └── ...
```


**Data Structures:**

```textmate
type TRNGData struct {
    ID        uint64    `json:"id"`
    Data      []byte    `json:"data"`
    Timestamp time.Time `json:"timestamp"`
    Consumed  bool      `json:"consumed"`
}

type FortunaData struct {
    ID        uint64    `json:"id"`
    Data      []byte    `json:"data"`
    Timestamp time.Time `json:"timestamp"`
    Consumed  bool      `json:"consumed"`
}

type UsageStat struct {
    Source    string    `json:"source"`
    BytesUsed int64     `json:"bytes_used"`
    Requests  int64     `json:"requests"`
    Timestamp time.Time `json:"timestamp"`
}
```


**Operations:**

1. **Store**: Add new data with auto-incrementing ID
2. **Retrieve**: Get unconsumed data, optionally mark as consumed
3. **Trim**: Remove oldest entries when queue is full
4. **Count**: Track polling, drops, consumption

## Communication Patterns

### HTTP REST

All inter-service communication uses HTTP REST:

**API → Controller:**
```
GET /generate?count=1 HTTP/1.1
Host: controller:8081

Response:
{
  "data": "a1b2c3d4..."
}
```


**API → Fortuna (Seeding):**
```
POST /seed HTTP/1.1
Host: fortuna:8082
Content-Type: application/json

{
  "seeds": ["a1b2c3...", "d4e5f6...", ...]
}

Response:
{
  "status": "reseeded",
  "count": 5
}
```


**API → Fortuna (Generation):**
```
GET /generate?size=256 HTTP/1.1
Host: fortuna:8082

Response:
{
  "data": "7f8e9d0c...",
  "size": 256
}
```


### Error Handling

- **Retry Logic**: Not implemented (fail-fast)
- **Fallback**: Controller uses time-based entropy if hardware unavailable
- **Logging**: All errors logged with context
- **Health Checks**: Regular polling for service availability

## Security Considerations

### Cryptographic Properties

**TRNG (Hardware):**
- True entropy from physical processes
- Passes NIST statistical tests (when using ATECC608A)
- No algorithmic predictability
- Limited by hardware generation rate

**Fortuna (Amplified):**
- Cryptographically secure (AES-256, SHA-256)
- Forward secrecy (compromising state doesn't reveal past outputs)
- Resilient to state compromise (through pool rotation)
- Periodic reseeding with TRNG maintains entropy

### Attack Surface

**Hardware Tampering:**
- Physical access required to manipulate ATECC608A
- I2C communication not encrypted (local bus only)
- Mitigation: Secure physical access to device

**Network Attacks:**
- Internal HTTP communication not encrypted
- No authentication between services
- Mitigation: Use Docker internal networks, add reverse proxy with TLS

**State Compromise:**
- If Fortuna state is compromised, attacker can predict future outputs
- Mitigation: Regular reseeding (every 30s), pool rotation

**Denial of Service:**
- Queue exhaustion through rapid consumption
- Mitigation: Rate limiting (not implemented), queue size tuning

### Best Practices

1. **Use TRNG for cryptographic keys** - Highest quality randomness
2. **Use Fortuna for high-volume needs** - Balance speed and security
3. **Deploy behind reverse proxy** - Add TLS, authentication, rate limiting
4. **Monitor queue levels** - Alert on low levels or high drops
5. **Secure physical access** - Protect Raspberry Pi and ATECC608A
6. **Regular backups** - Backup database for statistics (not random data)

## Performance Characteristics

### Throughput

**TRNG (Hardware):**
- Generation: ~10 hashes/second
- Polling overhead: ~100ms per poll
- Network latency: ~1-5ms (local Docker network)
- Database write: ~1ms per record
- **Effective rate**: ~8-10 samples/second

**Fortuna (Amplified):**
- Generation: 1000s/second (CPU-limited)
- AES-256 encryption: ~100MB/s (typical)
- Network latency: ~1-5ms
- Database write: ~1ms per record
- **Effective rate**: Limited by polling interval and queue size

### Latency

**First Request:**
- TRNG: 0-10 seconds (queue build-up)
- Fortuna: 0-5 seconds (queue build-up)

**Subsequent Requests:**
- If queue has data: <10ms
- If queue empty: Wait for next poll cycle

### Resource Usage

**Controller:**
- CPU: <5% (polling)
- Memory: ~10MB
- I2C: Minimal, periodic access

**Fortuna:**
- CPU: 10-30% (generation + polling)
- Memory: ~20MB (entropy pools)

**API:**
- CPU: 5-15% (request handling + polling)
- Memory: ~50MB base + (queue size × 31 bytes × 2)
- Disk: Grows with queue size, ~1KB per 31 bytes

**Example:**
- TRNG queue: 1000 items × 31 bytes = 31KB
- Fortuna queue: 5000 items × 256 bytes = 1.25MB
- Total DB size: ~2-5MB (with metadata and indexes)

## Scalability

### Vertical Scaling

**Increase throughput on single instance:**
- Increase `FORTUNA_QUEUE_SIZE` for more buffering
- Decrease `FORTUNA_POLL_INTERVAL_MS` for faster generation
- Increase `AMPLIFICATION_FACTOR` for more output per seed

### Horizontal Scaling

**Multiple Fortuna instances:**
```yaml
services:
  fortuna-1:
    image: lokey-fortuna
    ports: ["8082:8082"]
  
  fortuna-2:
    image: lokey-fortuna
    ports: ["8083:8082"]
  
  api:
    environment:
      - FORTUNA_ADDR=http://fortuna-1:8082
      # Poll multiple instances in round-robin
```


**Load balancing:**
- Use Nginx/HAProxy in front of API
- Multiple API instances with shared database (requires locking)
- Read replicas for statistics queries

### Limitations

- **Single ATECC608A**: Hardware generation rate is fixed (~10/sec)
- **Single BoltDB**: Write throughput limited by disk I/O
- **No distributed state**: Cannot share queues across API instances

## Next Steps

- **[Deployment Guide](deployment.md)** - Deploy to production
- **[API Examples](api-examples.md)** - Learn API usage
- **[Development Guide](development.md)** - Contribute to the project
- **[Hardware Setup](hardware-setup.md)** - Configure ATECC608A chip
