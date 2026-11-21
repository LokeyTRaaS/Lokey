{ config, pkgs, ... }:

{
  imports = [ ../modules/lokey.nix ];

  # Example Raspberry Pi 5 host running LoKey with hardware ATECC devices.
  networking.hostName = "lokey-pi5";

  services.lokey = {
    enable = true;
    domain = "lokey.example.com";
    tls = {
      mode = "acme";
      acmeEmail = "ops@example.com";
    };
    devices = [
      { id = "device_a"; i2cBus = 1; i2cAddress = "0x60"; mode = "hardware"; }
      { id = "device_b"; i2cBus = 1; i2cAddress = "0x61"; mode = "hardware"; }
    ];
  };

  # Boot checklist (also enforced by services.lokey module):
  # - I2C enabled (hardware.i2c + i2c-dev + dtparam=i2c_arm=on)
  # - cgroups + namespaces available for Docker
  # - HTTPS via Traefik (ACME or file certs)
}
