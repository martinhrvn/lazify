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
## Content panels ✅ (replaces `detail:`)
- [x] any panel can be a content panel: `content: {<list panel>|default: {tabs: [...]}}`
- [x] shows content for the **active** (last focused list) panel; several content panels run independently
- [x] `side: center`; three columns `[left][center][right]`, no implicit main area
- [x] content panels are focusable (`j/k` scroll); `[`/`]`, `J/K`, `ctrl+d/u` act on the focused or first one
- [x] `detail:` gives a migration error
- [ ] later: shorthand for single-tab entries (`commits: {cmd: ...}`)
- [ ] later: tab names optional when there is one tab (title from the panel)

## M4 — actions ✅
- [x] global actions (top-level `actions:` list) + panel actions (`panels[].actions`), panel wins on a clash
- [x] background (toast ⟳/✓/✗), interactive (`tea.ExecProcess`), `confirm`, `prompt` → `{{input}}`
- [x] after success: caches dropped, `refresh` panels (default: own panel) and content re-run
- [x] hint bar (fits the width, `? more`) and `?` help overlay (reusable `overlay`)
- [ ] later: full action output in a popup (`o`), multi-select / bulk actions, a spinner for running actions
## Marked rows ✅
- [x] `mark: .path` (truthy row value) or `mark: {source, match}` (match value in the command's output lines)
- [x] `*` gutter + accent colour; cursor starts on the first marked row until the user moves
- [x] mark command runs with its panel (refresh, actions), cached with rows; failure = no marks
- [ ] later: manual marks / multi-select (with bulk actions)

## Config & CLI ✅
- [x] app catalog: every `*.yaml` in `~/.config/lazify`, one app per file or several under `apps:`, found by `id`
- [x] `lazify <id>`, `lazify <file> [id]`, `lazify list` (table, invalid/duplicates marked), `lazify lint` (whole folder)
- [x] a broken app or file never blocks the others; `file:line` errors inside `apps:`
- [ ] later: global settings in config.yaml (theme, default layout, key overrides)
- [ ] later: project-local definitions (e.g. `.lazify.yaml` in the current repo)

## Enter on a row — popup or drill-down ✅
- [x] `enter: <panel>` drills down in the same slot; `enter: {panel, popup: true|full|{width, height}}` opens a popup
- [x] targets are ordinary panels (list or content) that read the parent's row via `{{parent.x}}`
- [x] stacks per slot + a popup stack; esc pops the top level; breadcrumb titles; `enter`/`esc` hints
- [x] `children:` gives a migration error
- [ ] later: remember the cursor per entered row; action output in a popup (`o`)

## Panel tabs ✅
- [x] `tab_of: <panel>` puts list panels in one slot; the slot title is a tab bar
- [x] `[`/`]` switch the focused slot's panel tabs (focus + active follow), else content tabs
- [x] drill-down stacks per tab; the active tab is remembered per slot
- [ ] later: load hidden tabs lazily (all tabs run today)

## Select panels (context) ✅
- [x] `select: true` list panels: show their chosen row; number/Enter opens a picker; Enter commits, Esc cancels
- [x] `values: [..]` rows without a command (any list panel); `default:`, `--set id=value`, mark as default
- [x] `env` may reference panels; cache keyed by env + command (switching back is instant)
- [x] `context:` / `{{ctx.x}}` report migration errors; `c` is no longer reserved
- [ ] later: remember choices between runs; share selects across apps (all AWS apps)

## Filter ✅
- [x] `/` filters the focused list panel (or an open picker) as you type; Enter keeps, Esc clears
- [x] terms match the displayed row text; cursor moves over matches only; no match = no selection
- [x] re-applied after refreshes; title shows `/text`; `(no matches)`
- [x] selects: `select: popup` (dialog) or `select: inline` (in place), never both
- [ ] later: search in content panels; fuzzy matching

## M5 ✅ — auto-refresh (columns, context as select panels, filter: done earlier)
- [x] `refresh: <interval>` re-runs a panel while it's on screen and idle; quiet, cursor kept by key
- [x] minimum 1s; timers live in the UI (`refreshMsg`), the engine decides (`AutoRefresh`)
- [ ] later: pause refreshing while the terminal is unfocused; per-content-tab refresh
## Small additions ✅
- [x] `default:` (and `--set id=row`) picks the starting row of any list panel, not only selects
- [x] `enter: {focus: <panel>|next}` makes Enter move focus instead of opening something

## Mouse ✅
- [x] click focuses a panel / selects a row; clicking the selected row = Enter; click a select = picker
- [x] wheel moves list selection, scrolls content and help; clicks outside a popup ignored; `--no-mouse`
- [ ] later: click tab names in a tab bar to switch tabs

## Helpers ✅
- [x] `style:` value → decoration lookup (icons, animated spinner, colours; `text: false` = icon only) on columns, labels (`{value, map}`) and whole rows (`row_style`)
- [x] `format:` ago, duration, bytes, basename, bar; styles key on the value before formatting
- [x] cursor/mark/dim highlights span coloured cells; spinner ticks only while shown; `loading…` spins
- [ ] later: themes (remap semantic colours, ASCII icons), more formatters as needed

## M6 — example definitions (git-lite, cloudwatch, ecs)
