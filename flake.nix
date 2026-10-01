{
  description = "lazify: one declarative YAML file → a lazygit-style TUI";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      forAllSystems = lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      version = "0.1.0-${self.shortRev or self.dirtyShortRev or "dirty"}";
    in
    {
      packages = forAllSystems (
        system:
        let
          lazify = nixpkgs.legacyPackages.${system}.callPackage ./nix/package.nix { inherit version; };
        in
        {
          inherit lazify;
          default = lazify;
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = lib.getExe self.packages.${system}.lazify;
        };
      });

      overlays.default = final: _: {
        lazify = final.callPackage ./nix/package.nix { inherit version; };
      };

      homeManagerModules = {
        lazify = import ./nix/hm-module.nix self;
        default = self.homeManagerModules.lazify;
      };

      checks = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          package = self.packages.${system}.lazify;
          hm-module = import ./nix/tests/hm-module.nix {
            inherit pkgs lib;
            module = self.homeManagerModules.lazify;
            package = self.packages.${system}.lazify;
          };
        }
      );

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
