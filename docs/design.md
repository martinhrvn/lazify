# lazify — design document (draft v0.1)

## 1. Summary

**lazify** is a TUI engine that turns a single declarative YAML file into a
lazygit-style app for anything you can query from a shell: `lazyecs`,
`lazycloudwatch`, `lazyk8s-lite`, a git browser, etc.

A definition declares **panels**: list panels (fed by shell commands) and content panels (tabs of command output)
(what to show for the focused row) and **actions** (keys that run commands on the
focused row). Panels reference each other's *selection* in their commands; the
engine derives the dependency graph from those references and re-runs downstream
panels when a selection changes — exactly how lazygit's commits panel follows the
selected branch.

## 2. Goals / non-goals

**Goals**
- One YAML file = one app. No code required for the common case.
- lazygit feel: stacked list panels on the left, content (diffs, logs) in the middle,
  everything reacts to the cursor, fast and non-blocking.
- Any shell command is a data source; JSON (queried with jq syntax) or plain lines.
- Safe by default: substituted values are shell-quoted.

**Non-goals**
- **Not a programming language.** No conditionals, loops, variables-with-logic,
  functions or pipes in templates. Complex logic goes in the `source` command
  (a shell pipeline or a script) — that is the escape hatch.
- No layout DSL. Layout is fixed and derived from declaration order.
- Not part of paleta (`plt`). Separate binary/repo; paleta can launch it as a tool later.

## 3. Core concepts

| Concept | Meaning |
|---|---|
| **Definition** | A YAML file describing one app. |
| **Panel** | A list. Has a `source` command, a way to turn output into **rows**, and how to display a row. |
| **Row** | One item: a JSON object (from `rows:` jq expr) or `{line: "..."}` for plain text output. |
| **Selection** | The row under the cursor in a panel. Always exactly one (or none if empty). |
| **Reference** | `{{panel.field}}` in any command — "the value of `field` in `panel`'s current selection". |
| **Drives** | Panel B references panel A ⇒ B is visible alongside A and re-runs when A's selection changes. |
| **Enter target** | `enter: B` on A ⇒ Enter on A's selected row opens panel B (list or content) — in A's slot (**drill down**) or over everything (**popup**). B reads the row through `{{A.x}}`. Levels nest; Esc closes the top one. |
| **List panel** | A panel with a `source`: rows, a cursor, a selection. |
| **Content panel** | A panel with `content` instead of `source`: shows **tabs** of command output (diffs, logs, live tails) for the **active** panel. Scrolls instead of selecting. |
| **Active panel** | The last focused list panel. Content panels show `content[active]`, else `content.default`. Focusing a content panel does not change it. |
| **Action** | A key that runs a command. **Global** actions (top-level `actions:`) work everywhere; **panel** actions (`panels[].actions`) work while that panel is focused and win over a global with the same key. |
| **Context** | App-wide switchable values (e.g. AWS profile/region) usable as `{{ctx.name}}`. |

## 4. Definition format

### 4.1 Minimal example — git browser (lazygit-lite)

