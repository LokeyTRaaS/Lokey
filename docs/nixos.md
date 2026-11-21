# NixOS deployment

LoKey’s most reproducible deploy path is the Nix flake in this repo. It builds a bootable Raspberry Pi 5 microSD image with Docker Compose, Traefik HTTPS prerequisites, and I2C/cgroup support.

CI **does not** build or publish the SD image (that is too heavy for GitHub Actions). CI only validates the flake. You build and flash locally.

## Requirements

- [Nix](https://nixos.org/download/) with flakes enabled
- An **aarch64** builder, or `qemu-aarch64` binfmt on x86_64
- A microSD card and a block device path (check with `lsblk`)

## Build the SD image

Prefer naming the result symlink so it matches the flash helper:

```bash
nix build .#packages.aarch64-linux.sdcard -o result-sdcard
# Compressed image under result-sdcard/ (usually *.img.zst)
```

Default `nix build` without `-o` creates `./result` instead — that also works if you decompress manually.

The image includes:

- NixOS for Raspberry Pi 5 (`nixos-hardware` + unstable aarch64 SD image)
- I2C / cgroup / Docker prerequisites
- LoKey compose + deploy tree staged for first boot
- User `lokey` / password `lokey` (change on first boot)
- Controllers started in **emulated** mode via the appliance host unit (no ATECC required for first boot)

## Flash

```bash
# Helper (asks for confirmation before dd); looks for result-sdcard then result:
./scripts/flash-sd.sh /dev/sdX

# Or manually (adjust path to the .img.zst Nix produced):
zstd -d result-sdcard/sd-image/*.img.zst -o lokey-pi5.img   # path may vary
sudo dd if=lokey-pi5.img of=/dev/sdX bs=4M status=progress conv=fsync
```

Use the whole disk device (`/dev/sdX` / `/dev/mmcblk0`), not a partition.

## First boot

1. SSH: `ssh lokey@lokey-pi5.local` (or the DHCP address), password `lokey`
2. Run `passwd` immediately
3. On first start Docker pulls LoKey images from GHCR (needs network)
4. For a real hostname/TLS, set `services.lokey.domain` and ACME email (or file certs) and rebuild the image/host

## Flake check (what CI runs)

```bash
nix flake show
nix eval .#nixosConfigurations.lokey-pi5.config.networking.hostName --raw
# -> lokey-pi5
```

## Module options (what actually works today)

The NixOS module ([`nix/modules/lokey.nix`](../nix/modules/lokey.nix)) enables:

| Requirement | Setting |
|-------------|---------|
| I2C | `hardware.i2c.enable = true`, `boot.kernelModules = [ "i2c-dev" ]` |
| cgroups | `cgroup_enable=memory` / `cgroup_memory=1` where needed; Docker `cgroupdriver=systemd` |
| namespaces | Standard NixOS kernel |
| Docker | `virtualisation.docker.enable = true` |
| Domain / ACME env | Passed into the staged Compose stack via systemd environment |

Do **not** strip cgroup/namespace support — this stack assumes capable hardware (Pi 5 class or server).

### `services.lokey.devices` limitation

The module **declares** `services.lokey.devices`, but it does **not** generate per-device Compose services from that list yet. Runtime controllers come from the staged [`docker-compose.yaml`](../docker-compose.yaml) (`controller-a` / `controller-b`). The appliance host ([`nix/hosts/pi5-sd.nix`](../nix/hosts/pi5-sd.nix)) forces `CONTROLLER_MODE=emulated` via the `lokey-stack` unit.

To use hardware ATECC today:

1. Wire the chip ([Hardware Setup](hardware-setup.md))
2. Override Compose / unit environment to `CONTROLLER_MODE=hardware` (and I2C bus as needed), **or** edit the staged compose on the device
3. Rebuild/redeploy the host if you bake changes into the image

## TLS / HTTPS

```nix
services.lokey.tls = {
  mode = "acme"; # or "file"
  acmeEmail = "ops@example.com";
  # certFile / keyFile for mode = "file"
};
```

Traefik terminates TLS on `:443` and redirects `:80` → HTTPS.

## Example hosts

- [`nix/hosts/pi5-sd.nix`](../nix/hosts/pi5-sd.nix) — appliance image used by `packages.aarch64-linux.sdcard`
- [`nix/hosts/pi5-example.nix`](../nix/hosts/pi5-example.nix) — documentation-only module import for an existing NixOS host (not a full `nixosConfigurations` entry)

## Local laptop (no ATECC)

Prefer Docker Compose with `CONTROLLER_MODE=emulated` — see the [README](../README.md). Hardware mode is for Pi deployment.
