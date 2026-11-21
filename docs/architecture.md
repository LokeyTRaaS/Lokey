# Architecture

Technical architecture and implementation details of the LoKey system.

## Table of Contents

- [System Overview](#system-overview)
- [Component Architecture](#component-architecture)
- [Data Flow](#data-flow)
- [Controller Service](#controller-service)
- [Fortuna Service](#fortuna-service)
- [VirtIO Service](#virtio-service)
- [API Service](#api-service)
- [Queue / storage design](#queue--storage-design)
- [Communication Patterns](#communication-patterns)
- [Security Considerations](#security-considerations)

## System Overview

LoKey is a microservices-based true random number generation system designed for high availability and throughput. The architecture separates concerns into four independent services that communicate via HTTP REST APIs.

### Design Principles

1. **Separation of Concerns** — each service has a single responsibility
2. **Ephemeral process state** — Controller is request-scoped; Fortuna keeps key/pools/counter in RAM only (lost on restart); API/VirtIO queues are in-memory
3. **In-memory queues** — API holds TRNG/Fortuna entropy in RAM (`memory://channels`); restart clears queues
4. **Fault Tolerance** — services restart independently; entropy buffers refill via polling/seeding
5. **Scale controllers, not Fortuna** — `CONTROLLER_ADDRS` supports multiple devices; `FORTUNA_ADDR` is a **single** URL (no multi-Fortuna round-robin)

### High-Level Architecture

```mermaid
flowchart TB
  Client["Client apps"] -->|HTTPS| Traefik
  Traefik --> API["API service"]
  Traefik --> Grafana
  API --> TRNGQ["In-mem TRNG queue"]
  API --> FQ["In-mem Fortuna queue"]
  API -->|poll + seed| CTRL["Controller"]
  API -->|seed + poll /generate| FORT["Fortuna CSPRNG"]
  API -->|seed| VIRT["VirtIO"]
  CTRL --> ATECC["ATECC608A/B TRNG"]
  VIRT -->|pipe / HTTP stream| VMs["VMs / containers"]
```

Compose networks: clients hit Traefik on `lokey-edge`; API/Fortuna/VirtIO share `lokey-app`; controllers sit on `lokey-devices`; Prometheus/Grafana on `lokey-metrics`.

## Component Architecture

### Controller Service (Port 8081)

**Purpose:** Interface with ATECC608A hardware for true random number generation.

**Responsibilities:**
- Initialize and configure ATECC608A chip
- Generate hardware random numbers on demand
- Provide health status
- Handle I2C communication

**Technology:**
- Go 1.27
- `github.com/d2r2/go-i2c` for I2C communication
- Gin web framework
- Stateless design

**Key Features:**
- One-time device configuration (irreversible)
- Hardware mode fails closed if the ATECC is unavailable (use `CONTROLLER_MODE=emulated` for laptop/CI)
- Automatic wake/sleep cycles for power efficiency
- CRC validation for data integrity

### Fortuna Service (Port 8082)

**Purpose:** Cryptographic amplification of random data using the Fortuna algorithm.

**Responsibilities:**
- Accept TRNG seed batches from the API (`POST /seed`)
- Mix seeds into 32 entropy pools and reseed the AES key
- Serve high-throughput CSPRNG output (`GET /generate`)

**Technology:**
- Go 1.27
- AES-256-CTR for generation
- SHA-256 for reseed key derivation
- Gin web framework
- In-process state only (key, pools, counters)

**Key Features:**
- Two distinct seeds: boot `crypto/rand` (starter key) + ongoing ATECC TRNG reseeds
- 32 entropy pools with classic Fortuna schedule (`reseedCount`, not the AES block counter)
- Seed quality validation (flat/0xFF/low-Shannon rejection)
- AES key rekey after every `GenerateRandomData`
- `/generate` and `/amplify` return 503 when unhealthy
- Default reseed intake: **5 TRNG samples ≈ 155 bytes every 30s** (from each healthy controller)

### VirtIO Service (Port 8083 HTTP)

**Purpose:** Provide high-throughput random data access for virtualization environments via named pipe (FIFO) and HTTP streaming. VirtIO is a **consumer endpoint** that serves data to VMs/containers. It does NOT read from hardware devices - all data comes from its queue, which is seeded by the API service.

**Responsibilities:**
- Accept seed data from API service (TRNG or Fortuna)
- Maintain internal circular queue for seeded data (100-500 KB capacity)
- Serve data to local VMs via named pipe (FIFO) - high-performance, low-latency
- Serve data to remote clients/Kubernetes via HTTP streaming endpoint
- Provide HTTP management API (port 8083)
- Provide health status (queue-based, not device-based)
- Support runtime queue size configuration
- **Note**: Device access (`/dev/hwrng`) is NOT required - VirtIO works purely from seeded queue data

**Technology:**
- Go 1.27
- Gin web framework (HTTP API)
- Named pipe (FIFO) for local VM access
- HTTP chunked streaming for network access
- In-memory circular queue (channel-based only)
- **No device access required** - works purely from seeded queue

**Key Features:**
- **Dual Access Methods**:
  - **Named Pipe (FIFO)**: High-performance local access for hypervisors (QEMU, Proxmox, VirtualBox, VMware)
    - Default path: `/var/run/lokey/virtio-rng`
    - ~500x faster than legacy EGD protocol
    - Blocking writes (standard FIFO behavior)
    - Automatic reconnection when reader disconnects
  - **HTTP Streaming (Port 8083)**: Network access for remote servers and Kubernetes sidecars
    - Endpoint: `GET /stream`
    - Chunked transfer encoding
    - Configurable chunk size and max bytes
    - Graceful client disconnection handling
- **HTTP API (Port 8083)**: Management, monitoring, seeding endpoints
  - `GET /health` - Health check
  - `GET /info` - Service information (includes pipe path)
  - `GET /generate` - Pull data from queue (testing/debugging)
  - `GET /stream` - Continuous data streaming
  - `POST /seed` - Accept seed data from API service
  - `GET /config/queue` - Get queue configuration
  - `PUT /config/queue` - Update queue size
- Circular queue for storing seeded data (default: 15,000 items ≈ 480 KB)
- **Consumer-only operation**:
  - Accepts seed data via `/seed` endpoint (from API service)
  - Serves data via named pipe to local VMs
  - Serves data via HTTP streaming to remote clients
- Runtime queue size updates via `/config/queue` endpoint
- Graceful handling of pipe reader disconnections
- Thread-safe concurrent access

**Named Pipe (FIFO) Details:**

The named pipe provides a high-performance interface for local hypervisor access:

- **Path**: Configurable via `VIRTIO_PIPE_PATH` (default: `/var/run/lokey/virtio-rng`)
- **Creation**: Automatically created on service startup
- **Permissions**: 0666 (read/write for all)
- **Behavior**: Blocking writes (standard FIFO behavior - blocks when pipe buffer is full)
- **Reconnection**: Automatically reopens pipe when reader disconnects
- **Performance**: ~500x faster than legacy EGD protocol
- **Queue Integration**: Continuously reads from circular queue and writes to pipe

**HTTP Streaming Details:**

The HTTP streaming endpoint enables network-based access:

- **Endpoint**: `GET /stream`
- **Query Parameters**:
  - `chunk_size` (optional, default: 1024): Bytes per chunk
  - `max_bytes` (optional): Maximum total bytes to stream
- **Headers**:
  - `Content-Type: application/octet-stream`
  - `Transfer-Encoding: chunked`
- **Behavior**: Continuously streams data from queue until client disconnects or max_bytes reached
- **Use Cases**: Remote servers, Kubernetes sidecars, network-based access

**Use Cases:**
- **Local VMs**: QEMU, Proxmox, VirtualBox, VMware via named pipe
- **Kubernetes/OpenShift**: Sidecar container reads from HTTP stream and provides pipe to pod
- **Remote Servers**: Network-based access via HTTP streaming
- **Cloud Deployments**: Where hardware TRNG is unavailable
- **Development and Testing**: High-throughput entropy for testing environments

### API Service (Port 8080)

**Purpose:** Unified REST API, data storage, and queue management.

**Responsibilities:**
- Accept client requests
- Poll controller and fortuna services (VirtIO is NOT polled - it's a consumer endpoint)
- Store random data in queues (TRNG and Fortuna only)
- Serve data in multiple formats
- Track statistics and metrics
- Health monitoring
- **Seed VirtIO service** (configurable source: TRNG, Fortuna, or both) - data flows one way: API → VirtIO

**Technology:**
- Go 1.27
- In-memory circular queues (`pkg/database` channel implementation)
- Gin web framework
- Prometheus metrics
- Swagger/OpenAPI documentation

**Key Features:**
- Multiple data formats (int8-64, uint8-64, binary)
- Configurable queue sizes (TRNG, Fortuna, VirtIO)
- Consumption tracking
- Delete-on-read behavior
- Comprehensive statistics
- VirtIO seeding configuration (TRNG, Fortuna, or both)
- Runtime configuration updates for VirtIO

## Data Flow

### TRNG Data Flow

```mermaid
flowchart TB
  ATECC["ATECC608A/B"] -->|"Random: status + 31 entropy bytes"| CTRL["Controller /generate"]
  CTRL -->|"poll TRNG_POLL_INTERVAL_MS"| API["API"]
  API --> Q["In-mem TRNG queue"]
  Q -->|"client request"| Client["Client formats int8…uint64 / binary"]
```

Hardware path strips the ATECC status byte, so each sample is **31 bytes** of entropy (emulator returns 32).

### Fortuna end-to-end

There are **two** seeds:

| Seed | When | Role |
|------|------|------|
| Boot seed | Fortuna process start | 32 bytes from `crypto/rand` → first AES key (safe `/generate` before first TRNG reseed) |
| TRNG reseed | Every 30s from API | 5 ATECC samples (~155 B) mixed into pools → new AES key |

```mermaid
flowchart TB
  subgraph boot ["1. Boot"]
    OS["crypto/rand 32 B"] --> IK["initial AES key"]
  end

  subgraph intake ["2. Every 30s — TRNG intake"]
    ATECC["ATECC608"] --> CTRL["controller /generate?count=5"]
    CTRL --> API["API quality-check"]
    API --> POST["POST fortuna /seed"]
    POST --> P0["pool 0"]
    POST --> P1["pool 1"]
    POST --> Pn["pools 2…"]
  end

  subgraph reseed ["3. Reseed"]
    P0 --> HASH["SHA-256(current key ‖ selected pools)"]
    P1 --> HASH
    Pn --> HASH
    IK -.-> HASH
    HASH --> NK["new AES key"]
    NK --> CLR["used pools cleared"]
  end

  subgraph gen ["4. /generate — CSPRNG"]
    NK --> CTR["AES-CTR(key, block counter)"]
    CTR --> OUT["bytes to API queue / client"]
    CTR --> REKEY["rekey from next CTR blocks"]
    REKEY --> NK
  end
```

API also **polls** Fortuna `/generate` on `FORTUNA_POLL_INTERVAL_MS` to fill the Fortuna queue for `source=fortuna` clients. Seeding and polling are separate loops.

### Fortuna pool schedule

Pool *i* is eligible on reseed *N* when `N % 2^i == 0` (classic Fortuna). Driven by a dedicated **`reseedCount`**, not the AES-CTR block counter (using the block counter caused intermittent empty-pool reseeds).

```mermaid
flowchart LR
  R1["reseed 1"] --> Pool0a["pool 0"]
  R2["reseed 2"] --> Pool0b["pool 0"]
  R2 --> Pool1["pool 1"]
  R4["reseed 4"] --> Pool0c["pool 0"]
  R4 --> Pool1b["pool 1"]
  R4 --> Pool2["pool 2"]
```

### VirtIO Seeding Flow

```mermaid
flowchart TB
  API["API every ~30s"] -->|"VIRTIO_SEEDING_SOURCE: trng / fortuna / both"| SEED["POST virtio /seed"]
  SEED --> Q["VirtIO circular queue"]
  Q -->|named pipe| Local["Local VMs"]
  Q -->|HTTP /stream| Remote["Remote / K8s"]
```

Data flows **one way**: API seeds VirtIO; VirtIO serves VMs. The API does **not** poll VirtIO.

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
│               │ • Extract entropy (31 bytes after status)
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
- No time-based entropy fallback in hardware mode

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

**Core state:**

```mermaid
flowchart TB
  Key["AES-256 key 32 B"] --> Cipher["AES-CTR generator"]
  Ctr["AES block counter"] --> Cipher
  RC["reseedCount"] --> Sched["pool schedule N % 2^i == 0"]
  Sched --> Pools["32 entropy pools"]
  Pools -->|"SHA-256(key ‖ pools)"| Key
```

| Field | Purpose |
|-------|---------|
| `key` | Current AES-256 key (drives all output) |
| `Counter` | AES-CTR block counter (advances on generate/rekey) |
| `reseedCount` | Fortuna schedule counter (advances once per `ReseedFromPools`) |
| `pools[0…31]` | Staging for TRNG events; cleared when used in a reseed |

**Generation (simplified):**

```text
func GenerateRandomData(length):
    for each AES block needed:
        out ‖= AES_Encrypt(key, encode(Counter))
        Counter++
    rekeyUnlocked()   // replace key with next CTR blocks (forward secrecy)
    return out
```

**Reseed (simplified):**

```text
func ReseedFromPools():
    reseedCount++
    for i in 0..31:
        if reseedCount % (1<<i) != 0: break   // classic schedule
        if pools[i] empty: continue
        take pools[i]; clear pools[i]
    newKey = SHA-256(key ‖ taken_pools...)
    key, cipher = newKey
    // AES Counter is NOT used for pool selection
```

**Catastrophic reseeding:** pool 0 every reseed (fast recovery); pool *i* every `2^i` reseeds. Fresh TRNG in unused higher pools survives even if the running key was exposed, until those pools are mixed in.

Predicting future `/generate` output requires the **live key + block counter**, not historical pool contents (pools are cleared after use).

### Endpoints

**GET /health** — generator health (explicit flag + max reseed interval)

**GET /info** — status, amplification factor, last reseed time

**GET /generate?size=N** — N bytes (1–1048576), hex-encoded; 503 if unhealthy

**POST /seed** — `{ "seeds": ["<hex>", ...] }`; spreads across pools `i % 32`, then `ReseedFromPools`

## VirtIO Service

### Implementation Details

The VirtIO service provides high-throughput random data access for virtualization environments via named pipe (FIFO) and HTTP streaming.

**Device Access:**
- **NOT REQUIRED** - VirtIO service does not read from hardware devices
- All data comes from the seeded queue (populated by API service)
- Device paths are ignored - VirtIO works purely from seeded data

**Queue Management:**
- Internal circular queue for seeded data
- Configurable size via `VIRTIO_QUEUE_SIZE` (default: 15,000 items ≈ 480 KB)
- Target capacity: 100-500 KB
- FIFO behavior (oldest data removed when full)
- Thread-safe concurrent access
- Runtime size updates via `/config/queue` endpoint
- Byte-based capacity calculation: `GetQueueSizeBytes()` = items × 32 bytes

**Operation Mode:**
- **Consumer Mode Only**: Accepts seed data via `/seed` endpoint and stores in queue
- Serves data to local VMs via named pipe (FIFO) - high-performance, low-latency
- Serves data to remote clients/Kubernetes via HTTP streaming
- `/generate` endpoint pulls from queue (for testing/debugging only, not for production API consumption)

**Named Pipe (FIFO) Implementation:**
- Created at path specified by `VIRTIO_PIPE_PATH` (default: `/var/run/lokey/virtio-rng`)
- Automatically created on service startup
- Permissions: 0666 (read/write for all)
- Blocking writes (standard FIFO behavior - blocks when pipe buffer is full)
- Automatic reconnection when reader disconnects
- Continuously reads from circular queue and writes to pipe
- ~500x faster than legacy EGD protocol

**HTTP Streaming Implementation:**
- Endpoint: `GET /stream`
- Chunked transfer encoding for continuous streaming
- Configurable chunk size and max bytes via query parameters
- Graceful client disconnection handling
- Suitable for remote servers and Kubernetes sidecars

### Endpoints

**GET /health**
- Returns service health status
- Checks queue availability (device access not checked)
- Returns 200 if healthy (queue exists and has capacity), 503 if unhealthy

**GET /info**
- Returns service information
- Current queue status
- Named pipe path

**GET /generate?count=N**
- Generates N random values (1-100)
- **Pulls from queue** (not from device)
- Returns hex-encoded data (32 bytes per value)
- **Note**: This endpoint is for testing/debugging. Production VMs should use named pipe or HTTP streaming

**GET /stream?chunk_size=N&max_bytes=M**
- Streams random data continuously
- Query parameters:
  - `chunk_size` (optional, default: 1024): Bytes per chunk
  - `max_bytes` (optional): Maximum total bytes to stream
- Headers: `Content-Type: application/octet-stream`, `Transfer-Encoding: chunked`
- Use cases: Remote servers, Kubernetes sidecars, network-based access

**POST /seed**
- Accepts array of hex-encoded seeds
- Stores seeds in circular queue
- Returns count of successfully seeded items

**GET /config/queue**
- Returns current queue configuration
- Queue size and current usage

**PUT /config/queue**
- Updates queue size dynamically
- Migrates existing data if possible
- Accepts `{"queue_size": <number>}` (10-1000000)

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

            // Calculate quality metrics BEFORE storing
            qualityTracker.ProcessData(data)

            // Store in database
            db.StoreTRNGData(data)

            // Increment counter
            db.IncrementPollingCount("trng")
        }
    }
}
```


**Polling Configuration:**
- `TRNG_POLL_INTERVAL_MS`: How often to fetch TRNG data (Compose default: `1000`)
- `FORTUNA_POLL_INTERVAL_MS`: How often to fetch Fortuna `/generate` into the API queue (Compose default: `100`)
- Fortuna **seeding**: every 30s with 5 TRNG samples (~155 B) — separate from polling

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

## Queue / storage design

### In-memory circular queues

LoKey does **not** use an on-disk database. The API holds entropy in RAM via
`ChannelDBHandler` (`pkg/database`), reported as `path: memory://channels`.

**Structure:**

```
memory://channels
├── TRNGQueue      # circular buffer of TRNG samples
├── FortunaQueue   # circular buffer of Fortuna samples
└── VirtIOQueue    # API-side VirtIO seed buffer (VirtIO service has its own queue)
```

**Properties:**
- FIFO circular buffers with configurable capacity (`TRNG_QUEUE_SIZE`, etc.)
- When full, oldest items are dropped (`queue_dropped`)
- Optional consume-on-read
- Runtime resize via `PUT /api/v1/config/queue`
- **Not durable** — container restart clears queues; polling refills them

**Data item shape:**

```go
type DataItem struct {
    ID        uint64
    Data      []byte
    Timestamp time.Time
}
```

**Operations:**

1. **Store**: Push into the circular buffer
2. **Retrieve**: Read with pagination; optionally consume
3. **Drop**: Overwrite oldest when capacity is exceeded
4. **Count**: Track polling, drops, consumption in memory

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

- **Retry Logic**: Exponential backoff for recovery attempts
- **Circuit Breaker**: Prevents request storms during persistent failures
- **Recovery Validation**: Post-recovery validation ensures device actually works
- **Seed Validation**: TRNG data validated before seeding Fortuna
- **Logging**: All errors logged with context
- **Health Checks**: Regular polling for service availability

### Randomness Quality Metrics

The API service continuously monitors TRNG data quality using three NIST statistical tests. Metrics are calculated **before** data is stored, ensuring real-time quality assessment.

#### Data Flow with Quality Metrics

```mermaid
flowchart TD
    A["Controller Service"] -->|"HTTP GET /generate"| B[FetchAndStoreTRNGData]
    B -->|"Hex Decode"| C["dataBytes []byte"]
    C -->|"Calculate Metrics"| D[TRNGQualityTracker]
    D -->|"Update Counters"| E[QualityMetrics]
    E -->|"Store Data"| F["DB.StoreTRNGData"]

    G["GetStatus Endpoint"] -->|"Query Metrics"| D
    D -->|"Return Quality Stats"| H["DetailedStats with trng_quality"]
```

#### Monobit (Frequency) Test

**Purpose**: Tests the proportion of zeros and ones in the entire sequence. For truly random data, the proportion should be approximately 0.5 (50% zeros, 50% ones).

**What it detects**: Bias toward zeros or ones, indicating non-randomness or hardware degradation.

**Calculation**:
- Counts total bits processed
- Separately counts zeros and ones
- Calculates average: `average = ones / total`
- For random data: average ≈ 0.5

**Expected Values**:
- **Healthy TRNG**: Average between 0.49 and 0.51
- **Warning**: Average between 0.45-0.49 or 0.51-0.55
- **Critical**: Average < 0.45 or > 0.55

**Storage**: 3 int64 counters + 1 float64 = ~32 bytes

**Example Output**:
```json
{
  "monobit": {
    "zeros": 12345678,
    "ones": 12345678,
    "total": 24691356,
    "average": 0.5000123
  }
}
```

#### NIST SP 800-90B Repetition Count Test

**Purpose**: Detects if a specific value repeats too many times consecutively, which would indicate hardware failure or stuck bits.

**What it detects**: Consecutive identical values exceeding a threshold (default: 35 for 8-bit values).

**Algorithm**:
1. Tracks the most recent sample value
2. Counts consecutive repetitions
3. If count exceeds cutoff (35), increments failure counter
4. Resets count when value changes

**Expected Values**:
- **Healthy TRNG**: `failures = 0`
- **Warning**: `failures > 0` but infrequent
- **Critical**: `failures` increasing rapidly

**Storage**: Small circular buffer (~40 bytes) + counters = ~60 bytes

**Example Output**:
```json
{
  "repetition_count": {
    "failures": 0,
    "current_run": 1,
    "last_value": 66
  }
}
```

#### NIST SP 800-90B Adaptive Proportion Test (APT)

**Purpose**: Detects statistical bias by checking if the proportion of a specific value in a sliding window exceeds expected random distribution.

**What it detects**: Systematic bias where certain values appear more frequently than expected in a window of samples.

**Algorithm**:
1. Maintains a sliding window of recent samples (default: 512 bytes)
2. For each new sample, counts its occurrences in the current window
3. Calculates cutoff: `floor(0.5 × window_size) + 3 × √(window_size × 0.25)`
4. For 512 samples: cutoff ≈ 290
5. If count exceeds cutoff, increments bias counter

**Expected Values**:
- **Healthy TRNG**: `bias_count = 0` or very low
- **Warning**: `bias_count` slowly increasing
- **Critical**: `bias_count` increasing rapidly

**Storage**: Sliding window (512 bytes default, configurable) + metadata = ~536 bytes

**Configuration**:
- `TRNG_APT_WINDOW_SIZE`: Window size in bytes (default: 512, range: 256-2048)
- Larger windows: Better bias detection, more memory
- Smaller windows: Less memory, faster updates, may miss subtle biases

**Example Output**:
```json
{
  "apt": {
    "window_size": 512,
    "cutoff": 290,
    "bias_count": 0,
    "samples_processed": 123456
  }
}
```

#### Total Storage Requirements

**Per-Tracker Storage**:
- Monobit: ~32 bytes
- Repetition Count: ~60 bytes
- APT: ~536 bytes (configurable via `TRNG_APT_WINDOW_SIZE`)
- **Total**: ~628 bytes per tracker instance

**Memory Impact**: Negligible compared to queue sizes (500K items × 31 bytes = 15.5MB)

#### Interpreting Metrics

**Healthy TRNG Indicators**:
- Monobit average: 0.49 - 0.51
- Repetition count failures: 0
- APT bias count: 0 or very low
- All metrics stable over time

**Warning Signs**:
- Monobit average drifting away from 0.5
- Repetition count failures appearing
- APT bias count increasing
- Metrics fluctuating significantly

**Action Items**:
1. **Monobit average outside 0.45-0.55**: Check hardware connections, verify I2C communication
2. **Repetition failures**: Hardware may be stuck or degraded, check device health
3. **APT bias detected**: Possible systematic bias, review hardware configuration
4. **All metrics degraded**: Consider hardware replacement or service restart

### Fault Tolerance and Recovery

The system implements comprehensive fault tolerance mechanisms:

#### Controller Recovery State Machine

```mermaid
stateDiagram-v2
    [*] --> Unknown
    Unknown --> Healthy: Initialize Success
    Unknown --> Failed: Initialize Failure
    Healthy --> Failed: Hardware Failure Detected
    Failed --> Recovering: Start Recovery
    Recovering --> Validating: Reinitialize Success
    Validating --> Healthy: Validation Passes
    Validating --> Failed: Validation Fails
    Recovering --> Failed: Max Retries Exceeded
    Failed --> CircuitOpen: Consecutive Failures > Threshold
    CircuitOpen --> Recovering: Cooldown Period Expired
```

**Recovery Process:**
1. **Detection**: Hardware failure detected (0xFF patterns, I2C errors)
2. **Recovery Start**: Device enters RECOVERING state
3. **Reinitialize**: I2C connection recreated, device reinitialized
4. **Validation**: Multiple random samples generated and validated for quality
5. **Cooldown**: Extended validation period (default 30s) with continued checks
6. **Healthy**: Device marked HEALTHY only after all validations pass

**Circuit Breaker:**
- Opens after 5 consecutive failures
- Rejects all requests immediately when OPEN
- Transitions to HALF_OPEN after cooldown (default 5 minutes)
- Closes after successful test request in HALF_OPEN state

**Configuration:**
- `CONTROLLER_RECOVERY_COOLDOWN_SECONDS`: Cooldown period (default: 30)
- `CONTROLLER_RECOVERY_VALIDATION_ATTEMPTS`: Validation samples (default: 3)
- `CONTROLLER_CIRCUIT_BREAKER_COOLDOWN_MINUTES`: Circuit breaker cooldown (default: 5)

#### Fortuna Health Management

**Health Check Criteria:**
- Generator must be explicitly healthy (`IsHealthy = true`)
- Last reseed must be within max interval (default: 1 hour, configurable)
- Seed data validated for entropy and pattern quality

**Seed Validation:**
- Rejects seeds with >90% same byte value (0xFF patterns)
- Requires minimum Shannon entropy (3.0 bits per byte)
- Validates all seeds before accepting for reseeding

**Configuration:**
- `FORTUNA_MAX_RESEED_INTERVAL_HOURS`: Max time between reseeds (default: 1)

#### Cascade Failure Prevention

The API service prevents cascade failures through:

1. **TRNG Data Validation**: All controller data validated before use
2. **Seed Quality Checks**: Fortuna seeds validated for entropy
3. **Circuit Breaker**: Stops seeding attempts when controller consistently fails
4. **Health State Tracking**: Proper state management across services

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
6. **No entropy DB backups** - Queues are in-memory only; persist nothing sensitive from `/status`

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
- RAM: Queue capacity is bounded by configured sizes (defaults are large; Compose overrides them)

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

`FORTUNA_ADDR` is a single URL (port **8082**). VirtIO owns **8083**. Scale by running multiple API/controller stacks behind Traefik; use `CONTROLLER_ADDRS` for multi-device controllers. Multi-Fortuna round-robin is not implemented.

**Load balancing:**
- Use Traefik (Compose default) or another reverse proxy in front of the API
- Multiple API instances each hold their own in-memory queues (not shared)

### Limitations

- **Single ATECC608A**: Hardware generation rate is fixed (~10/sec)
- **In-memory only**: Queues are lost on restart; size is bounded by RAM
- **No distributed state**: Cannot share queues across API instances

## Next Steps

- **[Deployment Guide](deployment.md)** - Deploy to production
- **[API Examples](api-examples.md)** - Learn API usage
- **[Development Guide](development.md)** - Contribute to the project
- **[Hardware Setup](hardware-setup.md)** - Configure ATECC608A chip
