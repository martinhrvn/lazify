# lazify — design document (draft v0.1)

## 1. Summary

**lazify** is a TUI engine that turns a single declarative YAML file into a
lazygit-style app for anything you can query from a shell: `lazyecs`,
`lazycloudwatch`, `lazyk8s-lite`, a git browser, etc.

A definition declares **panels** (lists fed by shell commands), **detail tabs**
(what to show for the focused row) and **actions** (keys that run commands on the
focused row). Panels reference each other's *selection* in their commands; the
engine derives the dependency graph from those references and re-runs downstream
panels when a selection changes — exactly how lazygit's commits panel follows the
selected branch.

## 2. Goals / non-goals

**Goals**
- One YAML file = one app. No code required for the common case.
- lazygit feel: stacked list panels on the left, detail view on the right,
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
| **Drill-in** | Panel B is a `children` of A ⇒ Enter on A replaces A with B in the same slot; Esc returns. |
| **Detail / main view** | Right-hand area showing the **tabs** for the focused panel's selected row. |
| **Action** | A key bound on a panel that runs a command against the selected row. |
| **Context** | App-wide switchable values (e.g. AWS profile/region) usable as `{{ctx.name}}`. |

## 4. Definition format

### 4.1 Minimal example — git browser (lazygit-lite)

```yaml
name: lazygit-lite
panels:
  - id: branches
    title: Branches
    source: git branch --format='%(refname:short)'     # plain lines → rows {line}
  - id: commits
    title: Commits
    source: git log --format='%h%x09%s' {{branches.line}}
    split: "\t"                                        # line → fields[0], fields[1]
    label: "{{.fields.0}} {{.fields.1}}"
    key: .fields.0
    children: files                                    # Enter drills in
  - id: files
    title: Files
    source: git show --name-only --format= {{commits.fields.0}}

detail:
  commits:
    tabs:
      - name: Diff
        cmd: git show --color=always {{.fields.0}}
  files:
    tabs:
      - name: Diff
        cmd: git show --color=always {{commits.fields.0}} -- {{.line}}

actions:
  branches:
    - key: space
      desc: Checkout
      cmd: git checkout {{.line}}
      refresh: [branches, commits]
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

detail:
  services:
    tabs:
      - { name: Events, cmd: "aws ecs describe-services --cluster {{clusters.clusterArn}} --services {{.serviceArn}} --query 'services[0].events[:30]' --output table" }
      - { name: JSON,   cmd: "echo {{.}}", format: json }
  tasks:
    tabs:
      - name: Logs
        mode: stream
        cmd: aws logs tail /ecs/{{services.serviceName}} --follow --format short

actions:
  services:
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
  tasks:
    - key: e
      desc: Exec shell
      mode: interactive          # suspends the TUI, gives the terminal to the command
      cmd: aws ecs execute-command --cluster {{clusters.clusterArn}} --task {{.taskArn}} --interactive --command /bin/sh
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
| `children` | no | Panel id to drill into on Enter. That panel is then not shown at top level. |
| `refresh` | no | Auto re-run interval (e.g. `10s`). |

**Detail tab**: `name`, `cmd`, `mode: once|stream` (default `once`), `format: text|json` (json = pretty-print/colourise).

**Action**: `key`, `desc`, `cmd`, `prompt` (text → `{{input}}`), `confirm: bool`,
`mode: background|interactive` (default background), `refresh: [panel ids]`.

**Context**: map of name → `{source | values, default}`. Switched with a picker (`c`).
Also overridable from CLI: `lazify ecs --set region=us-east-1`.

**env**: map applied to every command; values may use `{{ctx.*}}`.

## 5. Templates (deliberately tiny)

- Syntax: `{{ ref }}` where `ref` is one of
  - `.path` — field of *this* row (the panel owning the detail tab/action/column);
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
   Drill-in children also depend on their parent. **Cycles are a load error.**
2. A panel is **runnable** when every panel it depends on has a selection. Otherwise it shows
   an empty "no selection in X" state and does not run.
3. On selection change in A: debounce (~150 ms), then for each dependent B in topological order:
   cancel B's in-flight run (kill process group), render its command, run.
   B's new selection (cursor restored by `key`, else first row) propagates further.
4. **Cache** by *rendered command string + env*. Revisiting a selection shows cached rows
   instantly; `r` or the `refresh` interval re-runs. While re-running, old rows are shown
   **dimmed (stale)** rather than blanked.
5. Detail tabs follow the same rules keyed on the focused panel's selection; only the
   visible tab runs. `stream` tabs are killed when the selection or tab changes.
6. Context change invalidates the whole cache and re-runs roots.
7. Commands have a timeout (default 30 s, configurable); streams have none.

## 7. Layout & keys

- Left column: top-level panels stacked in declaration order. Focused panel gets more
  height (lazygit style). A drilled-in panel occupies its parent's slot with a breadcrumb
  title (`Commits › a1b2c3 › Files`).
- Right: main view = tab bar + scrollable viewport (ANSI passthrough) for the focused row.
- Bottom line: key hints for the focused panel's actions + status/errors.

| key | action |
|---|---|
| `j/k`, arrows | move cursor |
| `tab` / `shift-tab`, `1..9` | focus panel |
| `enter` / `esc` | drill in / back |
| `[` / `]` | previous / next detail tab |
| `ctrl-d/u`, `J/K` | scroll main view |
| `/` | filter rows in focused panel |
| `r` | refresh focused panel |
| `c` | context picker |
| `?` | help: all actions for focused panel |
| `q` | quit |

Action keys are per panel; a definition binding a reserved key is a load error.

## 8. Errors

- Non-zero exit: panel/tab shows stderr in an error style; keeps previous rows as stale if any.
- jq error / invalid JSON: shown the same way, with the first lines of output.
- Background action result: toast in the status line; failures stay until dismissed; full
  output viewable (`o`?).

## 9. CLI

- `lazify <file.yaml>` or `lazify <name>` → looks up `~/.config/lazify/<name>.yaml`.
- `lazify lint <file>` → validate (refs, cycles, reserved keys, jq compiles).
- `lazify list` → available definitions.
- `--set ctx=value`.
- Users get `lazyecs` via a shell alias; no special casing.

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
3. **M3 — detail view**: tabs, `once` + `stream` modes, scrolling.
4. **M4 — actions**: background/interactive, prompt, confirm, refresh-after.
5. **M5 — drill-in, context, columns, auto-refresh, filter.**
6. Ship example definitions: git-lite, cloudwatch, ecs.

## 12. Open questions / to iterate

- **Panel tabs** (lazygit's Local Branches / Remotes / Tags in one box): multiple panels
  sharing one slot, switched with `[`/`]` when the panel is focused? Conflicts with detail-tab keys.
- **Colours/status**: declarative value→colour map, e.g.
  `color: { value: "{{.lastStatus}}", map: { RUNNING: green, STOPPED: red } }` — no expressions.
- Display helpers (basename of an ARN, relative time) without becoming a language — a fixed
  small set of formatters, or push it into `rows:` jq (which already can)?
- Multi-select + bulk actions?
- Definition composition / shared context across files (all AWS apps share profile/region).
- ~~Name~~: decided — **lazify**.
- paleta integration: expose `~/.config/lazify/*.yaml` as plt tools.
