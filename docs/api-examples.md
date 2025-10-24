# API Examples

Practical examples for using the LoKey API.

## 📚 Full API Reference

**[Interactive API Documentation →](https://lokeytraas.github.io/Lokey/api-reference.html)**

Complete OpenAPI specification with all endpoints, schemas, and response codes. Auto-generated from source code.

**Download raw specs:**
- [swagger.json](https://lokeytraas.github.io/Lokey/swagger.json)
- [swagger.yaml](https://lokeytraas.github.io/Lokey/swagger.yaml)

## Quick Examples

### Get Random Integers (TRNG)

Generate true random integers from hardware:
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "int32",
"limit": 10,
"offset": 0,
"source": "trng"
}'
```
**Response:**
```
json
[1847293847, -293847562, 847562938, 1923847562, -847293847, 293847562, -1847293847, 847293847, 1293847562, -923847562]
```
### Get Random Unsigned Integers (Fortuna)

For high-throughput applications, use Fortuna amplified randomness:
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint64",
"limit": 100,
"source": "fortuna"
}'
```
**Response:**
```
json
[18446744073709551615, 9223372036854775807, 4611686018427387903, ...]
```
### Get Binary Random Data

Download raw binary random data (e.g., for cryptographic keys):
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "binary",
"limit": 1024,
"source": "trng"
}' --output random.bin
```
**Verify the file:**
```
bash
ls -lh random.bin
# Output: -rw-r--r-- 1 user user 1.0K Jan 15 10:30 random.bin

hexdump -C random.bin | head
```
### Get Small Random Values

Generate random bytes (0-255):
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint8",
"limit": 20,
"source": "trng"
}'
```
**Response:**
```
json
[142, 37, 219, 88, 193, 42, 156, 77, 231, 19, 105, 244, 63, 128, 91, 177, 15, 203, 66, 184]
```
## Health & Status

### Check System Health

Verify all services are operational:
```
bash
curl http://localhost:8080/api/v1/health
```
**Response:**
```
json
{
"status": "healthy",
"timestamp": "2024-01-15T10:30:00Z",
"details": {
"api": true,
"controller": true,
"fortuna": true,
"database": true
}
}
```
**Status meanings:**
- `healthy` - All systems operational
- `degraded` - Some services unavailable but API still working
- `unhealthy` - Critical failures

### Get System Status

View detailed queue levels, generation rates, and storage metrics:
```
bash
curl http://localhost:8080/api/v1/status
```
**Response:**
```
json
{
"trng": {
"polling_count": 1000,
"queue_current": 85,
"queue_capacity": 100,
"queue_percentage": 85.0,
"queue_dropped": 5,
"consumed_count": 500,
"unconsumed_count": 85,
"total_generated": 1000
},
"fortuna": {
"polling_count": 5000,
"queue_current": 92,
"queue_capacity": 100,
"queue_percentage": 92.0,
"queue_dropped": 50,
"consumed_count": 4500,
"unconsumed_count": 92,
"total_generated": 5000
},
"database": {
"size_bytes": 10485760,
"size_human": "10.0 MB",
"path": "/data/api.db"
}
}
```
**Key metrics:**
- `queue_current` - Available random values right now
- `queue_percentage` - Queue utilization (high = good, low = may run out)
- `queue_dropped` - Values discarded when queue was full
- `consumed_count` - Total values retrieved by clients
- `unconsumed_count` - Values available for retrieval

## Configuration

### Get Queue Configuration
```
bash
curl http://localhost:8080/api/v1/config/queue
```
**Response:**
```
json
{
"trng_queue_size": 100,
"fortuna_queue_size": 100
}
```
### Update Queue Sizes

Adjust queue capacity for your workload:
```
bash
curl -X PUT http://localhost:8080/api/v1/config/queue \
-H "Content-Type: application/json" \
-d '{
"trng_queue_size": 1000,
"fortuna_queue_size": 5000
}'
```
**Response:**
```
json
{
"trng_queue_size": 1000,
"fortuna_queue_size": 5000
}
```
**Guidelines:**
- Larger queues = more buffered data, higher memory usage
- Smaller queues = less memory, may run out during bursts
- TRNG queues typically smaller (slower generation)
- Fortuna queues can be larger (faster generation)

## Data Formats

### Supported Formats

| Format   | Description                | Bytes per Value | Range                      | Use Case                          |
|----------|----------------------------|-----------------|----------------------------|-----------------------------------|
| `int8`   | Signed 8-bit integer       | 1               | -128 to 127                | Small random values               |
| `uint8`  | Unsigned 8-bit integer     | 1               | 0 to 255                   | Bytes, passwords                  |
| `int16`  | Signed 16-bit integer      | 2               | -32,768 to 32,767          | Medium range values               |
| `uint16` | Unsigned 16-bit integer    | 2               | 0 to 65,535                | Port numbers, IDs                 |
| `int32`  | Signed 32-bit integer      | 4               | -2³¹ to 2³¹-1              | General purpose                   |
| `uint32` | Unsigned 32-bit integer    | 4               | 0 to 2³²-1                 | General purpose, hashes           |
| `int64`  | Signed 64-bit integer      | 8               | -2⁶³ to 2⁶³-1              | Large numbers, timestamps         |
| `uint64` | Unsigned 64-bit integer    | 8               | 0 to 2⁶⁴-1                 | Large numbers, UUIDs              |
| `binary` | Raw binary data            | 1               | 0x00 to 0xFF (per byte)    | Keys, salts, raw entropy          |

### Format Examples

**8-bit unsigned (bytes):**
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-d '{"format":"uint8","limit":16,"source":"trng"}'
```
**16-bit signed:**
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-d '{"format":"int16","limit":100,"source":"fortuna"}'
```
**64-bit unsigned (large numbers):**
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-d '{"format":"uint64","limit":10,"source":"trng"}'
```
## Data Sources

### TRNG (True Random Number Generator)

Hardware-based true random numbers from ATECC608A chip.
```
json
{
"source": "trng"
}
```
**Characteristics:**
- **Source**: Physical entropy from ATECC608A hardware
- **Rate**: ~10 samples/second (configurable via polling interval)
- **Quality**: True entropy from physical processes
- **Latency**: May need to wait for data generation
- **Use case**: Cryptographic keys, seeds, nonces, high-security applications

**Example:**
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "binary",
"limit": 32,
"source": "trng"
}' --output crypto-key.bin
```
### Fortuna (Cryptographic PRNG)

Cryptographically secure pseudo-random numbers amplified from TRNG seeds.
```
json
{
"source": "fortuna"
}
```
**Characteristics:**
- **Source**: Fortuna algorithm seeded by hardware TRNG
- **Rate**: High throughput (1000s of samples/second)
- **Quality**: Cryptographically secure, periodically reseeded with TRNG
- **Latency**: Low, usually immediate
- **Use case**: High-volume applications, simulations, gaming, testing

**Example:**
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint32",
"limit": 10000,
"source": "fortuna"
}' > simulation-data.json
```
## Common Patterns

### Batch Requests

Request multiple values efficiently:
```
bash
# Get 10,000 random integers
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint32",
"limit": 10000,
"source": "fortuna"
}'
```
### Pagination

Use `offset` for pagination (note: data is consumed after reading by default):
```
bash
# First page
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "int32",
"limit": 100,
"offset": 0,
"source": "trng"
}'

