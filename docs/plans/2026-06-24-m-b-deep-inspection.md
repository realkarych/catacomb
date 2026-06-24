# Milestone B — Deep Inspection + Robust Realtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal.** Make an open session deeply inspectable and the realtime feed robust, on the completed Milestone A foundations (sessions list → scoped graph → node drawer, all live). Per spec §6 this milestone delivers six sub-projects: (B-be1) an authorized, redaction-aware, **default-OFF** content/payload endpoint; (B-be2) SSE `Last-Event-ID` resume that replays only the tail; (B-be3) richer session aggregates (per-type / per-status counts + error rate) for the KPI strip and filters; (B-fe1) opt-in Input/Output viewing in the drawer; (B-fe2) a real timeline waterfall from A's stamped durations; (B-fe3) a session-header KPI strip; (B-fe4) structured filters + free-text search; (B-fe5) the SSE resume client + reconnect/stale UX.

**Architecture.** All backend work is additive to the existing daemon/reducer/CDC core and preserves the deterministic-reducer contract. The content endpoint reads the in-memory node's `model.Payload` (Input/Output `json.RawMessage`) through a new redaction pass, gated by `authedAllowQuery` (ADR-0013) AND a daemon-level `allowPayloadAccess` flag that defaults OFF (ADR-0008/0020). `Last-Event-ID` resume extends `handleSSE` to read the header/`?since=` and skip snapshot deltas at-or-below the cursor (the snapshot is already rev-tagged; the live tail is unchanged). Richer aggregates extend the existing `SessionSummary` struct + `summarizeSession` (`daemon/sessions.go`). The frontend builds against the **real** Milestone-A exports (`webui/web/src/lib/{api,types,reducer,stores,sse,format,pricing}.*` and the existing components) and keeps the split: pure logic (filter/timeline-scale/payload-state) gated 100% in Vitest; Svelte components and rune wiring are not line-gated; Playwright covers the new flows.

**Design principle (owner, firm): minimalist + functional, silence-when-healthy.** Every B addition earns its place by answering a real triage question (KPIs/timeline/filters) and stays quiet when healthy (content viewing is opt-in/off; the desync indicator shows only when desynced; the error chip appears only when a session has errors). No element for its own sake.

**Tech stack.** Backend: Go 1.26 stdlib (`net/http`, `crypto/subtle`, `encoding/json`, `regexp`, `strconv`, `errors`, `slices`/`sort`), `github.com/stretchr/testify` (in tree). **No new Go dependencies.** Frontend: the existing Vite + Svelte 5 toolchain (`webui/`), Vitest, Playwright; **no new npm deps** (the timeline is hand-rolled SVG/CSS — no chart lib; JSON pretty-print is `JSON.stringify(_, null, 2)`).

---

## CRITICAL pre-flight findings (verified against the real source — read before implementing)

These were checked against the tree on 2026-06-24 and reshape several decisions.

