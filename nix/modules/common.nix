{
  self,
  lib,
  pkgs,
}:
let
  inherit (lib) mkOption types;
  toml = pkgs.formats.toml { };
  system = pkgs.stdenv.hostPlatform.system;
in
{
  options = {
    enable = lib.mkEnableOption "Spytify, which records an application's audio, splits it into tracks and tags them";

    package = mkOption {
      type = types.package;
      default = self.packages.${system}.default;
      defaultText = lib.literalExpression "spytify.packages.\${pkgs.stdenv.hostPlatform.system}.default";
      description = ''
        The Spytify package. `spytify.packages.''${system}.headless` has
        spytifyd and spytify-cli only, with no desktop app.
      '';
    };

    settings = mkOption {
      inherit (toml) type;
      default = { };
      example = lib.literalExpression ''
        {
          output.dir = "~/Music/Recordings";
          encode.format = "flac";
          recorder.trim_silence = true;
          app.check_updates = false;
          privacy.bug_tracker = false;
        }
      '';
      description = ''
        Spytify's settings, written to config.toml. Every key is a row of
        internal/settings/table.go; one the table does not know is ignored
        with a warning, and a setting left out keeps its default.

        When this is not empty, config.toml is in the Nix store and read-only:
        the app's Settings screen shows the values but cannot save a change.
      '';
    };

    target = {
      process = mkOption {
        type = types.listOf types.str;
        default = [ ];
        example = [ "player" ];
        description = ''
          Process names of the player to record (SPYTIFY_TARGET_PROCESS),
          one per packaging of it. Empty: the package's default player, or
          the one chosen in the app.
        '';
      };
      mpris = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "org.mpris.MediaPlayer2.player";
        description = ''
          D-Bus name of the player's MPRIS interface (SPYTIFY_TARGET_MPRIS).
          Null: `org.mpris.MediaPlayer2.<process name, lowercase>`.
        '';
      };
      launch = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = ''
          Command that starts the player when a session starts and it is not
          running (SPYTIFY_TARGET_LAUNCH).
        '';
      };
    };

    environment = mkOption {
      type = types.attrsOf types.str;
      default = { };
      example = lib.literalExpression ''
        {
          SPYTIFY_FFMPEG = lib.getExe pkgs.ffmpeg-full;
        }
      '';
      description = ''
        More variables for spytifyd, spytify-cli and the app, such as
        SPYTIFY_FFMPEG or SPYTIFY_SOCKET. The package already points
        SPYTIFY_FFMPEG at its own ffmpeg.
      '';
    };

    service.enable = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Run spytifyd as a systemd user service from login, so it records with
        no window open. Without it, the app starts spytifyd itself.
      '';
    };
  };

  environment =
    cfg:
    lib.filterAttrs (_: v: v != null) {
      SPYTIFY_TARGET_PROCESS =
        if cfg.target.process == [ ] then null else lib.concatStringsSep "," cfg.target.process;
      SPYTIFY_TARGET_MPRIS = cfg.target.mpris;
      SPYTIFY_TARGET_LAUNCH = cfg.target.launch;
    }
    // cfg.environment;

  configFile = cfg: toml.generate "spytify-config.toml" cfg.settings;

  assertions = cfg: [
    {
      assertion =
        cfg.service.enable -> (cfg.target.process != [ ] || (cfg.package.defaultProcess or "") != "");
      message = ''
        spytify: service.enable runs spytifyd, which needs a player to record:
        set target.process, or use a package built with defaultProcess.
      '';
    }
  ];
}
