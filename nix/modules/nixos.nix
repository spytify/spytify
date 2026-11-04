self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.spytify;
  common = import ./common.nix { inherit self lib pkgs; };
  env =
    common.environment cfg
    // lib.optionalAttrs (cfg.settings != { }) {
      SPYTIFY_CONFIG = "/etc/spytify/config.toml";
    };
in
{
  options.programs.spytify = common.options;

  config = lib.mkIf cfg.enable {
    assertions = common.assertions cfg;

    environment.systemPackages = [ cfg.package ];

    environment.etc."spytify/config.toml" = lib.mkIf (cfg.settings != { }) {
      source = common.configFile cfg;
    };

    environment.sessionVariables = env;

    # The package's spytifyd.service: a user can enable it with
    # systemctl --user enable --now spytifyd, or service.enable does for all.
    systemd.packages = [ cfg.package ];

    systemd.user.services.spytifyd = {
      environment = env;
      # NixOS gives a unit a PATH of its own: keep the user's, so the
      # player's launch command and the system's PipeWire tools are found.
      path = [
        "/run/wrappers"
        "/etc/profiles/per-user/%u"
        "/run/current-system/sw"
      ];
      wantedBy = lib.mkIf cfg.service.enable [ "default.target" ];
    };
  };
}
