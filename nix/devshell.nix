{ pkgs, config, ... }:
let
  inherit (pkgs) lib;
  exe = {
    go = lib.getExe pkgs.go;
    gofmt = lib.getExe' pkgs.go "gofmt";
    staticcheck = lib.getExe' pkgs.go-tools "staticcheck";
    gotestsum = lib.getExe pkgs.gotestsum;
    treefmt = lib.getExe config.treefmt.build.wrapper;
  };
  gui = pkgs.callPackage ./gui-libs.nix { };
  # Each package's own folders: merged into the shell's profile, the X11
  # headers of libx11 and xorgproto collide.
  includePath = lib.makeSearchPath "include" gui.headers;
  pkgConfigPath = lib.concatStringsSep ":" [
    (lib.makeSearchPath "lib/pkgconfig" gui.headers)
    (lib.makeSearchPath "share/pkgconfig" gui.headers)
  ];
  libraryPath = lib.makeLibraryPath gui.libs;
in
{
  devshells.default =
    { extraModulesPath, ... }:
    {
      imports = [ "${extraModulesPath}/language/go.nix" ];

      devshell = {
        name = "spytify";
        motd = ''
          {202}spytify{reset}: Go ${pkgs.go.version}, cgo, ffmpeg and PipeWire
          $(type -p menu &>/dev/null && menu)
        '';
        packages = (
          with pkgs;
          [
            gcc
            binutils
            pkg-config
            gopls
            gotools
            go-tools
            gotestsum
            delve
            ffmpeg-headless
            pipewire
            wireplumber
            dbus
            nfpm
            config.treefmt.build.wrapper
          ]
        );
      };

      language.go.package = pkgs.go;

      env = [
        {
          name = "CGO_ENABLED";
          value = "1";
        }
        {
          name = "C_INCLUDE_PATH";
          prefix = includePath;
        }
        {
          name = "LIBRARY_PATH";
          prefix = libraryPath;
        }
        {
          name = "PKG_CONFIG_PATH";
          prefix = pkgConfigPath;
        }
        {
          name = "LD_LIBRARY_PATH";
          prefix = "${libraryPath}:/run/opengl-driver/lib";
        }
        # Nix's toolchain, not a download of the one go.mod names.
        {
          name = "GOTOOLCHAIN";
          value = "local";
        }
      ];

      commands = [
        {
          category = "build";
          name = "build";
          help = "build spytifyd, spytify-cli and the app into bin/";
          command = ''cd "$PRJ_ROOT" && ${exe.go} build -o bin/ ./cmd/spytifyd ./cmd/spytify-cli ./cmd/spytify-ui "$@"'';
        }
        {
          category = "build";
          name = "generate";
          help = "regenerate the protocol schema and the setting keys";
          command = ''cd "$PRJ_ROOT" && ${exe.go} generate ./pkg/proto ./internal/settings'';
        }
        {
          category = "check";
          name = "lint";
          help = "gofmt, go vet and staticcheck, as CI";
          command = ''
            cd "$PRJ_ROOT"
            unformatted=$(${exe.gofmt} -l .)
            if [ -n "$unformatted" ]; then echo "gofmt:" "$unformatted" >&2; exit 1; fi
            ${exe.go} vet ./... && ${exe.staticcheck} ./...
          '';
        }
        {
          category = "check";
          name = "tests";
          help = "the unit, scenario, UI and end-to-end tests, under -race as CI";
          command = ''cd "$PRJ_ROOT" && ${exe.gotestsum} --format testname -- -race -timeout 20m ./... "$@"'';
        }
        {
          category = "check";
          name = "integration";
          help = "the PipeWire and MPRIS tests, in a headless PipeWire session";
          command = ''
            cd "$PRJ_ROOT"
            . scripts/pipewire-session.sh
            ${exe.go} test -tags integration -v ./test/integration/ "$@"
          '';
        }
        {
          category = "nix";
          name = "fmt";
          help = "format the Go and Nix files (treefmt)";
          command = ''${exe.treefmt} "$@"'';
        }
      ];
    };
}
