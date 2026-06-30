# Web UI and terminal observer

## Web UI

Open the web UI with:

```sh
catacomb ui
```

Or paste the bearer URL directly into any browser:

```
http://127.0.0.1:<port>/?token=<bearer>
```

The URL is printed at daemon startup. `catacomb ui --no-open` prints it without opening
a browser. The bearer token is stored in the discovery file
(`~/.catacomb/run/daemon.json`, mode 0600).

### Sessions list

The landing page lists all observed sessions. Columns are sortable by started time,
duration, tokens, cost, tools used, and errors. A search box filters sessions by
attribute values.

### Session view

Selecting a session opens a 3-pane layout.

**Outline tree** — virtualized collapsible tree of session → prompt → turn →
tool/MCP call, with subagents nested and lazy-loaded on expand. Filters are available
by node type, status, and tier. System prompts can be shown or hidden.

**Node drawer** — shows type, status, timing (t_start/t_end/duration_ms), tokens,
cost, model, and phase info for marker nodes.

**Payload panel** — shows the content of the selected node's message or tool
input/output. Requires `--allow-payload-access`; displays `Forbidden` otherwise. Large
payloads are truncated. Redactions are displayed inline with their path and reason.

### Diff view

Compare two sessions using the diff view. Pick a session for each side and, optionally,
a phase per side using the phase pickers. The view shows counts of unchanged, changed,
added, and removed steps with per-field deltas.

The UI derives available phase names by reading `marker` nodes from the session graph.
There is no separate phases endpoint.

### Status pills

Nodes carry status pills: running, error, ok, blocked, or unknown. Following a
"silence when healthy" principle, status is surfaced only when it carries signal.

## Terminal observer

### Interactive observer

`catacomb observe` opens an interactive Bubbletea TUI connected to the live feed.

```sh
# Open the sessions list
catacomb observe

# Open a specific session directly
catacomb observe <session-hash>

# Disable ANSI color
catacomb observe --no-color
```

Key bindings:

| Key | Context | Action |
| --- | --- | --- |
| `q` / Ctrl-C | anywhere | Quit |
| `Tab` | anywhere | Cycle focus: sessions → tree → detail |
| `j` / `k` / ↑ / ↓ | sessions list | Move selection |
| `/` | sessions list | Filter |
| `Enter` | sessions list | Open session |
| `j` / `k` | tree | Move selection |
| `h` | tree | Collapse node |
| `l` | tree | Expand node |
| `Space` | tree | Toggle node |
| `Enter` | tree | Select node |
| `c` | detail | Fetch payload (requires `--allow-payload-access`) |
| `d` | detail | Toggle debug overlay |

### Non-interactive stream

`catacomb watch` prints JSON graph deltas to stdout over SSE. It is non-interactive
and suitable for piping or scripting.

```sh
catacomb watch
catacomb watch --run <run-id>
catacomb watch --type tool_call --type mcp_call
catacomb watch --tier core
```

Flags: `--run` filters by run ID; `--type` (repeatable) filters by node type;
`--tier` (repeatable) filters by tier (`core` or `detail`).
