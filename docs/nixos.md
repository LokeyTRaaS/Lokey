# NixOS deployment

LoKey’s most reproducible deploy path is the Nix flake in this repo. It builds a bootable Raspberry Pi 5 microSD image with Docker Compose, Traefik HTTPS prerequisites, and I2C/cgroup support.

CI **does not** build or publish the SD image (that is too heavy for GitHub Actions). CI only validates the flake. You build and flash locally.

## Requirements

- [Nix](https://nixos.org/download/) with flakes enabled
- An **aarch64** builder, or `qemu-aarch64` binfmt on x86_64
- A microSD card and a block device path (check with `lsblk`)

## Build the SD image

```bash
nix build .#packages.aarch64-linux.sdcard
# result -> compressed image (usually *.img.zst)
```

The image includes:

- NixOS for Raspberry Pi 5 (`nixos-hardware` + unstable aarch64 SD image)
- I2C / cgroup / Docker prerequisites
- LoKey compose + deploy tree staged for first boot
- User `lokey` / password `lokey` (change on first boot)
- Controllers in **emulated** mode so first boot works without ATECC wiring

## Flash

```bash
# Helper (asks for confirmation before dd):
./scripts/flash-sd.sh /dev/sdX

# Or manually:
zstd -d result/sd-image/*.img.zst -o lokey-pi5.img   # path may vary
sudo dd if=lokey-pi5.img of=/dev/sdX bs=4M status=progress conv=fsync
```

Use the whole disk device (`/dev/sdX` / `/dev/mmcblk0`), not a partition.

## First boot

1. SSH: `ssh lokey@lokey-pi5.local` (or the DHCP address), password `lokey`
2. Run `passwd` immediately
3. On first start Docker pulls LoKey images from GHCR (needs network)
4. For a real hostname/TLS, set `services.lokey.domain` and ACME email (or file certs)

To use hardware ATECC devices, set `services.lokey.devices.*.mode = "hardware"` and rebuild, or override compose `CONTROLLER_MODE=hardware`.

## Flake check (what CI runs)

```bash
nix flake show
nix eval .#nixosConfigurations.lokey-pi5.config.networking.hostName --raw
# -> lokey-pi5
```

## Module options

The NixOS module ([`nix/modules/lokey.nix`](../nix/modules/lokey.nix)) enables:

| Requirement | Setting |
|-------------|---------|
| I2C | `hardware.i2c.enable = true`, `boot.kernelModules = [ "i2c-dev" ]` |
| cgroups | `cgroup_enable=memory` / `cgroup_memory=1` where needed; Docker `cgroupdriver=systemd` |
| namespaces | Standard NixOS kernel |
| Docker | `virtualisation.docker.enable = true` |

Do **not** strip cgroup/namespace support — this stack assumes capable hardware (Pi 5 class or server).

## TLS / HTTPS

```nix
services.lokey.tls = {
  mode = "acme"; # or "file"
  acmeEmail = "ops@example.com";
  # certFile / keyFile for mode = "file"
};
```

Traefik terminates TLS on `:443` and redirects `:80` → HTTPS.

## Multi-device

```nix
services.lokey.devices = [
  { id = "device_a"; i2cBus = 1; i2cAddress = "0x60"; mode = "hardware"; }
  { id = "device_b"; i2cBus = 1; i2cAddress = "0x61"; mode = "hardware"; }
];
```

Controllers attach only to the `lokey-devices` Docker network.

## Example hosts

- [`nix/hosts/pi5-sd.nix`](../nix/hosts/pi5-sd.nix) — appliance image used by `packages.aarch64-linux.sdcard`
- [`nix/hosts/pi5-example.nix`](../nix/hosts/pi5-example.nix) — module-only example for an existing NixOS host

## Local laptop (no ATECC)

Prefer Docker Compose with `CONTROLLER_MODE=emulated` — see the [README](../ReadMe.md). Hardware mode is for Pi deployment.
