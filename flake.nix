{
  description = "Spytify: records an application's audio, splits it into tracks and tags them";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
    devshell = {
      url = "github:numtide/devshell";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{ self, flake-parts, ... }:
    let
      # A build of this flake is a development one, as scripts/version.sh
      # names a build after no tag, and reports its commit.
      version = "0.0.0-dev";
      reportedVersion = "${version}+${self.shortRev or self.dirtyShortRev or "unknown"}";
    in
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [
        inputs.devshell.flakeModule
        inputs.treefmt-nix.flakeModule
        ./nix/checks.nix
      ];

      # spytify-tap, the macOS capture helper, and the Windows build are
      # not packaged: Linux only.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      flake = {
        overlays.default = final: _prev: {
          spytify = final.callPackage ./nix/package.nix { inherit version reportedVersion; };
          spytify-headless = final.spytify.override { headless = true; };
        };

        nixosModules.default = import ./nix/modules/nixos.nix self;
        homeManagerModules.default = import ./nix/modules/home-manager.nix self;
      };

      perSystem =
        { pkgs, system, ... }:
        {
          _module.args.pkgs = import inputs.nixpkgs {
            inherit system;
            overlays = [ self.overlays.default ];
          };

          packages = {
            default = pkgs.spytify;
            headless = pkgs.spytify-headless;
          };

          imports = [
            ./nix/devshell.nix
          ];

          treefmt = import ./nix/treefmt.nix;
        };
    };
}
