#!/usr/bin/env bash
# Generate Swagger docs the same way CI does (build.yml + docs.yml).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SWAG_VERSION="${SWAG_VERSION:-v1.16.6}"

if ! command -v swag >/dev/null 2>&1; then
  echo "Installing swag ${SWAG_VERSION}..."
  go install "github.com/swaggo/swag/cmd/swag@${SWAG_VERSION}"
fi

echo "=== Generating Swagger documentation ==="
# Note: swag v1.16.x --parseDependency can fail on Go 1.27 stdlib (math/rand/v2).
# If generation fails, sync api.ControllerStatus manually in pkg/api/docs/{docs.go,swagger.*}.
swag init --parseDependency --parseInternal \
  --generalInfo main.go \
  --dir ./cmd/api/,./pkg/api \
  --output ./pkg/api/docs

echo ""
echo "=== Generated Swagger files ==="
ls -lah ./pkg/api/docs/
