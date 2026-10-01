self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.lazify;
  yaml = pkgs.formats.yaml { };

  # One file per app: <name>.yaml, so the app's id defaults to its attribute name.
  appFile =
    name: app:
    if builtins.isAttrs app then
      yaml.generate "${name}.yaml" app
    else if lib.types.path.check app then
      app
    else
      pkgs.writeText "${name}.yaml" app;

  files = pkgs.linkFarm "lazify-apps" (
    lib.mapAttrsToList (name: app: {
      name = "${name}.yaml";
      path = appFile name app;
    }) cfg.apps
  );

  # `lazify lint` with no arguments checks the whole config dir: every app,
  # and ids defined more than once. It only parses; nothing is run.
  validated = pkgs.runCommand "lazify-config" { nativeBuildInputs = [ cfg.package ]; } ''
    mkdir -p xdg empty
    ln -s ${files} xdg/lazify
    cd empty   # no .lazify.yaml here or above
    XDG_CONFIG_HOME=$NIX_BUILD_TOP/xdg lazify lint
    ln -s ${files} $out
  '';
in
{
  options.programs.lazify = {
    enable = lib.mkEnableOption "lazify, a declarative lazygit-style TUI";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.lazify;
      defaultText = lib.literalExpression "lazify.packages.\${system}.lazify";
      description = "The lazify package to install.";
    };

    apps = lib.mkOption {
      type = lib.types.attrsOf (
        lib.types.oneOf [
          lib.types.path
          lib.types.lines
          (lib.types.attrsOf yaml.type)
        ]
      );
      default = { };
      example = lib.literalExpression ''
        {
          git = ./git-lite.yaml;
          tail = '''
            panels:
              - id: files
                source: find . -name '*.log'
          ''';
          ecs.panels = [ { id = "clusters"; source = "aws ecs list-clusters"; } ];
        }
      '';
      description = ''
        Apps written to {file}`$XDG_CONFIG_HOME/lazify/<name>.yaml`. Each is a
        path to a YAML file, YAML text, or an attribute set rendered as YAML.
        The app's id defaults to the attribute name.
      '';
    };

    validate = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Check the apps with `lazify lint` at build time; a broken app fails the build.";
    };

    configDir = lib.mkOption {
      type = lib.types.package;
      readOnly = true;
      internal = true;
      default = if cfg.validate then validated else files;
      description = "The generated app files (linted when `validate` is on).";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
    xdg.configFile = lib.mapAttrs' (
      name: _: lib.nameValuePair "lazify/${name}.yaml" { source = "${cfg.configDir}/${name}.yaml"; }
    ) cfg.apps;
  };
}
