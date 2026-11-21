# Security Policy

## Supported versions

Security fixes are applied on `main` and the latest tagged release (currently `v1.1.0`).

## Reporting a vulnerability

Please open a **private** security advisory on GitHub for [LokeyTRaaS/Lokey](https://github.com/LokeyTRaaS/Lokey), or email the maintainers listed on the repository. Do not file public issues for exploitable flaws until a fix is available.

## Trust model (important)

LoKey is designed as a **trusted-network appliance**, not a public multi-tenant API.

- The **API**, Fortuna (`/seed`, `/generate`, `/amplify`), VirtIO (`/seed`, `/stream`), config endpoints, Swagger, and `/metrics` have **no application-level authentication**.
- Production Compose places controllers on an isolated `lokey-devices` network and fronts app services with **Traefik HTTPS**. Do not publish internal service ports to the internet.
- Default Grafana credentials (`admin` / `lokey`) and the NixOS SD image user (`lokey` / `lokey`) **must be changed** before any network-exposed deployment.
- `CORS_ALLOWED_ORIGIN` defaults to `*`; restrict it if browsers will call the API.
- Controllers in `CONTROLLER_MODE=emulated` use `crypto/rand` — suitable for development/CI, not a hardware TRNG.

Hardening already in tree includes Fortuna seeding from `crypto/rand`, AES rekey after generate, HTTP header timeouts, seccomp profiles, and dependency scanning (Grype / govulncheck in CI).
