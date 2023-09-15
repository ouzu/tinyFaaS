{ config, lib, pkgs, ... }: with lib; {
  options.services.mistify = {
    enable = mkEnableOption "Enable the mistify service";
    configFile = mkOption {
      default = "/var/lib/mistify/config.toml";
      type = types.path;
      description = "Path to the tinyFaaS and Mistify configuration file.";
    };
  };

  config = mkIf cfg.enable {
    virtualisation.docker.enable = true;

    systemd.services.mistify = {
      description = "Mistify Service";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      serviceConfig = {
        ExecStart = "${pkgs.tinyFaaS}/bin/mistify ${cfg.configFile}";
        WorkingDirectory = "/var/lib/mistify";
        User = "mistify";
        Group = "mistify";
        Restart = "always";
      };
    };

    systemd.services.tinyFaaS = {
      description = "tinyFaaS Service";
      wantedBy = [ "multi-user.target" ];
      after = [ "docker.service" "mistify.service" "network.target" ];
      requires = [ "docker.service" "mistify.service" ];
      serviceConfig = {
        ExecStart = "${pkgs.tinyFaaS}/bin/manager ${cfg.configFile}";
        WorkingDirectory = "/var/lib/mistify";
        User = "mistify";
        Group = "mistify";
        Restart = "always";
      };
    };

    users.users.mistify = {
      description = "Mistify Service User";
      isSystemUser = true;
      group = "mistify";
      home = "/var/lib/mistify";
      createHome = true;
    };

    users.groups.mistify = { };
  };
};
