#!/usr/bin/env bash
# Flash a LoKey Raspberry Pi 5 NixOS SD image to a block device.
# CI does not publish this image — build it locally from the flake.
#
# Usage:
#   ./scripts/flash-sd.sh /dev/sdX
#   ./scripts/flash-sd.sh /dev/sdX path/to/lokey-pi5.img
#   ./scripts/flash-sd.sh /dev/sdX path/to/lokey-pi5.img.zst
#
# If IMAGE is omitted, builds: nix build .#packages.aarch64-linux.sdcard

set -euo pipefail

DEVICE="${1:-}"
IMAGE="${2:-}"

if [[ -z "${DEVICE}" ]]; then
  echo "usage: $0 /dev/sdX [image|.img.zst]" >&2
  exit 1
fi

if [[ ! -b "${DEVICE}" ]]; then
  echo "error: ${DEVICE} is not a block device" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

if [[ -z "${IMAGE}" ]]; then
  echo "Building aarch64 SD image (this can take a while)..."
  nix build .#packages.aarch64-linux.sdcard -o result-sdcard
  IMAGE="$(find -L result-sdcard -type f \( -name '*.img' -o -name '*.img.zst' -o -name '*.img.gz' \) | head -n1)"
  if [[ -z "${IMAGE}" ]]; then
    echo "error: no image found under result-sdcard" >&2
    exit 1
  fi
  echo "Built: ${IMAGE}"
fi

TMP=""
cleanup() {
  if [[ -n "${TMP}" && -f "${TMP}" ]]; then
    rm -f "${TMP}"
  fi
}
trap cleanup EXIT

SRC="${IMAGE}"
case "${IMAGE}" in
  *.zst)
    TMP="$(mktemp /tmp/lokey-sd-XXXXXX.img)"
    echo "Decompressing zstd..."
    zstd -d -f "${IMAGE}" -o "${TMP}"
    SRC="${TMP}"
    ;;
  *.gz)
    TMP="$(mktemp /tmp/lokey-sd-XXXXXX.img)"
    echo "Decompressing gzip..."
    gzip -dc "${IMAGE}" >"${TMP}"
    SRC="${TMP}"
    ;;
esac

echo "About to write ${SRC} -> ${DEVICE}"
lsblk "${DEVICE}"
read -r -p "Type YES to continue: " confirm
if [[ "${confirm}" != "YES" ]]; then
  echo "aborted"
  exit 1
fi

sudo dd if="${SRC}" of="${DEVICE}" bs=4M status=progress conv=fsync
sync
echo "Done. Insert the card into the Pi 5, boot, then SSH as lokey / lokey (change the password)."
