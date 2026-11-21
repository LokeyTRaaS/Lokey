# NixOS deployment

LoKey can be deployed on NixOS (Raspberry Pi 5 ARM64 or x86_64) with Docker Compose and Traefik HTTPS.

## Flashable microSD image (Pi 5)

This repository ships a Nix flake that builds a bootable SD card image with:

- NixOS on Raspberry Pi 5 (`nixos-hardware` + unstable aarch64 SD image)
- I2C / cgroup / Docker prerequisites
- LoKey compose stack staged under `/etc/lokey`
- Default user `lokey` / password `lokey` (change on first boot)
- Controllers default to **emulated** mode so first boot works without ATECC wiring

### Build

On an aarch64 builder (or with `qemu-aarch64` binfmt):

```bash
nix build .#packages.aarch64-linux.sdcard
```

Release tags also publish `lokey-pi5-sdcard.img.zst` as a GitHub Release asset.

### Flash

```bash
# From a release asset:
zstd -d lokey-pi5-sdcard.img.zst -o lokey-pi5.img
sudo dd if=lokey-pi5.img of=/dev/sdX bs=4M status=progress conv=fsync

# Or build + flash in one step (interactive confirmation):
./scripts/flash-sd.sh /dev/sdX
```

Replace `/dev/sdX` with the SD device (not a partition). Use `lsblk` to confirm.

### First boot

1. SSH: `ssh lokey@lokey-pi5.local` (or the DHCP address), password `lokey`
2. `passwd` immediately
3. Docker pulls LoKey images from GHCR on first start (needs network)
4. HTTPS: set `services.lokey.domain` / ACME email for a real hostname, or use Traefik file certs

To switch to hardware ATECC devices, update the NixOS config `services.lokey.devices` modes to `"hardware"` and rebuild, or override compose env `CONTROLLER_MODE=hardware`.

## Module options

The NixOS module ([`nix/modules/lokey.nix`](../nix/modules/lokey.nix)) enables:

| Requirement | Setting |
|-------------|---------|
| I2C | `hardware.i2c.enable = true`, `boot.kernelModules = [ "i2c-dev" ]` |
| cgroups | `cgroup_enable=memory` / `cgroup_memory=1` kernel params where needed; Docker `cgroupdriver=systemd` |
| namespaces | Provided by the standard NixOS kernel; required for container isolation |
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

## Example host

See [`nix/hosts/pi5-example.nix`](../nix/hosts/pi5-example.nix) (module-only) and [`nix/hosts/pi5-sd.nix`](../nix/hosts/pi5-sd.nix) (SD appliance).

## Local laptop (no ATECC)

Use Docker Compose defaults with `CONTROLLER_MODE=emulated` — no NixOS required. Hardware mode is for Pi deployment.
