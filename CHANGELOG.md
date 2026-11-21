# Changelog

## Unreleased

### Features

* `/api/v1/status` and `/api/v1/health` report per-controller `healthy` and `emulator` from each controller’s `/health` body (with last-known `emulator` while unreachable)
* Prometheus `lokey_controller_emulated{device_id=...}` after the first successful controller health probe

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
