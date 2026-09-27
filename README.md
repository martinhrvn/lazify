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

Examples: `examples/git-lite.yaml` (git), `examples/ecs.yaml` (AWS ECS, read-only),
`examples/nix.yaml` (NixOS generations, closures, flake inputs; `lazify examples/nix.yaml`).

Choices (profile, region, flake, …) are remembered per app in `~/.local/state/lazify/<id>.yaml`;
`--no-remember` starts fresh.
Panels can offer **options** (`o`): a log window, a date, an author — see `options` in the design doc.
Inside tmux or zellij, interactive actions (a shell, a log follower) open in a floating pane;
`--no-float` keeps them in the terminal (tmux: `set -g focus-events on` also lets lazify pause refreshing).

Status: early development. See [docs/design.md](docs/design.md) and [TODO.md](TODO.md).
