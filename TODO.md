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
## M3 — detail view
## M4 — actions
## M5 — drill-in, context, columns, auto-refresh, filter
## M6 — example definitions (git-lite, cloudwatch, ecs)
