
# LoKey: True Random Number Generation Service

<img src="docs/assets/logo.jpeg" alt="LoKey Logo" width="300"/>

[![Build Status](https://github.com/LokeyTRaaS/Lokey/actions/workflows/build-arm64.yml/badge.svg)](https://github.com/LokeyTRaaS/Lokey/actions/workflows/build-arm64.yml)
[![Lint](https://github.com/LokeyTRaaS/Lokey/actions/workflows/linter.yml/badge.svg)](https://github.com/LokeyTRaaS/Lokey/actions/workflows/linter.yml)
[![API Docs](https://img.shields.io/badge/API-Documentation-blue)](https://lokeytraas.github.io/Lokey/api-reference.html)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Hardware-based true random number generation for ~€50**

True randomness from ATECC608A cryptographic chip + Fortuna CSPRNG amplification on Raspberry Pi Zero 2W.

## Quick Links

- 🚀 **[Quickstart](docs/quickstart.md)** - Running in 5 minutes with Docker
- 🔧 **[Hardware Setup](docs/hardware-setup.md)** - Raspberry Pi + ATECC608A wiring guide
- 🚢 **[Deployment](docs/deployment.md)** - Production deployment for Raspberry Pi
- 📡 **[API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)** - Auto-generated interactive docs
- 📝 **[API Examples](docs/api-examples.md)** - Quick curl examples
- 🏗️ **[Architecture](docs/architecture.md)** - How the system works
- 💻 **[Development](docs/development.md)** - Contributing and building from source

## What You Get

- ✅ **True randomness** from hardware TRNG (ATECC608A chip)
- ✅ **High throughput** via Fortuna cryptographic amplification
- ✅ **Multiple formats** (int8-64, uint8-64, binary)
- ✅ **REST API** with auto-generated OpenAPI documentation
- ✅ **Multi-arch support** (AMD64, ARM64, ARMv7)
- ✅ **Affordable** ~€50 bill of materials

## Quick Start

```bash
# Clone the repository
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey

# Run with Docker Compose
docker compose up -d

# Test the API
curl http://localhost:8080/api/v1/health

# Get random data
curl -X POST http://localhost:8080/api/v1/data \
  -H "Content-Type: application/json" \
  -d '{"format":"int32","limit":10,"source":"trng"}'
```


**Need hardware?** See [Hardware Setup Guide](docs/hardware-setup.md) for Raspberry Pi + ATECC608A wiring.

## Use Cases

Perfect for:
- 🔐 **Cryptography** - Key generation, seeds, nonces
- 🎲 **Gaming & Gambling** - Provably fair random outcomes
- 🔬 **Research** - Monte Carlo simulations, statistical sampling
- 🌐 **IoT Security** - Device authentication, secure boot
- 💰 **Finance** - Transaction codes, audit sampling

[Read more about use cases](docs/api-examples.md#use-cases)

## Architecture

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│  Controller │───▶│  API Service │◀───│   Fortuna   │
│   Service   │    │   (BoltDB)   │    │   Service   │
│ (ATECC608A) │    │              │    │   (CSPRNG)  │
└─────────────┘    └──────────────┘    └─────────────┘
│                    │                    │
└────────────────────┴────────────────────┘
HTTP/REST API
```


Three microservices working together:
- **Controller**: Interfaces with ATECC608A hardware for true random numbers
- **API**: Manages storage, polling, and provides unified REST API
- **Fortuna**: Cryptographic amplification for high-throughput randomness

Learn more: [Architecture Guide](docs/architecture.md)

## Project Status

- ✅ **Production Ready** - All core features implemented
- ✅ **Multi-Architecture** - AMD64, ARM64, ARMv7 binaries available
- ✅ **Documented** - Complete setup and API documentation
- ✅ **CI/CD** - Automated builds and releases
- 🚧 **Active Development** - Additional entropy sources planned

## Documentation

**Getting Started**
- [Quickstart Guide](docs/quickstart.md) - Get running in 5 minutes
- [Hardware Setup](docs/hardware-setup.md) - Physical device configuration

**API Documentation**
- [API Reference](https://lokeytraas.github.io/Lokey/api-reference.html) - Interactive OpenAPI docs
- [API Examples](docs/api-examples.md) - Common patterns and use cases
- [OpenAPI Spec](https://lokeytraas.github.io/Lokey/swagger.json) - Machine-readable specification

**Operations & Development**
- [Deployment Guide](docs/deployment.md) - Production deployment
- [Architecture](docs/architecture.md) - System design details
- [Development Guide](docs/development.md) - Contributing to the project

## License

MIT License - See [LICENSE](LICENSE) for details.

## Contributing

Contributions welcome! See [Development Guide](docs/development.md) for setup instructions.

---

**[Documentation](https://lokeytraas.github.io/Lokey/)** • **[Issues](https://github.com/LokeyTRaaS/Lokey/issues)** • **[Releases](https://github.com/LokeyTRaaS/Lokey/releases)**
