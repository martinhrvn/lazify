# TODO

See `docs/design.md` §11 for milestone scope.

## M1 — static lists ✅
- [x] `tmpl`: parse `{{ref}}`, render with resolver, shell-quote vs raw, missing-field errors
- [x] `def`: YAML → structs, validation (ids, refs, cycles, reserved keys, jq compiles), file:line errors
- [x] `rows`: jq (`rows:`) mode, line mode, `split`, `key` extraction
- [x] `runner`: `sh -c`, env, context cancel, process-group kill, timeout
- [x] `ui`: single panel renders rows, j/k, q
- [x] `cmd/lazify`: `lazify <file|name>`, `lazify lint`, `lazify list`

## Layout ✅
- [x] `side: left|right` — main view in the middle when right panels exist
- [x] `size: fit|<n>|<n>fr` per panel
- [x] `layout.focus: expand|equal`, `left_width`, `right_width`
- [x] focus order / numbering follows the screen

## M2 — reactive panels ✅
- [x] engine: re-run dependents on selection change (topological), cancel in-flight runs
- [x] debounce selection changes (~150 ms); cache hits apply instantly
- [x] cache by rendered command; stale-while-refreshing; errors not cached
- [x] wait for all inputs to settle (diamonds run once); drill-in children don't run until opened
- [ ] later: bound the cache (LRU) — currently unbounded
## M3 — detail view ✅
- [x] tabs for the focused panel (`[`/`]`), active tab remembered per panel
- [x] `once` tabs: cached by command, debounced on cursor moves, `format: json`, errors shown
- [x] `stream` tabs (live tail): incremental output, 10k-line buffer, killed on change, `r` restarts
- [x] scrolling (`J`/`K`, `ctrl+d`/`ctrl+u`), follow mode for streams; reusable `ui/viewport.go`
- [ ] later: search in the main view, `g`/`G` top/bottom, wrap long lines toggle
## M4 — actions
## Enter on a row — popup or drill-down
Each panel can bind Enter to one of two behaviours. Esc always goes back one level.
- [ ] **drill-down** (lazygit-style): Enter replaces the panel's contents with its `children` panel
      in the same slot, e.g. branches → commits of that branch → files of that commit.
      - [ ] stack of levels per slot; the title shows a breadcrumb (`Branches › main › a1b2c3`)
      - [ ] Esc pops one level and puts the cursor back on the row that was entered
      - [ ] children run only when opened (the engine already skips them)
      - [ ] decide: can a panel be both a top-level panel and a drill-down level? (lazygit
            shows commits both ways)
- [ ] **popup**: Enter opens an overlay showing a command's output for the row
      (`popup: {cmd, format, title}`)
      - [ ] popup layouts to design: `maximized` (full screen), `centered` (a floating box,
            sized by % or to fit content), maybe `main` (takes over only the main view)
      - [ ] scrolling, and Esc to close
      - [ ] decide: once or stream (reuse the detail tab modes from M3?)
- [ ] definition syntax: e.g. `enter: {drill: files}` vs `enter: {popup: {...}}`, replacing
      `children:`; validation (only one kind per panel; the drill target must exist)

## M5 — context, columns, auto-refresh, filter
## M6 — example definitions (git-lite, cloudwatch, ecs)
