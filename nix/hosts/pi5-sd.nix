{
  config,
  lib,
  pkgs,
  self,
  ...
}:

let
  # Package compose + deploy tree into the Nix store for the appliance image.
  lokeyTree = pkgs.runCommand "lokey-stack" { } ''
    mkdir -p $out
    cp ${self + "/docker-compose.yaml"} $out/docker-compose.yaml
    cp -r ${self + "/deploy"} $out/deploy
    # Optional local overrides are not required on the appliance image.
  '';
in
{
  # Raspberry Pi 5 LoKey appliance image (flash to microSD).
  networking.hostName = "lokey-pi5";
  system.stateVersion = "25.11";

  # SD image sizing — leave headroom for Docker images after first boot.
  sdImage = {
    compressImage = true;
    expandOnBoot = true;
  };

  boot.loader.grub.enable = false;
  boot.loader.generic-extlinux-compatible.enable = true;

  # I2C for ATECC608A; UART for debug header.
  hardware.i2c.enable = true;
  boot.kernelModules = [ "i2c-dev" ];
  boot.kernelParams = [
    "cgroup_enable=memory"
    "cgroup_memory=1"
    "console=ttyAMA10,115200n8"
    "console=tty0"
  ];

  users.users.lokey = {
    isNormalUser = true;
    extraGroups = [
      "wheel"
      "docker"
      "i2c"
      "dialout"
    ];
    # Change on first boot: passwd lokey
    initialPassword = "lokey";
  };
  security.sudo.wheelNeedsPassword = false;

  # SSH for headless first boot.
  services.openssh = {
    enable = true;
    settings.PasswordAuthentication = true;
  };

  networking.firewall.allowedTCPPorts = [
    22
    80
    443
  ];

  environment.systemPackages = with pkgs; [
    docker
    docker-compose
    git
    htop
    i2c-tools
    vim
  ];

  # Stage stack files and point the LoKey module at them.
  environment.etc."lokey".source = lokeyTree;

  services.lokey = {
    enable = true;
    domain = "lokey.local";
    tls = {
      mode = "acme";
      acmeEmail = "admin@lokey.local";
    };
    # Emulated by default so a fresh flash works without ATECC wiring.
    # Switch to hardware after attaching chips (see docs/nixos.md).
    devices = [
      {
        id = "device_a";
        i2cBus = 1;
        i2cAddress = "0x60";
        mode = "emulated";
      }
    ];
    composeFile = "${lokeyTree}/docker-compose.yaml";
  };

  # Pull / start stack after Docker is up (images from GHCR on first boot).
  systemd.services.lokey-stack = {
    serviceConfig = {
      WorkingDirectory = lib.mkForce "${lokeyTree}";
      # First boot may need a pull when images are not cached.
      ExecStartPre = [
        "-${pkgs.docker}/bin/docker compose -f ${lokeyTree}/docker-compose.yaml pull"
      ];
    };
    environment = {
      CONTROLLER_MODE = "emulated";
      LOKEY_DOMAIN = "lokey.local";
      LOKEY_ACME_EMAIL = "admin@lokey.local";
    };
  };
}
