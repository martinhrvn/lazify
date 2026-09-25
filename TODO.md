# TODO

See `docs/design.md` §11 for milestone scope.

## M1 — static lists ✅
- [x] `tmpl`: parse `{{ref}}`, render with resolver, shell-quote vs raw, missing-field errors
- [x] `def`: YAML → structs, validation (ids, refs, cycles, reserved keys, jq compiles), file:line errors
- [x] `rows`: jq (`rows:`) mode, line mode, `split`, `key` extraction
- [x] `runner`: `sh -c`, env, context cancel, process-group kill, timeout
- [x] `ui`: single panel renders rows, j/k, q
- [x] `cmd/lazify`: `lazify <file|name>`, `lazify lint`, `lazify list`

## M2 — reactive panels
- [ ] engine: re-run dependents on selection change (topological), cancel in-flight runs
- [ ] debounce selection changes (~150 ms)
- [ ] cache by rendered command + env; stale-while-refreshing
## M3 — detail view
## M4 — actions
## M5 — drill-in, context, columns, auto-refresh, filter
## M6 — example definitions (git-lite, cloudwatch, ecs)
