# Milestone D — CLI Onboarding + Visual Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal.** Milestones A (hero web flow: sessions list → scoped graph → node drawer, all live) and B (deep inspection: content endpoint, timeline, KPI strip, filters, SSE resume) are landed. Milestone D removes every first-run sharp edge and reaches the visual/a11y bar. Per spec §8 this milestone has **two halves**, and this plan **prioritizes the first** because it closes the owner's stated pain *"непонятно как пользоваться бинарём"* (unclear how to use the binary):

1. **CLI onboarding (HIGH priority).** `catacomb up` — one command that starts the daemon if it is not already running (detached + discovery written), idempotently installs hooks, `/healthz`-preflights, prints the bearer URL, opens the UI, and — if no live session appears within N seconds — offers to replay a bundled demo transcript. `catacomb demo` — replay a bundled/seeded synthetic transcript into the running daemon. `catacomb status` — addr / pid / uptime / token-age / session+node counts. **Typed error sentinels** that render human messages ending in the *exact* remediation command (no daemon → `No catacomb daemon running. Start one: catacomb up`). **Grouped cobra help** (Observe / Setup / Advanced). A README quickstart block. A **bundled demo transcript** (richer than the 5-line `cmd/catacomb/testdata/session.jsonl` — multiple turns, a subagent, tool + MCP calls, an error) embedded for `catacomb demo` and the `up` fallback. `?token=` stays (cookie handshake deferred per §10 Q7).

2. **Visual polish (LOWER priority).** Light theme + persisted toggle (respect OS `prefers-color-scheme`; the dark "lit excavation" stays default), illustrated empty/loading/error states, deep a11y (node-drawer focus-trap + focus-return, graph arrow-key traversal along edges, proper roles/aria), and a `--text-faint` contrast bump. (Favicon/title/wordmark and the connection-pill silence-when-healthy are already done — verified in `webui/web/style.css` and `App.svelte`.)

**Architecture.** All CLI work is additive to the existing `cmd/catacomb` package and reuses the established injectable-seam pattern (`openBrowser`/`startCmd`/`execCommand`/`listen`/`ReadDiscovery`). `catacomb up` is an **orchestrator** that composes the existing primitives (`runDaemonWith`-style detached start, `installHooks`, `ReadDiscovery`, a `/healthz` poll, `runUI`'s URL build + `openBrowser`, and `demo`'s replay) behind a struct of function seams so the whole command is unit-testable with **no real processes, no real browser, and no `time.Sleep`** (forbidigo bans it). `catacomb status` reads the discovery file + a daemon status surface and formats it. The demo transcript is a committed `.jsonl` embedded with `//go:embed` and fed through the existing hook/transcript ingestion path over the daemon's loopback HTTP API (the same path `catacomb hook`/`catacomb replay` use), so the demo exercises the real reconciliation core. Frontend polish extends `webui/web/style.css` (a `[data-theme]` / media-query token override), a tiny gated `theme.ts` resolver, and the existing components (`NodeDrawer.svelte` focus management, `GraphCanvas.svelte`/`GraphNode.svelte` keyboard traversal).

**Design principle (owner, FIRM): minimalist + functional, silence-when-healthy, no elements for their own sake.** Every D addition earns its place: `up`/`demo`/`status` exist to kill the multi-step first-run; the light theme respects the OS and stays quiet (dark default unchanged); the a11y work adds no visible chrome (focus rings already exist via `:focus-visible`); error sentinels are terse and end in a copy-pasteable command. No splash screens, no decorative illustrations beyond the single-glyph empty states already in the codebase.

**Tech stack.** Backend/CLI: Go 1.26 stdlib (`os`, `os/exec`, `net/http`, `context`, `errors`, `time`, `embed`, `encoding/json`, `text/tabwriter`/`fmt`), `github.com/spf13/cobra` (in tree), `github.com/stretchr/testify` (in tree). **No new Go dependencies.** Frontend: the existing Vite + Svelte 5 toolchain, Vitest, Playwright; **no new npm deps** (theme = CSS custom-property override + a `localStorage` read/write + a `matchMedia` seam; a11y = native focus APIs).

---

## CRITICAL pre-flight findings (verified against the real source on 2026-06-24 — read before implementing)

1. **The CLI seam pattern is well established and is the model for `catacomb up`.** `cmd/catacomb/ui.go` injects `openBrowser` (a package `var`) which delegates to `startCmd` (also a `var`); `cmd/catacomb/streamjson.go` injects `execCommand = exec.Command`; `cmd/catacomb/daemon.go`'s `runDaemonWith` takes `open`/`listen`/`listenGRPC`/`newToken` as function params; `cmd/catacomb/installhooks.go` injects `osExecutable`/`osUserHomeDir`. **`catacomb up` must follow this exactly:** a `runUp(out, cfg upDeps)` where `upDeps` is a struct of seams (`startDaemon`, `installHooks`, `readDiscovery`, `pollHealthz`, `openBrowser`, `sessionCount`, `replayDemo`, plus a `wait`/`after` channel seam for the no-session timeout). Tests inject fakes; no real daemon, browser, or sleep.

