# LoKey Documentation

Welcome to the LoKey documentation. LoKey provides hardware-based true random number generation using the ATECC608A cryptographic chip on affordable Raspberry Pi hardware.

## Getting Started

New to LoKey? Start here:

1. **[Quickstart Guide](quickstart.md)** - Get LoKey running in 5 minutes with Docker
2. **[Hardware Setup](hardware-setup.md)** - Connect ATECC608A to Raspberry Pi (if using hardware)
3. **[API Examples](api-examples.md)** - Learn the API with practical examples

## Documentation Sections

### 📚 For Users

- **[Quickstart](quickstart.md)** - Docker Compose setup and first API calls
- **[API Examples](api-examples.md)** - Common use cases with curl examples
- **[API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)** - Complete interactive API documentation

### 🔧 For Operators

- **[Hardware Setup](hardware-setup.md)** - Raspberry Pi + ATECC608A wiring and I2C configuration
- **[Deployment Guide](deployment.md)** - Production deployment, cross-compilation, environment configuration

### 💻 For Developers

- **[Architecture](architecture.md)** - System design, components, and technical implementation
- **[Development Guide](development.md)** - Building from source, testing, contributing
- **[Testing Guide](tests.md)** - Test suite documentation and best practices

## Quick Reference

### API Documentation

- **Interactive Docs**: [https://lokeytraas.github.io/Lokey/api-reference.html](https://lokeytraas.github.io/Lokey/api-reference.html)
- **OpenAPI JSON**: [https://lokeytraas.github.io/Lokey/swagger.json](https://lokeytraas.github.io/Lokey/swagger.json)
- **OpenAPI YAML**: [https://lokeytraas.github.io/Lokey/swagger.yaml](https://lokeytraas.github.io/Lokey/swagger.yaml)

### Common Tasks

- **Run locally**: See [Quickstart](quickstart.md)
- **Deploy to Raspberry Pi**: See [Deployment Guide](deployment.md#raspberry-pi-deployment)
- **Configure environment**: See [Deployment Guide](deployment.md#environment-variables)
- **Build from source**: See [Development Guide](development.md#building-from-source)
- **Understand the system**: See [Architecture](architecture.md)

## External Links

- **[GitHub Repository](https://github.com/LokeyTRaaS/Lokey)** - Source code and issue tracking
- **[Latest Release](https://github.com/LokeyTRaaS/Lokey/releases)** - Download binaries and Docker images
- **[GitHub Discussions](https://github.com/LokeyTRaaS/Lokey/discussions)** - Ask questions and share ideas
- **[Issue Tracker](https://github.com/LokeyTRaaS/Lokey/issues)** - Report bugs and request features

## What is LoKey?

LoKey is a high-availability, high-bandwidth true random number generation service built on affordable hardware. The system combines:

- **Hardware TRNG**: ATECC608A cryptographic chip for true random numbers
- **Cryptographic Amplification**: Fortuna algorithm for high-throughput generation
- **REST API**: Easy integration with any application
- **Multi-Architecture**: Runs on x86, ARM64, and ARMv7

Total hardware cost: ~€50 (Raspberry Pi Zero 2W + ATECC608A chip)

## Use Cases

LoKey is designed for applications requiring high-quality randomness:

- **Cryptography**: Key generation, seed values, nonces
- **Gaming & Gambling**: Fair and unpredictable game outcomes
- **Scientific Research**: Monte Carlo simulations, random sampling
- **IoT Security**: Device authentication, secure boot sequences
- **Financial Services**: Transaction codes, audit sampling

[See more use cases in API Examples](api-examples.md#use-cases)

## Support

- **Questions?** Ask in [GitHub Discussions](https://github.com/LokeyTRaaS/Lokey/discussions)
- **Found a bug?** Report an [Issue](https://github.com/LokeyTRaaS/Lokey/issues)
- **Want to contribute?** See [Development Guide](development.md#contributing)

## Project Status

LoKey is production-ready with active development:

- ✅ Core functionality complete and tested
- ✅ Multi-architecture support (AMD64, ARM64, ARMv7)
- ✅ Automated CI/CD pipeline
- ✅ Comprehensive documentation
- 🚧 Additional entropy sources planned
- 🚧 Performance optimizations in progress

## License

LoKey is open source software licensed under the [MIT License](../LICENSE).