# Second page (if available)
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "int32",
"limit": 100,
"offset": 100,
"source": "trng"
}'
```
**Note:** Data is consumed (deleted) after retrieval by default. Pagination works best when queue has sufficient data.

### Stream Processing

For continuous data needs:
```
bash
# Continuously fetch data
while true; do
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{"format":"uint32","limit":1000,"source":"fortuna"}' \
>> output.json
echo "," >> output.json
sleep 1
done
```
## Use Cases

### Cryptographic Key Generation

Generate a 256-bit AES key:
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "binary",
"limit": 32,
"source": "trng"
}' --output aes-256-key.bin

# Verify
ls -lh aes-256-key.bin
xxd aes-256-key.bin
```
### UUID Generation

Generate random components for UUIDs:
```
bash
# Get random bytes for UUID v4
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint8",
"limit": 16,
"source": "trng"
}'
```
### Password Salt Generation

Generate cryptographic salts:
```
bash
# 16-byte salt
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "binary",
"limit": 16,
"source": "trng"
}' --output salt.bin

# Base64 encode for storage
base64 salt.bin
```
### Monte Carlo Simulation

Generate large datasets for simulations:
```
bash
# 100,000 random values for simulation
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint32",
"limit": 100000,
"source": "fortuna"
}' > monte-carlo-data.json
```
### Gaming/Lottery

