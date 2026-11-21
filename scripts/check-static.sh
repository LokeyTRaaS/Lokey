#!/usr/bin/env bash
# Static checks mirrored by CI Lint and Test + pre-commit.
# Does not run go test, Docker builds, or container image scans.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

run_golangci_source() {
  golangci-lint run --config .golangci.yml ./pkg/... ./cmd/...
}

run_golangci_tests() {
  golangci-lint run --config .golangci-tests.yml ./tests/...
}

run_nix_flake() {
  local nix=(nix --extra-experimental-features "nix-command flakes")
  "${nix[@]}" flake metadata >/dev/null
  "${nix[@]}" flake show >/dev/null
  host="$("${nix[@]}" eval .#nixosConfigurations.lokey-pi5.config.networking.hostName --raw)"
  test "${host}" = "lokey-pi5"
  "${nix[@]}" eval .#packages.aarch64-linux.sdcard.name --raw >/dev/null
  echo "Nix flake OK"
}

run_grype() {
  grype dir:. --config .grype.yaml --fail-on high -o table
}

usage() {
  echo "usage: $0 [all|golangci-source|golangci-tests|nix-flake|grype]" >&2
  exit 1
}

target="${1:-all}"
case "${target}" in
  golangci-source) run_golangci_source ;;
  golangci-tests) run_golangci_tests ;;
  nix-flake) run_nix_flake ;;
  grype) run_grype ;;
  all)
    run_golangci_source
    run_golangci_tests
    run_nix_flake
    run_grype
    ;;
  *) usage ;;
esac