2. **`time.Sleep` is forbidden in tests (forbidigo) — the up-fallback timeout needs an injectable clock/timer seam, not a sleep.** The codebase already proves the no-sleep discipline: `cmd/catacomb/daemon_test.go` uses `require.Eventually` + channels; `daemon/sse_test.go` etc. never sleep. The "wait N seconds for a live session" logic must be a seam: e.g. `after func(time.Duration) <-chan time.Time` (default `time.After`, tests pass a closed/controllable channel) **plus** a `pollHealthz`/`sessionCount` seam the test drives synchronously. Production code may use `time.After`; **tests must not** rely on wall-clock — they inject the channel and the count function. (The daemon's own loops use `time.NewTicker`, never `time.Sleep`, for the same reason.)

3. **Detaching the daemon is the single hardest cross-platform/testability problem.** `catacomb up` must start a daemon that **outlives the `up` process** (`up` returns after opening the browser). Today nothing starts a detached daemon — `runDaemonWith` runs it **in-foreground** until ctx cancel. The plan introduces a `startDaemon` seam whose **production** implementation does `exec.Command(self, "daemon", ...flags)` with `Stdout/Stderr` redirected to a log file and **`cmd.Start()` (not `Wait`)** so it detaches, then waits (via the `pollHealthz` seam) for discovery+healthz. `self` comes from `os.Executable()` (already wrapped as `osExecutable`). **Tests inject a fake `startDaemon`** that does nothing (or writes a fake discovery file) — no real child process. `GOOS=windows` cleanliness: `exec.Command(self,...).Start()` is cross-platform; we do **not** use `syscall.SysProcAttr`/setsid (platform-specific, would need build-tagged files and is not required — a child whose parent exits is reparented and keeps running on all three OSes for our purpose). Document this in Gaps/Risks.

4. **`/healthz` exists and is unauthenticated.** `daemon/server.go` registers `GET /healthz` → 200 (no auth). The preflight is a plain `http.Get("http://"+addr+"/healthz")` against the discovered addr. `cmd/catacomb/daemon_test.go` already has the exact poll idiom (`awaitHealthz` + `require.Eventually`). The `up` `pollHealthz` seam wraps this.

5. **`catacomb status` needs addr/pid/uptime/token-age/session+node counts; only some are available today.** Available: `addr`/`token`/`grpc_addr` from `daemon.ReadDiscovery` (`daemon/discovery.go`); `uptime_seconds`/`shards`/`open_runs` from `GET /metrics` (`daemon.Metrics`, `daemon/daemon.go`). **NOT available:** `pid`, `token-age`, and an explicit session/node count. Resolution (pin): (a) **pid + token-age** — extend `daemon.Discovery` with `pid int` and `started_at string` (RFC3339), written in `runDaemonWith` (`os.Getpid()`, `nowFn().UTC()`); `status` derives token-age = `now - started_at` and pid from discovery. This is additive (both `omitempty`), back-compatible, and avoids a new endpoint for facts the daemon already knows at startup. (b) **session count + node count** — `GET /v1/sessions` already returns the per-session summaries (count = `len`, node count = Σ `node_count`), bearer-gated via `authedAllowQuery` (token from discovery). `status` fetches it with `?token=`. No new daemon endpoint is strictly required; uptime can come from `/metrics` or be derived from `started_at` in discovery (prefer `started_at` to avoid a second fetch — pin: derive uptime from discovery `started_at`, fetch `/v1/sessions` only for counts). See Gaps/Risks for the "daemon restarted, stale discovery" edge.

6. **There is no demo/seed anywhere.** `grep -rni 'demo\|seed'` over `cmd/`+`daemon/` (non-test) returns nothing. The richest existing fixture is `ingest/jsonl/testdata/subagent.jsonl` (2 lines, one subagent) and `cmd/catacomb/testdata/session.jsonl` (5 lines: user → tool_use Bash → tool_result → tool_use mcp → error result). The bundled demo must be **new and richer** (multiple turns, a subagent, tool + MCP calls, an error) and is **embedded in the binary** (not testdata) so `catacomb demo` works from an installed binary with no files on disk. The transcript JSONL format is the Claude Code shape parsed by `ingest/jsonl` (`type`/`uuid`/`parentUuid`/`sessionId`/`timestamp`/`message{...}`; subagents via `isSidechain`+`parent_tool_use_id`; usage via `message.usage`). Pin the demo session hash to a stable value (e.g. `demo-0001`) so the printed deep-link is deterministic.

7. **The demo must ingest over the running daemon, not build a throwaway graph.** `catacomb replay` (`replay.go`) builds a graph into a *separate* SQLite db and exits — it does **not** feed the running daemon, so the UI would not show it. `catacomb demo` must POST the demo transcript's lines into the **running daemon** so it appears in the live sessions list. The cleanest reuse: feed the demo as **transcript JSONL lines** to the daemon's ingestion. Two viable seams — pin (a): POST each demo line to `POST /v1/stream-json`? No — demo is transcript-shape, not stream-json. Pin (b): the daemon has no public "ingest transcript line over HTTP" endpoint; transcript ingestion is via the tailer (`d.IngestTranscript`). **Resolution:** `catacomb demo` converts the embedded transcript into **hook-style events is wrong too**. The honest, minimal path: replay the embedded JSONL through the **same hook-forward mechanism is not applicable** (hooks are a different shape). **Final pin:** add a tiny loopback ingestion route the demo uses, OR — simpler and preferred — `catacomb demo` writes the embedded transcript to a temp file inside the daemon's configured `--transcript-dir` so the **existing tailer** picks it up. That couples demo to a tailer-enabled daemon. The cleanest decoupled option that reuses an existing authenticated surface: **add `POST /v1/transcript` (bearer-gated) that calls `d.IngestTranscript(line, sessionID)`** — symmetric with the existing `POST /v1/stream-json` → `d.IngestStreamJSON`. This is one small additive daemon route (100%-testable, mirrors `handleStreamJSON`) and makes `catacomb demo` a thin client that streams the embedded JSONL to it. **This route is a CLI-onboarding prerequisite and is Task D-be1.** (See Gaps/Risks for why a route beats the temp-file-in-tailer-dir hack.)

8. **Cobra command grouping is supported via `cobra.Group` + `cmd.GroupID`.** Cobra (in tree, `github.com/spf13/cobra`) supports `root.AddGroup(&cobra.Group{ID, Title})` and per-command `GroupID`. The grouped-help task sets a `GroupID` on every subcommand and adds three groups (Observe / Setup / Advanced). Commands without a group fall under "Additional Commands"; the plan assigns **every** registered command a group so help is fully organized. `root.go` is the single registration point.

9. **Frontend theming is a token override, and the components already consume tokens.** `webui/web/style.css` defines all colors as OKLCH custom properties under `:root` with `color-scheme: dark`. Light theme = a `[data-theme='light']` block (and a `@media (prefers-color-scheme: light)` block scoped to `:root:not([data-theme='dark'])`) overriding the same tokens. Components reference `var(--bg)` etc., so **no component markup changes** are needed for the palette — only the stylesheet + a tiny resolver that sets `document.documentElement.dataset.theme` and a toggle control. The `--text-faint` contrast bump is a one-line token change verified against WCAG.

10. **A11y starting state (verified).** `NodeDrawer.svelte` has `role="complementary"`, `aria-hidden={!isOpen}`, ESC-to-close, and `:focus-visible` rings — but **no focus-trap and no focus-return** (focus is not moved into the drawer on open, nor restored to the triggering node on close). `GraphCanvas.svelte` uses Svelte Flow with `onnodeclick` → `selectNode`; nodes are focusable via Svelte Flow defaults but there is **no arrow-key traversal along edges** and `GraphNode.svelte` needs explicit `role`/`aria-label`/`tabindex` + a keydown handler. `:focus-visible { outline: 2px solid var(--ring) }` is global in `style.css`. These are the exact a11y gaps D closes.

---

## Global Constraints

Binding on every task; each task's steps re-verify them.

- **Pure Go, no cgo.** `GOOS=windows go build ./...`, `GOOS=linux go build ./...`, `GOOS=darwin go build ./...` all clean. The `up`/`open`/`status`/detach logic must be cross-platform — **no `syscall.SysProcAttr`, no setsid, no platform build tags** unless a Gap explicitly justifies one (none is expected). SQLite stays `modernc.org/sqlite` (untouched here).
- **No Go comments** except `//go:build`, `//go:embed`, `//go:generate`. Only `//go:embed` (for the demo transcript) appears in this milestone. Enforced by `internal/codepolicy` (`go test ./internal/codepolicy/`). Every Go snippet in this plan is comment-free and must be written comment-free. Generated-header files are skipped wholesale.
- **100% line coverage under `-race`**, TDD-first; the threshold never goes down. Every new/modified Go file (`cmd/catacomb/up.go`, `cmd/catacomb/demo.go`, `cmd/catacomb/status.go`, `cmd/catacomb/errors.go`, `cmd/catacomb/root.go`, `cmd/catacomb/daemon.go`, `daemon/transcript.go` new, `daemon/server.go`, `daemon/discovery.go`, the embedded-asset accessor) must stay 100%. Untestable code is a refactoring signal, not an exclusion — hence the seam discipline below.
- **Injectable seams for ALL I/O.** `catacomb up`/`status`/`demo` must be testable with no real processes, browsers, HTTP servers, or wall-clock waits. Follow the existing `openBrowser`/`startCmd`/`execCommand`/`runDaemonWith`-param model: pass function seams (and for time, an injectable `after`/`now` — **never** `time.Sleep`, forbidigo bans it). Where a real loopback server is convenient, use `httptest` (as `daemon/*_test.go` do) — never a real spawned daemon in a unit test.
- **Dependency inversion; sentinel errors via `errors.Is`/`errors.As`.** D adds typed sentinels (`ErrNoDaemon`, `ErrDaemonUnreachable`, `ErrHooksNotInstalled`, `ErrDaemonRestarted`/token-mismatch) whose user-facing string **ends in the exact remediation command**. `log/slog`/`log` per existing style; never log or serialize a secret (the token is printed only in the bearer URL, as today). `context.Context` first for I/O; `gofumpt`+`goimports` (local prefix `github.com/realkarych/catacomb`).
- **No global mutable state / no `init()` side effects.** Seams are package `var`s (like `openBrowser`/`execCommand`) reset in tests via `t.Cleanup`, consistent with the existing pattern; the embedded transcript is an immutable `//go:embed` `var`.
- **No `time.Sleep` in tests** (forbidigo). Use `require.Eventually`, channels, deadlines, `httptest`. Mirror `cmd/catacomb/daemon_test.go` (`awaitHealthz`, `readAddr`, `require.Eventually`), `cmd/catacomb/ui_test.go` (seam-swap + `t.Cleanup`).
- **Loopback + bearer trust boundary (ADR-0013).** The new `POST /v1/transcript` (demo ingestion) is bearer-gated via `d.authed(token, ...)` (header) exactly like `POST /v1/stream-json` — loopback-only, not query-token (it is a POST mutation, not an SPA GET). `?token=` for the UI URL stays (cookie handshake deferred, §10 Q7). `catacomb status`'s `/v1/sessions` fetch uses the discovery token via `?token=` (read path), as the SPA does.
- **Frontend deps via the JS toolchain only**; never `go mod tidy` for them; the embedded `webui/dist/` is data. **No new npm deps in D.** 100% Vitest on the new pure-logic module (`theme.ts` resolver); components and `.svelte`/`.svelte.ts` rune wiring are NOT line-gated; Playwright e2e; the `dist/`-drift check stays green (every FE task ends with `npm run build` + commit the rebuilt `dist/`). **Light theme must not regress the dark default** — a Playwright assertion verifies the default `:root` palette is unchanged when no theme is set / OS is dark.
- **Commit per task** (`feat(...)` / `fix(...)` / `docs(...)`); never commit to `master` mid-plan; branch first (`git checkout -b feat/m-d-onboarding-polish` from `master`); squash-merge via PR; no `--no-verify`. End commit messages with the `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `cmd/catacomb/errors.go` | Create | Typed CLI sentinels (`ErrNoDaemon`, `ErrDaemonUnreachable`, `ErrHooksNotInstalled`, `ErrDaemonRestarted`) whose `Error()` strings END in the exact remediation command. A `renderErr(error) string` mapping wrapped discovery/HTTP errors to the right sentinel message. |
| `cmd/catacomb/errors_test.go` | Create | 100%: each sentinel's message ends in the command; `errors.Is` matches through wraps; `renderErr` maps `os.IsNotExist` discovery errors → `ErrNoDaemon`, connection-refused → `ErrDaemonUnreachable`, 401 → `ErrDaemonRestarted`. |
| `cmd/catacomb/up.go` | Create | `newUpCmd()` + `runUp(out io.Writer, deps upDeps) error`. `upDeps` struct = seams (`startDaemon`, `installHooks`, `readDiscovery`, `pollHealthz`, `sessionCount`, `openBrowser`, `replayDemo`, `after`, `noOpen`, `waitSeconds`). Orchestrates: discover-or-start → install-hooks (idempotent) → healthz preflight → print bearer URL → open UI → if 0 sessions after N s, offer/replay demo. |
| `cmd/catacomb/up_test.go` | Create | 100%: daemon-already-running path (no start); daemon-not-running → start seam called; hooks installed idempotently; healthz fail → `ErrDaemonUnreachable`; URL printed; `--no-open` skips browser; no-session-after-N → demo offered/replayed (driven via injected `after` channel + `sessionCount` returning 0 then >0); every seam error surfaced. |
| `cmd/catacomb/demo.go` | Create | `newDemoCmd()` + `runDemo(out, deps demoDeps)`; reads the embedded transcript, POSTs each line to `POST /v1/transcript` on the discovered daemon (bearer header), prints the demo session deep-link. Seams: `readDiscovery`, `httpClient` (or a `post` func), `transcript` bytes (default embedded, overridable in tests). |
| `cmd/catacomb/demo_test.go` | Create | 100%: posts every line to an `httptest` daemon; no-daemon → `ErrNoDaemon`; HTTP non-2xx surfaced; prints the stable deep-link; bad transcript line handled. |
| `cmd/catacomb/demo_assets.go` | Create | `//go:embed testdata/demo.jsonl` → `var demoTranscript []byte` (the one allowed comment-form). Tiny accessor; embedded asset is data, skipped by codepolicy as it is the embed directive only. |
| `cmd/catacomb/testdata/demo.jsonl` | Create | The bundled demo transcript: multiple turns, a subagent (`isSidechain`), tool calls (Bash, Read), an MCP call, and an error result. Stable `sessionId` (`demo-0001`). Claude Code JSONL shape (parsed by `ingest/jsonl`). |
| `cmd/catacomb/status.go` | Create | `newStatusCmd()` + `runStatus(out, deps statusDeps)`; reads discovery (addr/pid/token/started_at), derives uptime + token-age from `started_at`, fetches `/v1/sessions` (`?token=`) for session+node counts, prints a `text/tabwriter` block. Seams: `readDiscovery`, `fetchSessions`/`httpClient`, `now`. |
| `cmd/catacomb/status_test.go` | Create | 100%: full status from a fake discovery + `httptest` sessions; no-daemon → `ErrNoDaemon`; unreachable sessions fetch → partial (addr/pid/uptime shown, counts "unavailable"); 401 → `ErrDaemonRestarted`; uptime/token-age math. |
| `cmd/catacomb/root.go` | Modify | Register `up`/`demo`/`status`; add three `cobra.Group`s (Observe / Setup / Advanced) and set `GroupID` on every subcommand. |
| `cmd/catacomb/root_test.go` | Create (or extend existing) | Assert groups exist, every command has a non-empty `GroupID`, and the three new commands are registered. |
| `cmd/catacomb/daemon.go` | Modify | Write `pid` (`os.Getpid()`) + `started_at` (`nowFn().UTC()`) into `daemon.Discovery` in `runDaemonWith`. (Provides `status`'s pid/uptime/token-age without a new endpoint.) |
| `cmd/catacomb/daemon_test.go` | Modify | Assert the written discovery carries a non-zero `pid` and a parseable `started_at`. |
| `daemon/discovery.go` | Modify | Extend `Discovery` with `Pid int json:"pid,omitempty"` and `StartedAt string json:"started_at,omitempty"`. (Additive, back-compatible; existing readers ignore unknown→present fields.) |
| `daemon/discovery_test.go` | Modify | Round-trip the new fields. |
| `daemon/transcript.go` | Create | `handleTranscript` (bearer via `d.authed`) — reads NDJSON body, `d.IngestTranscript(line, sessionID)` per line, mirrors `handleStreamJSON`'s scanner/recover shape. The demo-ingestion surface. |
| `daemon/transcript_test.go` | Create | 100%: posts demo-shape lines → graph populated + session queryable; recover-on-panic; scan error path; session id derived from line. |
| `daemon/server.go` | Modify | Register `POST /v1/transcript` → `d.authed(token, d.handleTranscript)`. |
| `daemon/server_test.go` | Modify | Route registration + bearer-gating (401 without token) for `/v1/transcript`. |
| `README.md` | Modify | Replace the `make`-only "Development" lede for users with a **Quickstart** block: `catacomb up` (one line), what it does, `catacomb demo`, `catacomb status`; keep the `make` block under Development. |
| `webui/web/src/lib/theme.ts` | Create | Pure theme resolver: `resolveTheme(stored, osPrefersLight) -> 'dark' \| 'light'`; `nextTheme(current) -> ...`; storage key constant. **Gated 100%.** |
| `webui/web/src/lib/theme.test.ts` | Create | 100% branches: stored override wins; absent stored → OS; toggle cycles; invalid stored → default dark. |
| `webui/web/style.css` | Modify | Add `[data-theme='light']` token block + `@media (prefers-color-scheme: light)` scoped override; bump `--text-faint` for contrast (both themes). Dark `:root` default unchanged. |
| `webui/web/src/lib/stores/theme.svelte.ts` | Create | Thin runes wiring: `$state` current theme, init from `localStorage` + `matchMedia` (seam), `toggleTheme()` persists + sets `document.documentElement.dataset.theme`. NOT gated. |
| `webui/web/src/components/ThemeToggle.svelte` | Create | A quiet topbar toggle button (sun/moon glyph), `aria-pressed`, calls `toggleTheme()`. NOT gated. |
| `webui/web/src/App.svelte` | Modify | Mount `<ThemeToggle/>` in the topbar; init theme on mount. |
| `webui/web/src/components/NodeDrawer.svelte` | Modify | Focus-trap (Tab/Shift+Tab cycle within drawer) + focus-return (restore focus to the previously-focused element / triggering node on close); `aria-modal`/labelledby tidy. NOT gated (Playwright covers). |
| `webui/web/src/components/GraphNode.svelte` | Modify | `role="button"`, `aria-label` (type + name + status), `tabindex`, keydown (Enter/Space → select; Arrow keys → traverse). NOT gated. |
| `webui/web/src/components/GraphCanvas.svelte` | Modify | Wire arrow-key edge traversal: a pure `nextNodeByDirection(currentId, edges, nodes, dir)` helper drives selection along edges; `role="application"`/`aria-label` on the canvas. NOT gated (helper is gated — below). |
| `webui/web/src/lib/graph-nav.ts` | Create | Pure `nextNodeByDirection(currentId, nodes, edges, dir)` — given selection + topology + an arrow direction, return the next node id to select (along outgoing/incoming edges; left/right = along edges, up/down = siblings). **Gated 100%.** |
| `webui/web/src/lib/graph-nav.test.ts` | Create | 100% branches: forward/back along an edge, sibling up/down, no-edge no-op, wrap/clamp, empty graph. |
| `webui/web/vitest.config.ts` | Modify | Add `src/lib/theme.ts`, `src/lib/graph-nav.ts` to coverage `include`. |
| `webui/web/e2e/onboarding.spec.ts` | Create | (Optional) Playwright: theme toggle persists + respects OS; drawer focus-trap/return; graph arrow traversal selects along edges; **dark default not regressed**. |
| `webui/dist/**` | Rebuild (committed) | Updated artifact embedding theme + a11y changes. |

---

## Contracts to PIN (frozen by this plan)

### Extended `daemon.Discovery` (additive — existing fields unchanged)

```go
type Discovery struct {
	Addr      string `json:"addr"`
	Token     string `json:"token"`
	GRPCAddr  string `json:"grpc_addr,omitempty"`
	Pid       int    `json:"pid,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
}
```

- `pid` — `os.Getpid()` of the daemon process, written by `runDaemonWith`. Used by `catacomb status`.
- `started_at` — RFC3339 UTC daemon start time, written by `runDaemonWith` (`nowFn().UTC().Format(time.RFC3339)`). `status` derives **uptime** (`now - started_at`) and **token-age** (same, since the token is minted at start) from it — no second fetch, no new endpoint.

### `POST /v1/transcript` (NEW, bearer-gated, loopback) — demo ingestion

- Request: NDJSON body (Claude Code transcript lines), `Authorization: Bearer <token>`. Per line: derive `sessionId`, call `d.IngestTranscript(line, sessionID)`. Mirrors `handleStreamJSON` exactly (scanner with 16 MiB cap, `recover`, always 200).
- Response: `200 OK` (ingestion is best-effort/quarantine-on-error like the other ingest routes); `401` without the bearer token.
- **Not** query-token gated (it is a mutation POST, not an SPA read) — uses `d.authed`, not `d.authedAllowQuery`.

### `upDeps` seam struct (NEW) — the testability contract for `catacomb up`

```go
type upDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	startDaemon   func() error
	installHooks  func() error
	pollHealthz   func(ctx context.Context, addr string) error
	sessionCount  func(ctx context.Context, disc daemon.Discovery) (int, error)
	openBrowser   func(string) error
	replayDemo    func(ctx context.Context, disc daemon.Discovery) error
	after         func(time.Duration) <-chan time.Time
	discoveryPath string
	waitSeconds   int
	noOpen        bool
	noDemo        bool
}
```

- The cobra `RunE` builds a production `upDeps` (real `startDaemon` = detached `exec`, `pollHealthz` = `/healthz` GET loop, `sessionCount` = `/v1/sessions` fetch, `replayDemo` = `runDemo`'s core, `after = time.After`). **Tests construct `upDeps` with fakes** — no process, no HTTP, no wall clock. This is the crux of making `up` unit-testable (Gaps/Risks #1).
- `after` is the **only** time seam; production passes `time.After`, tests pass a channel they close on demand. There is **no `time.Sleep`** anywhere.

### CLI error sentinels (NEW) — every message ENDS in the exact command

```go
var (
	ErrNoDaemon          = errors.New("no catacomb daemon running. Start one: catacomb up")
	ErrDaemonUnreachable = errors.New("catacomb daemon is unreachable. Restart it: catacomb up")
	ErrHooksNotInstalled = errors.New("catacomb hooks are not installed. Install them: catacomb install-hooks")
	ErrDaemonRestarted   = errors.New("catacomb daemon restarted (token mismatch). Re-open the UI: catacomb ui")
)
```

- `errors.Is`-checkable; wrapped with `fmt.Errorf("...: %w", ErrNoDaemon)` where context helps, but the **sentinel's own string is the terminal remediation** so even an unwrapped print ends in the command. `renderErr` maps low-level causes (a `daemon.ReadDiscovery` `os.IsNotExist`, a `syscall.ECONNREFUSED`, an HTTP 401) to the right sentinel before printing.

---

## Task ordering & dependency graph

```
CLI-ONBOARDING (HIGH — do first):
  D-be1 POST /v1/transcript ──┐ (demo ingestion surface)
  D-cli1 discovery pid/started_at ─┐
  D-cli2 error sentinels ──────────┼─▶ D-cli3 status ──┐
  D-cli4 demo (needs D-be1) ───────┼──────────────────┼─▶ D-cli5 up (orchestrates demo+status+seams)
                                    │                   │
  D-cli6 grouped help ─────────────┘ (independent)     │
  D-cli7 README quickstart ◀───────────────────────────┘ (after up/demo/status land)

