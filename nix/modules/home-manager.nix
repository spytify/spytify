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
  env = common.environment cfg;
in
{
  options.programs.spytify = common.options;

  config = lib.mkIf cfg.enable {
    assertions = common.assertions cfg;

    home.packages = [ cfg.package ];

    xdg.configFile."spytify/config.toml" = lib.mkIf (cfg.settings != { }) {
      source = common.configFile cfg;
    };

    # For the app, from a shell or from a launcher that systemd starts.
    home.sessionVariables = env;
    systemd.user.sessionVariables = env;

    systemd.user.services.spytifyd = lib.mkIf cfg.service.enable {
      Unit = {
        Description = "Spytify recorder (spytifyd)";
        After = [
          "pipewire.service"
          "pipewire-pulse.service"
        ];
      };
      Service = {
        ExecStart = lib.getExe' cfg.package "spytifyd";
        Environment = lib.mapAttrsToList (k: v: "${k}=${v}") env;
        Restart = "on-failure";
        RestartSec = 5;
      };
      Install.WantedBy = [ "default.target" ];
    };
  };
}
