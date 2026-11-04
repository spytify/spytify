{
  lib,
  buildGoModule,
  makeWrapper,
  addDriverRunpath,
  pkg-config,
  ffmpeg-headless,
  pipewire,
  installShellFiles,
  callPackage,
  stdenv,

  version ? "0.0.0-dev.0",
  # The version the app reports. The flake adds its commit, which stays out
  # of the derivation's version so that the Go modules are not fetched
  # again for every commit.
  reportedVersion ? version,
  headless ? false,
  # The player recorded when none is chosen: its bare process name
  # (SPYTIFY_DEFAULT_PROCESS of CONTRIBUTING.md).
  defaultProcess ? "",
  # The bug tracker's Sentry project. Empty: nothing is sent and the
  # switch is hidden.
  sentryDsn ? "",
  # The ffmpeg spytifyd and the app run, unless SPYTIFY_FFMPEG says
  # otherwise. It needs libmp3lame, aac and flac.
  ffmpeg ? ffmpeg-headless,
}:

let
  module = "github.com/spytify/spytify";

  gui = callPackage ./gui-libs.nix { };
in
buildGoModule (finalAttrs: {
  pname = if headless then "spytify-headless" else "spytify";
  inherit version;

  # Everything but the Nix files and CI, so editing those rebuilds nothing.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.difference ../. (
      lib.fileset.unions [
        ../nix
        ../flake.nix
        (lib.fileset.maybeMissing ../flake.lock)
        ../.github
      ]
    );
  };

  vendorHash = "sha256-gAkyij3F2x8hVBU2NvJmgBRMZn0dO7Rm5y2P9k7MBMQ=";

  subPackages = [
    "cmd/spytifyd"
    "cmd/spytify-cli"
  ]
  ++ lib.optional (!headless) "cmd/spytify-ui";

  env.CGO_ENABLED = if headless then "0" else "1";

  ldflags = [
    "-s"
    "-w"
    "-X ${module}/pkg/proto.Version=v${reportedVersion}"
    "-X ${module}/internal/target.DefaultProcess=${defaultProcess}"
    "-X ${module}/internal/telemetry.DSN=${sentryDsn}"
  ];

  nativeBuildInputs = [
    makeWrapper
    installShellFiles
  ]
  ++ lib.optionals (!headless) [
    pkg-config
    addDriverRunpath
  ];

  buildInputs = lib.optionals (!headless) (gui.libs ++ gui.protocols);

  # The tests need PipeWire, a display or minutes under -race: the flake's
  # checks run the ones that fit in the sandbox.
  doCheck = false;

  postInstall = ''
    install -Dm644 packaging/linux/spytifyd.service $out/lib/systemd/user/spytifyd.service
    substituteInPlace $out/lib/systemd/user/spytifyd.service \
      --replace-fail /usr/bin/spytifyd $out/bin/spytifyd
  ''
  + lib.optionalString (stdenv.buildPlatform.canExecute stdenv.hostPlatform) ''
    installShellCompletion --cmd spytify-cli \
      --bash <($out/bin/spytify-cli completion bash) \
      --fish <($out/bin/spytify-cli completion fish) \
      --zsh <($out/bin/spytify-cli completion zsh)
  ''
  + lib.optionalString (!headless) ''
    install -Dm644 packaging/linux/org.spytify.ui.desktop $out/share/applications/org.spytify.ui.desktop
    icons=$out/share/icons/hicolor
    install -Dm644 assets/logo.png $icons/128x128/apps/spytify.png
    install -Dm644 assets/logo256px.png $icons/256x256/apps/spytify.png
    for size in 48 64; do
      mkdir -p $icons/''${size}x$size/apps
      go run ./internal/tools/genico -size $size -o $icons/''${size}x$size/apps/spytify.png assets/logo256px.png
    done
  '';

  # GLFW opens the X11 or Wayland libraries with dlopen, and libglvnd finds
  # the graphics driver under /run/opengl-driver (NixOS's hardware.graphics).
  postFixup = ''
    for bin in $out/bin/*; do
      wrapProgram "$bin" \
        --set-default SPYTIFY_FFMPEG ${lib.getExe ffmpeg} \
        --suffix PATH : ${lib.makeBinPath [ pipewire ]}
    done
  ''
  + lib.optionalString (!headless) ''
    patchelf --add-rpath ${lib.makeLibraryPath gui.libs} $out/bin/.spytify-ui-wrapped
    addDriverRunpath $out/bin/.spytify-ui-wrapped
  '';

  # For the modules, which check that spytifyd has a player to record.
  passthru = { inherit defaultProcess headless; };

  meta = {
    description = "Records an application's audio, splits it into tracks and tags them";
    homepage = "https://spytify.github.io/spytify/";
    license = lib.licenses.mit;
    platforms = lib.platforms.linux;
    mainProgram = if headless then "spytify-cli" else "spytify-ui";
  };
})