VISUAL POLISH (LOWER — after CLI):
  D-fe1 theme resolver (gated) ─▶ D-fe2 light-theme CSS + toggle + store
  D-fe3 graph-nav helper (gated) ─▶ D-fe4 graph keyboard traversal
  D-fe5 drawer focus-trap/return (independent)
  D-fe6 --text-faint contrast bump (independent, tiny)
  D-fe7 e2e (after fe2/fe4/fe5)
```

Recommended sequence (each a commit): **D-be1 → D-cli1 → D-cli2 → D-cli4 (demo) → D-cli3 (status) → D-cli5 (up) → D-cli6 (grouped help) → D-cli7 (README)**, then **D-fe1 → D-fe2 → D-fe3 → D-fe4 → D-fe5 → D-fe6 → D-fe7**. CLI ships fully before any polish, per the prompt's priority. The shared `webui/dist/` rebuild is committed at the end of each FE task.

---

## CLI onboarding (HIGH priority — implement first)

### Task D-be1 — `POST /v1/transcript` demo-ingestion route (PREREQUISITE for `catacomb demo`)

**Why this exists (pre-flight #7):** `catacomb demo` must put the demo into the **running** daemon's live graph. `catacomb replay` builds a throwaway DB and exits; the tailer needs a configured dir. The minimal honest surface is a bearer-gated transcript-ingest route symmetric with the existing `POST /v1/stream-json`.

**Files:** Create `daemon/transcript.go`, `daemon/transcript_test.go`. Modify `daemon/server.go` (route), `daemon/server_test.go` (registration + bearer gate).

**Interfaces (produces):** `func (d *Daemon) handleTranscript(w http.ResponseWriter, r *http.Request)`; route `POST /v1/transcript` via `d.authed(token, d.handleTranscript)`.

**Behavior (pin):** mirror `handleStreamJSON` (`daemon/server.go`): `bufio.Scanner` with `sc.Buffer(make([]byte,0,1MiB), 16MiB)`, `defer recover()` logging, skip blank lines, copy each line, derive `sessionID` from the line (`streamSessionID` reads `session_id`; transcript lines use `sessionId` — pin: add a small `transcriptSessionID(line)` reading `sessionId` per the Claude Code shape), `_ = d.IngestTranscript(buf, sessionID)`, always `200`. `d.IngestTranscript` already exists (`daemon/daemon.go`) and quarantines on error — no new error surface.

**Steps:**

- [ ] **Step 1: Branch.** `cd /Users/karych/src/catacomb && git checkout -b feat/m-d-onboarding-polish`
- [ ] **Step 2: Write failing `daemon/transcript_test.go`** — `httptest.NewServer(d.Handler("tok"))`, POST the demo-shape lines with the bearer header, assert via `GET /v1/sessions` (or the in-memory graph) that the demo session appears with nodes; a panic-injecting line is recovered; a scan error is logged not fatal; 401 without token. Reuse `tempStore`/`ob(...)` helpers from `daemon/*_test.go`.
- [ ] **Step 3: Run — expect FAIL.** `go test ./daemon/ -run Transcript 2>&1 | head`
- [ ] **Step 4: Implement** `daemon/transcript.go` + register the route in `daemon/server.go` + `transcriptSessionID`.
- [ ] **Step 5: 100% coverage** for the touched daemon files. `go test -race -coverprofile=/tmp/cov.out ./daemon/ && go tool cover -func=/tmp/cov.out | grep -E 'transcript.go|server.go' | grep -v 100.0% || echo covered`
- [ ] **Step 6: Lint + cross-build + codepolicy.** `golangci-lint run ./daemon/ && GOOS=windows go build ./... && go test ./internal/codepolicy/`
- [ ] **Step 7: Commit** `feat(daemon): bearer-gated POST /v1/transcript for demo/live transcript ingestion`.

---

### Task D-cli1 — Discovery `pid` + `started_at` (enables `status`)

**Files:** Modify `daemon/discovery.go` (`Discovery` struct), `daemon/discovery_test.go`, `cmd/catacomb/daemon.go` (write the fields), `cmd/catacomb/daemon_test.go`.

**Interfaces:** the pinned `Discovery` additions (`Pid`, `StartedAt`). In `runDaemonWith`, populate `disc.Pid = os.Getpid()` and `disc.StartedAt = nowFn().UTC().Format(time.RFC3339)` before `WriteDiscovery`. (`nowFn` already exists in `daemon`; in `cmd` use `time.Now()` wrapped behind a small package `var nowFn = time.Now` if a test needs determinism — pin: add `var nowFn = time.Now` in `cmd/catacomb` for `status`/`daemon` time, reset in tests, consistent with `daemon`'s pattern.)

**Steps:**

- [ ] **Step 1: Write failing tests** — `daemon/discovery_test.go` round-trips `Pid`/`StartedAt`; `cmd/catacomb/daemon_test.go` asserts the written discovery (read back after `runDaemonWith` starts) has non-zero `Pid` and a `time.Parse(time.RFC3339, StartedAt)`-able value.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** the struct fields + the writes in `runDaemonWith`.
- [ ] **Step 4: 100% coverage** for `daemon/discovery.go` and `cmd/catacomb/daemon.go`.
- [ ] **Step 5: Lint + cross-build + codepolicy.**
- [ ] **Step 6: Commit** `feat(daemon): write pid + started_at into discovery for catacomb status`.

---

### Task D-cli2 — Typed CLI error sentinels with copy-pasteable remediation

**Files:** Create `cmd/catacomb/errors.go`, `cmd/catacomb/errors_test.go`.

**Interfaces (produces):** the four pinned sentinels + `func renderErr(err error) string` that, given a low-level error, returns the human message ending in the command. `renderErr` recognizes: a wrapped `daemon.ReadDiscovery` error where `errors.Is(err, os.ErrNotExist)` → `ErrNoDaemon`; a dial/connection-refused (`errors.Is(err, syscall.ECONNREFUSED)` or a net-op error) → `ErrDaemonUnreachable`; an HTTP-401 marker error → `ErrDaemonRestarted`; otherwise the error's own string. Keep it a pure function over `error` (no I/O) so it is trivially 100%-tested.

**Notes for implementer:** the sentinels' `Error()` strings are the contract — each ENDS in the exact command (`catacomb up` / `catacomb install-hooks` / `catacomb ui`). Do **not** add trailing punctuation after the command. `up`/`status`/`demo` call `renderErr` when printing a failure (or return the sentinel so the root's error printer shows it). Since `root.go` sets `SilenceErrors: true`, the commands print their own error via `cmd.PrintErrln(renderErr(err))` and return the sentinel — pin: commands `return` the sentinel (for `errors.Is` testability) **and** the top-level `main`/root prints `renderErr`. Simplest: a thin wrapper in each `RunE` that maps+prints+returns.

**Steps:**

- [ ] **Step 1: Write failing `errors_test.go`** — each sentinel string `strings.HasSuffix` the command; `errors.Is(fmt.Errorf("x: %w", ErrNoDaemon), ErrNoDaemon)`; `renderErr` table: not-exist→up, refused→unreachable, 401→restarted, generic→passthrough.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** `errors.go`.
- [ ] **Step 4: 100% coverage** for `cmd/catacomb/errors.go`.
- [ ] **Step 5: Lint + cross-build + codepolicy.**
- [ ] **Step 6: Commit** `feat(cli): typed error sentinels ending in copy-pasteable remediation`.

---

### Task D-cli4 — `catacomb demo` (bundled transcript replay)

**Files:** Create `cmd/catacomb/demo.go`, `cmd/catacomb/demo_assets.go`, `cmd/catacomb/testdata/demo.jsonl`, `cmd/catacomb/demo_test.go`. (Depends on D-be1, D-cli2.)

**Interfaces (produces):** `newDemoCmd()`; `func runDemo(ctx context.Context, out io.Writer, deps demoDeps) error` where `demoDeps{ readDiscovery func(string)(daemon.Discovery,error); post func(ctx, disc, body []byte) error; transcript []byte; discoveryPath string }`. Default `post` streams the body to `POST /v1/transcript` with the bearer header (a small `http.Client`, 2 s dial like `hook.go`); default `transcript = demoTranscript` (embedded). Prints the demo deep-link (`http://<addr>/?token=<tok>#/s/demo-0001`).

**The bundled transcript (pin, pre-flight #6):** `cmd/catacomb/testdata/demo.jsonl`, Claude Code JSONL shape, stable `sessionId:"demo-0001"`, richer than the existing fixtures — at minimum: a user prompt; an assistant turn with `usage` (so tokens/cost render); a `tool_use` Bash + its `tool_result`; a `tool_use` Read + result; an MCP `tool_use` (`mcp__*`) + result; a **subagent** branch (`isSidechain:true` + `parent_tool_use_id`) with its own tool call; and one **error** `tool_result` (`is_error:true`). Timestamps strictly increasing so durations stamp. This drives every UI surface (graph, drawer metrics, timeline, KPI strip, error chip) from one command.

**Notes for implementer:** `demo` is a thin client; all logic that matters (the transcript + the POST) is seam-injected, so `demo_test.go` runs against an `httptest` daemon (real `d.Handler("tok")`) and asserts the session is queryable — **no real daemon process**. No-daemon (discovery missing) → `ErrNoDaemon` via `renderErr`. The embedded asset uses the only-allowed `//go:embed` comment.

**Steps:**

- [ ] **Step 1: Author `cmd/catacomb/testdata/demo.jsonl`** (the rich transcript) and verify it parses: a quick `go test ./ingest/jsonl/` style check or a temporary `ParseReader` assertion in the demo test.
- [ ] **Step 2: Write failing `demo_test.go`** — spin `httptest.NewServer(d.Handler("tok"))`, write a discovery file pointing at it, run `runDemo`, assert (via `GET /v1/sessions` or `d` introspection) that `demo-0001` exists with the expected node kinds (subagent, tool, mcp, an error). Cases: no discovery → `ErrNoDaemon`; HTTP non-2xx → error surfaced; deep-link printed with the stable hash.
- [ ] **Step 3: Run — expect FAIL.**
- [ ] **Step 4: Implement** `demo_assets.go` (embed), `demo.go` (`runDemo` + cobra cmd).
- [ ] **Step 5: 100% coverage** for `cmd/catacomb/demo.go` (+ `demo_assets.go` is just the embed var). `go test -race -coverprofile=/tmp/cov.out ./cmd/catacomb/ && go tool cover -func=/tmp/cov.out | grep demo.go | grep -v 100.0% || echo covered`
- [ ] **Step 6: Lint + cross-build + codepolicy.**
- [ ] **Step 7: Commit** `feat(cli): catacomb demo replays a bundled synthetic transcript into the daemon`.

---

### Task D-cli3 — `catacomb status` (addr / pid / uptime / token-age / session+node counts)

**Files:** Create `cmd/catacomb/status.go`, `cmd/catacomb/status_test.go`. (Depends on D-cli1 for pid/started_at, D-cli2 for sentinels.)

**Interfaces (produces):** `newStatusCmd()`; `func runStatus(ctx context.Context, out io.Writer, deps statusDeps) error` where `statusDeps{ readDiscovery func(string)(daemon.Discovery,error); fetchSessions func(ctx, disc)([]daemon.SessionSummary, error); now func() time.Time; discoveryPath string }`. Reads discovery → addr/pid/token/started_at; derives **uptime** and **token-age** from `now() - started_at`; fetches `/v1/sessions` (`?token=`) for **session count** (`len`) and **node count** (Σ `node_count`). Renders a `text/tabwriter` block:

```
addr         127.0.0.1:53124
pid          40231
uptime       12m04s
token age    12m04s
sessions     3
nodes        47
```

**Notes for implementer:** if `readDiscovery` fails with not-exist → `ErrNoDaemon` (via `renderErr`). If the sessions fetch fails (daemon dead but stale discovery) → still print addr/pid/uptime/token-age and show `sessions`/`nodes` as `unavailable` (graceful partial — silence-when-broken is wrong here; the user needs the partial truth). A 401 from `/v1/sessions` → `ErrDaemonRestarted`. `fetchSessions` is seam-injected; tests use an `httptest` daemon (real `d.Handler`) or a fake returning summaries — no real daemon. Uptime/token-age formatting reuses a duration humanizer (a tiny pure helper, or `time.Duration.Truncate(time.Second).String()`).

**Steps:**

- [ ] **Step 1: Write failing `status_test.go`** — fake discovery (addr/pid/started_at=now-12m) + fake/`httptest` sessions → assert the rendered block has addr/pid/uptime≈12m/token-age≈12m/sessions=N/nodes=Σ. Cases: missing discovery → `ErrNoDaemon`; sessions fetch error → partial block with `unavailable`; 401 → `ErrDaemonRestarted`.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** `status.go`.
- [ ] **Step 4: 100% coverage** for `cmd/catacomb/status.go`.
- [ ] **Step 5: Lint + cross-build + codepolicy** (incl. `GOOS=windows` — `status` is pure read/format, trivially cross-platform).
- [ ] **Step 6: Commit** `feat(cli): catacomb status prints addr/pid/uptime/token-age/session+node counts`.

---

### Task D-cli5 — `catacomb up` (one-command bring-up + demo fallback)

**Files:** Create `cmd/catacomb/up.go`, `cmd/catacomb/up_test.go`. (Depends on D-be1, D-cli2, D-cli3, D-cli4.)

**Interfaces (produces):** `newUpCmd()`; `func runUp(ctx context.Context, out io.Writer, deps upDeps) error` (the pinned `upDeps`). Orchestration (pin):

1. `disc, err := deps.readDiscovery(path)` AND `deps.pollHealthz(ctx, disc.Addr)` to decide **is a daemon already live?** If discovery missing OR healthz fails → `deps.startDaemon()` (detached), then `deps.pollHealthz` until healthy (bounded; on timeout → `ErrDaemonUnreachable`), then re-`readDiscovery`.
2. `deps.installHooks()` — idempotent (the existing `installHooks` prunes-then-appends, so re-running is a no-op net change; verified in `installhooks.go`).
3. Print the bearer URL (reuse `runUI`'s URL build: `http://<addr>/?token=<tok>`); if `!deps.noOpen`, `deps.openBrowser(url)`.
4. Live-session fallback: if `!deps.noDemo`, `n, _ := deps.sessionCount(ctx, disc)`; if `n == 0`, wait on `deps.after(waitSeconds)` then re-check `sessionCount`; if still `0`, print "No live session detected — replaying a demo. (catacomb demo to replay)" and `deps.replayDemo(ctx, disc)`. (Pin: the offer is **non-interactive by default** — it just replays the demo and tells the user, because an interactive prompt is hard to test and the owner wants a populated UI on first run; a `--no-demo` flag opts out. Document this choice.)

**The production `upDeps` builder (in `RunE`):**

- `startDaemon` = `func() error { exe,_ := osExecutable(); c := execCommand(exe, "daemon", "--db", db, "--transcript-dir", ...optional...); c.Stdout/Stderr = logfile; return startCmd(c) }` — **`startCmd` (Start, not Wait)** so it detaches; reuse the existing `startCmd`/`execCommand`/`osExecutable` seams.
- `pollHealthz` = bounded `http.Get(addr+"/healthz")` loop using `deps.after` between attempts (NOT `time.Sleep`).
- `sessionCount` = the same `/v1/sessions` fetch `status` uses.
- `replayDemo` = `runDemo`'s core (call it directly with the discovered disc).
- `after = time.After`.

**Notes for implementer (THE testability crux — Gaps/Risks #1):** `runUp` touches **only** `deps.*` and `out` — never `exec`, `http`, `time.Sleep`, or a browser directly. `up_test.go` builds `upDeps` entirely from fakes and drives the timeout via the injected `after` channel:

- *daemon already running:* `readDiscovery` ok + `pollHealthz` ok → assert `startDaemon` NOT called.
- *not running:* `readDiscovery` not-exist (first call) → `startDaemon` called → second `readDiscovery` ok → `pollHealthz` ok.
- *healthz never comes up:* `pollHealthz` returns error → `ErrDaemonUnreachable`.
- *hooks:* assert `installHooks` invoked once; its error surfaced.
- *URL/open:* assert URL printed; `--no-open` → `openBrowser` not called.
- *demo fallback:* `sessionCount` returns 0 then (after the injected `after` fires) 0 again → `replayDemo` called; returns >0 → `replayDemo` NOT called; `--no-demo` → never called.
Every seam error path is its own case. No goroutine races on wall-clock — the test closes the `after` channel to advance "time".

**Steps:**

- [ ] **Step 1: Write failing `up_test.go`** — the full case matrix above, all via fake `upDeps`. Add a `newUpCmd` smoke test (flags `--no-open`/`--no-demo` registered; `RunE` builds prod deps — exercise the builder with seam swaps for `execCommand`/`startCmd`/`openBrowser` so the prod path is covered without a real daemon, mirroring `ui_test.go`'s `startCmd` swap).
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** `up.go` (`runUp` + `upDeps` + `newUpCmd` + the prod builder).
- [ ] **Step 4: 100% coverage** for `cmd/catacomb/up.go` — the detached-`exec` builder is covered via the `execCommand`/`startCmd` swaps (the actual child never runs). `go tool cover -func=... | grep up.go | grep -v 100.0% || echo covered`
- [ ] **Step 5: Lint + cross-build + codepolicy** — confirm **no `syscall`/build-tag** crept in; `GOOS=windows go build ./...` clean.
- [ ] **Step 6: Commit** `feat(cli): catacomb up — one-command daemon+hooks+UI bring-up with demo fallback`.

---

### Task D-cli6 — Grouped cobra help (Observe / Setup / Advanced)

**Files:** Modify `cmd/catacomb/root.go`; create/extend `cmd/catacomb/root_test.go`.

**Interfaces:** in `newRootCmd`, `root.AddGroup(&cobra.Group{ID:"observe", Title:"Observe:"}, &cobra.Group{ID:"setup", Title:"Setup:"}, &cobra.Group{ID:"advanced", Title:"Advanced:"})`; set `GroupID` on every subcommand (pin mapping from the prompt): **Observe** = `up`, `ui`, `watch`, `status` (and `observe` once C lands — not in D); **Setup** = `daemon`, `install-hooks`, `env`; **Advanced** = `hook`, `ingest`, `run`, `replay`, `demo`, `version`. (`demo` is Advanced — it is a power/diagnostic aid, not a front-door verb; `up` already triggers it. `version` is Advanced.)

**Notes for implementer:** set `GroupID` where each `newXCmd()` is constructed OR right after `AddCommand` in `root.go` (pin: set it in `root.go` after each `AddCommand`, keeping the per-command constructors group-agnostic and the mapping in one readable place). Cobra renders ungrouped commands under "Additional Commands" — assigning all of them keeps help clean.

**Steps:**

- [ ] **Step 1: Write failing `root_test.go`** — `newRootCmd()` then assert: the three groups present (`root.Groups()` IDs/titles); **every** `root.Commands()` entry has a non-empty `GroupID` that is one of the three; `up`/`demo`/`status` are registered. (Optionally snapshot `root.UsageString()` contains the three titles.)
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** groups + `GroupID` assignment in `root.go`.
- [ ] **Step 4: 100% coverage** for `cmd/catacomb/root.go`.
- [ ] **Step 5: Lint + cross-build + codepolicy.**
- [ ] **Step 6: Commit** `feat(cli): grouped cobra help — Observe / Setup / Advanced`.

---

### Task D-cli7 — README quickstart block

**Files:** Modify `README.md`.

**Content (pin):** a **Quickstart** section before "Development":

```
## Quickstart

    catacomb up

One command: starts the daemon (if needed), installs Claude Code hooks,
waits until it is healthy, prints a bearer URL, and opens the web UI.
If no live Claude session shows up, it replays a bundled demo so the UI
is never empty.

    catacomb demo      # replay the bundled synthetic transcript
    catacomb status    # addr / pid / uptime / token age / session+node counts
```

Keep the existing `make` block under **Development** (it is for contributors). Do not over-document; mirror the terse house style.

**Steps:**

- [ ] **Step 1: Edit `README.md`** — add the Quickstart block; keep Development. (No Go test; the repo's `lint-docs` job covers markdown.)
- [ ] **Step 2: Verify** `make lint` / the docs lint job is happy locally if runnable.
- [ ] **Step 3: Commit** `docs: README quickstart (catacomb up / demo / status)`.

---

## Visual polish (LOWER priority — implement after CLI)

> Every FE task: write failing Vitest for the pure helper → implement → 100% on the gated module → build the component → `npm run check` → `npm run build` → commit including rebuilt `webui/dist/`. Components and `*.svelte`/`*.svelte.ts` rune wiring are NOT line-gated. Add each new pure module to `vitest.config.ts` `coverage.include`. **Light theme must not regress the dark default.**

### Task D-fe1 — Theme resolver (pure, gated)

**Files:** Create `webui/web/src/lib/theme.ts` (+ `.test.ts`). Modify `vitest.config.ts`.

**Pure module (gated 100%) — `theme.ts`:**

```ts
export type Theme = 'dark' | 'light';
export const THEME_KEY = 'catacomb-theme';
export function resolveTheme(stored: string | null, osPrefersLight: boolean): Theme;
export function nextTheme(current: Theme): Theme;
```

**Rules (pin):** `resolveTheme` — a valid stored `'dark'|'light'` wins; else `osPrefersLight ? 'light' : 'dark'`; invalid/absent stored + no OS signal → `'dark'` (the firm default). `nextTheme` toggles. Pure, no DOM. This is the only line-gated theme code (§spec §10 Q6 / prompt's "theme-resolve helper").

**Steps:**

- [ ] Write failing `theme.test.ts` (stored dark/light wins; null+OS-light→light; null+OS-dark→dark; garbage stored→dark; `nextTheme` cycles). Implement `theme.ts`. 100% gate. Add to `vitest.config.ts` include.
- [ ] Commit `feat(webui): pure theme resolver (OS-aware, persisted-override)`.

### Task D-fe2 — Light-theme tokens + toggle + store

**Files:** Modify `webui/web/style.css`; create `webui/web/src/lib/stores/theme.svelte.ts`, `webui/web/src/components/ThemeToggle.svelte`; modify `webui/web/src/App.svelte`. (Depends on D-fe1.)

**CSS (pin):** add, after the `:root` dark block (which stays the default):

- `@media (prefers-color-scheme: light) { :root:not([data-theme='dark']) { /* light token overrides */ } }` — OS-respecting default.
- `[data-theme='light'] { /* same light token overrides */ }` — explicit override.
- `[data-theme='dark'] { /* (redundant; equals :root) */ }` is unnecessary; rely on `:root`.
The light palette overrides `--bg`/`--surface`/`--surface-2`/`--border`/`--border-strong`/`--text`/`--text-dim`/`--text-faint`/`--accent`/`--glow`/`--ring` + the status/node ramps for light backgrounds (raise lightness, keep hue; the "lit excavation" warmth inverts to a warm-paper light), and `color-scheme: light`. Node-color ramp stays at even perceived brightness on the light bg. **Do not touch the `:root` dark values** — verified default-unchanged by D-fe7.

**Store (`theme.svelte.ts`, not gated):** `$state` current theme; on init, `resolveTheme(localStorage.getItem(THEME_KEY), matchMedia('(prefers-color-scheme: light)').matches)` (matchMedia behind a tiny seam param for SSR/no-window guard, mirroring `App.svelte`'s `typeof window` guards); `toggleTheme()` = compute `nextTheme`, persist to `localStorage`, set `document.documentElement.dataset.theme`. Apply the resolved theme to `dataset.theme` on init too (so an explicit choice survives reload and an OS-default user gets no attribute — letting the media query drive).

**Toggle (`ThemeToggle.svelte`, not gated):** a quiet topbar button (sun/moon glyph), `aria-pressed={theme==='light'}`, `title`, calls `toggleTheme()`. Mount in `App.svelte`'s `.topbar` (next to the conn-pill, minimalist).

**Steps:**

- [ ] Add the light-theme CSS blocks (no dark regression). Build `theme.svelte.ts` + `ThemeToggle.svelte`; mount + init in `App.svelte`. `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): light theme + persisted OS-aware toggle (dark stays default)`.

### Task D-fe3 — Graph-nav helper (pure, gated)

**Files:** Create `webui/web/src/lib/graph-nav.ts` (+ `.test.ts`). Modify `vitest.config.ts`. (Independent.)

**Pure module (gated 100%) — `graph-nav.ts`:**

```ts
import type { Node, Edge } from './types';
export type NavDir = 'left' | 'right' | 'up' | 'down';
export function nextNodeByDirection(
  currentId: string | null, nodes: Node[], edges: Edge[], dir: NavDir,
): string | null;
```

**Rules (pin):** `right` → first outgoing-edge target of `currentId` (by deterministic order: target id sort); `left` → first incoming-edge source; `up`/`down` → previous/next **sibling** (nodes sharing a parent via `parent_child`, ordered by id or `sequence`); `currentId===null` → the layout root (a node with no incoming `parent_child`, lowest id) for any dir; no candidate → return `currentId` (no-op, never null-out an existing selection); empty graph → `null`. Pure, deterministic — mirrors the Go reducer's determinism discipline so traversal is stable.

**Steps:**

- [ ] Write failing `graph-nav.test.ts` (right/left along an edge; up/down siblings; no-edge no-op; null→root; empty→null; deterministic tie-break). Implement `graph-nav.ts`. 100% gate. Add to `vitest.config.ts` include.
- [ ] Commit `feat(webui): pure graph keyboard-navigation helper (arrow traversal along edges)`.

### Task D-fe4 — Graph keyboard traversal + node roles/aria

**Files:** Modify `webui/web/src/components/GraphNode.svelte`, `webui/web/src/components/GraphCanvas.svelte`. (Depends on D-fe3.)

**`GraphNode.svelte` (pin):** add `role="button"`, `tabindex={0}`, `aria-label` = `"{type} {name} — {status}"`, `aria-current` when selected; `onkeydown` Enter/Space → `selectNode(id)`. Keep the existing click selection.

**`GraphCanvas.svelte` (pin):** `role="application"` + `aria-label="Session graph"` on the root; a keydown handler that, when an arrow key is pressed with a selection, computes `nextNodeByDirection(selectedNodeId.value, graph.nodes, graph.edges, dir)` and `selectNode(...)` the result, then ensures the focused node scrolls into view (reuse the existing `FlowInternals` `focusNodeId` mechanism, which already centers a node). Prevent default so the canvas does not also pan. This makes the graph fully keyboard-traversable along edges (prompt's deep-a11y requirement).

**Steps:**

- [ ] Wire `graph-nav` into `GraphCanvas` keydown; add roles/aria/keydown to `GraphNode`. `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): graph arrow-key traversal along edges + node roles/aria`.

### Task D-fe5 — Node-drawer focus-trap + focus-return

**Files:** Modify `webui/web/src/components/NodeDrawer.svelte`. (Independent.)

**Behavior (pin, pre-flight #10):** on open (`isOpen` false→true), record `document.activeElement` as the return target and move focus to the drawer's first focusable (the close button or the title region with `tabindex=-1`); add `aria-modal` semantics is **not** appropriate (the drawer is non-modal/`role="complementary"` — keep it complementary but make it a focus scope). A `keydown` Tab/Shift+Tab handler **traps** focus within the drawer while open (cycle first↔last focusable). On close (ESC or button), restore focus to the recorded return target (the triggering graph node), so keyboard users are not dumped at the top. Use native `HTMLElement.focus()`; query focusables within the drawer root. No new deps.

**Notes for implementer:** the existing ESC handler stays; extend the same `$effect(() => { if(!isOpen) return; ... })` block to add the focus capture/return and the Tab trap, cleaning up listeners on close. Guard `typeof document` for SSR-safety as elsewhere.

**Steps:**

- [ ] Implement focus-trap + focus-return in `NodeDrawer.svelte`. `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): node-drawer focus-trap + focus-return for keyboard a11y`.

### Task D-fe6 — `--text-faint` contrast bump

**Files:** Modify `webui/web/style.css`. (Independent, tiny.)

**Change (pin):** raise `--text-faint` lightness in the dark `:root` (currently `oklch(0.55 0.010 80)` on `--bg oklch(0.16 ...)`) to clear WCAG AA for the small-text uses it has (metric `—`, hints, conn-pill). Pick the smallest bump that reaches ≥4.5:1 against `--bg`/`--surface` (e.g. `~0.62`), and set the light-theme `--text-faint` to clear AA against the light bg too. Verify the contrast ratio (any OKLCH→sRGB contrast check) and note the computed ratio in the commit body.

**Steps:**

- [ ] Bump `--text-faint` (dark + light); verify ≥4.5:1. `npm run build`; commit incl. `dist/`. `fix(webui): bump --text-faint to meet WCAG AA contrast`.

### Task D-fe7 — Playwright a11y + theme e2e (and dark-default guard)

**Files:** Create (or fold into existing) `webui/web/e2e/onboarding.spec.ts`, using `page.route()` mocks (the established hermetic pattern — no live daemon). (Depends on D-fe2/fe4/fe5.)

**Coverage (pin):**

- **Theme:** toggle flips `documentElement.dataset.theme` and a representative `getComputedStyle(--bg)` changes; reload persists the choice (localStorage); with `colorScheme:'light'` emulation and no stored choice, the light palette applies; **dark default unchanged** — with no stored choice and dark OS, `--bg` equals the known dark value (the regression guard).
- **Drawer a11y:** open a node → focus is inside the drawer; Tab cycles within; ESC → focus returns to the triggering node.
- **Graph traversal:** select a node, press ArrowRight → an edge-adjacent node becomes selected.

**Steps:**

- [ ] Write the specs against `page.route()` mocks for `/v1/sessions`, `/v1/sessions/{hash}/graph`, `/v1/subscribe` (reuse the existing mock helpers). `npm run test:e2e` green locally.
- [ ] Final: full Go suite + cross-build + codepolicy + FE vitest gate + `dist`-drift clean. Commit `test(webui): e2e for theme persistence, drawer focus, graph traversal, dark-default guard`.

---

## Self-Review

### Spec §8 Coverage Table

| Spec §8 sub-project | Requirement | Covered by | Gated vs component |
|---|---|---|---|
| CLI: `catacomb up` | start daemon if needed (detached+discovery) → idempotent hooks → /healthz preflight → print bearer URL → open UI → demo fallback if no live session in N s | D-cli5 (`up.go`, `upDeps` seams) + D-be1 (demo ingest) | Go 100% (seam-injected) |
| CLI: `catacomb demo` | bundled/seeded synthetic transcript | D-cli4 (`demo.go` + embedded `testdata/demo.jsonl` + D-be1 route) | Go 100% |
| CLI: `catacomb status` | addr/pid/uptime/token-age/session+node counts | D-cli3 (`status.go`) + D-cli1 (discovery pid/started_at) | Go 100% |
| CLI: typed error sentinels | human messages ending in the EXACT remediation command | D-cli2 (`errors.go`, sentinels + `renderErr`) | Go 100% |
| CLI: grouped cobra help | Observe / Setup / Advanced | D-cli6 (`root.go` groups + `GroupID`) | Go 100% |
| CLI: README quickstart | quickstart block | D-cli7 (`README.md`) | docs lint |
| CLI: keep `?token=` | cookie handshake deferred (§10 Q7) | unchanged (`runUI` URL build reused) | n/a |
| CLI: bundled demo transcript | richer than testdata — turns, subagent, tool+mcp, an error | D-cli4 (`testdata/demo.jsonl`, `sessionId:demo-0001`) | n/a (data, parses) |
| Polish: light theme + persisted toggle (respect OS, dark default) | OKLCH `[data-theme]`/media override + resolver + toggle | D-fe1 (`theme.ts`) + D-fe2 (CSS + store + `ThemeToggle`) | `theme.ts` gated 100%; CSS/component not gated |
| Polish: illustrated empty/loading/error states | empty-state glyph + headline/hint | **already present** (`style.css` `.empty-state*`, `GraphCanvas` empty state, `App`/`SessionView` load states) — D verifies, no regression | n/a |
| Polish: deep a11y — drawer focus-trap+return | focus capture/return + Tab trap | D-fe5 (`NodeDrawer.svelte`) | component (Playwright) |
| Polish: deep a11y — graph arrow-key traversal | arrow keys along edges | D-fe3 (`graph-nav.ts`) + D-fe4 (`GraphNode`/`GraphCanvas`) | helper gated 100%; components not |
| Polish: deep a11y — roles/aria | node/canvas roles + labels | D-fe4 | component |
| Polish: `--text-faint` contrast bump | WCAG AA | D-fe6 (`style.css`) | n/a (verified ratio) |
| Polish: favicon/title/wordmark + conn-pill silence | already done | **verified done** (`style.css` wordmark/conn-pill, `App.svelte` conditional pill) — no work | n/a |

### Testability seams for `catacomb up` / `status` / `demo` (summary)

- **`catacomb up`** is the hard one and is fully unit-testable via the pinned `upDeps` struct: `runUp` touches only `deps.*` + `out`. Tests inject fakes for `readDiscovery`/`startDaemon`/`installHooks`/`pollHealthz`/`sessionCount`/`openBrowser`/`replayDemo` and drive the N-second fallback through an injected `after func(time.Duration) <-chan time.Time` channel they close on demand — **no real daemon, no real browser, no `time.Sleep`** (forbidigo-safe). The production `RunE` builds `upDeps` from the existing `execCommand`/`startCmd`/`osExecutable` seams (detached `Start()`, not `Wait()`), and the prod builder path is covered by swapping those vars (exactly as `ui_test.go` swaps `startCmd`), so the child is never really spawned.
- **`catacomb status`** injects `readDiscovery` + `fetchSessions` + `now`; tests run against an `httptest` daemon (real `d.Handler`) or pure fakes. pid/uptime/token-age come from discovery (`pid`/`started_at`), avoiding a new endpoint and a second fetch; counts come from `/v1/sessions`. The "stale discovery / dead daemon" partial-output path is its own test.
- **`catacomb demo`** injects `readDiscovery` + `post` + the `transcript` bytes; tests POST the embedded JSONL to an `httptest` daemon and assert the demo session is queryable. The detached daemon is never needed because the test provides the server.

### Bundled demo-transcript approach (summary)

- A **new** committed `cmd/catacomb/testdata/demo.jsonl` in Claude Code JSONL shape (the format `ingest/jsonl` parses), **embedded** via `//go:embed` so `catacomb demo` works from an installed binary with no files on disk. Richer than the 5-line `testdata/session.jsonl`: multiple turns, a subagent (`isSidechain`+`parent_tool_use_id`), Bash + Read tool calls, an MCP (`mcp__*`) call, and an `is_error:true` result, with strictly increasing timestamps and a stable `sessionId:"demo-0001"` for a deterministic deep-link.
- It ingests over the **running daemon** via the new bearer-gated `POST /v1/transcript` (D-be1, symmetric with `POST /v1/stream-json`), so it appears in the live sessions list and exercises the real reconciliation core — not a throwaway `replay` DB. This is the cleanest reuse: one tiny additive route vs. coupling demo to a tailer-configured dir.

