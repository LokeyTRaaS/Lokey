# Quickstart Guide

Get the full LoKey stack running locally **without** an ATECC608A chip.

## Prerequisites

- Docker and Docker Compose
- Go 1.27+ (only if building binaries locally)

> Controllers default to `CONTROLLER_MODE=emulated` (`crypto/rand`). This is for development and CI — not a hardware TRNG. For production hardware, see [Hardware Setup](hardware-setup.md) and [NixOS](nixos.md).

## Start the stack

```bash
git clone https://github.com/LokeyTRaaS/Lokey.git
cd Lokey
mkdir -p run/lokey
docker compose up -d --build
```

Services (no profile required):

| Service | Role | Network |
|---------|------|---------|
| traefik | HTTPS ingress (:443) | edge + app + metrics |
| api | REST API + seeding | edge + app + devices |
| fortuna | CSPRNG amplifier | app |
| virtio | pipe + HTTP stream | edge + app |
| controller-a / controller-b | emulated TRNG | **devices only** |
| prometheus / grafana | observability | metrics (+ scrape nets) |

## Verify

```bash
docker compose ps
curl -k https://localhost/api/v1/health
curl -k https://localhost/api/v1/status | jq '.controllers'
```

Direct service ports are not published in production-style compose; use Traefik HTTPS.

Emulated controller check (from another container on `lokey-devices`, or temporarily publish ports in debug):

```bash
docker compose exec api wget -qO- http://controller-a:8081/info
# {"status":"running","mode":"emulated","device_id":"device_a"}
```

## First API call

```bash
curl -k -X POST https://localhost/api/v1/data \
  -H "Content-Type: application/json" \
  -d '{"format":"int32","limit":10,"source":"trng"}'
```

## Grafana

https://localhost/grafana (default **admin** / **lokey** — change in production).

Traefik dashboard: https://localhost/dashboard/ (same **admin** / **lokey**).

## Next

- [Deployment](deployment.md) — Traefik ACME, hardware profile
- [NixOS](nixos.md) — Pi 5 boot (I2C, cgroups, namespaces)
- [Architecture](architecture.md)
