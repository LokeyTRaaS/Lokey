# NixOS deployment

LoKey can be deployed on NixOS (Raspberry Pi 5 ARM64 or x86_64) with Docker Compose and Traefik HTTPS.

## Boot requirements

The NixOS module ([`nix/modules/lokey.nix`](../nix/modules/lokey.nix)) enables:

| Requirement | Setting |
|-------------|---------|
| I2C | `hardware.i2c.enable = true`, `boot.kernelModules = [ "i2c-dev" ]`, Pi firmware `dtparam=i2c_arm=on` |
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

See [`nix/hosts/pi5-example.nix`](../nix/hosts/pi5-example.nix).

## Local laptop (no ATECC)

Use Docker Compose defaults with `CONTROLLER_MODE=emulated` — no NixOS required. Hardware mode is for Pi deployment.
