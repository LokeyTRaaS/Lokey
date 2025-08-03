# Lokey: True Random Number Generation Service

<img src="docs/logo.jpeg" alt="Description" width="300"/>

## Table of Contents
<!-- TOC -->
* [Lokey: True Random Number Generation Service](#lokey-true-random-number-generation-service)
  * [Table of Contents](#table-of-contents)
  * [Project Overview](#project-overview)
  * [Services That Benefit from True Random Data](#services-that-benefit-from-true-random-data)
    * [Cryptographic Applications](#cryptographic-applications)
    * [Security Services](#security-services)
    * [Scientific & Research Applications](#scientific--research-applications)
    * [Gaming & Gambling](#gaming--gambling)
    * [IoT & Embedded Security](#iot--embedded-security)
    * [Communications](#communications)
    * [Financial Services](#financial-services)
    * [Testing & Quality Assurance](#testing--quality-assurance)
    * [Government & Military](#government--military)
    * [Implementation Advantages](#implementation-advantages)
  * [System Architecture](#system-architecture)
    * [Components](#components)
    * [Technical Architecture](#technical-architecture)
    * [Database Design](#database-design)
  * [Features](#features)
  * [API Reference](#api-reference)
    * [API Service](#api-service)
    * [Controller Service](#controller-service)
    * [Fortuna Service](#fortuna-service)
  * [Getting Started](#getting-started)
    * [Prerequisites](#prerequisites)
    * [Local Development](#local-development)
    * [Cross-Compilation & Raspberry Pi Deployment](#cross-compilation--raspberry-pi-deployment)
      * [Development Machine Setup](#development-machine-setup)
      * [Raspberry Pi Configuration](#raspberry-pi-configuration)
  * [Hardware Setup](#hardware-setup)
  * [Development](#development)
    * [Using Taskfile](#using-taskfile)
    * [Building from Source](#building-from-source)
    * [Testing](#testing)
  * [Environment Variables](#environment-variables)
  * [Project Goals](#project-goals)
  * [License](#license)
<!-- TOC -->


## Project Overview

Lokey is a high-availability, high-bandwidth true random number generation service. The name derives from Loki, the Norse god of chaos, reflecting the unpredictable nature of true randomness, and "key" indicating its accessibility and utility as a keystone service for cryptographic applications.

This project provides affordable and accessible true randomness through off-the-shelf components with a Go implementation. The implementation uses a Raspberry Pi Zero 2W and an ATECC608A cryptographic chip, creating a hardware-based solution with a modest bill of materials costing approximately €50.

## Services That Benefit from True Random Data

True random number generation (TRNG) is invaluable for many applications where unpredictability and randomness are essential. Here are key services that would benefit from your Lokey system:

### Cryptographic Applications
- **Key Generation**: Creating cryptographic keys for encryption, digital signatures, and secure communications
- **Certificate Authorities**: Generating truly random seeds for certificate creation
- **Password Management Systems**: Creating truly random passwords that are resistant to brute force
- **Blockchain & Cryptocurrency**: Seed generation for wallets and entropy sources for mining operations

### Security Services
- **Authentication Systems**: Generating one-time passwords (OTP) and authentication tokens
- **Secure Boot Mechanisms**: Trusted platform modules requiring random values
- **DRM (Digital Rights Management)**: Random token generation for content protection
- **Intrusion Detection Systems**: Creating unpredictable challenge-response patterns

### Scientific & Research Applications
- **Monte Carlo Simulations**: Financial modeling, physics simulations, risk analysis
- **Statistical Sampling**: Ensuring truly random sample selection
- **Quantum Computing Research**: Quantum random number verification and benchmarking
- **Machine Learning**: Random initialization of weights, dropout mechanisms, and cross-validation

### Gaming & Gambling
- **Online Casinos**: Ensuring fair and truly random outcomes for games of chance
- **Lottery Systems**: Drawing winning numbers with provable randomness
- **Gaming Servers**: Random map generation, loot drops, matchmaking
- **Competitive eSports**: Fair team/player selection and in-game randomized elements

### IoT & Embedded Security
- **IoT Device Authentication**: Generating device-specific keys and identifiers
- **Automotive Security**: Secure communication between vehicle components
- **Smart Home Security**: Random challenge-response between devices and hubs
- **Industrial Control Systems**: Secure authentication for critical infrastructure

### Communications
- **Secure Messaging Platforms**: End-to-end encryption key generation
- **VPN Services**: Creating session keys and initialization vectors
- **Satellite Communications**: Secure key exchange for encrypted channels
- **Secure VoIP**: Call encryption and authentication

### Financial Services
- **Banking Security**: Transaction authentication codes
- **Fraud Detection Systems**: Random sampling for pattern analysis
- **High-Frequency Trading**: Random timing variations to prevent pattern exploitation
- **Financial Auditing**: Random sampling of transactions for review

### Testing & Quality Assurance
- **Fuzz Testing**: Generating random inputs to test application resilience
- **Load Testing**: Creating unpredictable usage patterns
- **Penetration Testing**: Random attack vector generation
- **Software Resilience Testing**: Chaos engineering with random failure injection

### Government & Military
- **Secure Communications**: Key generation for classified communications
- **Intelligence Operations**: One-time pads and secure communication
- **Election Systems**: Random audit selection and verification processes
- **Secure Document Generation**: Random serial numbers and identifiers

### Implementation Advantages

The Lokey system offers several advantages for these applications:

1. **Hardware-based**: True randomness from physical processes rather than algorithmic generation
2. **Affordable**: €50 bill of materials makes it accessible for many applications
3. **High Availability**: Fault-tolerant architecture ensures continuous service
4. **Scalable**: Can be expanded with additional entropy sources
5. **Verifiable**: Output can be statistically tested for randomness properties

These services could integrate with your Lokey system via:
- Direct API calls for on-demand random data
- Scheduled batch retrievals for applications that consume randomness in bursts
- Local hardware deployment for air-gapped or high-security environments
- Integration with HSMs (Hardware Security Modules) for enterprise applications


## System Architecture

### Components

Lokey consists of three main microservices:

1. **Controller Service**: Interfaces with the ATECC608A chip to harvest true random numbers (TRNG) and process SHA-256 hashes
2. **Fortuna Service**: Amplifies the entropy using the Fortuna algorithm for enhanced randomness
3. **API Service**: Provides endpoints for configuration and both raw TRNG and Fortuna-amplified data retrieval

### Technical Architecture

The system uses a microservices architecture with three containerized applications that communicate via HTTP APIs:

- **Controller Service**: Generates random data using the ATECC608A hardware TRNG
- **Fortuna Service**: Amplifies random data using the Fortuna algorithm
- **API Service**: Provides a unified API for configuration and data retrieval

Each service operates independently, with shared volumes for database access.

### Database Design

The system uses BadgerDB as an embedded database solution for all components, providing:

- Embedded key-value storage optimized for SSDs
- No external database dependencies
- Consistent data model across all services
- Better performance for this specific use case
- Queue-based architecture for handling RNG requests
- Efficient counters for queue management

## Features

- **Hardware-based TRNG**: True Random Number Generation using ATECC608A cryptographic chip
- **High Availability**: Reliable and continuously available random number generation
- **Dual Access Modes**: Raw TRNG and Fortuna-amplified randomness options
- **Cryptographic Amplification**: Fortuna algorithm implementation for enhanced entropy
- **Efficient Storage**: Queue-based storage with configurable sizes
- **Multiple Data Formats**: Support for various formats including int8, int16, int32, int64, uint8, uint16, uint32, uint64, and binary
- **Flexible Consumption**: Configurable consumption behavior (e.g., delete-on-read)
- **Health Monitoring**: Comprehensive system health checks
- **API Documentation**: Swagger documentation for all endpoints

## API Reference

### API Service

**Configuration Endpoints:**
- `GET /api/v1/config/queue`: Get queue configuration
- `PUT /api/v1/config/queue`: Update queue configuration
- `GET /api/v1/config/consumption`: Get consumption behavior configuration
- `PUT /api/v1/config/consumption`: Update consumption behavior configuration

**Data Endpoints:**
- `POST /api/v1/data`: Retrieve random data in specified format

**Status Endpoints:**
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

## Getting Started

### Prerequisites

- Docker and Docker Compose
- ATECC608A cryptographic chip (for hardware TRNG)
- Go 1.24+ (for development)
- Docker Buildx (for cross-compiling ARM images)
- Raspberry Pi Zero 2W (for production deployment)

### Local Development

To run Lokey on your development machine:

1. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/lokey.git
   cd lokey
   ```

2. Start the services using Docker Compose:
   ```bash
   docker compose up -d
   ```

3. Access the API at http://localhost:8080
4. View API documentation at http://localhost:8080/swagger/index.html

### Cross-Compilation & Raspberry Pi Deployment

Lokey can be cross-compiled on a development machine and deployed to a Raspberry Pi:

#### Development Machine Setup

1. Create `buildx.toml` configuration file in your project root:

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

#### Raspberry Pi Configuration

1. Set up your Raspberry Pi following the hardware guide in `device/HowToSetup.md`

2. Configure Docker on the Raspberry Pi to trust your development machine's registry:

   ```bash
   # Replace with your development machine's IP address
   DEV_IP=192.168.1.100

   # Update Docker daemon configuration
   echo "{
     \"insecure-registries\": [\"$DEV_IP:5000\"]
   }" | sudo tee /etc/docker/daemon.json

   # Restart Docker to apply changes
   sudo systemctl restart docker
   ```

3. Create a `docker-compose.yaml` file on the Pi with your development machine's IP:

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
   volumes:
     trng-data:
     fortuna-data:
     api-data:
   ```

4. Pull and start the services on the Raspberry Pi:

   ```bash
   docker compose pull
   docker compose up -d
   ```

5. Verify the deployment:

   ```bash
   docker compose ps
   curl http://localhost:8080/api/v1/health
   ```

## Hardware Setup

Lokey requires minimal hardware to function:

- **Raspberry Pi Zero 2W**: Main computing platform
- **ATECC608A**: Cryptographic chip for true random number generation

The total bill of materials is approximately €50, making this a cost-effective solution for organizations requiring true randomness.

For detailed hardware setup instructions, refer to the [hardware setup guide](device/HowToSetup.md) which includes:
- Soldering instructions for connecting the ATECC608A
- Raspberry Pi Zero 2W configuration
- I2C setup and verification

## Development

### Using Taskfile

Lokey uses [Task](https://taskfile.dev/) as a convenient build tool. Install Task following the instructions at [taskfile.dev](https://taskfile.dev/installation/).

```bash
# Display all available tasks
task

# Build all components
task build

# Run all development tasks in sequence
task all

# Cross-compilation for Raspberry Pi
task build_images_and_registry

# Run services locally
task dev_up

# Stop local services
task dev_down
```

### Building from Source

To build the components manually:

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

### Testing

Running tests:

```bash
# Run all tests
task test

# Format code
task fmt

# Lint code
task lint
```

Testing the API:

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

## Environment Variables

The following environment variables can be configured for each service:

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

## Project Goals

Lokey aims to democratize access to true randomness with these key objectives:

1. **Accessibility**: Provide true randomness through affordable, off-the-shelf components
2. **High Availability**: Ensure the service is reliable and continuously available
3. **High Bandwidth**: Deliver sufficient random data throughput for demanding applications
4. **Flexibility**: Offer both raw TRNG and cryptographically amplified random data
5. **Extensibility**: Build a foundation that can be expanded with additional entropy sources

Future versions may incorporate additional entropy sources or higher-performance hardware, while maintaining the core design principles of accessibility and reliability.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
