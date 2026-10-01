# lazify

Turn a single YAML file into a lazygit-style TUI for anything you can query from a shell —
`lazyecs`, `lazycloudwatch`, a git browser, …

## Quick start

```sh
go build -o bin/lazify ./cmd/lazify
mkdir -p ~/.config/lazify
cp examples/git-lite.yaml ~/.config/lazify/git.yaml    # one app per file: id = file name
cp examples/config.yaml ~/.config/lazify/config.yaml   # or several under `apps:`

lazify list         # what's defined
lazify git          # run by id
lazify lint         # check every app
```

### Nix

`nix run github:martinhrvn/lazify -- list`, or declare apps with the Home Manager module:

```nix
# flake inputs: lazify.url = "github:martinhrvn/lazify";
imports = [ inputs.lazify.homeManagerModules.default ];
programs.lazify = {
  enable = true;
  apps = {
    git = ./git-lite.yaml;                         # a YAML file
    tail = ''
      panels:
        - id: files
          source: find . -name '*.log'
    '';                                            # YAML text
    disk.panels = [ { id = "mounts"; source = "df -h"; } ];  # Nix, rendered as YAML
  };
};
```

Each app becomes `~/.config/lazify/<name>.yaml` (id = the name) and is checked with
`lazify lint` at build time, so a broken app fails `home-manager switch` (`validate = false` to skip).

Examples: `examples/git-lite.yaml` (git), `examples/ecs.yaml` (AWS ECS, read-only),
`examples/nix.yaml` (NixOS generations, closures, flake inputs; `lazify examples/nix.yaml`).
`examples/dev/`: a project dashboard over process-compose — copy its `.lazify.yaml` into a
project with a `process-compose.yaml` and run plain `lazify` there.

Choices (profile, region, flake, …) are remembered per app in `~/.local/state/lazify/<id>.yaml`;
`--no-remember` starts fresh.
Panels can offer **options** (`o`): a log window, a date, an author — see `options` in the design doc.
Inside tmux or zellij, interactive actions (a shell, a log follower) open in a floating pane;
`--no-float` keeps them in the terminal (tmux: `set -g focus-events on` also lets lazify pause refreshing).

Status: early development. See [docs/design.md](docs/design.md) and [TODO.md](TODO.md).
