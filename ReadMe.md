# LoKey: True Random Number Generation Service

[![Build Status](https://github.com/LokeyTRaaS/Lokey/actions/workflows/build.yml/badge.svg)](https://github.com/LokeyTRaaS/Lokey/actions/workflows/build.yml)
[![Lint](https://github.com/LokeyTRaaS/Lokey/actions/workflows/linter.yml/badge.svg)](https://github.com/LokeyTRaaS/Lokey/actions/workflows/linter.yml)
[![API Docs](https://img.shields.io/badge/API-Documentation-blue)](https://lokeytraas.github.io/Lokey/api-reference.html)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

<div align="center">
  <img src="docs/assets/logo.jpeg" alt="LoKey Logo" width="300"/>
</div>

**Hardware-based true random number generation for Raspberry Pi 5 (ARM64) and x86_64**

ATECC608A TRNG + Fortuna CSPRNG + VirtIO entropy delivery, fronted by Traefik (HTTPS), with Prometheus/Grafana observability. Local development uses an emulated controller (no chip required).

## Quick Links

- **[Quickstart](docs/quickstart.md)** — full stack locally with emulator
- **[Hardware Setup](docs/hardware-setup.md)** — Pi 5 + ATECC608A
- **[Deployment](docs/deployment.md)** — production / Traefik HTTPS
- **[NixOS](docs/nixos.md)** — NixOS module, I2C + cgroups boot
- **[API Reference](https://lokeytraas.github.io/Lokey/api-reference.html)**
- **[Architecture](docs/architecture.md)**

## What You Get

- True randomness from ATECC608A (or `CONTROLLER_MODE=emulated` for laptop/CI)
- Fortuna amplification with Prometheus amplification metrics
- VirtIO named pipe + HTTPS streaming via Traefik
- Multi-device controllers on an isolated `lokey-devices` network + seccomp profiles
- Platforms: **linux/amd64** and **linux/arm64** only

## Quick Start (no hardware)

```bash
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey
mkdir -p run/lokey
docker compose up -d --build
curl -k https://localhost/api/v1/health
```

Controllers default to **emulated** mode. On a Raspberry Pi 5 with ATECC608A, use the `hardware` compose profile / NixOS module.

## Architecture

```
Client --HTTPS--> Traefik
                    |-- API / Fortuna / VirtIO  (lokey-app + edge)
                    |-- Grafana                 (lokey-metrics)
API -------------- Controllers                 (lokey-devices only)
Prometheus scrapes app + devices
```

## Project Status

- Production-oriented Docker Compose + NixOS deploy path
- Multi-arch CI (amd64, arm64) including VirtIO
- Pi Zero 2 / ARMv7 are **unsupported** (insufficient for cgroups/namespaces + Compose)

## License

MIT License — see [LICENSE](LICENSE).