```yaml
name: git-lite
layout:
  focus: equal                                         # like lazygit: panels keep equal heights
panels:
  - id: status
    title: Status
    source: git status -sb | head -1
    size: fit                                          # only as tall as its content
  - id: branches
    title: Branches
    source: git branch --format='%(refname:short)'     # plain lines → rows {line}
    mark: {source: git branch --show-current}           # * on the checked-out branch; cursor starts there
    actions:                                           # while Branches is focused
      - {key: space, desc: Checkout, cmd: "git checkout {{.line}}", refresh: [status, branches, commits]}
      - {key: n, desc: New branch, prompt: Name, cmd: "git checkout -b {{input}}", refresh: [status, branches]}
      - {key: D, desc: Delete, cmd: "git branch -D {{.line}}", confirm: true}
  - id: commits
    title: Commits
    source: git log --format='%h%x09%s' {{branches.line}}
    split: "\t"                                        # line → fields[0], fields[1]
    label: "{{.fields.0}} {{.fields.1}}"
    key: .fields.0
    enter: files                                       # Enter drills in
  - id: files
    title: Files
    source: git show --name-only --format= {{commits.fields.0}}
    enter: {panel: file, popup: true}                  # Enter: the whole file at that commit
  - id: file
    title: File
    content:
      default:
        tabs:
          - name: File
            cmd: git show {{commits.fields.0}}:{{files.line}}
  - id: tags
    title: Tags
    source: git tag --sort=-creatordate
    side: right                                        # right of the main view
    enter: {panel: tag, popup: {width: 70, height: 80}}
  - id: tag
    title: Tag
    content:
      default:
        tabs:
          - name: Show
            cmd: git show --stat --color=always {{tags.line}}
  - id: main                                         # a content panel: shows content for the
    title: Main                                      # last focused list panel (center by default)
    content:
      branches:
        tabs:
          - name: Log
            cmd: git log --oneline --graph --color=always {{.line}}
      commits:
        tabs:
          - name: Diff
            cmd: git show --color=always {{.fields.0}}
      files:
        tabs:
          - name: Diff
            cmd: git show --color=always {{commits.fields.0}} -- {{.line}}
      default:                                       # any other panel (status, tags)
        tabs:
          - name: Status
            cmd: git -c color.status=always status --short --branch

actions:                                             # global: work whatever is focused
  - {key: R, desc: Fetch, cmd: git fetch --all --prune, refresh: [branches, commits, tags]}
  - {key: e, desc: Shell, cmd: "${SHELL:-sh}", mode: interactive}
```

### 4.2 AWS example — lazyecs

```yaml
name: lazyecs
context:
  profile: { source: aws configure list-profiles, default: default }
  region:  { values: [eu-west-1, eu-central-1, us-east-1], default: eu-west-1 }
env:
  AWS_PROFILE: "{{ctx.profile}}"
  AWS_REGION:  "{{ctx.region}}"

panels:
  - id: clusters
    title: Clusters
    source: >
      aws ecs describe-clusters --output json
      --clusters $(aws ecs list-clusters --query clusterArns --output text)
    rows: .clusters[]
    key: .clusterArn
    label: "{{.clusterName}}"

  - id: services
    title: Services
    source: >
      aws ecs describe-services --cluster {{clusters.clusterArn}} --output json
      --services $(aws ecs list-services --cluster {{clusters.clusterArn}} --query serviceArns --output text)
    rows: .services[]
    key: .serviceArn
    columns:
      - { title: Service, value: "{{.serviceName}}" }
      - { title: Run/Des, value: "{{.runningCount}}/{{.desiredCount}}" }
      - { title: Status,  value: "{{.status}}" }
    refresh: 10s
    actions:
      - key: D
        desc: Force new deployment
        cmd: aws ecs update-service --cluster {{clusters.clusterArn}} --service {{.serviceArn}} --force-new-deployment
        confirm: true
        refresh: [services, tasks]
      - key: s
        desc: Scale
        prompt: Desired count
        cmd: aws ecs update-service --cluster {{clusters.clusterArn}} --service {{.serviceArn}} --desired-count {{input}}
        refresh: [services]

  - id: tasks
    title: Tasks
    source: >
      aws ecs describe-tasks --cluster {{clusters.clusterArn}} --output json
      --tasks $(aws ecs list-tasks --cluster {{clusters.clusterArn}}
               --service-name {{services.serviceName}} --query taskArns --output text)
    rows: .tasks[]
    key: .taskArn
    columns:
      - { title: Task,   value: "{{.taskArn}}" }        # TODO: basename formatting?
      - { title: Status, value: "{{.lastStatus}}" }
    actions:
      - key: e
        desc: Exec shell
        mode: interactive          # suspends the TUI, gives the terminal to the command
        cmd: aws ecs execute-command --cluster {{clusters.clusterArn}} --task {{.taskArn}} --interactive --command /bin/sh

  - id: main
    content:
      services:
        tabs:
          - { name: Events, cmd: "aws ecs describe-services --cluster {{clusters.clusterArn}} --services {{.serviceArn}} --query 'services[0].events[:30]' --output table" }
          - { name: JSON,   cmd: "echo {{.}}", format: json }
      tasks:
        tabs:
          - name: Logs
            mode: stream
            cmd: aws logs tail /ecs/{{services.serviceName}} --follow --format short
```

