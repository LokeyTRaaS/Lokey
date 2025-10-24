# Quickstart Guide

Get LoKey running in 5 minutes with Docker Compose.

## Prerequisites

- **Docker** and **Docker Compose** installed
- (Optional) ATECC608A hardware connected to your system

> **Note**: LoKey will run in fallback mode without hardware, using time-based entropy. For production use with true hardware randomness, see [Hardware Setup](hardware-setup.md).

## Installation

### 1. Clone the Repository
```
bash
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey
```
### 2. Start the Services
```
bash
docker compose up -d
```
This starts three services:
- **Controller** (port 8081) - ATECC608A interface
- **Fortuna** (port 8082) - Cryptographic amplifier
- **API** (port 8080) - Main REST API

### 3. Verify Services Are Running
```
bash
# Check all services are healthy
docker compose ps

# Check API health
curl http://localhost:8080/api/v1/health
```
Expected response:
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
## First API Calls

### Get Random Integers
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "int32",
"limit": 10,
"source": "trng"
}'
```
**Response:**
```
json
[1847293847, -293847562, 847562938, 1923847562, -847293847, ...]
```
### Get Random Binary Data
```
bash
curl -X POST http://localhost:8080/api/v1/data \
-H "Content-Type: application/json" \
-d '{
"format": "binary",
"limit": 32,
"source": "fortuna"
}' --output random.bin
```
### Check System Status
```
bash
curl http://localhost:8080/api/v1/status
```
**Response:**
```
json
{
"trng": {
"polling_count": 150,
"queue_current": 85,
"queue_capacity": 100,
"queue_percentage": 85.0
},
"fortuna": {
"polling_count": 750,
"queue_current": 92,
"queue_capacity": 100,
"queue_percentage": 92.0
},
"database": {
"size_bytes": 1048576,
"size_human": "1.0 MB"
}
}
```
## Access the Documentation

### Interactive API Documentation

Open in your browser:
```

http://localhost:8080/swagger/index.html
```
Or view the online version:
- **[API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)** - Complete interactive docs
- **[OpenAPI JSON](https://lokeytraas.github.io/Lokey/swagger.json)** - Machine-readable spec
- **[OpenAPI YAML](https://lokeytraas.github.io/Lokey/swagger.yaml)** - YAML format

### Prometheus Metrics
```
bash
curl http://localhost:8080/metrics
```
## Common Operations

### Stop Services
```
bash
docker compose down
```
### View Logs
```
bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f api
docker compose logs -f controller
docker compose logs -f fortuna
```
### Restart Services
```
bash
docker compose restart
```
### Update to Latest Version
```
bash
docker compose pull
docker compose up -d
```
## Configuration

### Environment Variables

Edit `docker-compose.yaml` to configure the services:
```
yaml
services:
api:
environment:
- PORT=8080
- TRNG_QUEUE_SIZE=1000        # Increase queue size
- FORTUNA_QUEUE_SIZE=5000     # Increase queue size
- TRNG_POLL_INTERVAL_MS=500   # Poll more frequently
```
See [Deployment Guide](deployment.md#environment-variables) for complete configuration options.

### Persist Data

By default, data is stored in a Docker volume. To use a host directory:
```
yaml
services:
api:
volumes:
- ./data:/data  # Use local directory
```
## Data Formats

LoKey supports multiple output formats:

| Format   | Description                | Example Request                          |
|----------|----------------------------|------------------------------------------|
| `int8`   | Signed 8-bit integer       | `{"format":"int8","limit":10,...}`       |
| `uint8`  | Unsigned 8-bit integer     | `{"format":"uint8","limit":10,...}`      |
| `int16`  | Signed 16-bit integer      | `{"format":"int16","limit":10,...}`      |
| `uint16` | Unsigned 16-bit integer    | `{"format":"uint16","limit":10,...}`     |
| `int32`  | Signed 32-bit integer      | `{"format":"int32","limit":10,...}`      |
| `uint32` | Unsigned 32-bit integer    | `{"format":"uint32","limit":10,...}`     |
| `int64`  | Signed 64-bit integer      | `{"format":"int64","limit":10,...}`      |
| `uint64` | Unsigned 64-bit integer    | `{"format":"uint64","limit":10,...}`     |
| `binary` | Raw binary data            | `{"format":"binary","limit":1024,...}`   |

## Data Sources

### TRNG (Hardware Random)
```
json
{
"source": "trng"
}
```
- True random numbers from ATECC608A chip
- Lower throughput (~10 samples/second)
- Highest quality randomness
- Best for: Cryptographic keys, seeds

### Fortuna (Amplified Random)

```json
{
  "source": "fortuna"
}
```
```


- Cryptographically secure pseudo-random numbers
- High throughput (1000s/second)
- Seeded by hardware TRNG
- Best for: High-volume applications, simulations

## Troubleshooting

### Services Won't Start

```shell script
# Check logs
docker compose logs

# Check if ports are available
netstat -tulpn | grep -E '8080|8081|8082'
```


### "No data available" Error

The system needs time to generate initial data:

```shell script
# Wait 30 seconds for data generation
sleep 30

# Check status
curl http://localhost:8080/api/v1/status
```


### Database Errors

```shell script
# Reset database (WARNING: deletes all data)
docker compose down -v
docker compose up -d
```


### Hardware Not Detected

If using ATECC608A hardware:

```shell script
# Check I2C device (on Linux host)
i2cdetect -y 1

# View controller logs
docker compose logs controller
```


See [Hardware Setup](hardware-setup.md) for detailed troubleshooting.

## Next Steps

- **[API Examples](api-examples.md)** - Learn common usage patterns
- **[Hardware Setup](hardware-setup.md)** - Connect ATECC608A for true hardware randomness
- **[Deployment Guide](deployment.md)** - Deploy to Raspberry Pi or production
- **[Architecture](architecture.md)** - Understand how the system works
- **[API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)** - Complete API documentation

## Example Use Cases

### Generate Cryptographic Key Material

```shell script
# Generate 256 bits (32 bytes) of random data
curl -X POST http://localhost:8080/api/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "format": "binary",
    "limit": 32,
    "source": "trng"
  }' --output key.bin

# Verify size
ls -lh key.bin
```


### Monte Carlo Simulation

```shell script
# Generate 10000 random floats (convert uint32 to float yourself)
curl -X POST http://localhost:8080/api/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "format": "uint32",
    "limit": 10000,
    "source": "fortuna"
  }' > simulation_data.json
```


### Random Password Generation

```shell script
# Get random bytes for password generation
curl -X POST http://localhost:8080/api/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "format": "uint8",
    "limit": 32,
    "source": "trng"
  }'
```


You're now ready to use LoKey! See [API Examples](api-examples.md) for more detailed examples.
