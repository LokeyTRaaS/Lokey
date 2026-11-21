{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.lokey;
in {
  options.services.lokey = {
    enable = mkEnableOption "LoKey true random number generation stack";

    domain = mkOption {
      type = types.str;
      default = "lokey.local";
      description = "Public hostname for Traefik HTTPS routers";
    };

    tls = {
      mode = mkOption {
        type = types.enum [ "acme" "file" ];
        default = "acme";
        description = "TLS certificate mode";
      };
      acmeEmail = mkOption {
        type = types.str;
        default = "admin@example.com";
      };
      certFile = mkOption {
        type = types.nullOr types.path;
        default = null;
      };
      keyFile = mkOption {
        type = types.nullOr types.path;
        default = null;
      };
    };

    devices = mkOption {
      type = types.listOf (types.submodule ({ ... }: {
        options = {
          id = mkOption { type = types.str; };
          i2cBus = mkOption { type = types.int; default = 1; };
          i2cAddress = mkOption { type = types.str; default = "0x60"; };
          mode = mkOption {
            type = types.enum [ "hardware" "emulated" ];
            default = "hardware";
          };
        };
      }));
      default = [ { id = "device_a"; i2cBus = 1; } ];
      description = "ATECC608A controller devices (each becomes a controller container on lokey-devices)";
    };

    composeFile = mkOption {
      type = types.path;
      default = ../.. + "/docker-compose.yaml";
      description = "Path to docker-compose.yaml";
    };
  };

  config = mkIf cfg.enable {
    # I2C + container isolation prerequisites
    hardware.i2c.enable = true;
    boot.kernelModules = [ "i2c-dev" ];
    boot.kernelParams = [
      # Ensure cgroup memory accounting where platform still needs it
      "cgroup_enable=memory"
      "cgroup_memory=1"
    ];

    # Docker with cgroup v2 / namespaces (NixOS defaults already include namespaces)
    virtualisation.docker = {
      enable = true;
      daemon.settings = {
        exec-opts = [ "native.cgroupdriver=systemd" ];
      };
    };

    # On Raspberry Pi OS / firmware images, also set in config.txt:
    #   dtparam=i2c_arm=on
    #   dtparam=i2c_arm_baudrate=100000

    environment.sessionVariables = {
      LOKEY_DOMAIN = cfg.domain;
      LOKEY_ACME_EMAIL = cfg.tls.acmeEmail;
    };

    systemd.services.lokey-stack = {
      description = "LoKey Docker Compose stack";
      after = [ "docker.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      requires = [ "docker.service" ];
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        WorkingDirectory = dirOf (toString cfg.composeFile);
        ExecStart = "${pkgs.docker}/bin/docker compose -f ${cfg.composeFile} up -d";
        ExecStop = "${pkgs.docker}/bin/docker compose -f ${cfg.composeFile} down";
      };
      environment = {
        LOKEY_DOMAIN = cfg.domain;
        LOKEY_ACME_EMAIL = cfg.tls.acmeEmail;
      };
    };
  };
}
