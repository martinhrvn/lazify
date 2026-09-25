{ pkgs, ... }:

{
  packages = with pkgs; [
    go
    gopls
    delve
    golangci-lint
    gotools
  ];

  languages.go.enable = true;

  scripts.build.exec = "go build -o $DEVENV_ROOT/bin/lazify $DEVENV_ROOT/cmd/lazify";
  scripts.test.exec = "go test ./...";
  scripts.lint.exec = "golangci-lint run";
  scripts.dev.exec = "go run ./cmd/lazify \"$@\"";
}