### Gaps and Risks

1. **Making `catacomb up` unit-testable without a real daemon/browser is the headline risk — mitigated by the `upDeps` seam struct.** The orchestrator is a pure composition over injected functions + an injected `after` channel; the *only* code that touches real processes/HTTP/clock lives in the prod `upDeps` builder, itself covered via the existing `execCommand`/`startCmd`/`openBrowser` var-swaps (no child actually runs). There is **zero** `time.Sleep` (forbidigo) — the timeout is a channel seam. Residual risk: the prod builder's detached-`exec` line is exercised but its real-world detach behavior (does the daemon truly outlive `up`?) is **not** asserted by a unit test (we never spawn it). Mitigation: an optional, build-tagged or `-run`-gated **integration** smoke (not in the 100% unit gate) can spawn a real `catacomb up` against a temp dir and assert healthz; keep it out of the coverage gate to honor no-sleep/no-real-process in units. Document as a known coverage-vs-realism boundary.

2. **Detaching the daemon cross-platform without `syscall.SysProcAttr`.** Pin: `exec.Command(self,"daemon",...).Start()` and let `up` exit; the child is reparented (to init/launchd/Windows session) and keeps running — sufficient for the first-run UX on darwin/linux/windows, and `GOOS=windows go build` stays clean with **no** platform build tags. Risk: on some setups a child may receive SIGHUP when the parent terminal closes; if that proves flaky in practice, a follow-up can add build-tagged `setsid`/`CREATE_NEW_PROCESS_GROUP` files — explicitly **deferred** (it would split the file by GOOS and complicate the 100% gate; not needed for the milestone's acceptance). The daemon also already self-manages discovery + signal handling, so a second `up` safely detects the running one via discovery+healthz and does not double-start.

3. **`catacomb status` freshness / stale discovery.** Discovery is a file; a crashed daemon leaves a stale `daemon.json` with a dead `addr`/`pid`. `status` shows addr/pid/uptime/token-age from the file but marks `sessions`/`nodes` `unavailable` when `/v1/sessions` is unreachable — an honest partial, not a hang. token-age is derived from `started_at` (the token is minted at start), which is correct unless a future change rotates tokens at runtime (none planned). pid liveness is not verified (no `os.FindProcess`/signal probe — that is platform-nuanced and out of scope); the unreachable-sessions signal is the practical liveness tell. Acceptable for D.

4. **Demo ingestion route vs. alternatives.** `POST /v1/transcript` is a new (small, additive, 100%-tested) authenticated surface. The considered alternatives — (a) reuse `replay` (rejected: builds a throwaway DB, never reaches the live daemon), (b) drop the demo file into a `--transcript-dir` for the tailer (rejected: couples demo to a tailer-enabled daemon and a filesystem race) — are worse. The route mirrors the existing `POST /v1/stream-json` exactly (scanner, recover, always-200, bearer-gated), so it adds no new error model and honors ADR-0013 (loopback + bearer; POST mutation uses header auth, not query-token). The demo's payloads are synthetic (no secrets), so the SSE payload-strip and the default-off content endpoint are unaffected.

5. **Light-theme token strategy / no dark regression.** Light = a token **override** layer (`[data-theme='light']` + an OS media query scoped to `:root:not([data-theme='dark'])`), never a rewrite of `:root`. Components consume `var(--*)` already (verified), so **no markup changes** for the palette. The firm dark default is protected by (a) leaving `:root` untouched and (b) a Playwright assertion that `--bg` equals the known dark OKLCH when no theme is chosen and the OS is dark. Risk: the node-color ramp must stay legible on a light bg — addressed by raising lightness while preserving hue and re-checking even perceived brightness; if any node hue washes out, nudge its lightness in the light block only. The `--text-faint` bump (D-fe6) must clear AA in **both** themes — verified per-theme.

6. **A11y focus-management correctness.** The drawer is `role="complementary"` (non-modal) — D adds a focus **trap while open** + **return on close** without making it a true `aria-modal` dialog (which would over-claim and hide the rest from AT). Risk: trapping focus in a non-modal region can surprise screen-reader users; mitigation: the trap is active only while `isOpen`, ESC always exits and returns focus, and the graph remains reachable. Graph arrow-traversal relies on the pure `graph-nav.ts` (gated 100%) being deterministic; the component wiring (focus + scroll-into-view via existing `FlowInternals`) is Playwright-covered, not line-gated (consistent with the A/B component policy). Svelte Flow's own focus handling may compete with our keydown — `preventDefault` on handled arrows and scoping the handler to the canvas mitigates double-handling.

7. **No new deps, both sides.** Backend/CLI add only stdlib (`os/exec`, `embed`, `text/tabwriter`, `context`, `errors`, `time`) + in-tree cobra/testify. Frontend adds zero npm deps (theme = CSS + `localStorage`/`matchMedia`; a11y = native focus APIs; demo/e2e use existing `page.route()` mocks). If a reviewer pushes for a TUI-style spinner lib or a CSS framework, resist — it violates the minimalism principle and the bundle-size/dist-drift posture.

### Placeholder / TODO scan

No load-bearing TODOs. Pinned decision points with stated defaults: (a) `catacomb up`'s demo fallback is **non-interactive** (auto-replay + a note, `--no-demo` to opt out) because an interactive prompt is hard to unit-test and the owner wants a populated first-run UI — Gap-free, documented; (b) daemon detach uses plain `exec.Start()` without platform build tags, with a build-tagged `setsid`/process-group follow-up explicitly deferred (Gap #2); (c) `status` does not probe pid liveness, using the sessions-fetch reachability as the liveness tell (Gap #3); (d) demo ingests via a new `POST /v1/transcript` rather than `replay`/tailer (Gap #4). All explicit, none hidden.

### Determinism / coverage / no-sleep check

- **No reducer change in D** — the demo flows through the existing deterministic reducer via `d.IngestTranscript`; no new NodeType/EdgeType/Status.
- **No `time.Sleep` anywhere** — `up`'s timeout is an injected `after` channel; `pollHealthz`/`sessionCount` are seam functions; tests drive "time" by closing the channel; production may use `time.After`/`time.NewTicker` (never `Sleep`), matching the daemon's existing loops.
- **Go 100% under `-race`** gates every backend/CLI task (`up`/`demo`/`status`/`errors`/`root`/`daemon`/`transcript`/`discovery`); the detached-`exec` line is covered via `execCommand`/`startCmd` swaps (no real child); `internal/codepolicy` + `GOOS={windows,linux,darwin} go build ./...` gate each.
- **FE:** all branching (theme resolution, graph nav) lives in plain `.ts` gated 100% (`theme.ts`, `graph-nav.ts`); components and `.svelte.ts` are thin and excluded; the drawer/graph/theme wiring is Playwright-covered. The `dist`-drift + vitest gate + Playwright gate end every FE task. **Light theme must not regress the dark default** — enforced by the D-fe7 dark-`--bg` assertion.