1. **Redaction is NOT implemented anywhere in Go.** `grep -ri redact --include=*.go` returns nothing; ADR-0008/0020 describe a policy that has no code. `model.Payload{Input,Output json.RawMessage; Hash string}` is stored **raw** on the in-memory `model.Node.Payload` and hashed by `model.HashPayload` (`model/payload.go`, plain pre-redaction sha256). There is **no** payload mode (`full+hash+redact` / `refs-only` / `all`), no value-scanning regex pack, no key-glob redactor. **Consequence:** B-be1 cannot "honor the existing redactor" — it must **introduce** a minimal redaction pass as part of this milestone (a new comment-free `redact` package), because the content endpoint is the FIRST surface to serve payload bytes to a client and MUST NOT leak secrets (ADR-0008/0020 binding). This is the single biggest scope item in B and is called out again under B-be1 and in Gaps/Risks. The redaction pack must be conservative (default-deny on uncertainty is acceptable for a viewing surface) and 100%-tested. The full ADR-0020 surface (whole-node redaction across `name`/`attrs`, per-sink modes, post-redaction hashing, local HMAC) is larger than B needs; B implements the **value-scanning + key-glob redactor applied to the payload Input/Output served by the content endpoint** and leaves the store/exporter-wide application as a documented follow-up (it does not regress today's behavior, which already ships raw to exporters — that is a pre-existing ADR gap, not introduced here).

2. **Payloads are reachable from the in-memory graph.** `copyNode` (`daemon/copy.go`) does `nc := *n` (shallow), so `copyNode(n).Payload` aliases the same `*model.Payload`; SSE/sessions then set `nc.Payload = nil` on the copy. The content endpoint resolves the node via `g.Nodes[nodeId]` under `d.mu` and reads `n.Payload` directly (do **not** mutate it; build a redacted copy of the bytes). Payloads also persist in the store via observations, but the **in-memory node is the authoritative live source** and is what every other read path uses; B-be1 reads in-memory (no new store query — consistent with §10's "compute from resident data" stance). By-`payload_hash` lookup scans the session's nodes for a matching `PayloadHash`.

3. **Milestone A is fully landed and is the contract.** `daemon/sessions.go` already has `SessionSummary` (with `Status/DurationMS/TokensIn/TokensOut/CostUSD/CostSource/NodeCount/ToolCount/ErrorCount/ModelID/RunIDs`), `summarizeSession`, `sessionGraphDeltas`, `ErrSessionNotFound`; `daemon/server.go` already registers `GET /v1/sessions` and `GET /v1/sessions/{hash}/graph` via `authedAllowQuery`; `daemon/sse.go` already emits `id: <rev>` and strips `n.Payload=nil`, and `parseSubFilter` already reads `?session=`/`?run=`/`?type=`/`?tier=`. The FE already has `fetchSessions`/`fetchSessionGraph` (`api.ts`), the `SessionSummary`/`Node`/`Edge`/`SseEvent` types, the reducer, the normalized stores (`nodesById`/`edgesById`/`sessionsById`/`selectedNodeId`/`connectionState`), the SSE `connect()` client, `format.ts`, `pricing/provenance.ts`, and the `NodeDrawer`/`SessionView`/`GraphCanvas`/`SessionsList` components. **B extends these in place; it does not recreate them.**

4. **`Daemon` uses `SetX` mutators, not an Options struct.** Config is wired in `cmd/catacomb/daemon.go`'s `runDaemonWith` via `d.SetReaperWindow(...)` etc. The default-OFF content flag follows that exact pattern: a new `d.SetAllowPayloadAccess(bool)` + a `--allow-payload-access` cobra flag (default `false`). `runDaemonWith`'s signature gains one `bool` param (and its single caller + tests update). The daemon zero-value (`daemon.New(s)`) leaves `allowPayloadAccess=false`, so it is off by default everywhere, including every existing test, with no change required to them.

5. **SSE `id:` is already emitted; `Last-Event-ID` is simply never read.** `writeEvent` in `handleSSE` writes `id: %d\n` for `delta.Rev > 0`. The snapshot deltas carry the node/edge `Rev`. The browser `EventSource` automatically sends `Last-Event-ID` on reconnect. So B-be2 is: read the header (and accept `?since=` as an explicit override for non-EventSource clients), parse to `uint64`, and **skip snapshot deltas with `Rev <= cursor`** so a reconnect replays only the catch-up tail instead of the whole snapshot. The live tail loop is unchanged (it only ever sends new deltas). See Gaps/Risks for the coalesce-bus semantics caveat.

---

## Global Constraints

Binding on every task; each task's steps re-verify them.

- **Pure Go, no cgo.** `GOOS=windows go build ./...`, `GOOS=linux go build ./...`, `GOOS=darwin go build ./...` all clean. SQLite stays `modernc.org/sqlite` (untouched here).
- **No Go comments** except `//go:build`, `//go:embed`, `//go:generate`. Enforced by `internal/codepolicy` (`go test ./internal/codepolicy/`). Every Go snippet in this plan is comment-free and must be written comment-free. Generated-header files are skipped wholesale.
- **100% line coverage under `-race`**, TDD-first; the threshold never goes down. New packages (`redact`) and every modified Go file (`daemon/payload.go` new, `daemon/sse.go`, `daemon/server.go`, `daemon/daemon.go`, `daemon/sessions.go`, `cmd/catacomb/daemon.go`) must stay 100%. Untestable code is a refactoring signal, not an exclusion.
- **Deterministic reducer.** No reducer change in B (durations/cost/flush all landed in A). The richer-aggregate computation is a pure read over the resident graph; it ships a stable-output test.
- **Dependency inversion**; sentinel errors via `errors.Is`/`errors.As` (B adds `ErrPayloadAccessDisabled`, `ErrPayloadNotFound`; reuses `ErrSessionNotFound`); `log/slog`/`log` per existing style; never log or serialize a secret (the redaction pass exists precisely to uphold this); `context.Context` first for I/O; `gofumpt`+`goimports` (local prefix `github.com/realkarych/catacomb`).
- **No `time.Sleep` in tests** (forbidigo). Use `require.Eventually`, channels, deadlines, `httptest`. Mirror `daemon/sessions_test.go`, `daemon/sse_test.go`, `daemon/server_test.go` (helpers `tempStore`, `fixedExecID`, `loopbackListener`, `httptest.NewServer(d.Handler("tok"))`, `ob(...)`).
- **Loopback + bearer trust boundary (ADR-0013).** The content endpoint is bearer-gated via `authedAllowQuery` (header or `?token=` so the SPA `fetch` works), constant-time-compared, AND gated by the default-OFF `allowPayloadAccess` flag. Static assets stay open; data is gated; payload is double-gated.
- **Redaction (ADR-0008/0020).** The content endpoint NEVER returns raw secrets. It returns redacted Input/Output with redaction metadata (count + reasons). Default-off at the daemon; redaction-on whenever access is enabled. The pre-redaction `payload_hash` already on the node is returned for integrity reference only (it is the existing hash; B does not change the hashing — see Gaps/Risks #3).
- **Frontend deps via the JS toolchain only**; never `go mod tidy` for them; the embedded `webui/dist/` is data. **No new npm deps in B.** 100% Vitest on new pure-logic modules; components/rune wiring not line-gated; Playwright e2e; `dist/`-drift check stays green (every FE task ends with `npm run build` + commit the rebuilt `dist/`).
- **Commit per task** (`feat(...)` / `fix(...)`); never commit to `master` mid-plan; branch first (`git checkout -b feat/m-b-deep-inspection` from `master`); squash-merge via PR; no `--no-verify`.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `redact/redact.go` | Create | Pure redactor: value-scanning regex pack + key-path globs over `json.RawMessage`; returns redacted bytes + `[]Finding{Path,Reason}`. No I/O, no globals beyond immutable rule slices. |
| `redact/redact_test.go` | Create | 100% TDD: each rule (connection-string, `AKIA…`/`ghp_…`/`sk-…`/`xox…`, JWT, PEM, high-entropy), key-glob, nested JSON, non-UTF8/binary → `‹binary…›`, no-op on clean payload, invalid JSON path. |
| `daemon/payload.go` | Create | `PayloadView` response struct; `nodePayloadView(hash, nodeOrHashSelector)` (under `d.mu`); sentinels `ErrPayloadAccessDisabled`, `ErrPayloadNotFound`; by-id and by-`payload_hash` resolution; calls `redact.Redact`. |
| `daemon/payload_test.go` | Create | 100%: disabled→sentinel, enabled+found by id, by payload_hash, unknown session, unknown node, node with nil payload, redacted-content assertions, metadata shape. |
| `daemon/daemon.go` | Modify | Add `allowPayloadAccess bool` field + `SetAllowPayloadAccess(bool)`; zero-value false. |
| `daemon/server.go` | Modify | Register `GET /v1/sessions/{hash}/nodes/{nodeId}/payload` (bearer via `authedAllowQuery`); `handleNodePayload` mapping sentinels → 403 (disabled) / 404 (not found). |
| `daemon/server_test.go` | Modify | Route registration + bearer-gating + default-off (403) + enabled paths for the payload endpoint. |
| `daemon/sse.go` | Modify | `parseLastEventID(r)` (header `Last-Event-ID`, else `?since=`); `handleSSE` skips snapshot deltas with `Rev <= cursor`. |
| `daemon/sse_test.go` | Modify | Resume: with `Last-Event-ID`, snapshot is filtered to `Rev > cursor`; bad/empty header → full snapshot; live tail unaffected. |
| `daemon/sessions.go` | Modify | Extend `SessionSummary` with `CountsByType map[string]int`, `CountsByStatus map[string]int`, `ErrorRate float64`; populate in `summarizeSession`. |
| `daemon/sessions_test.go` | Modify | Assert the new aggregate fields (counts by type/status, error rate; deterministic). |
| `cmd/catacomb/daemon.go` | Modify | `--allow-payload-access` flag (default false) → `d.SetAllowPayloadAccess(...)`; thread through `runDaemonWith`. |
| `cmd/catacomb/daemon_test.go` | Modify | New flag param in `runDaemonWith` callers/tests. |
| `webui/web/src/lib/types.ts` | Modify | Add `PayloadView` + `RedactionFinding` types; extend `SessionSummary` with `counts_by_type`/`counts_by_status`/`error_rate`. |
| `webui/web/src/lib/api.ts` | Modify | `fetchNodePayload(hash, nodeId, token)` → `PayloadView`; typed `ForbiddenError` for 403 (disabled). |
| `webui/web/src/lib/api.test.ts` | Create/Modify | 100% on the new fetch (200/403/404/parse). |
| `webui/web/src/lib/timeline.ts` | Create | Pure waterfall layout: nodes+durations → `{rows: TimelineRow[], scale}`; offsets relative to session start; flag unknown-duration; drop rows w/o timing. **Gated 100%.** |
| `webui/web/src/lib/timeline.test.ts` | Create | 100% branches (empty, single, proportional widths, unknown-duration marker, rows-without-timing hidden, clamp). |
| `webui/web/src/lib/filters.ts` | Create | Pure structured filter + free-text: `filterNodes(nodes, FilterState)`; predicates for status/type/model/has-error + name substring. **Gated 100%.** |
| `webui/web/src/lib/filters.test.ts` | Create | 100% branches (each predicate, combined, empty filter identity, case-insensitive search, has-error chip). |
| `webui/web/src/lib/payload-view.ts` | Create | Pure helpers: `prettyJSON(raw)`, `payloadState(view, enabled, error)` → `'disabled'|'redacted'|'empty'|'ready'`. **Gated 100%.** |
| `webui/web/src/lib/payload-view.test.ts` | Create | 100% branches. |
| `webui/web/src/components/PayloadPanel.svelte` | Create | Opt-in Input/Output reveal: toggle, pretty JSON, copy, redaction notice, disabled/empty states. Not gated. |
| `webui/web/src/components/NodeDrawer.svelte` | Modify | Mount `PayloadPanel` (opt-in, below metrics, above Advanced); pass `hash`/`nodeId`/`token`/`enabled`. |
| `webui/web/src/components/Timeline.svelte` | Create | Waterfall view-mode of the session (SVG/CSS bars from `timeline.ts`). Not gated. |
| `webui/web/src/components/SessionHeader.svelte` | Create | Sticky KPI strip (cost/tokens/wall-clock/counts/errors/model) from `sessionsById[hash]`. Not gated. |
| `webui/web/src/components/FilterBar.svelte` | Create | Structured filter controls + search input + error chip. Not gated. |
| `webui/web/src/components/SessionView.svelte` | Modify | Add `SessionHeader`; add a `graph | timeline` view-mode toggle; mount `FilterBar`; pass filtered node set to graph/timeline. |
| `webui/web/src/lib/stores/stores.svelte.ts` | Modify | Add `filterState` ($state), `desync` indicator state, `lastSeenRev`; expose a filtered `sessionGraph` read or a `filteredSessionGraph(hash, filter)` (delegates to pure `filters.ts`). |
| `webui/web/src/lib/sse/client.ts` | Modify | Track last-seen rev; reconnect backoff; surface parse errors via `onParseError`; expose a stale/desync signal. (Logic gated where pure; the EventSource seam stays thin.) |
| `webui/web/src/lib/sse/client.test.ts` | Modify | 100% on resume/backoff/parse-error-surfacing logic with the fake `EventSourceLike`. |
| `webui/web/src/App.svelte` | Modify | Pass `lastSeenRev` to `connect`; reflect desync; pass `token`/`allowPayloadAccess` capability down. |
| `webui/web/vitest.config.ts` | Modify | Add `src/lib/timeline.ts`, `src/lib/filters.ts`, `src/lib/payload-view.ts`, `src/lib/api.ts` to coverage `include`. |
| `webui/web/e2e/*.spec.ts` | Create | Playwright: content reveal (mocked enabled + redacted), timeline bars, KPI strip, filter+search narrowing, resume (Last-Event-ID sent on reconnect) + desync indicator. |
| `webui/dist/**` | Rebuild (committed) | Updated artifact embedding the new views. |

---

## Contracts to PIN (frozen by this plan)

### `PayloadView` JSON for `GET /v1/sessions/{hash}/nodes/{nodeId}/payload` (NEW)

Go struct (comment-free) with exact field names + tags. `nodeId` may be a node id OR a `payload_hash` (the handler tries id first, then hash within the session scope).

```go
type RedactionFinding struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type PayloadView struct {
	NodeID      string             `json:"node_id"`
	PayloadHash string             `json:"payload_hash,omitempty"`
	Input       json.RawMessage    `json:"input,omitempty"`
	Output      json.RawMessage    `json:"output,omitempty"`
	Redactions  []RedactionFinding `json:"redactions"`
	Redacted    bool               `json:"redacted"`
}
```

- `node_id` — the resolved node id (always the canonical id, even when looked up by hash).
- `payload_hash` — the node's existing `PayloadHash` (pre-redaction sha256, integrity reference only; see Gaps/Risks #3).
- `input` / `output` — the **redacted** payload bytes (valid JSON with `‹redacted:reason›` string spans substituted; binary/non-UTF8 replaced by `"‹binary:len,sha256›"`). Omitted when the source side was empty.
- `redactions` — never `null`; emit `[]`. Each `{path, reason}` names what was scrubbed (path is a dotted JSON path or `value` for free-text matches).
- `redacted` — `true` iff `len(redactions) > 0`.

**Status codes:** 200 found; **403** when `allowPayloadAccess` is OFF (`ErrPayloadAccessDisabled` — distinct from auth 401 so the UI can show "content viewing disabled by the daemon"); **404** when the session or node/hash is unknown, or the node has no payload (`ErrSessionNotFound` / `ErrPayloadNotFound`); **401** when the bearer/token is missing/wrong (existing `authedAllowQuery`).

### Extended `SessionSummary` (additive — existing fields unchanged)

```go
	CountsByType   map[string]int `json:"counts_by_type"`
	CountsByStatus map[string]int `json:"counts_by_status"`
	ErrorRate      float64        `json:"error_rate"`
```

- `counts_by_type` — node count keyed by `NodeType` string over the session scope; never `null` (emit `{}`).
- `counts_by_status` — node count keyed by `Status` string; never `null`.
- `error_rate` — `ErrorCount / NodeCount` (0 when `NodeCount==0`); a `float64` in `[0,1]`.

### `Last-Event-ID` resume semantics (NEW behavior, no wire change)

- `handleSSE` reads `Last-Event-ID` header; if absent, reads `?since=<rev>`; parses to `uint64` (parse failure or 0 → no cursor → full snapshot, today's behavior).
- With a cursor `c`: snapshot deltas with `Rev <= c` are **skipped**; deltas with `Rev > c` are sent; then the live tail streams as today. (Skipping at-or-equal is correct because `id:` is the delta `Rev` and the client has already applied `c`.)

### Frontend types (mirror the above) — pinned in `types.ts`

```ts
export interface RedactionFinding { path: string; reason: string; }
export interface PayloadView {
  node_id: string;
  payload_hash?: string;
  input?: unknown;
  output?: unknown;
  redactions: RedactionFinding[];
  redacted: boolean;
}
// SessionSummary additions:
//   counts_by_type: Record<string, number>;
//   counts_by_status: Record<string, number>;
//   error_rate: number;
```

---

## Task ordering & dependency graph

```
B-be3 aggregates ─┐
B-be2 resume ─────┼─▶ (independent backend; land first, each its own commit)
redact + B-be1 ───┘
                                B-fe3 KPI strip ◀── B-be3
B-fe5 resume client ◀── B-be2
B-fe1 content panel ◀── B-be1 (+ redact)
B-fe2 timeline ◀── A durations (already landed)
B-fe4 filters ◀── A reducer/stores (already landed)
```

Recommended sequence (each a commit): **redact → B-be1 → B-be2 → B-be3** (backend), then **B-fe2 → B-fe4 → B-fe3 → B-fe1 → B-fe5** (frontend; timeline/filters are pure-logic-heavy and unblock fastest; content + resume close the loop). Backend tasks are mutually independent. The shared `webui/dist/` rebuild is committed at the end of each FE task.

---

## Backend

### Task redact — Pure redaction package (PREREQUISITE for B-be1)

**Why this exists (read pre-flight #1):** there is no redactor in-tree. The content endpoint is the first surface to serve payload bytes; serving them raw would violate ADR-0008/0020. This package is the minimal, conservative, 100%-tested redactor the endpoint needs.

**Files:** Create `redact/redact.go`, `redact/redact_test.go`.

**Interfaces (produces):**

```go
package redact

type Finding struct {
	Path   string
	Reason string
}

type Result struct {
	Data       []byte
	Findings   []Finding
}

func Redact(raw []byte) Result
```

`Redact` accepts a `json.RawMessage` (or arbitrary bytes):
- If `raw` is empty → `Result{Data: raw, Findings: nil}`.
- If `raw` is valid JSON → walk it; for every **string value** (at any depth) and every **object key matching a key-glob** (`*api_key*`, `authorization`, `*secret*`, `*token*`, `*password*`, case-insensitive), run the value-scanning pack; replace a matched span (or the whole value for key-glob hits) with `"‹redacted:<reason>›"`; record `{path, reason}` (dotted path, array indices as `[i]`). Re-marshal deterministically (stable key order via `json.Marshal` of the reconstructed structure — note: decode into `map[string]any` loses key order, which is acceptable for a viewing surface; document this).
- If `raw` is **not** valid JSON or **not** valid UTF-8 → treat as opaque free text: scan for value patterns; if it is non-UTF8/binary, replace wholesale with `"‹binary:<len>,<sha256-prefix>›"` and one finding `{path:"", reason:"binary"}`.

**Value-scanning pack (ADR-0020 §Decision 1), each with a `reason`:** connection-string URIs with embedded credentials (`scheme://user:pass@host`), AWS keys (`AKIA[0-9A-Z]{16}`), GitHub tokens (`gh[pousr]_[A-Za-z0-9]{36,}`), OpenAI-style (`sk-[A-Za-z0-9]{20,}`), Slack (`xox[baprs]-[A-Za-z0-9-]{10,}`), JWTs (`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), PEM blocks (`-----BEGIN [A-Z ]+PRIVATE KEY-----`), and high-entropy base64/hex runs (length ≥ 40, charset-gated) as a catch-all. Patterns are package-level immutable `var` slices of `*regexp.Regexp` compiled once (no `init()` side effect beyond `regexp.MustCompile` in a `var` block, consistent with existing `var` table style; if codepolicy/`gochecknoglobals` objects, expose via a constructor func returning the slice).

**Notes for implementer:**
- Conservative default for a viewing surface: a false positive (over-redaction) is acceptable; a false negative (leaked secret) is not. The high-entropy catch-all biases toward redaction.
- This package is pure (no clock, no I/O). The sha256 used in the binary-ref reason is `crypto/sha256` over the raw bytes (prefix hex), distinct from `model.HashPayload`.
- Do NOT attempt the full ADR-0020 whole-node surface here; B-be1 only feeds payload Input/Output through this.

**Steps:**
- [ ] **Step 1: Branch.** `cd /Users/karych/src/catacomb && git checkout -b feat/m-b-deep-inspection`
- [ ] **Step 2: Write failing `redact/redact_test.go`** — one table-driven test per rule + nested JSON path + key-glob + binary + clean no-op + invalid-JSON-free-text. Assert `Findings` paths/reasons and that no raw secret substring survives in `Result.Data`.
- [ ] **Step 3: Run — expect build/FAIL.** `go test ./redact/ 2>&1 | head`
- [ ] **Step 4: Implement `redact/redact.go`.**
- [ ] **Step 5: 100% coverage.** `go test -race -coverprofile=/tmp/cov.out ./redact/ && go tool cover -func=/tmp/cov.out | grep -v 100.0% || echo covered`
- [ ] **Step 6: Lint + cross-build + codepolicy.** `golangci-lint run ./redact/ && GOOS=windows go build ./... && go test ./internal/codepolicy/`
- [ ] **Step 7: Commit** `feat(redact): conservative payload redactor (value-scan + key-glob) for the content endpoint`.

---

### Task B-be1 — Authorized, default-OFF content endpoint

**Endpoint:** `GET /v1/sessions/{hash}/nodes/{nodeId}/payload` (bearer-gated via `authedAllowQuery`; `{nodeId}` resolves by node id first, then by `payload_hash` within the session). Default **OFF**.

**Files:** Create `daemon/payload.go`, `daemon/payload_test.go`. Modify `daemon/daemon.go` (flag + setter), `daemon/server.go` (route + handler), `daemon/server_test.go`. (Depends on `redact`.)

**Interfaces (produces):**
- `daemon/daemon.go`: field `allowPayloadAccess bool`; `func (d *Daemon) SetAllowPayloadAccess(v bool)`.
- `daemon/payload.go`: `PayloadView`, `RedactionFinding` (the pinned structs); sentinels `ErrPayloadAccessDisabled`, `ErrPayloadNotFound`; `func (d *Daemon) nodePayloadView(hash, selector string) (PayloadView, error)` (caller holds `d.mu`); `handleNodePayload`.

**Default-OFF mechanism (pin):** the gate is checked **inside** `nodePayloadView` (or at the top of `handleNodePayload`): if `!d.allowPayloadAccess` → return `ErrPayloadAccessDisabled` → handler writes **403**. This is independent of and in addition to `authedAllowQuery`'s 401. `daemon.New` leaves the field false, so the endpoint is off for every existing test and every default deployment; only `--allow-payload-access` (cmd) or `SetAllowPayloadAccess(true)` (tests) enables it.

**Resolution + redaction (pin):**
1. `execs := d.executionsForSession(hash)`; empty → `ErrSessionNotFound` (404).
2. Search the session's graphs for a node with `n.ID == selector`; if none, search for `n.PayloadHash == selector`. None → `ErrPayloadNotFound` (404).
3. If `n.Payload == nil` (or both Input and Output empty) → `ErrPayloadNotFound` (404). (A node with no captured payload is "nothing to view", distinct from "found".)
4. `in := redact.Redact(n.Payload.Input)`, `out := redact.Redact(n.Payload.Output)`; build `PayloadView{NodeID:n.ID, PayloadHash:n.PayloadHash, Input:in.Data, Output:out.Data, Redactions: merge(in.Findings,out.Findings), Redacted: len>0}`. Never read or copy `n.Payload` into the response un-redacted.
5. The handler maps sentinels: `ErrPayloadAccessDisabled`→403, `ErrSessionNotFound`/`ErrPayloadNotFound`→404; else 200 JSON.

**Notes for implementer:** hold `d.mu` for the graph read; do not call `redact` under the lock if payloads are large enough to matter — copy the `Input`/`Output` byte slices out under the lock, release, then redact (redaction is pure and CPU-bound). Keep `n.Payload` untouched (read-only). The endpoint must never appear in any non-loopback surface (it is on the loopback mux, same as the rest).

**Steps:**
- [ ] **Step 1: Write failing `daemon/payload_test.go`** mirroring `sessions_test.go` setup (`tempStore`, `fixedExecID`, ingest `ob(...)` with a payload). Cases: default-off → `ErrPayloadAccessDisabled`; enabled+by-id → redacted view; enabled+by-`payload_hash` → same node; unknown session → `ErrSessionNotFound`; unknown selector → `ErrPayloadNotFound`; node with nil payload → `ErrPayloadNotFound`; a payload carrying a fake secret → assert the secret substring is absent and a finding is present. Add `server_test.go` HTTP cases: 401 (no token), 403 (token, disabled), 200 (token, enabled), 404 (enabled, bad node), via `httptest.NewServer(d.Handler("tok"))`.
- [ ] **Step 2: Run — expect FAIL.** `go test ./daemon/ -run Payload 2>&1 | head`
- [ ] **Step 3: Implement** `daemon.go` field+setter, `daemon/payload.go`, `daemon/server.go` route+handler.
- [ ] **Step 4: 100% coverage** for the touched daemon files. `go test -race -coverprofile=/tmp/cov.out ./daemon/ && go tool cover -func=/tmp/cov.out | grep -E 'payload.go|server.go|daemon.go' | grep -v 100.0% || echo covered`
- [ ] **Step 5: Lint + cross-build + codepolicy.**
- [ ] **Step 6: Wire the flag** in `cmd/catacomb/daemon.go` (`--allow-payload-access`, default false) → `d.SetAllowPayloadAccess(...)`; thread the new `bool` through `runDaemonWith`; update `cmd/catacomb/daemon_test.go` callers. Run `go test ./cmd/...`. (This can be its own small commit.)
- [ ] **Step 7: Commit** `feat(daemon): authorized default-off redaction-aware node payload endpoint`.

---

### Task B-be2 — SSE `Last-Event-ID` resume

**Files:** Modify `daemon/sse.go`, `daemon/sse_test.go`.

**Interfaces:** new unexported `func parseLastEventID(r *http.Request) uint64` (header `Last-Event-ID`, else `?since=`; `strconv.ParseUint`; 0/parse-error → 0). `handleSSE` computes `cursor := parseLastEventID(r)` and, in the snapshot loop, skips `snap.Rev <= cursor` (when `cursor > 0`).

**Behavior (pin, read pre-flight #5):**
- No header, no `?since=` → unchanged (full snapshot).
- Header `Last-Event-ID: <rev>` → snapshot filtered to `Rev > cursor`; live tail unchanged.
- `?since=<rev>` honored when the header is absent (non-EventSource clients / explicit catch-up).
- Malformed value → treated as 0 → full snapshot (safe default).

**Notes for implementer:** this is a pure filter in the snapshot emit loop; `writeEvent`/`streamSSE`/the ticker are untouched. See Gaps/Risks #2 for why this is correct under the coalesce bus: the snapshot is built from the live graph (current node/edge `Rev`s), so skipping `Rev <= cursor` cannot drop a state the client has not seen — any node updated after the client's cursor carries a `Rev > cursor` and is re-sent in full.

**Steps:**
- [ ] **Step 1: Write failing `daemon/sse_test.go` cases** — using the existing SSE e2e harness (loopback server + `EventSource`-style request with the header, or a direct `httptest` request reading the body): assert that with `Last-Event-ID` set above some snapshot revs, only higher-rev deltas appear in the snapshot prefix; with no header, all appear; with `?since=`, same as header.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement `parseLastEventID` + the snapshot skip.**
- [ ] **Step 4: 100% coverage** for `daemon/sse.go` (cover header path, `?since=` path, malformed, no-cursor).
- [ ] **Step 5: Lint + cross-build + codepolicy.**
- [ ] **Step 6: Commit** `feat(daemon): SSE Last-Event-ID/since resume replays only the tail`.

---

### Task B-be3 — Richer session aggregates

**Files:** Modify `daemon/sessions.go`, `daemon/sessions_test.go`.

**Interfaces:** extend `SessionSummary` with `CountsByType map[string]int`, `CountsByStatus map[string]int`, `ErrorRate float64` (pinned tags above). Populate in `summarizeSession`: initialize both maps (never nil), increment `CountsByType[string(n.Type)]` and `CountsByStatus[string(n.Status)]` in the existing node loop, and set `ErrorRate = float64(ErrorCount)/float64(NodeCount)` (guard `NodeCount==0` → 0) after the loop.

**Notes for implementer:** purely additive to the existing `summarizeSession`; the existing fields and their tests are unchanged. Status keys use the raw `model.Status` string; an empty status (no status set) keys under `""` — acceptable, or skip empties (pin: include `""` so counts sum to `NodeCount`; document). Emit `{}` not `null` for both maps (initialize with `map[string]int{}`).

**Steps:**
- [ ] **Step 1: Write failing `daemon/sessions_test.go` cases** asserting `counts_by_type`/`counts_by_status` sums equal `NodeCount`, error_rate math, and JSON emits `{}` not `null` for an empty session (extend `TestSessionSummariesEmpty`/`TestSessionSummariesBasic`).
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement the additive fields.**
- [ ] **Step 4: 100% coverage** for `daemon/sessions.go`.
- [ ] **Step 5: Lint + cross-build + codepolicy.**
- [ ] **Step 6: Commit** `feat(daemon): session aggregates — counts by type/status + error rate`.

---

## Frontend

> Every FE task: write failing Vitest for pure logic → implement → 100% on the gated module → build the component → `npm run check` → `npm run build` → commit including rebuilt `webui/dist/`. Components and `*.svelte.ts` rune wiring are NOT line-gated. Add each new pure module to `vitest.config.ts` `coverage.include`.

### Task B-fe2 — Timeline waterfall (pure layout gated; component + e2e)

**Files:** Create `webui/web/src/lib/timeline.ts` (+ `.test.ts`), `webui/web/src/components/Timeline.svelte`. Modify `SessionView.svelte` (view-mode toggle), `vitest.config.ts`.

**Pure module (gated 100%) — `timeline.ts`:**

```ts
import type { Node } from './types';
export interface TimelineRow {
  id: string; label: string;
  offsetFrac: number;   // start offset / total span, in [0,1]
  widthFrac: number;    // duration / total span, in (0,1]
  unknownDuration: boolean;
  status?: string;
}
export interface TimelineModel { rows: TimelineRow[]; spanMs: number; startMs: number; }
export function buildTimeline(nodes: Node[]): TimelineModel;
```

**Rules (pin):** consider only nodes with a `t_start` (rows without timing are **hidden** — spec §6). `startMs = min(t_start)`, `endMs = max(t_end ?? t_start)`; `spanMs = max(endMs - startMs, 1)`. For each row: `offsetFrac = (t_start - startMs)/spanMs`; if `duration_ms` known → `widthFrac = clamp(duration_ms/spanMs, ε, 1)`, `unknownDuration=false`; else → a fixed tiny marker width (e.g. a sentinel `widthFrac` the component renders as a diamond, not a full-width bar) and `unknownDuration=true`. `label = name ?? type`. Deterministic order: sort by `offsetFrac` then `id`. Empty input → `{rows:[], spanMs:0, startMs:0}`.

**Component — `Timeline.svelte`:** SVG/CSS rows, time axis (a few tick labels from `spanMs` via `format.formatDuration`), proportional bars colored by `--node-{type}` (reuse the design tokens / `node-legend.ts`), unknown-duration rendered as a distinct marker (diamond/hatch), human labels, click a row → `selectNode(id)` (reuse the shared store so selection stays unified with the graph/drawer). No chart lib.

**SessionView wiring:** add a minimal `view: 'graph' | 'timeline'` toggle in the session header area (two quiet buttons; default `graph`). Hide the timeline entirely when `buildTimeline(nodes).rows.length === 0` (silence-when-healthy: no empty timeline chrome). Feed it the same node set the graph uses (post-filter once B-fe4 lands).

**Steps:**
- [ ] Write failing `timeline.test.ts` (empty, single, proportional widths, unknown-duration marker, rows-without-timing hidden, clamp on tiny/huge durations). Implement `timeline.ts`. 100% gate green. Add to `vitest.config.ts` include.
- [ ] Build `Timeline.svelte` + the SessionView toggle. `npm run check` clean.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): timeline waterfall from stamped durations`.

### Task B-fe4 — Structured filters + free-text search (pure filter gated; component + e2e)

**Files:** Create `webui/web/src/lib/filters.ts` (+ `.test.ts`), `webui/web/src/components/FilterBar.svelte`. Modify `stores.svelte.ts` (`filterState` + a `filteredSessionGraph`/filter-aware read), `SessionView.svelte`, `vitest.config.ts`.

**Pure module (gated 100%) — `filters.ts`:**

```ts
import type { Node } from './types';
export interface FilterState {
  query: string;
  statuses: string[];   // empty = all
  types: string[];      // empty = all
  models: string[];     // empty = all
  hasError: boolean;    // true = only sessions/nodes with status error
}
export function emptyFilter(): FilterState;
export function filterNodes(nodes: Node[], f: FilterState): Node[];
export function isActive(f: FilterState): boolean;
```

**Rules (pin):** `query` is case-insensitive substring over `name ?? type` (and `id` as a fallback). `statuses`/`types` are membership tests (empty list = no constraint). `models` matches `attrs.model_id ?? attrs.model`. `hasError` → keep only `status==='error'`. Empty filter (`isActive===false`) returns the **same reference** (memo-friendly). Returns a new array otherwise; never mutates.

**Component — `FilterBar.svelte`:** a quiet row of controls: a search input (binds `filterState.query`), small multi-select chips for status/type (sourced from the live node set or the fixed `NodeType`/`Status` enums via `node-legend.ts`), a model dropdown, and an **error chip** that toggles `hasError` and is **styled to stand out only when a session has errors** (read `sessionsById[hash].error_count > 0`; otherwise the chip is muted/absent — silence-when-healthy). The bar narrows BOTH the graph and the timeline (they consume the filtered node set), and the same predicates can drive the sessions-list search later (reuse).

**Store wiring:** add `filterState = $state(emptyFilter())`; expose `filteredSessionGraph(hash)` that calls `sessionGraph(hash)` then `filterNodes(...)` with the current `filterState` (keep the pure work in `filters.ts`; the `.svelte.ts` stays thin). Edges whose endpoints are filtered out are dropped from the rendered set (compute in a pure helper, gated).

**Steps:**
- [ ] Write failing `filters.test.ts` (each predicate, combined, empty identity/reference, case-insensitive, hasError, model from attrs, edge-drop helper). Implement `filters.ts`. 100% gate. Add to `vitest.config.ts` include.
- [ ] Build `FilterBar.svelte`; wire `filterState` + `filteredSessionGraph`; feed graph + timeline. `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): structured filters + free-text search with error chip`.

### Task B-fe3 — Session-header KPI strip (component)

**Files:** Create `webui/web/src/components/SessionHeader.svelte`. Modify `SessionView.svelte`.

**Behavior (pin, minimalist):** a sticky strip in the session view reading `sessionsById[hash]` (the live `SessionSummary`, now with the B-be3 aggregates). Show ONLY meaningful KPIs: total cost (`formatCost` + provenance badge via `pricing/provenance`), tokens in→out (`formatTokens`), wall-clock (`formatDuration(duration_ms)` or "running" when no `ended_at`), node/tool counts, error count (rendered as a quiet number, but **red and prominent only when `error_count > 0`** — silence-when-healthy), and model id. Each KPI uses the existing `MetricRow`-style label/value and shows `—` when unknown. No sparkline, no decorative chrome. Reuse `format.ts`.

**SessionView wiring:** mount `<SessionHeader {hash} />` above the graph/timeline area, alongside the existing back button / hash. Keep it sticky (`position: sticky; top: 0`) and quiet.

**Steps:**
- [ ] Build `SessionHeader.svelte` from `sessionsById[hash]` (pure formatting via existing `format.ts`/`provenance.ts` — no new gated logic needed; if any non-trivial KPI derivation appears, factor it into a tiny gated helper). `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): sticky session-header KPI strip`.

### Task B-fe1 — Content viewing in the drawer (api + pure state gated; component + e2e)

**Files:** Modify `webui/web/src/lib/types.ts` (PayloadView/RedactionFinding + SessionSummary additions), `webui/web/src/lib/api.ts` (`fetchNodePayload` + `ForbiddenError`), create `api.test.ts`, create `webui/web/src/lib/payload-view.ts` (+ `.test.ts`), create `webui/web/src/components/PayloadPanel.svelte`. Modify `NodeDrawer.svelte`, `App.svelte` (thread `token`/capability), `vitest.config.ts`. (Depends on B-be1.)

**API (gated) — `api.ts`:**

```ts
export class ForbiddenError extends Error {}
export async function fetchNodePayload(
  hash: string, nodeId: string, token: string, f = fetch,
): Promise<PayloadView>;
// 403 -> ForbiddenError (content viewing disabled); 404 -> NotFoundError; else throw.
```

**Pure state (gated) — `payload-view.ts`:**

```ts
import type { PayloadView } from './types';
export type PayloadState = 'disabled' | 'empty' | 'redacted' | 'ready';
export function prettyJSON(value: unknown): string;     // JSON.stringify(value, null, 2); undefined -> ''
export function payloadState(view: PayloadView | null, forbidden: boolean): PayloadState;
// forbidden -> 'disabled'; null/empty input&output -> 'empty'; view.redacted -> 'redacted'; else 'ready'
```

**Component — `PayloadPanel.svelte`:** opt-in. Renders a collapsed-by-default **"Reveal content"** control (silence-when-healthy / privacy-first). Only on user reveal does it call `fetchNodePayload(hash, nodeId, token)`. States: `disabled` → "Content viewing is disabled by the daemon (start it with `--allow-payload-access`)."; `empty` → "No captured payload."; `redacted` → render Input/Output pretty-printed with an inline notice "N value(s) redacted" listing reasons compactly; `ready` → Input/Output pretty JSON. Copy buttons per panel (`navigator.clipboard`). Reuses the drawer's existing Advanced/`.mono` styling.

**NodeDrawer wiring:** mount `<PayloadPanel {hash} nodeId={node.id} {token} />` between the metrics section and the Advanced disclosure. The panel is inert until revealed; nothing is fetched on selection (opt-in, off-by-default UX even when the daemon enables it).

**App wiring:** `NodeDrawer` already receives `hash`; thread `token` (already in `App.svelte`) down through `SessionView` → `NodeDrawer` → `PayloadPanel`. A 403 on reveal renders the `disabled` state (the UI does not need a separate capability probe; the first reveal attempt reveals capability).

**Steps:**
- [ ] Write failing `api.test.ts` (200 → PayloadView, 403 → ForbiddenError, 404 → NotFoundError, network/parse) and `payload-view.test.ts` (every state + prettyJSON branches). Implement `api.ts` additions + `payload-view.ts`. 100% gate (add both to `vitest.config.ts` include). Pin the new `types.ts` shapes.
- [ ] Build `PayloadPanel.svelte`; mount in `NodeDrawer.svelte`; thread `token` through `SessionView`. `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): opt-in redaction-aware content panel in the node drawer`.

### Task B-fe5 — SSE resume client + reconnect/desync UX (logic gated; e2e)

**Files:** Modify `webui/web/src/lib/sse/client.ts` (+ `client.test.ts`), `webui/web/src/lib/stores/stores.svelte.ts` (desync/lastSeenRev state), `App.svelte`. (Depends on B-be2.)

**Client changes (pin):**
- Track the last-seen `rev` (from `onEvent` payloads) so a reconnect can pass it. Browser `EventSource` sends `Last-Event-ID` automatically from the last `id:` it saw; the explicit `lastSeenRev` is a belt-and-suspenders for the `?since=` query and for the fake test seam. Extend `ConnectOptions` with `onParseError?: (raw: string, err: unknown) => void` and `getLastRev?: () => number` (or have `connect` own a `lastRev` it appends as `&since=` on reconnect).
- **Reconnect backoff:** on `onerror`, schedule a reconnect with capped exponential backoff (e.g. 0.5s→…→10s) using an injectable timer seam (default `setTimeout`; tests inject a fake), so there is **no `time.Sleep`/real timer in tests**. Reset backoff on a successful `open`.
- **Surface parse errors:** the current `connect` swallows `JSON.parse` failures (`catch {}`). Change to call `onParseError` (and increment a desync counter) instead of silently dropping — spec §6 "surface (not swallow) parse errors".
- **Desync signal:** expose a `'stale' | 'live'` (or a counter) the store maps to a visible indicator. Stale = repeated `onerror` without `open`, or any parse error. This stays quiet when healthy.

**Store + App:** add `desync = $state({ stale: boolean, parseErrors: number })`; the SSE `$effect` in `App.svelte` feeds `onParseError`/status into it; the topbar shows a quiet "reconnecting…/desynced" pill only when stale (extend the existing `conn-pill`, which already renders only for `connecting`/`error` — add a desync variant). Pass `lastSeenRev`/`since` on reconnect.

**Notes for implementer:** keep ALL branching (backoff math, stale decision, parse-error routing) in pure functions in `client.ts` so the 100% gate covers it via the fake `EventSourceLike` + fake timer; the `.svelte.ts`/`App.svelte` wiring stays thin. Do not introduce a real reconnect loop the tests can't drive synchronously.

**Steps:**
- [ ] Write failing `client.test.ts` (resume passes last rev/`since`; backoff schedules via fake timer and resets on open; `onParseError` fires on malformed data instead of swallowing; stale signal toggles). Implement `client.ts`. 100% gate.
- [ ] Wire `desync`/`lastSeenRev` into `stores.svelte.ts` + `App.svelte` topbar (quiet pill). `npm run check`.
- [ ] `npm run build`; commit incl. `dist/`. `feat(webui): SSE resume + reconnect backoff + visible desync, surfaced parse errors`.

### Task B-fe-e2e — Playwright for the B flows

**Files:** Create `webui/web/e2e/{content,timeline,kpi,filters,resume}.spec.ts` (or fold into existing spec files), using `page.route()` mocks (the established hermetic pattern — no live daemon).

**Coverage:**
- **Content:** mock the payload endpoint enabled → reveal shows redacted Input/Output + the "N redacted" notice + copy; mock 403 → reveal shows the "disabled" state.
- **Timeline:** mock a session graph with stamped durations → toggle to timeline → proportional bars present, an unknown-duration node shows the distinct marker, a node without `t_start` is absent.
- **KPI strip:** mock `/v1/sessions` with cost/tokens/counts/errors → header shows the values; error count is prominent only when `>0`.
- **Filters:** type a search term → graph/timeline narrow; toggle a status chip → narrows; error chip appears only for an error session.
- **Resume:** assert the reconnect request carries `Last-Event-ID`/`since` (intercept the SSE request URL/headers on the second connect) and the desync pill appears on a forced error then clears on reconnect.

**Steps:**
- [ ] Write the specs against `page.route()` mocks for `/v1/sessions`, `/v1/sessions/{hash}/graph`, `/v1/sessions/{hash}/nodes/{id}/payload`, `/v1/subscribe`.
- [ ] `npm run test:e2e` green locally.
- [ ] Final: full Go suite + cross-build + codepolicy + FE vitest gate + `dist`-drift clean. Commit `test(webui): e2e for content/timeline/kpi/filters/resume`.

---

## Self-Review

### Spec §6 Coverage Table

| Spec §6 sub-project | Requirement | Covered by | Gated vs component |
|---|---|---|---|
| Content endpoint | bearer-gated, **default off**, redaction-aware, by node id / payload_hash; sentinels | redact + B-be1 (`daemon/payload.go`, `GET …/nodes/{nodeId}/payload`, `SetAllowPayloadAccess`+`--allow-payload-access`) | Go 100% (redact, payload.go) |
| Content viewing UI | Input/Output panels, opt-in reveal, pretty-print, copy; disabled/redacted states | B-fe1 (`PayloadPanel.svelte`) | `api.ts`+`payload-view.ts` gated 100%; panel = component |
| Timeline waterfall | time axis, proportional bars, unknown-duration marker, hide rows w/o timing | B-fe2 (`timeline.ts` + `Timeline.svelte`) | `timeline.ts` gated 100%; component |
| KPI strip | sticky cost/tokens/wall-clock/counts/errors/model | B-fe3 (`SessionHeader.svelte`) + B-be3 aggregates | aggregates Go 100%; strip = component |
| Filters + search | status/type/model/has-error + free-text + error chip | B-fe4 (`filters.ts` + `FilterBar.svelte`) | `filters.ts` gated 100%; bar = component |
| Richer aggregates | per-session counts by NodeType + Status + error rate | B-be3 (`SessionSummary` extension) | Go 100% |
| SSE resume + UX | `Last-Event-ID`/`since` tail replay; reconnect backoff; visible desync; surfaced parse errors | B-be2 (server) + B-fe5 (client) | server Go 100%; client logic gated 100% |

### Content-endpoint security / redaction design (summary)

- **Double gate:** (1) `authedAllowQuery` (loopback + bearer header or `?token=`, constant-time) — 401 on failure; (2) daemon `allowPayloadAccess bool`, **default false**, checked in the handler — 403 (`ErrPayloadAccessDisabled`) when off, so the SPA distinguishes "auth failed" from "feature disabled".
- **Default-off mechanism:** `daemon.New` leaves `allowPayloadAccess=false`; only `SetAllowPayloadAccess(true)` (tests) or `--allow-payload-access` (cmd, default false) turns it on. Matches the existing `SetX` config pattern; existing tests/deployments are off with no change.
- **Redaction-aware:** because no redactor exists in-tree, B ships a conservative `redact` package (value-scan pack from ADR-0020 §1 + key-globs); the endpoint feeds `n.Payload.Input/Output` through it and returns redacted bytes + `[]RedactionFinding` + `redacted bool`. Raw `n.Payload` is never copied into the response. Binary/non-UTF8 → `‹binary:len,sha256›` ref. The endpoint reads the in-memory node under `d.mu` (payloads are resident; no new store query).

### Gaps and Risks

1. **Redaction correctness is the headline risk.** There is NO existing redactor (verified); B introduces one. A new regex pack will have false positives (over-redaction, acceptable for a viewing surface) and could have false negatives (a leaked secret — unacceptable). Mitigation: conservative bias (high-entropy catch-all redacts aggressively), 100% per-rule tests asserting no raw-secret substring survives, and the endpoint is default-OFF so the blast radius is operator-opted-in on loopback only. **Scope honesty:** B redacts only the payload Input/Output served by this one endpoint. The full ADR-0020 surface (whole-node redaction across `name`/`subagent_type`/`attrs`, per-sink modes, post-redaction hashing, local-HMAC integrity hash, store/exporter-wide application) is **larger than B** and remains a documented gap — note that today's store/exporters already persist raw payloads (a pre-existing ADR-0008/0020 gap this milestone does not fix and does not worsen). Recommend a follow-up milestone to apply `redact` at ingest/persistence so it covers all sinks.

2. **`Last-Event-ID` semantics vs the coalesce bus.** The coalesce bus (`cdc`) can drop intermediate deltas for a node under backpressure, re-emitting the latest state (A made the flush rev-ordered). Resume is built on the **snapshot**, not the dropped tail: on reconnect the server rebuilds the snapshot from the current live graph and skips `Rev <= cursor`. This is correct because any node/edge changed after the cursor carries `Rev > cursor` and is re-sent in full — the client cannot be left with a stale node it has not seen. The risk is a **monotonicity assumption**: it relies on node/edge `Rev` being strictly increasing per entity (true today) and on `id:` equalling the delta `Rev` (true in `writeEvent`). If a future change makes a delta carry `Rev=0` (e.g. a non-graph lifecycle delta), `parseLastEventID`'s `Rev > cursor` skip and the `id:` emission (guarded by `Rev > 0`) both already handle it. Edge case: a client that applied a `node_status` partial then reconnects — the snapshot only emits `node_upsert`/`edge_upsert` (full nodes), so the resumed state is full, not partial; this is strictly better than today.

3. **`payload_hash` is pre-redaction (ADR-0020 §3 unmet).** The node's `PayloadHash` (returned in `PayloadView` for integrity reference) is computed over the **raw** payload by `model.HashPayload`; ADR-0020 wants stored/exported hashes computed **post-redaction** (a low-entropy secret is brute-forceable from a pre-redaction hash exposed next to a redacted span). B returns the existing hash unchanged (it is already on the node and already exported elsewhere). Mitigation/decision: this is a **pre-existing** condition, not introduced by B; the content endpoint is loopback + bearer + default-off, so the exposure is minimal. Flag for the same redaction follow-up as #1 (recompute hashes post-redaction, keep a local-HMAC pre-redaction hash off-box). Do NOT silently "fix" hashing in B — it ripples into the store/exporters/grpc and is out of scope (spec §11 names the store/exporter schemas as out of scope).

4. **Payload-at-rest access path.** B reads payloads from the **in-memory** node, not the store. A node evicted from memory (terminal + reaped) has no in-memory payload → the endpoint returns 404 (`ErrPayloadNotFound`) for it. This is acceptable for B (the spec's content viewing targets live/open sessions; §10 explicitly defers historical/store-backed reads). If historical payload viewing is wanted, that is a B-follow-up store query (`ObservationsForExecution` already exists and carries payloads) — noted, not built.

5. **Minimalism calls (owner principle).** Several deliberate "less" decisions: (a) timeline is hidden entirely when no node has timing (no empty-state chrome); (b) the error chip / KPI error count is prominent only when `error_count > 0`, otherwise muted; (c) content viewing is collapsed and fetches nothing until the user reveals it (privacy + quiet); (d) the desync pill shows only when stale; (e) no chart library and no decorative KPI sparklines — hand-rolled SVG/CSS. If the owner wants the timeline always visible or the KPIs always loud, those are one-line changes, but the default is silence-when-healthy.

6. **Filter ↔ graph relayout.** Filtering changes the node/edge set fed to `GraphCanvas`, which (per A's design) recomputes layout on topology change. Aggressive filtering = frequent relayout. Mitigation: `filters.ts` returns the same reference for an inactive filter (no relayout when not filtering); debounce the search input in the component (not gated). Risk is performance only, bounded by A's ≤2,000-node target.

7. **No new deps, both sides.** Backend adds only stdlib (`regexp`, `strconv`, `crypto/sha256` already used). Frontend adds zero npm deps (timeline = SVG/CSS, pretty-print = `JSON.stringify`). If a reviewer pushes for a chart lib, resist — it violates the minimalism principle and the dist-drift/bundle-size posture.

### Placeholder / TODO scan

No load-bearing TODOs. Two pinned decision points with stated defaults: (a) redaction scope = payload-only-at-the-endpoint (full ADR-0020 deferred, justified in Gap #1); (b) `payload_hash` left pre-redaction (Gap #3, justified as pre-existing + out-of-scope). Both are explicit, not hidden.

### Determinism / coverage / no-sleep check

- No reducer change in B; aggregate computation is a pure read with a stable-output test (B-be3).
- `redact.Redact` is pure (no clock/I/O); 100% per-rule.
- SSE resume is a pure snapshot filter; tested via `httptest` (no sleep).
- FE: all branching (timeline scale, filter predicates, payload state, backoff/stale/parse-error routing) lives in plain `.ts` gated 100%; components and `.svelte.ts` are thin and excluded. SSE client tests use injected `EventSourceLike` + fake timer — no real timers, no `time.Sleep` equivalent.
- Go: `go test -race ./...` + `internal/codepolicy` + `GOOS={windows,linux,darwin} go build ./...` gate every backend task; `dist`-drift + vitest gate + Playwright gate every FE task.
