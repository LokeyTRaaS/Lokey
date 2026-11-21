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

The root [`docker-compose.yaml`](../docker-compose.yaml) runs the **full stack**: Traefik (HTTPS), API, Fortuna, VirtIO, emulated controllers, Prometheus, and Grafana. Only Traefik publishes host ports; API/controllers are not bound to `localhost:8080`.

### Quick Start

```bash
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey
cp .env.template .env   # optional; adjust ports/domain/passwords
mkdir -p run/lokey
docker compose up -d --build
curl -k https://localhost/api/v1/health
```

Override ports via `.env` (`TRAEFIK_HTTPS_PORT=9443`, etc.). Controllers are selected with `COMPOSE_PROFILES`:

| Profile | Services | Use when |
|---------|----------|----------|
| `emulated` (default in `.env.template`) | `controller-a` + `controller-b` | Laptop / CI (no chip) |
| `hardware` | `controller-hw-a` only | Pi with ATECC608A/B on I2C bus 1 (`/dev/i2c-1`, address `0x60`) |

For hardware, also set `CONTROLLER_ADDR` / `CONTROLLER_ADDRS` to `http://controller-hw-a:8081` (see `.env.template`). Wiring: [Hardware Setup](hardware-setup.md).

### Using Task

If you have [Task](https://taskfile.dev/) installed:

```bash
task            # list tasks
task dev_up
task dev_down
task dev_logs
task dev_rebuild
```

### Networks

| Network         | Role                                      |
|-----------------|-------------------------------------------|
| `lokey-edge`    | Traefik                                   |
| `lokey-app`     | API, Fortuna, VirtIO                      |
| `lokey-devices` | Controllers only (no host publish)        |
| `lokey-metrics` | Prometheus / Grafana                      |

Swagger UI (via Traefik): `https://localhost/swagger/`. Grafana: `https://localhost/grafana/` (default password from `GRAFANA_PASSWORD`).

## Raspberry Pi Deployment

### Prerequisites

- Raspberry Pi 5 (ARM64) or x86_64 host
- ATECC608A chip properly connected (see [Hardware Setup](hardware-setup.md))
- Raspberry Pi OS 64-bit (or NixOS) on Pi 5
- Docker Engine + Compose plugin

### Host notes (Pi OS / Debian multiarch)

On some Raspberry Pi OS images, `iptables` is installed as **armhf** while the kernel/userspace is **arm64**. That makes `iptables` segfault and Docker fail to start (`failed to create NAT chain DOCKER`). Fix by installing the arm64 package:

```bash
# Confirm: file $(sudo which xtables-nft-multi) should say ARM aarch64, not ARM EABI5
curl -fsSLO http://deb.debian.org/debian/pool/main/i/iptables/libip4tc2_1.8.11-2_arm64.deb
curl -fsSLO http://deb.debian.org/debian/pool/main/i/iptables/libip6tc2_1.8.11-2_arm64.deb
curl -fsSLO http://deb.debian.org/debian/pool/main/i/iptables/iptables_1.8.11-2_arm64.deb
sudo dpkg -i libip4tc2_1.8.11-2_arm64.deb libip6tc2_1.8.11-2_arm64.deb iptables_1.8.11-2_arm64.deb
sudo systemctl restart docker
```

Also ensure the `lokey` user is in the `docker` group (`sudo usermod -aG docker lokey`).

**Cgroups / namespaces (required for Docker + cAdvisor):** on Pi OS, `/boot/firmware/cmdline.txt` must include `cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1`. Pi firmware may still inject `cgroup_disable=memory` early in `/proc/cmdline`; the enable flags override it — verify with `cat /sys/fs/cgroup/cgroup.controllers` (must list `memory`). Kernel namespaces (`CONFIG_NAMESPACES`, pid/net/user/mnt/uts/ipc) are on by default on Pi OS 64-bit.

**cAdvisor + Docker 29:** Docker 29 defaults to the containerd snapshotter (`Storage Driver: overlayfs`). cAdvisor before v0.54 cannot resolve container layers and only exposes `id="/"`. This compose file pins `ghcr.io/google/cadvisor:v0.61.1` (or newer) for that layout.

### Quick bring-up (GHCR images)

```bash
git clone https://github.com/LokeyTRaaS/Lokey.git && cd Lokey
mkdir -p run/lokey
cp .env.template .env
# Defaults track GHCR :latest (pull_policy: always). To pin a release, see docs/releasing.md:
# LOKEY_IMAGE_API=ghcr.io/lokeytraas/lokey/api:1.2.0
# (same for controller / fortuna / virtio)
docker compose pull
docker compose up -d
curl -k https://localhost/api/v1/health
```

### Step 1: Set Up Development Machine

Create a local registry for cross-compiled images.

**Create `buildx.toml` in project root:**
```toml
[registry."192.168.1.100:5000"]  # Replace with your dev machine IP
insecure = true
http = true

insecure-entitlements = ["network.host", "security.insecure"]
```
**Build and push images:**
```bash
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
```bash
ssh pi@raspberrypi.local
```
**Configure Docker to trust your local registry:**
```bash
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
```bash
# Check I2C device exists
ls -l /dev/i2c-1

# Scan for ATECC608A (should show 0x60)
i2cdetect -y 1
```
### Step 3: Create Docker Compose File on Pi

**Option A: Use the provided docker-compose.yaml (Recommended)**

Copy the `docker-compose.yaml` from the repository to your Raspberry Pi and start the stack:

```bash
# On Raspberry Pi
docker compose up -d
```

Entropy queues are held in memory (no on-disk database). Prefer SSD/NVMe on Pi 5 when possible; the old `local-tmpfs` binary-copy profile has been removed.

**Option B: Create custom docker-compose.yaml**

**Create `docker-compose.yaml` on the Raspberry Pi:**
```yaml
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
- CONTROLLER_ADDR=http://controller:8081
- FORTUNA_ADDR=http://fortuna:8082
- TRNG_QUEUE_SIZE=1000
- FORTUNA_QUEUE_SIZE=5000
- TRNG_POLL_INTERVAL_MS=1000
- FORTUNA_POLL_INTERVAL_MS=1000
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

**Using the provided docker-compose.yaml (Recommended):**
```bash
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

**Using custom docker-compose.yaml:**
```bash
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

> **Note**: Queue data lives in RAM only. After a restart, polling refills the buffers. Size queues for available memory on the Pi.
### Step 5: Set Up as System Service (Optional)

To start LoKey automatically on boot:

**Create `/etc/systemd/system/lokey.service`:**
```ini
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
```bash
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

### GitHub Actions Build

The CI/CD pipeline automatically builds multi-architecture images. See `.github/workflows/build.yml`.

**Pull pre-built images:**
```bash
# AMD64
docker pull ghcr.io/lokeytraas/lokey/controller:latest-amd64
docker pull ghcr.io/lokeytraas/lokey/api:latest-amd64
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-amd64

# ARM64 (Raspberry Pi 4/5)
docker pull ghcr.io/lokeytraas/lokey/controller:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/api:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-arm64

```
### Local Cross-Compilation

**Build for specific architecture:**
```bash
# Set up buildx
docker buildx create --use --name multi-arch-builder

# Build for ARM64
docker buildx build \
--platform linux/arm64 \
-t lokey-controller:arm64 \
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
```yaml
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
```yaml
environment:
# High throughput
- TRNG_QUEUE_SIZE=5000
- FORTUNA_QUEUE_SIZE=50000

# Low latency
- TRNG_QUEUE_SIZE=100
- FORTUNA_QUEUE_SIZE=1000
```
**Polling Intervals:**
```yaml
environment:
# Faster generation (higher CPU usage)
- TRNG_POLL_INTERVAL_MS=100
- FORTUNA_POLL_INTERVAL_MS=100

# Slower generation (lower CPU usage)
- TRNG_POLL_INTERVAL_MS=5000
- FORTUNA_POLL_INTERVAL_MS=5000
```
**Resource Limits:**
```yaml
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

`FORTUNA_ADDR` accepts a **single** URL. For higher availability today, run redundant full stacks behind Traefik/load balancer, or scale controllers via `CONTROLLER_ADDRS` (`id=http://host:port,...`). Multi-Fortuna round-robin is not implemented.

**Queue / storage notes:**

There is no on-disk entropy database to back up. Status reports in-memory usage:

```bash
curl -sk https://localhost/api/v1/status | jq '.database'
# {"size_bytes":0,"size_human":"0 B (in-memory)","path":"memory://channels"}
```

After `docker compose down` / restart, queues are empty until polling refills them.
## Environment Variables

Defaults below match [`cmd/api/main.go`](../cmd/api/main.go) and sibling services. Compose often **overrides** queues/intervals to smaller values — see `docker-compose.yaml`.

### Compose / Traefik (host)

| Variable | Description | Default |
|----------|-------------|---------|
| `TRAEFIK_HTTP_PORT` | Host HTTP port | `80` |
| `TRAEFIK_HTTPS_PORT` | Host HTTPS port | `443` |
| `LOKEY_DOMAIN` | Hostname for Traefik / Grafana URLs | `localhost` |
| `LOKEY_ACME_EMAIL` | ACME email when using Let's Encrypt | (empty / file certs) |
| `GRAFANA_USER` / `GRAFANA_PASSWORD` | Grafana admin | `admin` / `lokey` |

### API Service

| Variable | Description | Default | Notes |
|----------|-------------|---------|-------|
| `PORT` | API listen port | `8080` | Not published on host when behind Traefik |
| `CONTROLLER_ADDR` | Single controller URL (legacy) | `http://controller:8081` | Used if `CONTROLLER_ADDRS` unset |
| `CONTROLLER_ADDRS` | Multi-controller list | (empty) | Format `id=http://host:port,id2=...` |
| `FORTUNA_ADDR` | Fortuna URL (**single** host) | `http://fortuna:8082` | No CSV multi-instance support |
| `VIRTIO_ADDR` | VirtIO URL | `http://virtio:8083` | |
| `TRNG_QUEUE_SIZE` | In-memory TRNG queue capacity | `1000000` | Compose usually lowers this |
| `FORTUNA_QUEUE_SIZE` | In-memory Fortuna queue capacity | `1000000` | |
| `VIRTIO_QUEUE_SIZE` | API-side VirtIO queue capacity | `500000` | VirtIO service has its own queue |
| `TRNG_POLL_INTERVAL_MS` | Controller poll interval | `100` | |
| `FORTUNA_POLL_INTERVAL_MS` | Fortuna poll interval | `100` | |
| `VIRTIO_POLL_INTERVAL_MS` | Parsed/logged only | `100` | VirtIO is **not** polled; seeding is timer-based (~30s hardcoded) |
| `VIRTIO_SEEDING_SOURCE` | Seeding source | `trng` | `trng`, `fortuna`, or `both` |
| `CORS_ALLOWED_ORIGIN` | CORS `Access-Control-Allow-Origin` | `*` | Restrict in browser-exposed deploys |

### Controller Service

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Listen port | `8081` |
| `CONTROLLER_MODE` | `emulated` or `hardware` | Compose: `emulated` |
| `I2C_BUS_NUMBER` | I2C bus for ATECC608A | `1` |
| `FORCE_CONFIG` | Force ATECC configuration | `false` |
| `DISABLE_AUTO_CONFIG` | Disable automatic configuration | `false` |
| `CONTROLLER_RECOVERY_COOLDOWN_SECONDS` | Cooldown after recovery | `30` |
| `CONTROLLER_RECOVERY_VALIDATION_ATTEMPTS` | Validation samples after recovery | `3` |
| `CONTROLLER_CIRCUIT_BREAKER_COOLDOWN_MINUTES` | Circuit breaker cooldown | `5` |

### API Service - Quality Metrics

| Variable                  | Description                                    | Default | Valid Range |
|---------------------------|------------------------------------------------|---------|-------------|
| `TRNG_APT_WINDOW_SIZE`    | APT test window size in bytes                  | `512`   | 256-2048    |

**APT Window Size Trade-offs**:
- **Larger values (1024-2048)**: Better bias detection, more memory (~1-2KB), slower updates
- **Default (512)**: Balanced detection and performance (~536 bytes)
- **Smaller values (256-512)**: Less memory, faster updates, may miss subtle biases

### Fortuna Service

| Variable                        | Description                              | Default | Valid Range |
|---------------------------------|------------------------------------------|---------|-------------|
| `PORT`                          | Fortuna server port                      | `8082`  | 1-65535     |
| `AMPLIFICATION_FACTOR`          | Data amplification multiplier            | `4`     | 1-100       |
| `FORTUNA_MAX_RESEED_INTERVAL_HOURS` | Maximum time between reseeds (hours) | `1`     | 1-24        |

### VirtIO Service

| Variable                  | Description                          | Default              | Valid Range |
|---------------------------|--------------------------------------|----------------------|-------------|
| `PORT`                    | VirtIO HTTP API port                 | `8083`               | 1-65535     |
| `RNG_DEVICE`              | (Ignored - VirtIO doesn't use device) | N/A                   | N/A         |
| `VIRTIO_QUEUE_SIZE`       | Internal queue capacity (items)     | `15000` (~480 KB)    | 10-1000000  |
| `VIRTIO_PIPE_PATH`        | Named pipe (FIFO) filesystem path   | `/var/run/lokey/virtio-rng` | Valid path |
| `LOG_LEVEL`               | Logging level                        | `INFO`               | DEBUG, INFO, WARN, ERROR |

**Queue Size Calculation:**
- Default: 15,000 items × 32 bytes/item = 480,000 bytes (480 KB)
- Target capacity: 100-500 KB for optimal performance
- Can be adjusted based on expected consumption rate and boot spike tolerance

**Named Pipe Setup:**
- The named pipe directory must exist on the host before starting the service
- Create the directory with appropriate permissions:
  ```bash
  sudo mkdir -p /var/run/lokey
  sudo chmod 755 /var/run/lokey
  ```
- The pipe file is automatically created by the VirtIO service
- Ensure the Docker volume mount includes the pipe directory:
  ```yaml
  volumes:
    - /var/run/lokey:/var/run/lokey
  ```

**Hypervisor Configuration:**

**QEMU:**
```bash
qemu-system-x86_64 \
  -object rng-random,id=rng0,filename=/var/run/lokey/virtio-rng \
  -device virtio-rng-pci,rng=rng0 \
  ...
```

**Proxmox:**
- Edit VM configuration and add:
  ```
  rng0: /var/run/lokey/virtio-rng
  ```
- Or via command line:
  ```bash
  qm set <vmid> -rng0 /var/run/lokey/virtio-rng
  ```

**VirtualBox:**
- Configure VM to use host device `/var/run/lokey/virtio-rng` as RNG source

**VMware:**
- Configure VM to use host device `/var/run/lokey/virtio-rng` as RNG source

**Kubernetes/OpenShift:**
- Use a sidecar container that reads from HTTP streaming endpoint (`GET /stream`)
- Sidecar creates a named pipe in the pod for application consumption
- See Kubernetes deployment examples in architecture documentation

### Example Configuration

**Development:**
```yaml
environment:
- TRNG_QUEUE_SIZE=100
- FORTUNA_QUEUE_SIZE=100
- TRNG_POLL_INTERVAL_MS=1000
- FORTUNA_POLL_INTERVAL_MS=5000
```
**Production (High Throughput):**
```yaml
environment:
- TRNG_QUEUE_SIZE=5000
- FORTUNA_QUEUE_SIZE=50000
- TRNG_POLL_INTERVAL_MS=500
- FORTUNA_POLL_INTERVAL_MS=100
- AMPLIFICATION_FACTOR=10
```
**Production (Low Resource):**
```yaml
environment:
- TRNG_QUEUE_SIZE=500
- FORTUNA_QUEUE_SIZE=2000
- TRNG_POLL_INTERVAL_MS=2000
- FORTUNA_POLL_INTERVAL_MS=2000
- AMPLIFICATION_FACTOR=4
```
## Docker Images

### Dockerfile pattern

All LoKey services use multi-stage Alpine images:

1. Build the Go binary in a `golang:1.27-alpine` builder stage
2. Copy the binary to `/usr/local/bin/<service>` in a minimal Alpine runtime
3. `ENTRYPOINT` the binary directly (no `/app-bin` copy script)
4. Exec-form `HEALTHCHECK` with busybox `wget` (keeps Docker healthchecks without a shell)

Entropy queues are in-memory only — no `api-data` volume is required for the API.

### Official Images

Pre-built images are available on GitHub Container Registry:
```bash
# Latest stable
docker pull ghcr.io/lokeytraas/lokey/api:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/controller:latest-arm64
docker pull ghcr.io/lokeytraas/lokey/fortuna:latest-arm64

# Specific version
docker pull ghcr.io/lokeytraas/lokey/api:v1.0.0-arm64
```
### Building Custom Images

**Development builds (from source):**
```bash
# Build all services
docker compose build

# Build specific service
docker compose build api
```

**Production builds (pre-built binaries):**
```bash
# Build with Dockerfile.action (uses pre-built binaries)
docker build -t lokey-api:custom -f cmd/api/Dockerfile.action .
```

**Dockerfile Types:**
- `Dockerfile` - Builds from Go source code (development)
- `Dockerfile.action` - Uses pre-built binaries (CI/CD, production)

Both ship the binary at `/usr/local/bin/<service>` with exec-form healthchecks.
## Monitoring

### Health Checks

All services provide health endpoints:
```bash
# API health
curl http://localhost:8080/api/v1/health

# Controller health
curl http://localhost:8081/health

# Fortuna health
curl http://localhost:8082/health

# VirtIO health
curl http://localhost:8083/health
```
### Prometheus Metrics

Metrics available at `/metrics`:
```bash
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
```bash
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
```yaml
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

### Controller Recovery Loop

If the controller is stuck in a recovery loop:

**Symptoms:**
- Logs show repeated "recovery successful" but device still returns 0xFF patterns
- Health checks fail immediately after recovery
- High CPU usage from recovery attempts

**Solutions:**
1. **Check I2C Hardware:**
   ```bash
   # Verify device is connected
   i2cdetect -y 1
   # Should show 0x60 for ATECC608A

   # Check I2C permissions
   ls -l /dev/i2c-1
   # Should be readable/writable by container user
   ```

2. **Adjust Recovery Settings:**
   ```yaml
   environment:
     - CONTROLLER_RECOVERY_VALIDATION_ATTEMPTS=5  # More validation
     - CONTROLLER_RECOVERY_COOLDOWN_SECONDS=60    # Longer cooldown
   ```

3. **Check Circuit Breaker:**
   - Circuit breaker opens after 5 consecutive failures
   - Wait for cooldown period (default 5 minutes)
   - Check logs for "Circuit breaker opened" messages

4. **Hardware Diagnostics:**
   ```bash
   # Test I2C communication
   docker compose exec controller i2cdetect -y 1

   # Check for I2C errors in kernel logs
   dmesg | grep i2c
   ```

### Fortuna Health Check Failures

If Fortuna shows as unhealthy (container up but `/health` 503 / API `fortuna: false`):

- Confirm API logs show `Successfully seeded Fortuna` every ~30s (not circuit breaker OPEN)
- `/seed` failures used to leave the generator stuck unhealthy when pool selection followed the AES block counter; current builds use a dedicated `reseedCount` (see [Architecture](architecture.md#fortuna-end-to-end))
- Restarting Fortuna clears in-RAM key/pools; the next successful TRNG reseed restores the long-term entropy path

If Fortuna shows as unhealthy:

**Symptoms:**
- Health endpoint returns 503
- "Haven't been reseeded" errors
- System shows degraded status

**Solutions:**
1. **Check Reseed Interval:**
   ```yaml
   environment:
     - FORTUNA_MAX_RESEED_INTERVAL_HOURS=2  # Increase tolerance
   ```

2. **Verify Seeding:**
   ```bash
   # Check if API is successfully seeding Fortuna
   docker compose logs api | grep "seeded Fortuna"

   # Test seeding manually
   curl -X POST http://localhost:8082/seed \
     -H "Content-Type: application/json" \
     -d '{"seeds": ["a1b2c3..."]}'
   ```

3. **Check Controller Status:**
   - Fortuna depends on controller for seeds
   - If controller is failing, Fortuna won't get reseeded
   - Check controller health: `curl http://localhost:8081/health`

### I2C Communication Errors

**Symptoms:**
- "write /dev/i2c-1: input/output error"
- "I2C sleep command failed"
- Device not responding

**Solutions:**
1. **Hardware Connection:**
   - Verify ATECC608A is properly connected
   - Check SDA/SCL connections
   - Verify power supply (3.3V)

2. **I2C Bus Issues:**
   ```bash
   # Enable I2C in Raspberry Pi config
   sudo raspi-config
   # Interface Options -> I2C -> Enable

   # Reload I2C module
   sudo modprobe -r i2c-dev
   sudo modprobe i2c-dev
   ```

3. **Container Permissions:**
   ```yaml
   # Ensure device is accessible
   devices:
     - /dev/i2c-1:/dev/i2c-1
   ```

### Seed Validation Failures

If seeding fails with "low quality detected":

**Symptoms:**
- "TRNG data validation failed" errors
- "seed quality detected" messages
- Fortuna not accepting seeds

**Solutions:**
1. **Controller Hardware Issue:**
   - Controller returning 0xFF patterns indicates hardware failure
   - Check I2C connection and device health
   - May require hardware replacement

2. **Temporary Workaround:**
   - System will automatically retry
   - Circuit breaker prevents wasted attempts
   - Check controller recovery status

### Services Won't Start
```bash
# Check logs
docker compose logs

# Check if ports are available
sudo netstat -tulpn | grep -E '8080|8081|8082'

# Verify Docker is running
sudo systemctl status docker
```
### No Hardware Detected
```bash
# Check I2C device (on Raspberry Pi)
ls -l /dev/i2c-1

# Scan I2C bus
i2cdetect -y 1

# Check container has device access
docker compose exec controller ls -l /dev/i2c-1
```
### Queue / memory issues
```bash
# Confirm in-memory storage
curl -sk https://localhost/api/v1/status | jq '.database'
# expect path: memory://channels

# Restart clears in-memory queues (polling refills them)
docker compose restart api

# Check host memory pressure if queues are large
free -h
docker stats
```
### Performance Issues
```bash
# Check resource usage
docker stats

# Check queue status
curl http://localhost:8080/api/v1/status

# Adjust polling intervals
# Edit docker-compose.yaml and restart
```
### Network Issues
```bash
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
