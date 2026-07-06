# UX overhaul — session-first observability design

**Status:** Superseded by [ADR-0026](../adr/0026-form-factor-pivot-offline-eval-gate.md) (2026-07-06) — the web UI and TUI this spec designs were deleted in PV-3; historical record only
**Date:** 2026-06-24
**Supersedes (UI only):** [`2026-06-22-m5-webui-design.md`](2026-06-22-m5-webui-design.md) — the vanilla-JS/SVG web UI it shipped is deleted and rebuilt; the M5 Go serve path, `authedAllowQuery`, and `catacomb ui` survive.
**Consumes:** the M1c CDC bus (`cdc.Bus`/`Consumer`/`GraphDelta`), the M3a SSE subscription core (`daemon.SubFilter`/`Subscribe`/`Subscription`), the deterministic reducer (`reduce.Graph`), the loopback+bearer trust boundary (ADR-0013), the redaction policy (ADR-0008/ADR-0020).

## 1. Summary

Catacomb is backend-complete and UI-broken: the daemon, reducer, persistence, and exporters work and are well-tested, but the web UI is a placeholder that shipped unvalidated (the M5 plan's own "best-effort, operator-verified" deferral) and fails every part of the owner's job-to-be-done — observe one Claude session by its hash, watch its graph live, click any node, read tokens and cost. This document specifies the rework: a **full rewrite of the web UI** (Vite + Svelte 5, built `dist/` committed and `go:embed`-served), a **new bubbletea TUI** for the same observe-flow, **backend foundations** (session-by-hash API, hybrid pricing engine, duration stamping, deterministic delta ordering, an authorized content endpoint), and **CLI onboarding polish**. The work is organized as a **thin vertical slice first** (Milestone A — the hero "open the UI → find a session → click a node → see tokens/cost" flow on honest foundations), then three deepening milestones. Every backend change stays pure Go, no cgo, comment-free, 100% covered TDD-first, with the deterministic-reducer contract preserved.

## 2. Diagnosis

The through-line: **the UI is built around the wrong primary entity and the wrong rendering model**, and that surfaces as the owner's four complaints.

### 2.1 The owner's complaints, mapped to causes

| Complaint (verbatim) | Root cause in the code |
|---|---|
| **"ничего не кликается"** (nothing is clickable) | Selection bumps a node's SVG stroke 1.5px→3px (imperceptible at scale) and the only detail surface is a *separate* "Inspector" tab guarded by `if (activeView === inspector)`. From the default Graph view a click produces zero on-screen response. Before/after screenshots of clicking Bash are pixel-identical. |
| **"непонятно как пользоваться бинарём"** (unclear how to use the binary) | No one-command path: ~5 undocumented manual steps (build → daemon → install-hooks → generate/replay activity → ui). The most common first-run state (no daemon) surfaces a raw wrapped Go error (`daemon.ReadDiscovery read: open …: no such file or directory`); `catacomb ui` then opens a browser to a dead address. README documents only `make` targets. |
| **"всё лагает"** (everything lags) | Every SSE delta mutates in-memory `Map`s, then **rebuilds the entire SVG as a string**, assigns `innerHTML`, re-attaches a click listener to every node group, and re-runs full BFS layout from scratch (`webui/web/app.js`). No diffing, no virtualization, no stable identity; a real session blows past 16 ms/frame and reshuffles nodes under the cursor. |
| **observe by hash + tokens + price** | The owner thinks in **Claude session hashes**; the UI exposes an internal **"Runs"** concept and has no session entry point, no search box, no per-session scope. The SSE snapshot front-loads the union of *every* graph in the daemon. Cost is never computed. Tokens exist but are buried in the hidden tab behind raw ULIDs and sha256 hashes. |

### 2.2 The mental-model crux: session hash vs execution id

The single most important backend fact (`model/ids.go`, `daemon/daemon.go`):

- `executionID` is an internal ULID, one per ingestion scope. `d.graphs` is keyed by `executionID`.
- The **session node id** is `"session:" + executionID` (`model.SessionNodeID`) — **not** the Claude session hash.
- The **Claude session hash** lives in `Correlation.SessionID` and `Run.SessionIDs`.
- A **Run** can span multiple sessions (forest model); in the replay demo `run_id` happened to equal the session hash `"s1"`, which masked the distinction.

The daemon already keeps `d.execBySession map[string]string` (Claude session hash → executionID, populated from `sessionIDOf(payload)` / stream-json `session_id`). This is exactly the index the session-by-hash API needs — but it is a private field with no accessor, no SSE-filter wiring, and one consistency caveat (§5.1.1).

### 2.3 Genuine backend / reducer bugs the critique found

These are correctness bugs, not polish, and Milestone A fixes the ones on the hero path:

1. **`cost_usd` is never populated.** `reduce.applyTokens` sets only `TokensIn`/`TokensOut`. `Node.CostUSD` exists in the model and the old inspector tries to render it, but nothing ever writes it; `cost_usd` only appears as a raw cumulative `attrs["cost_usd"]` from stream-json. The most-requested number renders nowhere. (Fixed in A: §5.2.)
2. **`duration_ms` / `t_end` unset for most node types → false timeline.** `t_end` is stamped only for `session_end` and `subagent_stop`; `tool_call`/`mcp_call`/`assistant_turn` nodes never get an end stamp on their end observation, so every timeline bar spans start→now and renders identical full-width. (Fixed in A: §5.3; the waterfall view that consumes it is B.)
3. **`node_status`-before-upsert clobber + rev-dedup drop.** A `node_status` delta arriving before `node_upsert` stores the status-only partial (id/status/rev/t_end) as the canonical node; when the real upsert arrives, a client `existing.rev >= ev.rev` guard can drop it, leaving a node permanently missing name/type/tokens/attrs. The old client reducer has zero tests, so this shipped unnoticed. (Fixed in A on the client: §6.2.)
4. **Nondeterministic coalesce flush order.** `cdc.Consumer.deliver` drains the `dirty` coalesce map with `for k := range c.dirty`, i.e. **Go map-iteration order** — deltas are not rev-ordered on flush, defeating any client monotonic-rev assumption and making realtime feel untrustworthy. This is the server-side root cause of (3). (Fixed in A: §5.4.)
5. **SSE reconnect re-downloads the full snapshot.** The server emits `id: <rev>` but never reads `Last-Event-ID`, and the client never sends it, so every `EventSource` reconnect (sleep, network blip) replays the entire snapshot — a render-storm on each reconnect. (Fixed in B: §7.)

## 3. Target experience

### 3.1 Web

A developer opens the UI and lands on a **searchable Sessions list** keyed on the Claude session hash, showing per-session status, duration, tokens in/out, cost, tool count, and error count, sortable for triage. Pasting or clicking a hash opens that one session at a **stable, shareable URL** (`/s/{hash}`, `/s/{hash}/n/{nodeId}`) that survives reload and supports back/forward. The session view is **master-detail**: the center is the session graph laid out by dagre/ELK (rankdir LR, crossing minimization), rendered via Svelte Flow with pan/zoom/fit-to-view/minimap, opening already framed and centered. Clicking any node anywhere produces an **instant (<100 ms), unmistakable selection highlight everywhere** and opens an **in-place right-hand detail drawer** that leads with a metric header (status pill, duration, tokens in→out, cost with a reported/estimated badge, model), with raw ids/hash/sources collapsed under "Advanced". Realtime updates are **incremental** — one node's status/token change mutates only that node, no relayout, no SVG teardown — via a normalized `byId` store. Visuals reach the Langfuse/Sentry bar: a two-typeface system (Inter for UI, mono only for ids/code), OKLCH design tokens, dark theme. The acceptance test: a screenshot could sit next to Langfuse in a slide deck without looking like a prototype.

### 3.2 TUI

`catacomb observe <session-hash>` opens a bubbletea master-detail viewer over the **same SSE feed and the same session→graph→node model** as the web UI. Left: a live Sessions list (hash, status dot, node count, tokens, cost, wall-time). Center: the session's graph as an **expandable indented tree** (parent_child → hierarchy, sequence → ordering — a true 2D DAG in a terminal is a trap), k9s/lazygit-style. Right: a node detail pane (type, status, duration, tokens in/out, cost, opt-in content), always showing the metric labels (`—` when unknown) and tucking raw ids behind a debug toggle. Keys: `j`/`k` move, `enter` drill, `/` filter, `ESC` back out; live updates apply in place without flicker.

### 3.3 Backend

Session is a **first-class addressable entity**. A `?session=` filter on `/v1/subscribe` matches `Correlation.SessionID`; `GET /v1/sessions` returns a sortable list with per-session aggregates; `GET /v1/sessions/{hash}/graph` returns one scoped graph. A **hybrid pricing engine** populates `Node.CostUSD` (Claude-reported when present, else a versioned model-price table with cache tiers) and rolls up to session/run totals, distinguishing reported vs estimated. `duration_ms`/`t_end` are stamped by closing tool/mcp/assistant nodes on their end observation. An authorized, bearer-gated, redaction-aware content endpoint returns payloads by node id / payload_hash, default off. The coalesce flush is deterministically rev-ordered, and `Last-Event-ID` resume replays only deltas past the client cursor. CLI gains `catacomb up`, `catacomb observe <hash>`, `catacomb status`, grouped help, and typed error sentinels with copy-pasteable remediation.

## 4. Cross-cutting decisions & constraints

### 4.1 Confirmed product decisions (settled — not re-opened)

1. **Scope:** rework the web UI (full rewrite) + add a full TUI + backend foundations + CLI onboarding polish.
2. **Frontend stack:** Vite + Svelte 5; built `dist/` committed to git and embedded via `go:embed` (the `webui.Handler()` / `http.FileServerFS` model is unchanged). The old vanilla `webui/web/{index.html,app.js,style.css}` is **deleted entirely** — clean rebuild, no patching, no quick-wins on the old UI.
3. **Coverage:** Go backend stays **100% coverage, TDD-first** (unchanged). Frontend: **100% coverage on pure logic** (client reducer, stores, pricing/format helpers) via Vitest; **Playwright e2e** for the key flows; Svelte components are **not** line-coverage-gated. CI enforces the logic-coverage gate and runs e2e.
4. **Graph scale:** target **≤ ~2,000 interactive nodes** → Svelte Flow (`@xyflow/svelte`) with **dagre or ELK** layered layout (rankdir LR, crossing minimization), pan/zoom/fit/minimap. A thin render abstraction keeps a future WebGL/Canvas engine possible, but it is **not** built now.
5. **Pricing:** **hybrid** — use Claude-reported cost when present (stream-json `total_cost_usd` / per-message cost), else compute from a **versioned Go model-price table** (input/output + cache-read/write tiers). Populate `Node.CostUSD`, tag reported-vs-estimated, roll up to per-session and per-run totals. Unknown model id → "estimate unavailable", never crash.
6. **Payload/content:** an **authorized (bearer-gated), redaction-aware, default-OFF** endpoint returns node payload by node id / payload_hash; UI shows Input/Output opt-in. SSE keeps stripping payloads (`daemon/sse.go` `n.Payload = nil`).
7. **Primary entity = Session** (Claude session hash, `Correlation.SessionID`). Run is demoted to a secondary grouping, shown only when a run genuinely spans multiple sessions. User-facing **"Runs" → "Sessions"**.
8. **Approach:** thin vertical slice first, built on honest (non-throwaway) foundations, then deepen.

### 4.2 Non-negotiable repo constraints (binding on every backend change)

- **Pure Go, no cgo**, single static cross-platform binary; SQLite via `modernc.org/sqlite`. `GOOS=windows go build ./...` clean.
- **No comments in Go** except `//go:build`, `//go:embed`, `//go:generate`; generated-header files are skipped. Enforced by `internal/codepolicy`. Any Go shown in this spec or in plans is illustrative and must be written comment-free.
- **100% line coverage under `-race`**, TDD-first; the threshold never goes down. Untestable code is a refactoring signal, not an exclusion.
- **Deterministic reducer:** same observations, any order → same graph. Every reducer change preserves this and is proven by a determinism test.
- **Dependency inversion** (consumer declares the interface), no global mutable state, no `init()` side effects, sentinel errors checked with `errors.Is`/`errors.As`, `log/slog` JSON, never log/serialize secrets, `context.Context` first for I/O, `gofumpt`+`goimports` (local prefix `github.com/realkarych/catacomb`), **no `time.Sleep` in tests** (forbidigo).
- **Loopback + bearer trust boundary** (ADR-0013): all TCP/realtime surfaces are loopback-only and bearer-gated; static assets are open, data is gated.
- **Frontend deps** are added via the JS toolchain only; **never `go mod tidy`** for them, and they never enter the Go module graph (the embedded `dist/` is data).

## 5. Milestone A — the hero vertical slice

**Goal.** From a built binary, a user opens the UI, finds a session by hash, watches its graph live, clicks any node, and instantly sees tokens in→out, cost, duration, and model — on foundations that are not throwaway. This is the minimum that makes the product do its one job.

**Acceptance (whole milestone).** From a built binary: the user opens the UI and sees a **searchable Sessions list** with status / duration / tokens / cost per session; clicks a row **or pastes a session hash** and gets **that session's realtime, scoped graph**, fit-to-view; clicks **any** node and instantly sees an **inline drawer** leading with tokens in→out + cost (+reported/estimated badge) + duration + model; realtime updates are **incremental** (no full re-render); the **deep-link URL restores state** on reload/back/forward; **Go 100%** + **frontend logic 100%** + **e2e green**; and it **does not look like a prototype**.

**Milestone A explicitly EXCLUDES** (these are B/C/D): content/payload viewing; the timeline waterfall; the TUI; light theme; full chrome polish (illustrated empty states, connection pill, favicon/wordmark beyond the minimum); SSE `Last-Event-ID` resume; and `catacomb up` / `demo` / `status`.

### 5.1 Backend (a) — session-by-hash API

Three additions, all under `d.mu`, reusing the M3a subscription core and the `daemon/subscribe.go` matcher shape.

**`?session=` SSE filter.** Extend `SubFilter` with a `SessionID string` field and `parseSubFilter` to read `?session=`. The filter matches on the **Claude session hash**, not the execution id, so it cannot be a simple `delta.RunID` compare. Two parts:

- **Snapshot** (`SubscribeFiltered`, which iterates `d.graphs` by `execID`): include a graph's deltas only if that execution belongs to the requested session — i.e. its `Run.SessionIDs` contains `f.SessionID` (equivalently, `d.execBySession[f.SessionID]` resolves to that `execID`).
- **Live stream** (`matchDelta`): `GraphDelta` carries `ExecutionID` but **not** the session hash. Resolve once at subscribe time to the set of execution ids for the session and match `delta.ExecutionID ∈ set`; or stamp the session hash onto the delta envelope. The execution-set approach needs no wire change and is preferred for A.

#### 5.1.1 The session-hash ↔ execution-id mapping (resolve explicitly)

`d.execBySession` is the authoritative index, but it has a consistency caveat to resolve in A:

- `ingestLocked` writes `d.execBySession[sessionIDOf(payload)] = execID` — keyed on the **Claude session hash**. Correct.
- `Recover` writes `d.execBySession[o.RunID] = o.ExecutionID` — keyed on the **run id**, not the session hash. In the replay demo `run_id == "s1" == sessionHash`, so the two agree by accident; for real data where a run id is a ULID, recovery would index by the wrong key and a `?session=<hash>` filter would miss recovered graphs.

**Resolution (A):** make the index honestly session-keyed everywhere. Build a single authoritative lookup, exposed via a daemon accessor (the consumer declares the interface), e.g. `executionsForSession(hash) []string` derived from each graph's `Run.SessionIDs` (the canonical source) rather than the dual-written map; or fix `Recover` to key on `Correlation.SessionID`. Either way the snapshot, the live filter, `GET /v1/sessions/{hash}/graph`, and the list endpoint all consult **one** session→executions function. This is a prerequisite for every session-scoped surface and must land with a determinism/recovery test (replay → recover → `?session=` returns the same scoped graph).

**`GET /v1/sessions`** (bearer-gated, header or `?token=` so the SPA can fetch it). Returns a JSON array of per-session summaries: session hash, status (rolled up), started/ended, duration, tokens in/out, cost (with reported/estimated provenance), node count, tool count, error count, model id, and the run ids it participates in. For A the aggregate may be the **minimum the list table needs** (status, duration, tokens, cost, counts); the richer rollups are B. Computed from the in-memory graphs under `d.mu` (no new store query is strictly required for A — see §10 for the deferred-aggregate question).

**`GET /v1/sessions/{hash}/graph`** (bearer-gated). Returns the scoped graph as the same `node_upsert`/`edge_upsert` delta envelope the SSE snapshot uses, so the SPA applies the REST bulk fetch and the SSE tail through one code path. Scope is the union of the session's executions via the §5.1.1 function. Unknown hash → 404. This is the REST snapshot the SPA loads first; SSE `?session=` is then the live tail.

### 5.2 Backend (b) — hybrid pricing engine → `Node.CostUSD` + session totals

A new pricing package (consumer-declared interface; the reducer depends on the abstraction, not the table). Behavior:

- **Reported-first.** When Claude reports cost (stream-json `total_cost_usd` cumulative and/or per-message cost), attribute it to the node and tag `provenance = reported`. The cumulative `total_cost_usd` is a session-level running total; per-node attribution uses per-message cost where available, else the session total rolls up at the session level only (documented; never double-count).
- **Estimate fallback.** Otherwise compute from a **versioned Go model-price table** keyed by model id, with input / output / cache-read / cache-write tiers, multiplied by the node's token counts; tag `provenance = estimated`.
- **Unknown model id → "estimate unavailable"**: leave `CostUSD` nil, surface the state in the UI, never crash.
- Populate `Node.CostUSD`; roll up to per-session and per-run totals (the latter consumed by §5.1's `GET /v1/sessions`).

The table lives in Go, versioned, updated by manual PR (§10). Pricing is a **pure function** of (model id, token tiers, reported cost) so it is trivially 100%-testable and preserves reducer determinism. Provenance (reported vs estimated) is carried on the node (e.g. an attr or a typed field) so the UI badge and the rollup can distinguish them.

> Pricing model facts (model ids, per-token tiers, cache-read/write rates) MUST be sourced from the current Anthropic pricing reference when the table is authored, not from memory — the `claude-api` skill is the canonical source.

### 5.3 Backend (c) — duration / `t_end` stamping

Close `tool_call` / `mcp_call` / `assistant_turn` nodes on their **end observation** so `TEnd` and `DurationMS` are populated deterministically (today only `session_end` and `subagent_stop` stamp `TEnd`). The end signal already exists in the reducer's inputs: `tool_result` carries the terminal status for a tool/mcp call (`applyTool` sets status from `attrs["status"]` but not `t_end`); assistant turns have a final usage/stop. On the terminal observation, set `n.TEnd = &eventTime` and `n.DurationMS = (t_end - t_start)` when both are known, using the same source-precedence discipline as `stamp` so out-of-order observations still converge. Determinism is preserved (the latest end observation by precedence wins) and proven by a reorder test. The waterfall that consumes durations is B; A only stamps them (they also feed the drawer's duration metric).

### 5.4 Backend (d) — deterministic rev-ordered coalesce flush

In `cdc.Consumer.deliver`, the `dirty` coalesce map is drained with `for k, pending := range c.dirty` — **Go map-iteration order**, which is randomized. Order the dirty entries by `Rev` (then by a stable tiebreak — coalesce key) before draining, so flushed deltas are monotonic-rev. This removes the nondeterministic flush that defeats the client's rev assumption and is the **server-side root cause** of the partial-node clobber (§2.3 #3). Pure change to `deliver`; covered by a test asserting flush order is rev-sorted regardless of insertion order, preserving the bus's existing coalesce-drop semantics.

### 5.5 Frontend (e) — Vite + Svelte 5 foundation + `go:embed` + CI + baseline design system

- **Toolchain.** Vite + Svelte 5 (runes) under `webui/` (e.g. `webui/web/` sources, `webui/dist/` build output). The Go `webui.Handler()` / `http.FileServerFS` model is **unchanged**; only the `go:embed` target moves to the built `dist/`. The built `dist/` is **committed** (data, not Go; skipped by `internal/codepolicy` and excluded from the Go coverage gate already). Delete the old `web/{index.html,app.js,style.css}`.
- **CI.** A frontend job runs **Vitest** with a **100% coverage gate on pure-logic modules** (reducer, stores, pricing/format helpers) and **Playwright e2e** for the hero flow; both must be green to merge, mirroring the Go bar. A build step regenerates `dist/`; CI verifies the committed `dist/` is up to date.
- **Design system (dark only in A).** Inter for UI text, a monospace face **only** for ids/hashes/tokens/code. An OKLCH design-token layer (`--bg`/`--surface`/`--border`/`--text`/`--accent` + a semantic node-color ramp at even perceived brightness). A defined type scale. Dark theme only (light is D). This is the visual floor that makes A "not a prototype"; full chrome (illustrated states, connection pill, wordmark, favicon set) is D, but a correct title and a single embedded favicon are included to kill the 404 and the lowercase tab.

### 5.6 Frontend (f) — pure tested reducer + normalized `byId` store (fixes the clobber/rev bug)

A framework-free `reduce.ts` feeding a **normalized `byId`** store (nodes and edges keyed by id), the single source of truth for every view. The fix for §2.3 #3:

- `node_status` is merged as a **field-wise patch** (status, t_end, cancel_cause…) that **never establishes node identity** and is **always overwritten by a full `node_upsert`**, regardless of rev. A status patch arriving before the upsert seeds only those fields; the later upsert fills name/type/tokens/attrs and is never dropped.
- `node_upsert` applies rev-guarded against other upserts only (not against status patches), so monotonic-rev de-dup is safe now that the server flushes rev-ordered (§5.4).
- A **Vitest property test** proves **same-deltas-any-order → same-state**, mirroring the Go reducer's determinism contract. This is the most correctness-sensitive frontend code and is in the 100%-logic-coverage gate.

### 5.7 Frontend (g) — Sessions-list landing + client routing / deep-links

- Landing surface is the **searchable Sessions list**, backed by `GET /v1/sessions`, with the columns the acceptance test names (status / duration / tokens / cost; counts as available). Search/paste a hash to jump straight to a session.
- **Client routes** `/s/{hash}` (session view) and `/s/{hash}/n/{nodeId}` (session view with a node selected). A hash-router suffices (local-first); reload, back/forward, and copy-link all work, and the active scope is reflected in the URL. (Structured filters and `?view=` are B.)

### 5.8 Frontend (h) — graph via Svelte Flow + dagre/ELK

- Replace the BFS/`innerHTML` renderer with **Svelte Flow** (`@xyflow/svelte`) + **dagre or ELK** layered layout (rankdir LR, crossing minimization). Pan / zoom / **fit-to-view** (on load and on session switch) / minimap / controls come for free.
- **Incremental updates:** a node's status/token change updates exactly that node in place — layout is recomputed only on topology change, not on every delta.
- `parent_child` drives layering; `sequence` / `data_dep` render as secondary styled edges so **no node is orphaned** (the old BFS dropped the `user_prompt` node).
- **Unmistakable selection** that highlights the node everywhere (graph + drawer) via the shared selection store: accent outline + fill change + cursor/hover affordance; ESC to deselect. (Full keyboard-traversal a11y is D; basic focusability ships with Svelte Flow.)
- A **thin render abstraction** wraps the engine so a future WebGL/Canvas backend is swappable; not built now.

### 5.9 Frontend (i) — inline node-detail drawer

A right-hand **drawer that opens in place on selection** (not a separate tab — the old Inspector tab is gone). It **leads with a metric header**: status pill, duration, **tokens in→out**, **cost + reported/estimated badge**, model. Metrics always render with their labels, showing `—` when unknown (never silently dropped), formatted (humanized durations, thousands-separated tokens). Raw ids / payload_hash / sources are **collapsed under "Advanced"** with copy buttons. Content (Input/Output) viewing is **not** in A (it is B, gated on the content endpoint).

### 5.10 Milestone A dependency graph

```
(d) rev-ordered flush ─┐
(a) session API ───────┼─▶ (g) sessions list + routing ─┐
(b) pricing ───────────┤                                 ├─▶ acceptance
(c) duration stamping ─┘                                 │
                                                          │
(e) Vite+Svelte foundation ─▶ (f) reducer+byId ─▶ (h) graph engine ─▶ (i) detail drawer
                                     ▲                                         │
                                     └── consumes (a) session API + (b) cost ─┘
```

(a)–(d) are independent of each other and of the frontend; (f) depends on (d) for its rev assumption; (g) depends on (a)+(b); (h)/(i) depend on (f) and consume (a)+(b)+(c).

## 6. Milestone B — content + timeline + filters + resume

**Goal.** Make the open session deeply inspectable and the realtime feed robust.

**Sub-projects.**

- **Authorized content endpoint** + opt-in Input/Output viewing: bearer-gated, redaction-aware (ADR-0008/0020), **default off**, returning payload by node id / payload_hash; the drawer gains Input/Output panels with a reveal toggle, JSON pretty-print, and copy.
- **Real timeline waterfall** backed by A's stamped durations: a time axis, proportional bars, relative offsets, human node names, and a distinct marker (not a full-width bar) for unknown durations. Hidden until durations exist.
- **Session-header KPI strip:** sticky totals (cost / tokens / wall-clock / counts / errors / model) when a session is open.
- **Structured filters** (status / type / model / has-error) + **free-text search** over node names/content; an error chip so failed sessions jump out.
- **Richer aggregates:** per-session counts by `NodeType` and `Status`, error rate, exposed via `GET /v1/sessions` (extends A's minimum aggregate).
- **SSE `Last-Event-ID` resume + reconnect UX:** client passes the last-seen rev; server reads `Last-Event-ID` and replays only deltas with `rev > cursor`; reconnect backoff, a visible stale/desync indicator, and surfaced (not swallowed) parse errors.

**Dependencies.** Content endpoint depends on A's session API; the waterfall depends on A's duration stamping; the KPI strip and filters depend on A's reducer + aggregates; resume depends on A's rev-ordered flush.

**Acceptance.** Opening a node reveals its prompt/response/tool I/O under the redaction policy (opt-in); the timeline shows proportional, labeled bars; the session header shows live KPIs; filters and search narrow the graph and list; a laptop-sleep reconnect replays only the tail (no full re-download) and any desync is visible.

## 7. Milestone C — bubbletea TUI

**Goal.** A fast terminal observe-flow over the same model.

**Sub-project.** `catacomb observe <hash>` opens a **bubbletea** (+lipgloss/bubbles) **master-detail** TUI over the **same SSE feed** and a **shared session/node model**: left = live Sessions list (hash, status dot, node count, tokens, cost, wall-time); center = the session graph as an **expandable indented tree** (parent_child → hierarchy, sequence → ordering — not ASCII 2D boxes); right = node detail (type, status, duration, tokens, cost, **opt-in content** via B's endpoint), labels always shown with `—` when unknown, raw ids behind a debug toggle. Keys: `j`/`k`/`enter`/`/`/`ESC`; live updates apply in place without flicker. Pure Go, 100% covered (the reducer/model are shared and unit-testable; the bubbletea program is driven by injected messages, not a live terminal).

**Dependencies.** A's session API + pricing + duration; B's content endpoint; the shared session/node reduction (the TUI reuses the Go reducer and the session-aggregate logic rather than duplicating it).

**Acceptance.** `catacomb observe <hash>` shows the session list, drills into the scoped graph tree, and shows per-node tokens/cost/duration (+opt-in content), updating live, all in the terminal.

## 8. Milestone D — full polish + CLI onboarding

**Goal.** Reach the visual/onboarding bar and remove every first-run sharp edge.

**Sub-projects.**

- **Visual polish:** light theme + persisted toggle (respect OS), illustrated empty/loading/error states, a connection pill (green/amber/red + disconnect toast), favicon set + theme-color + context-aware `<title>` ("Catacomb — Session …") + wordmark, deep a11y / keyboard traversal of the graph (arrow keys along edges, focus rings, roles/aria).
- **CLI onboarding:** `catacomb up` (one-command bring-up: start daemon if needed → idempotently install hooks → print bearer URL → open UI → if no live session within N seconds, offer the bundled demo), `catacomb demo` (seeded synthetic transcript), `catacomb status` (addr / pid / uptime / token age / session+node counts), **grouped cobra help** (Observe / Setup / Advanced), **typed error sentinels** rendering human messages ending in the exact remediation command, `/healthz` preflight before opening the browser, a safer token handshake (defer the cookie exchange here; keep `?token=` until then), and a README quickstart.

**Dependencies.** Builds on A–C; the demo dataset is a small embedded synthetic transcript (§10).

**Acceptance.** A new user runs one command and lands on a populated (or demo) UI; light/dark both ship and persist; empty/error states are designed; failures print copy-pasteable remediation; the CLI groups front-door verbs apart from plumbing; the UI passes a keyboard-only and a screenshot-next-to-Langfuse bar.

## 9. Quick wins folded into A

These are not a separate track; they are absorbed into Milestone A's rewrite (the old UI is deleted, not patched): a correct capitalized `<title>` + a single embedded favicon (kills the 404 and the lowercase tab); metric rows that always render (`—` when unknown) above the raw ids; perceptible selection; the "Runs" → "Sessions" rename; and a README quickstart (the fuller onboarding is D).

## 10. Deferred open questions — each with a proposed default

These are resolved **per sub-project**, not blocking; the default is the recommendation to adopt unless a plan argues otherwise.

| # | Question | Proposed default |
|---|---|---|
| 1 | Run vs session as the canonical addressable unit | **Session hash primary, run secondary; no first-class run view yet.** A session is the URL/CLI key; a run surfaces only as a grouping when it genuinely spans multiple sessions. If a user has only a run id, resolve it to its session(s) and land on the session list scoped to that run. |
| 2 | Pricing-table maintenance & freshness | **Versioned Go table, updated by manual PR; unknown model id → "estimate unavailable".** No runtime fetch. Cache-read/write tiers modeled. Provenance (reported vs estimated) carried on the node. Table facts sourced from the current Anthropic pricing reference (`claude-api` skill), not memory. |
| 3 | Content redaction default | **Denylist/secret-redaction ON, opt-in reveal per request, never persist decrypted.** The endpoint is default-off, bearer-gated; reveal is a per-request action under ADR-0008; nothing is written back decrypted. Daemon-config vs per-session toggle is a B detail. |
| 4 | Committed `dist/` + coverage scope | **Commit `dist/`; codepolicy already skips generated/JS; logic-only frontend gate.** The 100% mandate extends to pure-logic JS (reducer/stores/helpers), not Svelte components. CI builds the front end and verifies the committed `dist/` is current. |
| 5 | Scale target | **Settled: ≤ ~2,000 nodes via Svelte Flow + dagre/ELK.** A render abstraction keeps WebGL/Canvas/LOD possible later; not built now. |
| 6 | Light/dark default & theming scope | **Dark first in A, light in D, respect OS by default with a manual persisted override.** The TUI uses the terminal's theme (no separate light theme). |
| 7 | Token handshake security model | **Defer the cookie handshake to D; keep `?token=` for now** (documented loopback+same-user cost, ADR-0013). On 401/token mismatch show "daemon restarted — re-run catacomb ui" instead of an endless spinner (D). |
| — | Demo dataset (panel Q7) | **Embed a small synthetic transcript in the binary; ship in D** (powers `catacomb demo` and the no-live-session fallback). |
| — | Does `GET /v1/sessions` need a new store query? | **No for A** — compute aggregates from in-memory graphs under `d.mu` (the data is already resident). Revisit only if aggregates must survive eviction or cover historical sessions not in memory; that would be a B store query, decided there. |

## 11. Out of scope

This overhaul does **not** touch, beyond the specific additive changes named above:

- The **reconciliation core semantics** — the four-source capture (hooks / OTLP / stream-json / transcript JSONL), source precedence/ranking, the cascade/abandonment rules, and the canonical node/edge/run model are unchanged except for the additive `CostUSD` population (§5.2), `t_end`/`duration_ms` stamping (§5.3), and the rev-ordered flush (§5.4). No NodeType/EdgeType/Status is added or removed.
- The **exporters** (jsonl / OTLP / neo4j / postgres) and their wire schemas.
- The **persistence layer** and SQLite store, except the optional B aggregate query (§10).
- The **ADRs** and the loopback+bearer trust boundary (extended, not changed: new endpoints are bearer-gated; the content endpoint honors ADR-0008/0020).
- The **gRPC streaming surface** (M3b) — the UI/TUI consume SSE; gRPC is untouched.
