# Changelog

## [2.0.0](https://github.com/LokeyTRaaS/Lokey/compare/v1.2.4...v2.0.0) (2026-11-23)

### ⚠ BREAKING CHANGES

* fortuna_seed and virtio_sources ratios replace the unmetered reseed and the single VirtIO source. Client consume is set per ring. source=mixed samples by stride, and a ratio must be 0 or 1/n.

### Features

* draw stored rings by ratio and mix the data read ([b7e85fc](https://github.com/LokeyTRaaS/Lokey/commit/b7e85fceb1d3623ed6a9dd5e24101c7f42c442a3))

## [1.2.4](https://github.com/LokeyTRaaS/Lokey/compare/v1.2.3...v1.2.4) (2026-11-22)

### Bug Fixes

* **ci:** publish images and binaries when notes push is rejected ([442c2f2](https://github.com/LokeyTRaaS/Lokey/commit/442c2f24a9899acc3cba2071293cc754425c26fe))

## [1.2.3](https://github.com/LokeyTRaaS/Lokey/compare/v1.2.2...v1.2.3) (2026-11-21)

### Bug Fixes

* **fortuna:** stop reseeds from depending on AES counter bits ([94ae504](https://github.com/LokeyTRaaS/Lokey/commit/94ae504695ff9cdf316446dae0c53c9f58524ffb))

## [1.2.2](https://github.com/LokeyTRaaS/Lokey/compare/v1.2.1...v1.2.2) (2026-11-21)

### Bug Fixes

* trigger semantic-release after RELEASE_TOKEN rotation. ([f0735eb](https://github.com/LokeyTRaaS/Lokey/commit/f0735ebdc57fa9d33e3c094dc905afd568cfc28f))

## [1.2.1](https://github.com/LokeyTRaaS/Lokey/compare/v1.2.0...v1.2.1) (2026-11-21)

### Bug Fixes

* always pull floating :latest LoKey images in Compose. ([d36070b](https://github.com/LokeyTRaaS/Lokey/commit/d36070b6dcdf8d07f545e18c2026d9b2ab2b760c))

## Unreleased

### Continuous Integration

* semantic-release on `main` (conventional commits → `v*` tag, changelog, GitHub Release; dispatches `build.yml` for GHCR/binaries)

### Documentation

* [docs/releasing.md](docs/releasing.md) — release flow, conventional commits, `RELEASE_TOKEN` setup, GHCR pinning

## 1.2.0 (2026-11-21)

### Features

* `/api/v1/status` and `/api/v1/health` report per-controller `healthy` and `emulator` from each controller’s `/health` body (with last-known `emulator` while unreachable)
* Prometheus `lokey_controller_emulated{device_id=...}` after the first successful controller health probe
* Compose `COMPOSE_PROFILES=hardware` for a single ATECC608A/B controller; cAdvisor v0.61.1 for Docker 29

### Breaking Changes

* `controllers` on `/api/v1/status` is now `map[string]{healthy,emulator}` instead of `map[string]bool`

## 1.1.0 (2026-11-21)

### Features

* Nix flake + Raspberry Pi 5 SD image (local build/flash; CI validates flake only)
* Multi-arch GHCR manifests for api, controller, fortuna, virtio
* Pre-commit static checks aligned with CI (golangci-lint, Nix flake, Grype)
* Traefik HTTPS Compose stack with isolated device network and seccomp profiles

### Security

* Fortuna initial seed from `crypto/rand`; AES rekey after every generate
* HTTP `ReadHeaderTimeout` on all services; configurable `CORS_ALLOWED_ORIGIN`
* Dependency bumps (gin/quic-go/validator and related modules)

### Bug Fixes

* Release downloads only `lokey-*` artifacts (exclude Docker build records)
* Build Summary no longer pulls non-zip `*.dockerbuild` artifacts
* GHCR cleanup hardened (dry-run on schedule; protect semver/manifest tags)

### Documentation

* Deploy paths: NixOS SD, Compose/GHCR, per-service release binaries (no mega-tarballs)
* Hardware guide retargeted to Raspberry Pi 5

## 1.0.0 (2025-11-01)

### Bug Fixes

* update golangci-lint config for newer version ([5300fc7](https://github.com/LokeyTRaaS/Lokey/commit/5300fc71c34686c55f8d07b0242e169c1712fc3d))