### 4.3 Field reference

**Panel**
| key | required | meaning |
|---|---|---|
| `id` | yes | Identifier used in references. |
| `title` | no | Panel title (defaults to id). |
| `source` | yes | Shell command (`sh -c`). May reference other panels / ctx. |
| `rows` | no | jq expression producing one JSON value per row. Absent ⇒ one row per non-empty output line: `{line}`. |
| `split` | no | For line output: separator; adds `fields: [...]`. |
| `label` | no | Row display template. Default: `.line` or the whole value. |
| `columns` | no | List of `{title, value}` rendered as aligned columns (replaces `label`). |
| `key` | no | Row path (template-ref syntax, e.g. `.fields.0`, not jq) giving a stable row identity; used to keep the cursor across refreshes. Default: label. |
| `enter` | no | What Enter opens for the selected row: `enter: <panel id>` drills down (the target replaces this panel in its slot), `enter: {panel: <id>, popup: true \| full \| {width, height}}` opens it in a popup (default 80×80 %). A target has one parent, is hidden until entered, takes no `side`/`size`, and may be a content panel. (Replaces the old `children:`.) |
| `refresh` | no | Auto re-run interval (e.g. `10s`). |
| `mark` | no | Highlights "current" rows with `*` (and starts the cursor on the first one). Either a row path — `mark: .current` marks rows where it is truthy (null, false, 0 and blank strings are not) — or a command — `mark: {source: git branch --show-current, match: "{{.line}}"}` marks rows whose `match` (default: `key`, else label) is one of its output lines. The command runs with the panel (refresh, actions) and is cached like rows; failures just show no marks. |
| `size` | no | Height in its column: `fit` (content height, capped at a fair share), `<n>` (fixed lines) or `<n>fr` (flex weight). Default `1fr`. Not allowed on drill-in children. |
| `side` | no | `left`, `center` or `right`; list panels default to `left`, content panels to `center`. Not allowed on drill-in children. |
| `content` | — | Makes this a **content panel** (instead of `source`): map of list-panel id or `default` → `{tabs: [...]}`. List-only fields (`rows`, `split`, `label`, `columns`, `key`, `children`, `refresh`) are not allowed. |

**Content tab**: `name`, `cmd`, `mode: once|stream` (default `once`), `format: text|json` (json = pretty-print/colourise).

**Action** (top-level `actions:` list = global; `panels[].actions` = per panel): `key`, `desc`,
`cmd`, `prompt` (asks for text → `{{input}}`), `confirm: bool` (shows the rendered command,
`[y/N]`), `mode: background|interactive` (default background; interactive hands the terminal
to the command), `refresh: [panel ids]` (default: the owning list panel; none for globals).
`{{.x}}` is the owning list panel's row, or the active panel's for globals and content-panel
actions. After a successful action all caches are dropped, `refresh` panels re-run (after their
inputs) and content panels re-run. Keys use Bubble Tea names (`D`, `ctrl+x`, `f5`) or `space`.

**Context**: map of name → `{source | values, default}`. Switched with a picker (`c`).
Also overridable from CLI: `lazify ecs --set region=us-east-1`.

**env**: map applied to every command; values may use `{{ctx.*}}`.

## 5. Templates (deliberately tiny)

- Syntax: `{{ ref }}` where `ref` is one of
  - `.path` — field of *this* row (the active panel for a content tab; the owning panel for an action/column);
    `.` alone = whole row as JSON.
  - `panelId.path` — field of that panel's current selection.
  - `ctx.name` — context value.
  - `input` — value from an action's `prompt`.
- `path` = dotted fields and numeric indexes (`fields.0`, `containers.0.name`). No functions, pipes, conditionals.
- `\{{` is a literal `{{` — for commands that use Go templates themselves, e.g. `docker ps --format '\{{.Names}}'`.
- **Every substitution into a command is shell-quoted.** In `label`/`columns` (display) values are inserted raw.
- A reference to a missing field renders empty in display, and is an **error** in commands (panel shows "missing field X" instead of running a wrong command).
- Validation at load: every reference must name an existing panel/ctx; parse errors point at file:line.

## 6. Reactive model

