# lazify

TUI engine: one declarative YAML file → a lazygit-style app. Design: `docs/design.md`.

## Ways of working

Work in TDD style, with clear separation of concerns.

Every feature should start with a test, and the code should be written to pass that test.

Tests represent the source of truth, you should not just change the tests to make it pass.

The todos are in TODO.md, and should be updated as you complete them.

## Layout

- `internal/def` — YAML parsing, validation, reference extraction, dependency graph.
- `internal/tmpl` — `{{ref}}` templates; shell-quoted (commands) vs raw (display) rendering.
- `internal/rows` — command output → rows (embedded gojq, or plain lines/split).
- `internal/runner` — runs `sh -c` commands: cancellation, process-group kill, timeouts.
- `internal/engine` — pure state machine (no I/O); `internal/ui` talks only to it.
- `cmd/lazify` — CLI wiring.

Run tests with `go test ./...`. Never use `npx tsc`.
