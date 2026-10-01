# Evaluates programs.lazify against a minimal stand-in for Home Manager (just
# the options the module sets), so the flake needs no home-manager input.
{
  pkgs,
  lib,
  module,
  package,
}:
let
  hmStub =
    { lib, ... }:
    {
      options.home.packages = lib.mkOption {
        type = lib.types.listOf lib.types.package;
        default = [ ];
      };
      options.xdg.configFile = lib.mkOption {
        type = lib.types.attrsOf (
          lib.types.submodule { options.source = lib.mkOption { type = lib.types.path; }; }
        );
        default = { };
      };
    };

  eval =
    cfg:
    (lib.evalModules {
      modules = [
        hmStub
        module
        {
          _module.args.pkgs = pkgs;
          programs.lazify = {
            enable = true;
          }
          // cfg;
        }
      ];
    }).config;

  panels = [
    {
      id = "files";
      source = "ls";
    }
  ];

  ok = eval {
    apps = {
      fromAttrs = { inherit panels; };
      fromString = ''
        panels:
          - id: files
            source: ls
      '';
      fromPath = ../../examples/tail.yaml;
    };
  };

  broken = {
    bad = {
      panels = panels ++ [
        {
          id = "main";
          content.nope.tabs = [
            {
              name = "x";
              cmd = "cat {{.line}}";
            }
          ];
        }
      ];
    };
  };
  brokenValidated = eval { apps = broken; };
  brokenUnvalidated = eval {
    apps = broken;
    validate = false;
  };

  duplicates = eval {
    apps = {
      one = {
        id = "same";
        inherit panels;
      };
      two = {
        id = "same";
        inherit panels;
      };
    };
  };

  src = name: c: c.xdg.configFile."lazify/${name}.yaml".source;

  assertEq =
    what: got: want:
    lib.assertMsg (got == want) "${what}: got ${builtins.toJSON got}, want ${builtins.toJSON want}";

  pure =
    assert assertEq "config files" (builtins.attrNames ok.xdg.configFile) [
      "lazify/fromAttrs.yaml"
      "lazify/fromPath.yaml"
      "lazify/fromString.yaml"
    ];
    assert assertEq "home.packages" ok.home.packages [ package ];
    assert assertEq "disabled writes nothing" ((lib.evalModules {
      modules = [
        hmStub
        module
        { _module.args.pkgs = pkgs; }
      ];
    }).config.xdg.configFile
    ) { };
    true;
in
assert pure;
pkgs.runCommand "lazify-hm-module-test"
  {
    nativeBuildInputs = [ pkgs.yq-go ];
    brokenFails = pkgs.testers.testBuildFailure brokenValidated.programs.lazify.configDir;
    duplicatesFail = pkgs.testers.testBuildFailure duplicates.programs.lazify.configDir;
  }
  ''
    set -euo pipefail

    # attrset → YAML that round-trips
    got=$(yq -o=json -I=0 . ${src "fromAttrs" ok})
    want='${builtins.toJSON { inherit panels; }}'
    [ "$got" = "$want" ] || { echo "fromAttrs: got $got, want $want"; exit 1; }

    # string → the text as given; path → the file's contents
    grep -q 'source: ls' ${src "fromString" ok}
    cmp ${src "fromPath" ok} ${../../examples/tail.yaml}

    # lint failures fail the build, with lint's message
    grep -q 'unknown panel "nope"' $brokenFails/testBuildFailure.log
    grep -q 'defined more than once' $duplicatesFail/testBuildFailure.log

    # validate = false writes the broken app as is
    grep -q nope ${src "bad" brokenUnvalidated}

    touch $out
  ''