1. At load, collect references from each panel's `source` → edges `A → B` ("B depends on A").
   Enter targets also depend on the panel they are entered from. **Cycles are a load error.**
2. A panel is **runnable** when every panel it depends on is settled (not loading or pending)
   and has a selection. While an input is loading the panel **waits** (old rows shown stale);
   if an input has no rows it shows an empty "no selection in X" state and does not run.
   Enter targets run only while open (drilled into or in a popup).
3. On selection change in A, each dependent B (topological order) re-renders its command:
   - same command as its current rows → nothing to do (e.g. the referenced field didn't change);
   - cached → rows swap in **immediately**, no debounce;
   - otherwise B's in-flight run is cancelled (process group killed) and B goes **pending**;
     after the debounce (~150 ms, only the latest move counts) pending panels run.
   When B finishes, its selection (cursor restored by `key`, else first row) propagates further.
   Diamonds run once: a panel waits until all of its inputs have settled.
4. **Cache** by *rendered command string* (env is fixed per session; context changes clear it). Errors are not cached. Revisiting a selection shows cached rows
   instantly; `r` or the `refresh` interval re-runs. While re-running, old rows are shown
   **dimmed (stale)** rather than blanked.
5. **Content panels** follow the same rules, keyed on the active panel's selection (only when
   the tab uses `{{.x}}`) plus any panels the tab's `cmd` references; each content panel runs
   independently and only its visible tab runs. only the visible tab runs. Cursor moves use the cache or
   wait for the debounce; focus and tab changes run at once. `once` output is cached by command
   (`format: json` pretty-prints it). `stream` tabs (live tail) run without a timeout, merge
   stdout+stderr, keep the last 10 000 lines, are never cached, and are killed (process group)
   when the selection, tab or focus changes; `r` restarts them. With no entry for the active panel and
   no `default`, a content panel shows "nothing for X".
6. Context change invalidates the whole cache and re-runs roots.
7. Commands have a timeout (default 30 s, configurable); streams have none.

## 7. Layout & keys

- Columns: `[left][center][right]`, each a stack of panels in declaration order, sized with
  `size:`. List panels default to the left, content panels to the center. Empty columns vanish;
  without center panels the left column takes the center's width. There is no implicit main area.
  A drilled-in panel occupies its parent's slot with a breadcrumb title (`Commits › a1b2c3 › Files`);
  a popup is drawn centered over the UI and keeps focus (`tab`/`1-9` do nothing) until `esc`.
  Esc closes the top popup, else the focused slot's deepest level, else dismisses an action error.
- A content panel = tab bar (its title) + scrollable viewport (ANSI passthrough). Streams
  follow the tail: scrolling up pauses following, scrolling back to the bottom resumes it.
  Tabs expand to 4 spaces and `\r` progress lines keep only their final state.
- Panel numbers, `tab` order and `1..9` follow the screen: left, center, right, top→bottom.
  Content panels are focusable: `j/k` then scroll them. `[`/`]`, `J/K` and `ctrl-d/u` act on
  the focused content panel, or the first one when a list panel is focused.
- Bottom line: key hints for the focused panel's actions + status/errors.

```yaml
layout:
  focus: expand     # expand (default) | equal
  left_width: 40    # % of width; default 40, or 30 when some panel is on the right
  right_width: 25   # %; default 25
```

Height within each column: `<n>` panels get n lines; `fit` panels get their content (capped at
an equal share); flex (`<n>fr`) panels share the rest by weight. With `focus: expand` the
focused flex panel takes the spare height and other flex panels shrink to their content
(accordion); with `focus: equal` flex panels keep their share regardless of focus (lazygit).
The lazygit look is `layout: {focus: equal}` plus `size: fit` on a status panel.

| key | action |
|---|---|
| `j/k`, arrows | move cursor |
| `tab` / `shift-tab`, `1..9` | focus panel |
| `enter` / `esc` | open the Enter target (drill down or popup) / go back one level or close the popup |
| `[` / `]` | previous / next content tab |
| `ctrl-d/u`, `J/K` | scroll content panel |
| `/` | filter rows in focused panel |
| `r` | refresh focused panel |
| `c` | context picker |
| `?` | help overlay: focused panel's actions, globals, navigation |
| `esc` | dismiss an action error (and close prompts/help) |
| `q` | quit |

