# Lokey: True Random Number Generation Service

<img src="docs/logo.jpeg" alt="Description" width="300"/>

<!-- TOC -->
* [Lokey: True Random Number Generation Service](#lokey-true-random-number-generation-service)
  * [Project Overview](#project-overview)
  * [System Components](#system-components)
  * [Architecture](#architecture)
  * [Features](#features)
  * [API Endpoints](#api-endpoints)
    * [Configuration](#configuration)
    * [Data Retrieval](#data-retrieval)
    * [Status](#status)
  * [Getting Started](#getting-started)
    * [Prerequisites](#prerequisites)
    * [Running the System](#running-the-system)
  * [Hardware](#hardware)
  * [Development](#development)
    * [Using Taskfile](#using-taskfile)
    * [Building from Source Manually](#building-from-source-manually)
    * [Testing the API](#testing-the-api)
  * [Project Goals](#project-goals)
  * [License](#license)
<!-- TOC -->


## Project Overview

Lokey is a high-availability, high-bandwidth true random number generation service. The name derives from Loki, the Norse god of chaos, reflecting the unpredictable nature of true randomness, and "key" indicating its accessibility and utility as a keystone service for cryptographic applications.

This project aims to provide affordable and accessible true randomness through off-the-shelf components with a Go implementation. The first prototype uses a Raspberry Pi Zero 2W and an ATECC608A cryptographic chip, creating a hardware-based solution with a modest bill of materials costing approximately €50.

## System Components

1. **Controller**: Interfaces with the ATECC608A chip to harvest true random numbers (TRNG) and process SHA-256 hashes
2. **Fortuna Processor**: Amplifies the entropy using the Fortuna algorithm for enhanced randomness
3. **API Server**: Provides endpoints for configuration and both raw TRNG and Fortuna-amplified data retrieval
4. **BadgerDB**: Provides embedded key-value storage for the queue-based data storage system

## Architecture

The system uses a microservices architecture with three containerized applications:

- **Controller Service**: Generates random data using the ATECC608A hardware TRNG
- **Fortuna Service**: Amplifies random data using the Fortuna algorithm
- **API Service**: Provides a unified API for configuration and data retrieval

Each service operates independently and communicates via HTTP APIs, with shared volumes for database access.

## Features

- Hardware-based True Random Number Generation (TRNG) using ATECC608A cryptographic chip
- High availability and bandwidth of true randomness
- Dual access to both raw TRNG and Fortuna-amplified randomness
- Cryptographic amplification using the Fortuna algorithm for enhanced entropy
- Queue-based storage with configurable sizes for efficient data management
- Multiple data format options (int8, int16, int32, int64, uint8, uint16, uint32, uint64, binary)
- Configurable consumption behavior (delete-on-read)
- Comprehensive health monitoring
- Swagger documentation for all API endpoints

## API Endpoints

### Configuration

- `GET /api/v1/config/queue` - Get queue configuration
- `PUT /api/v1/config/queue` - Update queue configuration
- `GET /api/v1/config/consumption` - Get consumption behavior configuration
- `PUT /api/v1/config/consumption` - Update consumption behavior configuration

### Data Retrieval

- `POST /api/v1/data` - Retrieve random data in specified format

### Status

- `GET /api/v1/status` - Get system status and queue information
- `GET /api/v1/health` - Check health of all system components

## Getting Started

### Prerequisites

- Docker and Docker Compose
- ATECC608A connected via I2C
- Go 1.24+ (for development)
- Docker Buildx (for cross-compiling ARM images)

### Running Locally (Development Machine)

1. Clone the repository
2. Configure I2C settings in `docker-compose.yaml`
3. Start the services:

```bash
docker compose up -d
```

4. Access the API at http://localhost:8080
5. View API documentation at http://localhost:8080/swagger/index.html

### Cross-Compilation & Raspberry Pi Deployment

Lokey can be cross-compiled on a development machine and deployed to a Raspberry Pi using the following workflow:

#### 1. Development Machine Setup

1. Create a buildx.toml configuration file:

```toml
[registry."lokey-registry:5000"]
  insecure = true
  http = true

# insecure-entitlements allows insecure entitlements, disabled by default.
insecure-entitlements = [ "network.host", "security.insecure" ]
```

2. Build ARM images and push to local registry:

```bash
# Sets up a local registry and builds ARM images
task build_images_and_registry
```

This task:
- Creates a Docker network for registry communication
- Runs a local registry container
- Sets up a Buildx builder with appropriate configuration
- Cross-compiles ARM-compatible images
- Pushes the images to the local registry

#### 2. Raspberry Pi Configuration

1. Edit the Docker daemon configuration on the Raspberry Pi:

```bash
sudo nano /etc/docker/daemon.json
```

2. Add the development machine's IP to the insecure registries list:

```json
{
  "insecure-registries": ["192.168.1.100:5000"]
}
```

3. Restart Docker service:

```bash
sudo systemctl restart docker
```

#### 3. Deployment to Raspberry Pi

1. Create a docker-compose.yaml on the Raspberry Pi with the following content:

```yaml
version: '3.8'

services:
  controller:
    image: 192.168.1.100:5000/lokey-controller:latest
    ports:
      - "8081:8081"
    environment:
      - PORT=8081
      - I2C_BUS_NUMBER=1
      - DB_PATH=/data/trng.db
      - HASH_INTERVAL_MS=1000
      - TRNG_QUEUE_SIZE=100
    volumes:
      - trng-data:/data
    devices:
      - /dev/i2c-1:/dev/i2c-1
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 128M

  fortuna:
    image: 192.168.1.100:5000/lokey-fortuna:latest
    ports:
      - "8082:8082"
    environment:
      - PORT=8082
      - DB_PATH=/data/fortuna.db
      - CONTROLLER_URL=http://controller:8081
      - PROCESS_INTERVAL_MS=5000
      - FORTUNA_QUEUE_SIZE=100
      - AMPLIFICATION_FACTOR=4
      - SEED_COUNT=3
    volumes:
      - fortuna-data:/data
    depends_on:
      - controller
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 128M

  api:
    image: 192.168.1.100:5000/lokey-api:latest
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
      - DB_PATH=/data/api.db
      - CONTROLLER_ADDR=http://controller:8081
      - FORTUNA_ADDR=http://fortuna:8082
      - TRNG_QUEUE_SIZE=100
      - FORTUNA_QUEUE_SIZE=100
    volumes:
      - api-data:/data
    depends_on:
      - controller
      - fortuna
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 128M
volumes:
  trng-data:
  fortuna-data:
  api-data:
```

2. Pull and start the services:

```bash
docker compose pull
docker compose up -d
```

3. Verify the deployment:

```bash
docker compose ps
curl http://localhost:8080/api/v1/health
```

## Hardware

Lokey uses minimal hardware to achieve its goals:

- **Raspberry Pi Zero 2W**: Serves as the main computing platform
- **ATECC608A**: Cryptographic chip providing true random number generation capabilities

The total bill of materials is approximately €50, making this a cost-effective solution for organizations requiring true randomness for cryptographic applications, simulations, or other purposes requiring high-quality random data.

## Development

### Using Taskfile

Lokey uses [Task](https://taskfile.dev/) as a convenient build tool. Install Task following the instructions at [taskfile.dev](https://taskfile.dev/installation/), then use these commands:

```bash
# Display all available tasks
task

# Build all components
task build

# Build specific components
task build-controller
task build-api
task build-fortuna

# Cross-compilation for Raspberry Pi
task build_images_and_registry  # Build ARM images and push to local registry
# RNG Service

This service provides true random number generation (TRNG) and Fortuna-based random number generation.

## Architecture

The service consists of three main components:

1. **API Service**: Frontend service that handles client requests
2. **Controller Service**: Interfaces with ATECC608A hardware for true random number generation
3. **Fortuna Service**: Implements the Fortuna algorithm for pseudo-random number generation

## Database Architecture

The service uses BadgerDB as an embedded database solution for all components. This provides:

- Embedded key-value storage optimized for SSDs
- No external database dependencies
- Consistent data model across all services
- Better performance for this use case
- Simplified deployment with each service managing its own data store

### Key Design Features

- Centralized data storage with prefix-based namespacing
- Queue-based architecture for handling RNG requests
- Efficient counters for queue management
- Consistent interface across all services

## Setup and Configuration

### Requirements

- Go 1.24 or later
- I2C-enabled device (for Controller service)

### Environment Variables

| Variable | Service | Description | Default |
|----------|---------|-------------|--------|
| PORT | All | HTTP port | 8080/8081/8082 |
| DB_PATH | All | Path to BadgerDB storage | Service-specific |
| CONTROLLER_ADDR | API | Controller service URL | http://controller:8081 |
| FORTUNA_ADDR | API | Fortuna service URL | http://fortuna:8082 |
| CONTROLLER_URL | Fortuna | Controller service URL | http://controller:8081 |
| TRNG_QUEUE_SIZE | API, Controller | TRNG request queue size | 100 |
| FORTUNA_QUEUE_SIZE | API, Fortuna | Fortuna request queue size | 100 |
| I2C_BUS_NUMBER | Controller | I2C bus number for ATECC608A | 1 |
| HASH_INTERVAL_MS | Controller | Interval between hash generations | 1000 |
| PROCESS_INTERVAL_MS | Fortuna | Interval between Fortuna generations | 5000 |
| AMPLIFICATION_FACTOR | Fortuna | Data amplification multiplier | 4 |
| SEED_COUNT | Fortuna | Number of TRNG seeds to use | 3 |

## Deployment

Each service can be deployed in a separate container or as standalone services.

```bash
# Build all services
go build -o rng-api ./cmd/api
go build -o rng-controller ./cmd/controller
go build -o rng-fortuna ./cmd/fortuna
```

## API Reference

### API Service

- `GET /api/v1/config/queue`: Get queue configuration
- `PUT /api/v1/config/queue`: Update queue configuration
- `GET /api/v1/config/consumption`: Get consumption behavior configuration
- `PUT /api/v1/config/consumption`: Update consumption behavior configuration
- `POST /api/v1/data`: Retrieve random data in specified format
- `GET /api/v1/status`: Get system status and queue information
- `GET /api/v1/health`: Check health of all system components
- `GET /swagger/*any`: Swagger documentation

### Controller Service

- `GET /health`: Check controller health
- `GET /info`: Get controller information
- `GET /generate`: Generate a new TRNG hash

### Fortuna Service

- `GET /health`: Check Fortuna health
- `GET /info`: Get Fortuna information
- `GET /generate`: Generate Fortuna random data
# Run tests
task test

# Tidy Go modules
task tidy

# Format code
task fmt

# Build Docker images
task docker-build

# Start all services
task docker-up

# Stop all services
task docker-down
```

### Building from Source Manually

```bash
# Build the controller
cd cmd/controller
go build

# Build the Fortuna processor
cd ../fortuna
go build

# Build the API server
cd ../api
go build
```

### Testing the API

```bash
# Get queue configuration
curl -X GET http://localhost:8080/api/v1/config/queue

# Retrieve random data in int32 format
curl -X POST http://localhost:8080/api/v1/data \
  -H "Content-Type: application/json" \
  -d '{"format":"int32","chunk_size":32,"limit":10,"offset":0,"source":"fortuna"}'

# Check system health
curl -X GET http://localhost:8080/api/v1/health
```

## Project Goals

Lokey aims to democratize access to true randomness with these key objectives:

1. **Accessibility**: Provide true randomness through affordable, off-the-shelf components
2. **High Availability**: Ensure the service is reliable and continuously available
3. **High Bandwidth**: Deliver sufficient random data throughput for demanding applications
4. **Flexibility**: Offer both raw TRNG and cryptographically amplified random data
5. **Extensibility**: Build a foundation that can be expanded with additional entropy sources

Future versions may incorporate additional entropy sources or higher-performance hardware, while maintaining the core design principles of accessibility and reliability.

## License

MIT
