#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== Go 1.27 pins =="
if rg -n '^go ' go.mod cmd/*/go.mod | rg -v 'go 1\.27'; then
  echo "bad go.mod pin"; exit 1
fi
if rg -n "GO_VERSION: ['\"]?1\\." .github/workflows Taskfile.yaml | rg -v '1\.27'; then
  echo "bad GO_VERSION pin"; exit 1
fi
if rg -n 'go-version: ' .github/workflows | rg -v '1\.27|GO_VERSION'; then
  echo "bad go-version pin"; exit 1
fi
if rg -n 'FROM golang:' cmd/*/Dockerfile | rg -v '1\.27'; then
  echo "bad Dockerfile pin"; exit 1
fi

echo "== No ARMv7 in shipping paths =="
if rg -n 'armv7|GOARM|linux/arm/v7' .github/workflows Taskfile.yaml docker-compose.yaml ReadMe.md docs \
  --glob '!**/medium-article*.md' --glob '!**/nixos.md'; then
  exit 1
fi

echo "== Compose networks =="
docker compose config -q
docker compose config | rg -q 'lokey-edge'
docker compose config | rg -q 'lokey-app'
docker compose config | rg -q 'lokey-devices'
docker compose config | rg -q 'lokey-metrics'

echo "== Controller isolation / Traefik ports =="
# Match the controller-a service block (not depends_on references)
docker compose config | rg -n '^  controller-a:' -A40 | rg -q 'lokey-devices'
# Traefik must not join lokey-devices
if docker compose config | rg -n '^  traefik:' -A60 | rg -q 'lokey-devices'; then
  echo "traefik must not attach to lokey-devices"; exit 1
fi
# Accept default 443 or remapped host ports from .env (compose loads .env; this script may not)
if [ -f .env ]; then
  # shellcheck disable=SC1091
  set -a; . ./.env; set +a
fi
https_port="${TRAEFIK_HTTPS_PORT:-443}"
docker compose config | rg -q "published: [\"']?${https_port}[\"']?"

echo "== Seccomp allowlists + no privileged / hwrng =="
for f in deploy/seccomp/{api,controller,fortuna,virtio}.json; do
  test -f "$f"
  python3 -m json.tool "$f" >/dev/null
  # Allowlist model: default deny, explicit allows
  python3 -c "import json,sys; d=json.load(open('$f')); assert d.get('defaultAction')=='SCMP_ACT_ERRNO', '$f must be allowlist (SCMP_ACT_ERRNO)'; assert any(s.get('action')=='SCMP_ACT_ALLOW' for s in d.get('syscalls',[])), '$f missing SCMP_ACT_ALLOW entries'"
done
docker compose config | rg -q 'seccomp=.*controller\.json'
# cAdvisor may be privileged; LoKey app services must not be
if docker compose config | rg -n '^\s+(api|fortuna|virtio|controller-[ab]):' -A40 | rg -q 'privileged:\s*true'; then
  echo "LoKey app services must not be privileged"; exit 1
fi
! rg -n '/dev/hwrng|VIRTIO_POLL_INTERVAL_MS' docker-compose.yaml
! rg -n 'bbolt|BoltDB|DB_PATH|DB_IMPLEMENTATION|app-bin|local-tmpfs' \
  pkg/database cmd/api docker-compose.yaml cmd/*/Dockerfile cmd/*/Dockerfile.action

echo "== TLS / Traefik =="
rg -q 'websecure|certificatesresolvers|tls=true' deploy/traefik docker-compose.yaml

echo "== Metrics names =="
rg -q 'lokey_randomness_|lokey_fortuna_seed_bytes|lokey_fortuna_output_bytes|amplification' \
  pkg/ cmd/ deploy/grafana

echo "== Emulator =="
rg -q 'CONTROLLER_MODE' cmd/controller
docker compose config | rg -q 'emulated'

echo "== NixOS docs =="
test -f docs/nixos.md
rg -q 'hardware.i2c|i2c-dev|cgroup|services.lokey.tls|lokey-devices' nix/

echo "== Unit tests =="
export GOTOOLCHAIN=go1.27.0
go test ./pkg/api/... ./pkg/fortuna/... ./pkg/virtio/... ./pkg/atecc608a/... ./tests/...

echo "ALL CHECKS PASSED"
