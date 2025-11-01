# Deployment Guide

Complete guide for deploying LoKey to production environments.

## Table of Contents

- [Local Development](#local-development)
- [Raspberry Pi Deployment](#raspberry-pi-deployment)
- [Cross-Compilation](#cross-compilation)
- [Production Considerations](#production-considerations)
- [Environment Variables](#environment-variables)
- [Docker Images](#docker-images)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)

## Local Development

### Quick Start
```
bash
# Clone repository
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey

# Start services
docker compose up -d

# Verify
curl http://localhost:8080/api/v1/health
```
### Using Task

If you have [Task](https://taskfile.dev/) installed:
```
bash
# View available tasks
task

# Start development environment
task dev_up

# Stop services
task dev_down

# View logs
task dev_logs

# Rebuild services
task dev_rebuild
```
### Development Docker Compose

The default `docker-compose.yaml` is configured for development:
```
yaml
services:
controller:
build: ./cmd/controller
ports:
- "8081:8081"
environment:
- PORT=8081
- I2C_BUS_NUMBER=1
devices:
- /dev/i2c-1:/dev/i2c-1  # Hardware access

fortuna:
build: ./cmd/fortuna
ports:
- "8082:8082"
environment:
- PORT=8082
- AMPLIFICATION_FACTOR=4

api:
build: ./cmd/api
ports:
- "8080:8080"
environment:
- PORT=8080
- DB_PATH=/data/api.db
- CONTROLLER_ADDR=http://controller:8081
- FORTUNA_ADDR=http://fortuna:8082
- TRNG_QUEUE_SIZE=100
- FORTUNA_QUEUE_SIZE=100
- TRNG_POLL_INTERVAL_MS=1000
- FORTUNA_POLL_INTERVAL_MS=5000
volumes:
- api-data:/data

volumes:
api-data:
```
## Raspberry Pi Deployment

### Prerequisites

- Raspberry Pi Zero 2W (or newer)
- ATECC608A chip properly connected (see [Hardware Setup](hardware-setup.md))
- Raspberry Pi OS (64-bit recommended for Pi 4/5, 32-bit for older models)
- Docker installed on the Pi

### Step 1: Set Up Development Machine

Create a local registry for cross-compiled images.

**Create `buildx.toml` in project root:**
```
toml
[registry."192.168.1.100:5000"]  # Replace with your dev machine IP
insecure = true
http = true

insecure-entitlements = ["network.host", "security.insecure"]
```
**Build and push images:**
```
bash
# Using Task (recommended)
task build_images_and_registry

# Or manually
docker network create buildx-network
docker run -d -p 5000:5000 --network buildx-network --name registry registry:2

docker buildx create --use --name lokey-builder \
--config buildx.toml \
--driver-opt network=buildx-network

docker buildx build --platform linux/arm64 \
-t localhost:5000/lokey-controller:latest \
--push \
-f cmd/controller/Dockerfile.action .

docker buildx build --platform linux/arm64 \
-t localhost:5000/lokey-fortuna:latest \
--push \
-f cmd/fortuna/Dockerfile.action .

docker buildx build --platform linux/arm64 \
-t localhost:5000/lokey-api:latest \
--push \
-f cmd/api/Dockerfile.action .
```
### Step 2: Configure Raspberry Pi

**SSH into your Raspberry Pi:**
```
bash
ssh pi@raspberrypi.local
```
**Configure Docker to trust your local registry:**
```
bash
# Replace with your development machine's IP
DEV_IP=192.168.1.100

# Update Docker daemon configuration
echo "{
\"insecure-registries\": [\"$DEV_IP:5000\"]
}" | sudo tee /etc/docker/daemon.json

# Restart Docker
sudo systemctl restart docker
```
**Verify I2C is enabled:**
```
bash
# Check I2C device exists
ls -l /dev/i2c-1

# Scan for ATECC608A (should show 0x60)
i2cdetect -y 1
```
### Step 3: Create Docker Compose File on Pi

**Create `docker-compose.yaml` on the Raspberry Pi:**
```
yaml
version: '3.8'

services:
controller:
image: 192.168.1.100:5000/lokey-controller:latest  # Replace with your dev machine IP
container_name: lokey-controller
ports:
- "8081:8081"
environment:
- PORT=8081
- I2C_BUS_NUMBER=1
- FORCE_CONFIG=false  # Set to true only for initial device configuration
devices:
- /dev/i2c-1:/dev/i2c-1
restart: unless-stopped
logging:
driver: "json-file"
options:
max-size: "10m"
max-file: "3"

fortuna:
image: 192.168.1.100:5000/lokey-fortuna:latest
container_name: lokey-fortuna
ports:
- "8082:8082"
environment:
- PORT=8082
- AMPLIFICATION_FACTOR=4
depends_on:
- controller
restart: unless-stopped
logging:
driver: "json-file"
options:
max-size: "10m"
max-file: "3"

api:
image: 192.168.1.100:5000/lokey-api:latest
container_name: lokey-api
ports:
- "8080:8080"
environment:
- PORT=8080
- DB_PATH=/data/api.db
- CONTROLLER_ADDR=http://controller:8081
- FORTUNA_ADDR=http://fortuna:8082
- TRNG_QUEUE_SIZE=1000
- FORTUNA_QUEUE_SIZE=5000
- TRNG_POLL_INTERVAL_MS=1000
- FORTUNA_POLL_INTERVAL_MS=1000
volumes:
- ./data:/data  # Persist on host filesystem
depends_on:
- controller
- fortuna
restart: unless-stopped
logging:
driver: "json-file"
options:
max-size: "10m"
max-file: "3"
```
### Step 4: Deploy on Raspberry Pi
```
bash
# Pull images from your local registry
docker compose pull

# Start services
docker compose up -d

# Verify all services are running
docker compose ps

# Check logs
docker compose logs -f

# Test API
curl http://localhost:8080/api/v1/health
```
### Step 5: Set Up as System Service (Optional)

To start LoKey automatically on boot:

**Create `/etc/systemd/system/lokey.service`:**
```
ini
[Unit]
Description=LoKey Random Number Generation Service
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/home/pi/lokey
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down
User=pi

[Install]
WantedBy=multi-user.target
```
**Enable and start:**
```
bash
sudo systemctl daemon-reload
sudo systemctl enable lokey
sudo systemctl start lokey

# Check status
sudo systemctl status lokey
```
## Cross-Compilation

### Architecture Support

LoKey supports multiple architectures:

- **AMD64** (x86_64) - Desktop, laptop, cloud servers
- **ARM64** (aarch64) - Raspberry Pi 4/5, Apple Silicon, cloud ARM instances
- **ARMv7** (armhf) - Raspberry Pi 2/3, older ARM devices

### GitHub Actions Build

The CI/CD pipeline automatically builds multi-architecture images. See `.github/workflows/build-arm64.yml`.

**Pull pre-built images:**
```
bash
# AMD64
docker pull ghcr.io/lokeytraas/lokey/controller:latest-amd64
docker pull ghcr.io/lokeytraas/lokey/api:latest-amd64
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-amd64

# ARM64 (Raspberry Pi 4/5)
docker pull ghcr.io/lokeytraas/lokey/controller:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/api:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-arm64

# ARMv7 (Raspberry Pi 2/3)
docker pull ghcr.io/lokeytraas/lokey/controller:latest-armv7
docker pull ghcr.io/lokeytraas/lokey/api:latest-armv7
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-armv7
```
### Local Cross-Compilation

**Build for specific architecture:**
```
bash
# Set up buildx
docker buildx create --use --name multi-arch-builder

# Build for ARM64
docker buildx build \
--platform linux/arm64 \
-t lokey-controller:arm64 \
-f cmd/controller/Dockerfile.action \
--load \
.

# Build for ARMv7
docker buildx build \
--platform linux/arm/v7 \
-t lokey-controller:armv7 \
-f cmd/controller/Dockerfile.action \
--load \
.
```
## Production Considerations

### Security

**1. Use HTTPS/TLS**

Place LoKey behind a reverse proxy with TLS:
```
nginx
# Nginx example
server {
listen 443 ssl http2;
server_name lokey.example.com;

    ssl_certificate /etc/ssl/certs/lokey.crt;
    ssl_certificate_key /etc/ssl/private/lokey.key;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```
**2. Restrict Network Access**
```
yaml
# docker-compose.yaml
services:
controller:
networks:
- internal
# Don't expose port externally

api:
networks:
- internal
ports:
- "127.0.0.1:8080:8080"  # Only local access

networks:
internal:
internal: true  # No external access
```
**3. Authentication (Recommended)**

Add authentication middleware in front of the API:

- API Gateway (Kong, Tyk)
- OAuth2 Proxy
- Custom authentication service

### Performance Tuning

**Queue Sizes:**

Adjust based on your workload:
```
yaml
environment:
# High throughput
- TRNG_QUEUE_SIZE=5000
- FORTUNA_QUEUE_SIZE=50000

# Low latency
- TRNG_QUEUE_SIZE=100
- FORTUNA_QUEUE_SIZE=1000
```
**Polling Intervals:**
```
yaml
environment:
# Faster generation (higher CPU usage)
- TRNG_POLL_INTERVAL_MS=100
- FORTUNA_POLL_INTERVAL_MS=100

# Slower generation (lower CPU usage)
- TRNG_POLL_INTERVAL_MS=5000
- FORTUNA_POLL_INTERVAL_MS=5000
```
**Resource Limits:**
```
yaml
services:
api:
deploy:
resources:
limits:
cpus: '1.0'
memory: 512M
reservations:
cpus: '0.5'
memory: 256M
```
### High Availability

**Multiple Fortuna Instances:**
```
yaml
services:
fortuna-1:
image: lokey-fortuna:latest
# ... config ...

fortuna-2:
image: lokey-fortuna:latest
# ... config ...

api:
environment:
- FORTUNA_ADDR=http://fortuna-1:8082,http://fortuna-2:8082
```
**Database Backups:**
```
bash
# Backup script
#!/bin/bash
DATE=$(date +%Y%m%d_%H%M%S)
docker compose exec api cp /data/api.db /data/backup_${DATE}.db

# Rotate old backups
find /path/to/data -name "backup_*.db" -mtime +7 -delete
```
### Storage Management

**Monitor Database Size:**
```
bash
# Check size
docker compose exec api ls -lh /data/api.db

# Set up alerts when size exceeds threshold
curl http://localhost:8080/api/v1/status | jq '.database.size_bytes'
```
**Database Maintenance:**

The BoltDB database is self-managing, but you can:
```
bash
# Stop services
docker compose down

# Compact database (optional)
# BoltDB automatically handles compaction

# Start services
docker compose up -d
```
## Environment Variables

### API Service

| Variable                  | Description                          | Default                  | Valid Range         |
|---------------------------|--------------------------------------|--------------------------|---------------------|
| `PORT`                    | API server port                      | `8080`                   | 1-65535             |
| `DB_PATH`                 | Path to BoltDB file                  | `/data/api.db`           | Any valid path      |
| `CONTROLLER_ADDR`         | Controller service URL               | `http://controller:8081` | Valid HTTP URL      |
| `FORTUNA_ADDR`            | Fortuna service URL                  | `http://fortuna:8082`    | Valid HTTP URL      |
| `TRNG_QUEUE_SIZE`         | TRNG data queue capacity             | `100`                    | 10-10000            |
| `FORTUNA_QUEUE_SIZE`      | Fortuna data queue capacity          | `100`                    | 10-10000            |
| `TRNG_POLL_INTERVAL_MS`   | TRNG polling interval (milliseconds) | `1000`                   | 100-60000           |
| `FORTUNA_POLL_INTERVAL_MS`| Fortuna polling interval (ms)        | `5000`                   | 100-60000           |

### Controller Service

| Variable          | Description                        | Default | Valid Range |
|-------------------|------------------------------------|---------|-------------|
| `PORT`            | Controller server port             | `8081`  | 1-65535     |
| `I2C_BUS_NUMBER`  | I2C bus for ATECC608A              | `1`     | 0-10        |
| `FORCE_CONFIG`    | Force ATECC608A configuration      | `false` | true/false  |
| `DISABLE_AUTO_CONFIG` | Disable automatic configuration | `false` | true/false  |

### Fortuna Service

| Variable               | Description                    | Default | Valid Range |
|------------------------|--------------------------------|---------|-------------|
| `PORT`                 | Fortuna server port            | `8082`  | 1-65535     |
| `AMPLIFICATION_FACTOR` | Data amplification multiplier  | `4`     | 1-100       |

### Example Configuration

**Development:**
```
yaml
environment:
- TRNG_QUEUE_SIZE=100
- FORTUNA_QUEUE_SIZE=100
- TRNG_POLL_INTERVAL_MS=1000
- FORTUNA_POLL_INTERVAL_MS=5000
```
**Production (High Throughput):**
```
yaml
environment:
- TRNG_QUEUE_SIZE=5000
- FORTUNA_QUEUE_SIZE=50000
- TRNG_POLL_INTERVAL_MS=500
- FORTUNA_POLL_INTERVAL_MS=100
- AMPLIFICATION_FACTOR=10
```
**Production (Low Resource):**
```
yaml
environment:
- TRNG_QUEUE_SIZE=500
- FORTUNA_QUEUE_SIZE=2000
- TRNG_POLL_INTERVAL_MS=2000
- FORTUNA_POLL_INTERVAL_MS=2000
- AMPLIFICATION_FACTOR=4
```
## Docker Images

### Official Images

Pre-built images are available on GitHub Container Registry:
```
bash
# Latest stable
docker pull ghcr.io/lokeytraas/lokey/api:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/controller:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-arm64

# Specific version
docker pull ghcr.io/lokeytraas/lokey/api:v1.0.0-arm64
```
### Building Custom Images
```
bash
# Build all services
docker compose build

# Build specific service
docker compose build api

# Build with specific Dockerfile
docker build -t lokey-api:custom -f cmd/api/Dockerfile.action .
```
## Monitoring

### Health Checks

All services provide health endpoints:
```
bash
# API health
curl http://localhost:8080/api/v1/health

# Controller health
curl http://localhost:8081/health

# Fortuna health
curl http://localhost:8082/health
```
### Prometheus Metrics

Metrics available at `/metrics`:
```
bash
curl http://localhost:8080/metrics
```
**Key metrics to monitor:**
- `trng_queue_percentage` - TRNG queue utilization
- `fortuna_queue_percentage` - Fortuna queue utilization
- `database_size_bytes` - Database size
- `trng_consumed` - Total TRNG values consumed
- `fortuna_consumed` - Total Fortuna values consumed

### Logging

View logs:
```
bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f api

# Last 100 lines
docker compose logs --tail=100 api

# Follow with timestamps
docker compose logs -f -t api
```
### Alerting Rules

**Prometheus alert examples:**
```
yaml
groups:
- name: lokey
  rules:
    - alert: LowTRNGQueue
      expr: trng_queue_percentage < 20
      for: 5m
      annotations:
      summary: "TRNG queue running low"

    - alert: HighDatabaseSize
      expr: database_size_bytes > 1073741824  # 1GB
      for: 10m
      annotations:
      summary: "Database size exceeding 1GB"

    - alert: ServiceDown
      expr: up{job="lokey"} == 0
      for: 1m
      annotations:
      summary: "LoKey service is down"
```
## Troubleshooting

### Services Won't Start
```
bash
# Check logs
docker compose logs

# Check if ports are available
sudo netstat -tulpn | grep -E '8080|8081|8082'

# Verify Docker is running
sudo systemctl status docker
```
### No Hardware Detected
```
bash
# Check I2C device (on Raspberry Pi)
ls -l /dev/i2c-1

# Scan I2C bus
i2cdetect -y 1

# Check container has device access
docker compose exec controller ls -l /dev/i2c-1
```
### Database Errors
```
bash
# Check database permissions
docker compose exec api ls -la /data

# Check disk space
df -h

# Reset database (WARNING: deletes all data)
docker compose down -v
docker compose up -d
```
### Performance Issues
```
bash
# Check resource usage
docker stats

# Check queue status
curl http://localhost:8080/api/v1/status

# Adjust polling intervals
# Edit docker-compose.yaml and restart
```
### Network Issues
```
bash
# Test service connectivity
docker compose exec api curl http://controller:8081/health
docker compose exec api curl http://fortuna:8082/health

# Check network
docker network ls
docker network inspect lokey_default
```
## Next Steps

- **[Hardware Setup](hardware-setup.md)** - Configure ATECC608A chip
- **[Architecture](architecture.md)** - Understand system design
- **[API Examples](api-examples.md)** - Learn API usage patterns
- **[Development Guide](development.md)** - Contribute to the project
