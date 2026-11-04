{ inputs, self, ... }:
{
  perSystem =
    { pkgs, self', ... }:
    let
      inherit (pkgs) lib;
      spytify = self'.packages.default;
      exe = {
        go = lib.getExe pkgs.go;
        gofmt = lib.getExe' pkgs.go "gofmt";
      };

      goChecks = pkgs.stdenv.mkDerivation {
        name = "spytify-go-checks";
        inherit (spytify) src;
        nativeBuildInputs = spytify.nativeBuildInputs ++ [
          pkgs.go
          pkgs.ffmpeg-headless
        ];
        inherit (spytify) buildInputs;

        configurePhase = ''
          export GOCACHE=$TMPDIR/go-cache GOPATH=$TMPDIR/go GOFLAGS=-mod=vendor GOPROXY=off GOTOOLCHAIN=local
          cp -r --no-preserve=mode ${spytify.goModules} vendor
        '';

        buildPhase = ''
          echo "gofmt"
          unformatted=$(${exe.gofmt} -l $(find . -name '*.go' -not -path './vendor/*'))
          if [ -n "$unformatted" ]; then echo "not gofmt'd: $unformatted" >&2; exit 1; fi

          echo "go vet"
          ${exe.go} vet ./...

          echo "generated files"
          cp pkg/proto/schema.json internal/settings/keys_gen.go $TMPDIR/
          ${exe.go} generate ./pkg/proto ./internal/settings
          diff -u $TMPDIR/schema.json pkg/proto/schema.json
          diff -u $TMPDIR/keys_gen.go internal/settings/keys_gen.go

          echo "go test"
          export HOME=$TMPDIR/home XDG_RUNTIME_DIR=$TMPDIR/run
          mkdir -p $HOME $XDG_RUNTIME_DIR
          # The sandbox has no /etc/machine-id, which names the computer to Sentry.
          ${exe.go} test -timeout 20m -skip '^TestProblemsAreCountedAndSampled$' ./...
        '';

        installPhase = "touch $out";
      };

      nixos =
        (inputs.nixpkgs.lib.nixosSystem {
          modules = [
            self.nixosModules.default
            {
              nixpkgs.hostPlatform = pkgs.stdenv.hostPlatform.system;
              boot.loader.grub.enable = false;
              fileSystems."/" = {
                device = "nodev";
                fsType = "tmpfs";
              };
              system.stateVersion = "26.05";
              programs.spytify = {
                enable = true;
                service.enable = true;
                target.process = [ "player" ];
                settings = {
                  encode.format = "flac";
                  privacy.bug_tracker = false;
                };
              };
            }
          ];
        }).config;
    in
    {
      checks = {
        inherit (self'.packages) default headless;
        go = goChecks;
        nixos-module = pkgs.runCommand "spytify-nixos-module" { } ''
          grep -qx 'format = "flac"' ${nixos.environment.etc."spytify/config.toml".source}
          unit=${nixos.systemd.user.units."spytifyd.service".unit}/spytifyd.service
          grep -q 'SPYTIFY_TARGET_PROCESS=player' $unit
          grep -q 'SPYTIFY_CONFIG=/etc/spytify/config.toml' $unit
          touch $out
        '';
      };
    };
}