Fair random number generation:
```
bash
# Roll a die (1-6)
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "uint8",
"limit": 1,
"source": "trng"
}' | jq '.[0] % 6 + 1'

# Pick lottery numbers (1-49)
curl -X POST http://localhost:8080/api/v1/data \
-d '{"format":"uint8","limit":6,"source":"trng"}' \
| jq 'map(. % 49 + 1)'
```
### Session Token Generation

Generate secure session tokens:
```
bash
# 32-byte session token
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "binary",
"limit": 32,
"source": "trng"
}' --output token.bin

# Convert to hex for use as token
xxd -p token.bin | tr -d '\n'
```
## Monitoring & Metrics

### Prometheus Metrics

Metrics available at `/metrics` and `/api/v1/metrics`:
```
bash
curl http://localhost:8080/metrics
```
**Key metrics:**
- `trng_queue_current` - Current TRNG queue size
- `trng_queue_capacity` - Maximum TRNG queue size
- `trng_queue_percentage` - TRNG queue utilization %
- `trng_consumed` - Total TRNG values consumed
- `trng_unconsumed` - Current unconsumed TRNG values
- `fortuna_queue_current` - Current Fortuna queue size
- `fortuna_queue_capacity` - Maximum Fortuna queue size
- `fortuna_queue_percentage` - Fortuna queue utilization %
- `fortuna_consumed` - Total Fortuna values consumed
- `fortuna_unconsumed` - Current unconsumed Fortuna values
- `database_size_bytes` - Database size in bytes

### Grafana Dashboard Example

Query examples for visualization:
```
promql
# Queue utilization over time
trng_queue_percentage
fortuna_queue_percentage

# Generation rate
rate(trng_consumed[5m])
rate(fortuna_consumed[5m])

# Database growth
database_size_bytes
```
## Error Responses

### Not Enough Data Available

```json
{
  "error": "No data available"
}
```
```


**Solution:** 
- Wait for more data to be generated
- Check queue status: `curl http://localhost:8080/api/v1/status`
- Use Fortuna source for higher throughput
- Increase polling interval (see [Deployment Guide](deployment.md))

### Invalid Request Format

```json
{
  "error": "Invalid request body"
}
```


**Solution:** Check that:
- `format` is one of the supported types
- `limit` is between 1 and 100,000
- `offset` is >= 0
- `source` is either "trng" or "fortuna"
- JSON is properly formatted

### Service Unavailable

```json
{
  "error": "Database is not healthy"
}
```


**Solution:**
- Check service health: `curl http://localhost:8080/api/v1/health`
- View logs: `docker compose logs api`
- Restart services: `docker compose restart`

### Queue Configuration Error

```json
{
  "error": "Failed to update queue configuration"
}
```


**Solution:** Ensure queue sizes are within valid range (10-10000).

## Python Integration Example

```textmate
import requests
import json

API_URL = "http://localhost:8080"

def get_random_data(format="int32", limit=10, source="trng"):
    """Get random data from LoKey API"""
    response = requests.post(
        f"{API_URL}/api/v1/data",
        headers={"Content-Type": "application/json"},
        json={
            "format": format,
            "limit": limit,
            "offset": 0,
            "source": source
        }
    )
    response.raise_for_status()
    return response.json()

def get_random_bytes(num_bytes, source="trng"):
    """Get random binary data"""
    response = requests.post(
        f"{API_URL}/api/v1/data",
        headers={"Content-Type": "application/json"},
        json={
            "format": "binary",
            "limit": num_bytes,
            "source": source
        }
    )
    response.raise_for_status()
    return response.content

# Examples
random_ints = get_random_data(format="int32", limit=100, source="fortuna")
random_bytes = get_random_bytes(32, source="trng")
```


## Next Steps

- **[API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)** - Complete interactive API documentation
- **[Architecture](architecture.md)** - Understand how the system works
- **[Deployment Guide](deployment.md)** - Deploy to production
- **[Development Guide](development.md)** - Contribute to the project
