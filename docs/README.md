# LoKey Documentation

Hardware-based true random number generation for **Raspberry Pi 5 (ARM64)** and **x86_64**, with Traefik HTTPS, seccomp hardening, multi-device controllers, and Prometheus/Grafana.

## Getting Started

1. **[Quickstart](quickstart.md)** — local stack with emulated controllers
2. **[Hardware Setup](hardware-setup.md)** — ATECC608A on Pi 5
3. **[NixOS](nixos.md)** — NixOS module + boot checklist
4. **[API Examples](api-examples.md)**

## Operators

- **[Deployment](deployment.md)** — Traefik TLS, networks (`lokey-edge` / `lokey-app` / `lokey-devices` / `lokey-metrics`), multi-arch images
- **[Hardware Setup](hardware-setup.md)**

## Developers

- **[Architecture](architecture.md)** — service map + [Fortuna seed/generate flow](architecture.md#fortuna-end-to-end)
- **[Development](development.md)** — Go 1.27, amd64/arm64 builds
- **[Releasing](releasing.md)** — semantic-release, `RELEASE_TOKEN`, GHCR tags
- **[Testing](tests.md)**

## Platforms

Supported: **linux/amd64**, **linux/arm64**.

Unsupported: Raspberry Pi Zero 2, ARMv7 (insufficient for cgroups/namespaces + Compose).

## What is LoKey?

- ATECC608A TRNG (or emulator for laptop/CI)
- Fortuna amplification with per-source Prometheus quality metrics
- VirtIO pipe + HTTPS stream
- Isolated privileged device network + seccomp profiles

## License

LoKey is open source software licensed under the [MIT License](../LICENSE).
