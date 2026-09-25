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

Status: early development. See [docs/design.md](docs/design.md) and [TODO.md](TODO.md).
