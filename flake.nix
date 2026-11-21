{
  description = "LoKey — NixOS SD card image for Raspberry Pi 5 and host modules";

  inputs = {
    # Pi 5 family boot files are on unstable; stable stock images lack them.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    nixos-hardware.url = "github:NixOS/nixos-hardware/master";
  };

  outputs =
    {
      self,
      nixpkgs,
      nixos-hardware,
      ...
    }:
    let
      lib = nixpkgs.lib;
    in
    {
      nixosModules.lokey = import ./nix/modules/lokey.nix;

      nixosConfigurations.lokey-pi5 = nixpkgs.lib.nixosSystem {
        system = "aarch64-linux";
        specialArgs = { inherit self; };
        modules = [
          nixos-hardware.nixosModules.raspberry-pi-5
          "${nixpkgs}/nixos/modules/installer/sd-card/sd-image-aarch64.nix"
          ./nix/modules/lokey.nix
          ./nix/hosts/pi5-sd.nix
        ];
      };

      packages.aarch64-linux = {
        # Flashable microSD image for Raspberry Pi 5.
        #   nix build .#packages.aarch64-linux.sdcard
        #   ./scripts/flash-sd.sh /dev/sdX
        sdcard = self.nixosConfigurations.lokey-pi5.config.system.build.sdImage;
        default = self.nixosConfigurations.lokey-pi5.config.system.build.sdImage;
      };

      # Expose the same attribute on x86_64 so `nix build .#sdcard` can target
      # the aarch64 image when binfmt/qemu or a remote builder is configured.
      packages.x86_64-linux.sdcard = self.packages.aarch64-linux.sdcard;

      formatter = lib.genAttrs [ "x86_64-linux" "aarch64-linux" ] (
        system: (import nixpkgs { inherit system; }).nixfmt-rfc-style
      );
    };
}