Key lookup: open prompt/confirm/help first, then the built-in keys above (reserved: binding one
is a load error), then the focused panel's actions, then global actions. The bottom line shows
the last action's toast (`⟳` running, `✓` done, `✗` failed — stays until `esc`) and as many
hints as fit (panel actions, globals, navigation), with `? more` pinned at the right.

## 8. Errors

- Non-zero exit: panel/tab shows stderr in an error style; keeps previous rows as stale if any.
- jq error / invalid JSON: shown the same way, with the first lines of output.
- Background action result: toast in the status line; failures show the first error line and
  stay until `esc`. (Full output in a popup: later.)

## 9. CLI and the app catalog

Apps live in `$XDG_CONFIG_HOME/lazify` (default `~/.config/lazify`): every `*.yaml` / `*.yml`
there is scanned. A file holds one app (`id` defaults to the file name) or several under
`apps:` (each needs an `id`); mixing the two in one file is an error. `name` is for display
and defaults to the id. A broken app never blocks the others; duplicate ids are reported
when used and by `lint`.

```yaml
# ~/.config/lazify/config.yaml
apps:
  - id: ecs
    panels: [...]
  - id: git
    name: git-lite
    panels: [...]
```

- `lazify <id>` → run the app with that id; `lazify <file.yaml> [id]` → run from a file.
- `lazify list` → id, name and file of every app, with invalid/duplicate ones marked.
- `lazify lint [id|file]...` → validate (refs, cycles, reserved keys, jq compiles); without
  arguments, everything in the config folder.
- `--set ctx=value`.
- Users get `lazyecs` via a shell alias (`alias lazyecs='lazify ecs'`); no special casing.

## 10. Architecture (Go, Bubble Tea — same stack as paleta)

Packages, each testable on its own (TDD, same separation as paleta where the TUI does no I/O):

- `def` — YAML parsing into typed structs, validation, reference extraction, graph + topo sort.
- `tmpl` — parse `{{ref}}`, render with a resolver; shell-quoting vs raw modes.
- `rows` — output → rows (gojq `github.com/itchyny/gojq` embedded, so no runtime jq dep; line/split mode).
- `runner` — executes commands with context cancellation, process-group kill, timeouts, streaming
  output channel. Interface so tests use a fake.
- `engine` — **pure state machine**: selections, cache, stale flags, debounce, which commands to
  (re)run given an event. Emits "run X" intents, consumes "X finished". Unit-tested without a TUI or shell.
- `ui` — Bubble Tea model: layout, key handling, rendering; talks only to `engine`.
- `cmd/lazify` — CLI wiring.

## 11. Milestones

1. **M1 — static lists**: `def` + `tmpl` + `rows` + `runner`; single panel renders; `lint`.
2. **M2 — reactive panels**: driven panels, debounce/cancel, cache, stale, cursor-by-key.
3. **M3 — content view** (now content panels): tabs, `once` + `stream` modes, scrolling.
4. **M4 — actions**: background/interactive, prompt, confirm, refresh-after.
5. **M5 — drill-in, context, columns, auto-refresh, filter.**
6. Ship example definitions: git-lite, cloudwatch, ecs.

## 12. Open questions / to iterate

- **Panel tabs** (lazygit's Local Branches / Remotes / Tags in one box): multiple panels
  sharing one slot, switched with `[`/`]` when the panel is focused? Conflicts with content-tab keys.
- ~~Current row indicator~~: done — `mark:` (row path or command output as a set).
- **Colours/status**: declarative value→colour map, e.g.
  `color: { value: "{{.lastStatus}}", map: { RUNNING: green, STOPPED: red } }` — no expressions.
- Display helpers (basename of an ARN, relative time) without becoming a language — a fixed
  small set of formatters, or push it into `rows:` jq (which already can)?
- Multi-select + bulk actions?
- Definition composition / shared context across files (all AWS apps share profile/region).
- ~~Name~~: decided — **lazify**.
- paleta integration: expose `~/.config/lazify/*.yaml` as plt tools.
