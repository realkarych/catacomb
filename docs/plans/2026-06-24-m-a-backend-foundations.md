# Milestone A — Backend Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the four independent, pure-Go, TDD backend sub-projects that unblock the Catacomb UX-overhaul web UI: a deterministic rev-ordered coalesce flush (`cdc`), `t_end`/`duration_ms` stamping for tool/mcp/assistant nodes (`reduce`), a hybrid pricing engine that populates `Node.CostUSD` with reported-vs-estimated provenance (new `pricing` package + reducer consumer interface), and a session-by-hash API (`?session=` SSE filter, `GET /v1/sessions`, `GET /v1/sessions/{hash}/graph`, plus a single authoritative session→executions lookup that fixes the `Recover` mis-keying).

**Architecture:** All four are additive to the existing daemon/reducer/CDC core and preserve the deterministic-reducer contract. They are wired bottom-up: (d) the CDC flush ordering is a pure change to `cdc.Consumer.deliver`; (c) duration stamping is a pure change to `reduce` (`applyTool`/`assistant_turn`/`subagent_stop`) gated by a new `endRank` precedence field on `fieldStamps`; (b) pricing is a new comment-free package whose *consumer* (the reducer) declares the `Pricer` interface it needs, attaching cost during `applyTokens`/`assistant_turn` and carrying provenance via `attrs["cost_source"]` to avoid a `model.Node` field rippling into exporters/grpc/store; (a) the session API adds one authoritative `executionsForSession(hash) []string` derived from each graph's `Run.SessionIDs` (the canonical source) consulted by every session-scoped surface, extends `SubFilter`/`parseSubFilter`/`matchDelta`/`SubscribeFiltered`, and registers two new bearer-gated REST routes returning the exact wire shapes the SPA consumes.

**Tech Stack:** Go 1.26, stdlib (`net/http`, `crypto/subtle`, `encoding/json`, `sort`/`slices`, `errors`), `github.com/stretchr/testify` (already in tree), `github.com/oklog/ulid/v2` (already in tree, tests only). **No new Go dependencies** — the pricing table is hand-written Go data; the model-price tier values are sourced from the `claude-api` skill at implementation time (see Task B, §"Sourcing the price table").

## Global Constraints

These are binding on every task; each task's steps re-verify them.

- **Pure Go, no cgo.** `GOOS=windows go build ./...`, `GOOS=linux go build ./...`, `GOOS=darwin go build ./...` all clean. SQLite stays `modernc.org/sqlite` (untouched here).
- **No Go comments** except `//go:build`, `//go:embed`, `//go:generate`. Enforced by `internal/codepolicy` (`go test ./internal/codepolicy/`). Every Go snippet in this plan is comment-free and must be written comment-free. Generated-header files are skipped wholesale.
- **100% line coverage under `-race`**, TDD-first; the threshold never goes down. Untestable code is a refactoring signal, not an exclusion. New packages (`pricing`) and all modified files (`cdc/cdc.go`, `reduce/reduce.go`, `reduce/graph.go`, `daemon/subscribe.go`, `daemon/sse.go`, `daemon/server.go`, `daemon/daemon.go`) must stay 100%.
- **Deterministic reducer.** Same observations, any order → same graph. Every reducer/pricing/CDC change is proven by an explicit reorder/determinism test (Tasks b, c, d each ship one; Task a ships a replay→recover→`?session=` determinism test).
- **Dependency inversion.** The pricing *consumer* (the reducer) declares the `Pricer` interface; the `pricing` package provides a concrete type satisfying it. No reaching across a boundary into another package's concrete struct.
- **Sentinel errors** checked with `errors.Is`/`errors.As`, never by string; wrap with `fmt.Errorf("pkg.Op: %w", err)`. The session-graph 404 path uses a sentinel.
- **No global mutable state, no `init()` side effects.** The price table is a package-level immutable `map` literal (read-only after construction; never mutated) — acceptable as data, consistent with the existing `var` table style in `reduce` (`sourceRank` etc. are funcs; the price table is a `map` literal returned by a constructor or read by a pure func, never reassigned).
- **No `time.Sleep` in tests** (forbidigo). Use `require.Eventually`, channels, deadlines, or `testing/synctest`, mirroring `daemon/sse_test.go` and `cdc/cdc_test.go`.
- **Table-driven tests + testify** (`require` for fatal, `assert` otherwise). Mirror the established style: `tempStore(t)`, `fixedExecID(d)`, `loopbackListener(t)`, `ob(kind,...)`, `drainLatestForID`.
- **Loopback + bearer trust boundary (ADR-0013).** New endpoints (`GET /v1/sessions`, `GET /v1/sessions/{hash}/graph`) are bearer-gated via `authedAllowQuery` (header or `?token=`, so the SPA `fetch` works), constant-time-compared. Static assets stay open; data is gated.
- **`gofumpt` + `goimports`** (local prefix `github.com/realkarych/catacomb`); `golangci-lint run ./...` clean (forbidigo bans `time.Sleep`; errcheck; bodyclose; govet shadow; unparam).
- **Commit per task** (`feat(...)` / `fix(...)`); never commit to `master` mid-plan; branch first (`git checkout -b feat/m-a-backend-foundations` from `master`); merge is squash; no `--no-verify`.
- **Never `go mod tidy`** for unrelated deps. Pricing adds **no** deps; do not run `go mod tidy` at all unless a genuinely-needed in-tree dep is missing (it is not).

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `cdc/cdc.go` | Modify | Rev-order the `dirty` coalesce drain in `Consumer.deliver` (stable tiebreak on coalesce key) |
| `cdc/cdc_test.go` | Modify | Test: flush order is rev-sorted regardless of insertion order; coalesce-drop semantics preserved |
| `reduce/reduce.go` | Modify | Stamp `TEnd`/`DurationMS` on `tool_call`/`mcp_call`/`assistant_turn` terminal observation via new `endRank` precedence; call `Pricer` during cost attribution; carry `attrs["cost_source"]` |
| `reduce/graph.go` | Modify | Add `pricer Pricer` field + `NewGraphWithPricer`; keep `NewGraph` as a no-cost default (nil-safe) |
| `reduce/pricing_iface.go` | Create | Consumer-declared `Pricer` interface (comment-free) the reducer depends on |
| `reduce/reduce_test.go` | Modify | Reorder/determinism tests for duration stamping; cost attribution + provenance tests with an injected fake `Pricer` |
| `pricing/pricing.go` | Create | `New() *Engine`, `Engine.Cost(in Inputs) (Result, bool)` pure function; versioned price table (`map[string]Tier`); reported-first then estimate; unknown model → not-found |
| `pricing/pricing_test.go` | Create | Pure-function tests with INJECTED representative rates (not real prices); reported-first, estimate tiers, unknown-model, zero-token edge cases — 100% |
| `daemon/daemon.go` | Modify | Fix `Recover` mis-keying; add `executionsForSession(hash) []string` (from `Run.SessionIDs`); wire `pricing.New()` into graph construction |
| `daemon/subscribe.go` | Modify | `SubFilter.SessionID`; `matchDelta` resolves session→exec set at subscribe time; `SubscribeFiltered` scopes snapshot by session |
| `daemon/sse.go` | Modify | `parseSubFilter` reads `?session=`; resolve exec-set once at subscribe; build session-scoped match closure |
| `daemon/sessions.go` | Create | `SessionSummary` struct + `sessionSummaries()`; `sessionGraphDeltas(hash)` returning `[]sseEvent`; `ErrSessionNotFound` sentinel |
| `daemon/server.go` | Modify | Register `GET /v1/sessions` + `GET /v1/sessions/{hash}/graph` (bearer-gated via `authedAllowQuery`); handlers |
| `daemon/sessions_test.go` | Create | Tests for summaries, scoped-graph deltas, 404; replay→recover→`?session=` determinism |
| `daemon/subscribe_test.go` | Modify | `?session=` snapshot + live `matchDelta` tests |
| `daemon/sse_test.go` | Modify | `parseSubFilter` `?session=`; SSE e2e scoped by `?session=` |
| `daemon/server_test.go` | Modify | Route registration + bearer-gating tests for the two new endpoints |

---

## Contracts to PIN (the web UI consumes these — exact)

These are frozen by this plan. The SPA is built against them in later milestones.

### `sseEvent` JSON (UNCHANGED — already in `daemon/sse.go:23`)

```
{kind, rev, run_id, execution_id, node?, edge?, old_id?, new_id?}
```

Go struct verbatim (already exists; do not modify its shape):

```go
type sseEvent struct {
	Kind        string      `json:"kind"`
	Rev         uint64      `json:"rev"`
	RunID       string      `json:"run_id,omitempty"`
	ExecutionID string      `json:"execution_id,omitempty"`
	Node        *model.Node `json:"node,omitempty"`
	Edge        *model.Edge `json:"edge,omitempty"`
	OldID       string      `json:"old_id,omitempty"`
	NewID       string      `json:"new_id,omitempty"`
}
```

### `SessionSummary` JSON for `GET /v1/sessions` (NEW — define exactly)

Returns a JSON array (`[]SessionSummary`). The Go struct (comment-free) with exact field names + JSON tags:

```go
type SessionSummary struct {
	Session    string   `json:"session"`
	Status     string   `json:"status"`
	StartedAt  string   `json:"started_at,omitempty"`
	EndedAt    string   `json:"ended_at,omitempty"`
	DurationMS *int64   `json:"duration_ms,omitempty"`
	TokensIn   int64    `json:"tokens_in"`
	TokensOut  int64    `json:"tokens_out"`
	CostUSD    *float64 `json:"cost_usd,omitempty"`
	CostSource string   `json:"cost_source"`
	NodeCount  int      `json:"node_count"`
	ToolCount  int      `json:"tool_count"`
	ErrorCount int      `json:"error_count"`
	ModelID    string   `json:"model_id,omitempty"`
	RunIDs     []string `json:"run_ids"`
}
```

Field semantics (pin these so the table renders deterministically):

- `session` — the Claude session **hash** (`Correlation.SessionID`), the addressable key. Never the execution id.
- `status` — rolled-up session status string (lattice rule below).
- `started_at` / `ended_at` — RFC3339 UTC strings; `ended_at` omitted while running. Derived from the session node's `TStart`/`TEnd` across the session's executions (min start, max end).
- `duration_ms` — `*int64`; present only when both start and end are known (`ended_at - started_at`). `nil` while running → renders `—`.
- `tokens_in` / `tokens_out` — summed across the session's nodes (always present, default 0).
- `cost_usd` — `*float64`; `nil` when no node in the session has a cost (estimate unavailable / unknown model) → renders `—`.
- `cost_source` — `"reported"` | `"estimated"` | `""`. `""` when `cost_usd` is `nil`. If any contributing node is `reported`, the session is `reported`; else if any is `estimated`, `estimated`; else `""`.
- `node_count` — total nodes in the session's scope.
- `tool_count` — count of `tool_call` + `mcp_call` nodes.
- `error_count` — count of nodes with `Status == error`.
- `model_id` — first non-empty `Run.ModelID` among the session's runs (else `""`).
- `run_ids` — the run ids the session participates in (always present; never `null` — emit `[]`).

### `GET /v1/sessions/{hash}/graph` (NEW)

Returns `[]sseEvent` containing **only** `node_upsert` and `edge_upsert` envelopes — the exact shape the SSE snapshot emits (`SubscribeFiltered` → `deltaToSSE`), payloads stripped (`n.Payload = nil`, same as `daemon/sse.go:46`). Scope = union of the session's executions via `executionsForSession(hash)`. Unknown hash → HTTP 404 (sentinel `ErrSessionNotFound`). Bearer-gated.

---

## Contradictions / gaps found vs the code (read before implementing)

These were verified against the real source and shape several decisions below.

1. **`Recover` mis-keying is real and worse than "by accident".** `daemon/daemon.go:148` writes `d.execBySession[o.RunID] = o.ExecutionID` — keyed on **run id**, while `ingestLocked` (`daemon.go:185-188`) keys on the **Claude session hash** via `sessionIDOf(payload)`. In the replay demo `run_id == "s1" == sessionHash` so they coincide; for real data (run id = ULID) recovery indexes the wrong key. **Decision (Task a):** do **not** depend on `d.execBySession` as the authoritative session index. Build `executionsForSession(hash) []string` by scanning `d.graphs` and consulting each graph's `Run.SessionIDs` (the canonical source, populated below). Additionally fix the `Recover` line to key on the session hash so the dual-written map stops carrying a wrong entry — but the new lookup is the source of truth and is what every session-scoped surface consults. Note `execBySession` is also written by `MarkLossy`/ingest and read by `testsupport.go:execForTest`/`dropShardForTest`; leaving those readers intact, we only correct the `Recover` write.

2. **`Run.SessionIDs` is populated in `ensureRun` (`reduce/reduce.go:323`)** via `r.SessionIDs = appendUnique(r.SessionIDs, o.Correlation.SessionID)`. `appendUnique` (`reduce.go:343`) drops `""`. So a run accrues every distinct non-empty session hash it has seen. This is the canonical session↔run↔exec linkage. Caveat: hook observations only carry `Correlation.SessionID` when the parser sets it; in the replay/test fixtures `ob(...)` sets `Correlation.SessionID = runID` (`reduce_test.go:31`) and `sessionIDOf` extracts `session_id` from the hook payload, which `hook.Parse` must thread into `Correlation.SessionID`. **Verify during Task a** that hook-ingested observations carry `Correlation.SessionID` (the daemon already keys `execBySession` off `sessionIDOf`, so the hash is known at ingest; confirm it reaches `Run.SessionIDs`). The `executionsForSession` lookup depends on this; the determinism test (replay→recover→`?session=`) is the guard.

3. **`cdc.Consumer.deliver` flush is the documented nondeterminism (`cdc/cdc.go:101-115`).** It drains `for k, pending := range c.dirty` — Go map order. The fix orders by `Rev` then coalesce key. Subtlety: `deliver` first drains pending, then tries to send `d`; if `d` itself can't be sent it is coalesced. The rev-ordering must apply to the *drain* loop only; `d`'s own coalesce-on-full path (`cdc.go:112`) is unchanged. The existing drop-semantics tests (`TestPublishDropsAndCoalescesWhenFull`, `TestDroppedDeltaReEmittedWithLatestStateOnNextPublish`, `TestLifecycleDeltaNotCoalescedAwayByNodeChurn`) must still pass unchanged — the new behavior only fixes *order*, not *which* deltas survive.

4. **The `stamp`/precedence mechanism in `reduce` uses per-field rank gates on `fieldStamps` (`reduce.go:197-236`).** `stamp` sets `TStart` by `sourceRank` (OTel>Hook>JSONL); `applyTokens` gates by `tokenRank`; `setName`/`mergePayload`/`upsertParentToolEdge` each have their own rank field. There is **no** end-timestamp field today — `TEnd` is set unconditionally for `session_end` (`reduce.go:36`) and `subagent_stop` (`reduce.go:120`), which is order-independent only because those observation kinds are terminal-by-definition. **Decision (Task c):** add `endRank int` + `haveEnd bool` to `fieldStamps` and a `stampEnd(n, o)` helper mirroring `stamp`'s precedence discipline so the terminal observation with the highest `sourceRank` wins (ties broken by latest `EventTime`, matching the "latest end observation by precedence wins" requirement), guaranteeing reorder-convergence. `DurationMS` is recomputed whenever both `TStart` and `TEnd` are known.

5. **Cost provenance via `attrs` (not a struct field) — confirmed safe.** `model.Node.CostUSD *float64` already exists (`model/model.go:106`) and is already consumed by `daemon/grpc.go:166`, `export/otlp/export.go:264`, `export/postgres/export.go:258`. Populating it is additive (those readers already nil-check). Carrying provenance in `attrs["cost_source"]` (a `map[string]any`, already exported/copied by `copyNode`) avoids a new `model.Node` field that would ripple into the grpc proto (`gen/catacomb/v1/graph.pb.go`), the postgres DDL, and the OTLP attrs. This matches the existing pattern (`reduce.go:389` already writes `attrs["cancel_cause"]`; streamjson `cost_usd` lands in `attrs` at `ingest/streamjson/streamjson.go:129`).

6. **`stream-json` cumulative `total_cost_usd` is already in `attrs["cost_usd"]`** (`ingest/streamjson/streamjson.go:128-129`, attached to the `result`→`assistant_turn` partial). This is the **session-level cumulative** running total, not per-message. **Decision (Task b):** the reducer's reported-first path reads `attrs["cost_usd"]` as the reported per-node cost **only for the node it is attached to** (the `assistant_turn` carrying it), tagging `cost_source=reported`; the session rollup sums node `CostUSD`. To avoid double-counting the cumulative across multiple turns, the plan documents the known limitation (§Task b behavior) and the rollup uses per-node attributed costs only — the cumulative `total_cost_usd` is treated as the cost of its own turn node. Per-message stream-json cost is not currently parsed into a distinct field; if/when it is, it slots into the same `Inputs.ReportedUSD`. This is called out as a documented modeling choice, consistent with the spec's "never double-count" instruction.

7. **No existing `pricing`, `SessionSummary`, or `/v1/sessions` references** anywhere in-tree (verified). Clean greenfield for those names.

8. **`assistant_turn` token attribution already exists** (`reduce.go:55` calls `applyTokens`). Cost attribution hooks in at the same site so it shares the token rank gate and stays order-independent. `tool_call`/`mcp_call` nodes do **not** carry tokens today (only `assistant_turn` does), so per-node cost is attributed at the assistant turn; tool/mcp nodes get durations (Task c) but not cost (no token basis) — this matches the model and the spec's token-tier estimate basis.

---

## Task D — Deterministic rev-ordered coalesce flush

**Files:**

- Modify: `cdc/cdc.go`
- Modify: `cdc/cdc_test.go`

**Interfaces:**

- Produces: no new exported symbols. `Consumer.deliver(d GraphDelta)` (unexported, `cdc/cdc.go:101`) drains `c.dirty` in ascending `Rev` order, ties broken by `coalesceKey`, before attempting to send `d`.
- Consumes: existing `coalesceKey(d GraphDelta) string` (`cdc/cdc.go:76`), `GraphDelta.Rev` (`cdc/cdc.go:24`).

**Notes for implementer:** This is the smallest, most isolated change — do it first. The only behavioral change is **ordering** of the drained pending deltas; the set of deltas delivered and the coalesce-drop accounting (`c.dropped`) must be byte-identical to today. Go map iteration is randomized, so the current drain emits pending deltas in arbitrary order; we sort the pending entries by `Rev` (then coalesce key for a stable tiebreak when two pending share a rev — rare, but keeps the test deterministic). Keep the second half of `deliver` (`select { case c.ch <- d: default: coalesce }`) unchanged.

- [ ] **Step 1: Write the failing test in `cdc/cdc_test.go`**

Add a test that fills the buffer, queues several dirty deltas with out-of-insertion-order revs, then drains and asserts the flushed order is ascending by `Rev`. Mirror the existing `drainLatestForID` drain style.

```go
func TestDirtyFlushIsRevOrdered(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)

	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 100, Node: &model.Node{ID: "occupy", Rev: 100}})

	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 30, Node: &model.Node{ID: "c", Rev: 30}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 10, Node: &model.Node{ID: "a", Rev: 10}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 20, Node: &model.Node{ID: "b", Rev: 20}})

	first := <-c.C
	assert.Equal(t, uint64(100), first.Rev)

	b.Publish(GraphDelta{Kind: DeltaRunStarted, Rev: 999, RunID: "flush"})

	var revs []uint64
	for {
		select {
		case d := <-c.C:
			revs = append(revs, d.Rev)
		default:
			assert.Equal(t, []uint64{10, 20, 30, 999}, revs)
			return
		}
	}
}
```

(The trailing `999` is the flush-trigger delta itself, which sends after the now-drained, rev-ordered pending set; the three coalesced upserts come out `10,20,30` regardless of the `30,10,20` insertion order.)

- [ ] **Step 2: Run the test to verify it fails (flakily, on map order)**

```bash
cd /Users/karych/src/catacomb && go test -run TestDirtyFlushIsRevOrdered -count=20 ./cdc/ 2>&1 | tail -20
```

Expected: at least one `FAIL` across the 20 runs (current code emits in random map order; `-count=20` defeats a lucky pass).

- [ ] **Step 3: Implement the rev-ordered drain in `cdc/cdc.go`**

Add `"slices"` and `"sort"` to the import block (use `slices.SortFunc` to avoid a separate `sort` import — pick one; `slices.SortFunc` is cleaner). Replace the drain loop in `deliver`:

```go
func (c *Consumer) deliver(d GraphDelta) {
	if len(c.dirty) > 0 {
		keys := make([]string, 0, len(c.dirty))
		for k := range c.dirty {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b string) int {
			pa, pb := c.dirty[a], c.dirty[b]
			if pa.Rev != pb.Rev {
				if pa.Rev < pb.Rev {
					return -1
				}
				return 1
			}
			return strings.Compare(a, b)
		})
		for _, k := range keys {
			select {
			case c.ch <- c.dirty[k]:
				delete(c.dirty, k)
			default:
			}
		}
	}
	select {
	case c.ch <- d:
	default:
		c.dirty[coalesceKey(d)] = d
		c.dropped++
	}
}
```

Add `"slices"` and `"strings"` to the imports (the file currently imports only `"sync"` and the `model` package). Keep `delete` on successful send so an again-full channel re-coalesces (preserving the re-emit semantics covered by `TestDroppedDeltaReEmittedWithLatestStateOnNextPublish`).

- [ ] **Step 4: Run the new test repeatedly + the full cdc suite with race**

```bash
cd /Users/karych/src/catacomb && go test -race -count=20 -run TestDirtyFlushIsRevOrdered ./cdc/ && go test -race -count=1 ./cdc/ -v 2>&1 | tail -30
```

Expected: all PASS, no race. The pre-existing drop/coalesce/lifecycle tests must still pass unchanged.

- [ ] **Step 5: Coverage 100% for `cdc/cdc.go`**

```bash
cd /Users/karych/src/catacomb && go test -race -coverprofile=/tmp/covd.out ./cdc/ && go tool cover -func=/tmp/covd.out | grep -v '100.0%' | grep 'cdc/cdc.go' || echo "cdc.go fully covered"
```

Expected: `cdc.go fully covered`. (The `len(c.dirty) > 0` guard's false branch is hit by any publish with an empty dirty map — the very first publish in many existing tests; the true branch by the new test.)

- [ ] **Step 6: Lint + cross-platform build**

```bash
cd /Users/karych/src/catacomb && golangci-lint run ./cdc/ && GOOS=windows go build ./... && GOOS=linux go build ./...
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
cd /Users/karych/src/catacomb && git add cdc/cdc.go cdc/cdc_test.go && git commit -m "fix(cdc): drain coalesce dirty map in rev order (stable tiebreak) for monotonic flush

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task C — Duration / `t_end` stamping

**Files:**

- Modify: `reduce/reduce.go`
- Modify: `reduce/reduce_test.go`

**Interfaces:**

- Produces: no new exported symbols. New unexported helper `func (g *Graph) stampEnd(n *model.Node, o model.Observation)` and `func setDuration(n *model.Node)`; new fields `endRank int` + `haveEnd bool` on `fieldStamps` (`reduce.go:197`).
- Consumes: existing `sourceRank` (`reduce.go:127`), `fieldStamps`/`stampsFor` (`reduce.go:211`), `resolveStatus`/`rank`.

**Notes for implementer:** Today only `session_end`/`subagent_stop` stamp `TEnd`, and they do it unconditionally because those kinds are terminal-by-definition. For `tool_call`/`mcp_call` the terminal signal is the `tool_result` observation (`applyTool`, `reduce.go:81` — it sets status from `attrs["status"]` but never `TEnd`); for `assistant_turn` the terminal signal is the observation that carries final usage/stop (the `assistant_turn` kind itself, `reduce.go:52`). Because these observations can arrive out of order and from multiple sources, the end stamp **must** use the same precedence discipline as `stamp` (highest `sourceRank` wins; ties → latest `EventTime`) so reordering converges. `DurationMS` is `(TEnd - TStart)` in milliseconds, recomputed whenever both are set.

Critically: `applyTool` is called for **both** `assistant_tool_use` and `tool_result` (`reduce.go:57`). Only the **terminal** observation should stamp the end. The cleanest signal: `tool_result` (the result arrival) is the terminal one. Detect it via `o.Kind == "tool_result"`. For assistant turns, the `assistant_turn` observation is itself the end signal (it carries the final usage). For mcp the same `tool_result` path applies (mcp calls also produce `tool_result`).

- [ ] **Step 1: Write failing determinism tests in `reduce/reduce_test.go`**

Add tests asserting (a) a tool gets `TEnd`+`DurationMS` on its `tool_result`, and (b) forward and reversed observation order converge to identical `TEnd`/`DurationMS` (mirroring `TestApplyOrderIndependentFields`, `reduce_test.go:101`).

```go
func TestToolCallStampsEndAndDuration(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(2 * time.Second)
	use := ob("assistant_tool_use", "toolu_d1", t0)
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_d1", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	n := g.Nodes[model.ToolCallID(execID, "toolu_d1")]
	require.NotNil(t, n.TStart)
	require.NotNil(t, n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, t1, *n.TEnd)
	assert.Equal(t, int64(2000), *n.DurationMS)
}

func TestDurationStampOrderIndependent(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(3 * time.Second)
	use := ob("assistant_tool_use", "toolu_d2", t0)
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_d2", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{use, res})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{res, use})

	id := model.ToolCallID(execID, "toolu_d2")
	assert.Equal(t, *fwd.Nodes[id].TEnd, *rev.Nodes[id].TEnd)
	assert.Equal(t, *fwd.Nodes[id].DurationMS, *rev.Nodes[id].DurationMS)
}

func TestAssistantTurnStampsDurationWhenStartAndEndKnown(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)
	first := ob("assistant_turn", "", t0)
	first.Correlation.MessageID = "m1"
	first.Attrs = map[string]any{"tokens_in": int64(1)}
	last := ob("assistant_turn", "", t1)
	last.Correlation.MessageID = "m1"
	last.Attrs = map[string]any{"tokens_in": int64(1)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{first, last})

	n := g.Nodes[model.AssistantTurnID(execID, "m1")]
	require.NotNil(t, n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(1000), *n.DurationMS)
}

func TestEndRankHigherSourceWins(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	jsonlEnd := t0.Add(time.Second)
	otelEnd := t0.Add(5 * time.Second)

	use := ob("assistant_tool_use", "toolu_d3", t0)
	use.Attrs = map[string]any{"name": "Bash"}

	resJSONL := ob("tool_result", "toolu_d3", jsonlEnd)
	resJSONL.Source = model.SourceJSONL
	resJSONL.Attrs = map[string]any{"status": string(model.StatusOK)}

	resOTel := ob("tool_result", "toolu_d3", otelEnd)
	resOTel.Source = model.SourceOTel
	resOTel.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, resJSONL, resOTel})
	rg := NewGraph()
	rg.ApplyAll([]model.Observation{resOTel, resJSONL, use})

	id := model.ToolCallID(execID, "toolu_d3")
	assert.Equal(t, otelEnd, *g.Nodes[id].TEnd)
	assert.Equal(t, otelEnd, *rg.Nodes[id].TEnd)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd /Users/karych/src/catacomb && go test -run 'TestToolCallStampsEnd|TestDurationStamp|TestAssistantTurnStampsDuration|TestEndRank' ./reduce/ -v 2>&1 | tail -30
```

Expected: `FAIL` — `n.TEnd` is nil for tool nodes today.

- [ ] **Step 3: Add `endRank`/`haveEnd` to `fieldStamps` and a `stampEnd` helper in `reduce/reduce.go`**

Extend the `fieldStamps` struct (`reduce.go:197`):

```go
type fieldStamps struct {
	timingRank  int
	haveTiming  bool
	nameSeq     uint64
	haveName    bool
	tokenRank   int
	haveToken   bool
	payloadRank int
	havePayload bool
	structRank  int
	haveStruct  bool
	structSrc   string
	endRank     int
	haveEnd     bool
}
```

Add the helpers (place near `stamp`):

```go
func setDuration(n *model.Node) {
	if n.TStart == nil || n.TEnd == nil {
		return
	}
	ms := n.TEnd.Sub(*n.TStart).Milliseconds()
	n.DurationMS = &ms
}

func (g *Graph) stampEnd(n *model.Node, o model.Observation) {
	fs := g.stampsFor(n.ID)
	r := sourceRank(o.Source)
	switch {
	case !fs.haveEnd || r > fs.endRank:
		ts := o.EventTime
		n.TEnd = &ts
		fs.endRank = r
		fs.haveEnd = true
	case r == fs.endRank && (n.TEnd == nil || o.EventTime.After(*n.TEnd)):
		ts := o.EventTime
		n.TEnd = &ts
	}
	setDuration(n)
}
```

- [ ] **Step 4: Call `stampEnd` from the terminal observation sites**

In `applyTool` (`reduce.go:81`), after `g.stamp(n, o)` and the status handling, stamp the end only on `tool_result`:

```go
func (g *Graph) applyTool(o model.Observation) {
	id := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	nodeType := model.NodeToolCall
	if name, _ := o.Attrs["name"].(string); isMCP(name) {
		nodeType = model.NodeMCPCall
	}
	n := g.node(id, o.RunID, nodeType)
	if n.Type == model.NodeToolCall && nodeType == model.NodeMCPCall {
		n.Type = model.NodeMCPCall
	}
	g.stamp(n, o)
	if o.Kind == "tool_result" {
		g.stampEnd(n, o)
	}
	if name, ok := o.Attrs["name"].(string); ok {
		g.setName(n, o, name)
	}
	if s, ok := o.Attrs["status"].(string); ok {
		n.Status = resolveStatus(n.Status, model.Status(s))
		if n.Status == model.StatusCancelled || n.Status == model.StatusSuperseded {
			g.cascadeStatus(n.ID, n.Status, o.Seq)
		}
	}
	g.mergePayload(n, o.Payload, o.Source)
	g.emitNode(n, o)
	parent := model.SessionNodeID(o.ExecutionID)
	if o.Correlation.MessageID != "" {
		parent = model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID)
	}
	g.upsertEdgeGated(o, parent, id)
	g.upsertParentToolEdge(o)
}
```

For `assistant_turn` (`reduce.go:52`), stamp the end on every assistant_turn observation (the turn's own arrival is its end signal; precedence + latest-time keeps it order-independent and `setDuration` only fires once both ends exist):

```go
	case "assistant_turn":
		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		g.stampEnd(n, o)
		g.applyTokens(n, o.Attrs, o.Source)
		g.emitNode(n, o)
```

`session_end` (`reduce.go:33`) and `subagent_stop` (`reduce.go:111`) currently set `n.TEnd = &ts` directly; route them through `stampEnd` too so their durations populate and they share the precedence rule. Replace the manual `ts := o.EventTime; n.TEnd = &ts` lines in those two cases with `g.stampEnd(n, o)` (keeping the surrounding status logic). This also gives session/subagent nodes a `DurationMS` for free (the drawer's duration metric), with no behavior change to their existing `TEnd` assertions (`TestSessionEnd`, `TestSubagentStop`) since a single terminal observation still sets the same timestamp.

- [ ] **Step 5: Run the new + existing reduce tests with race**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./reduce/ -v 2>&1 | tail -40
```

Expected: all PASS, including pre-existing `TestSessionEnd`/`TestSubagentStop`/`TestApplyOrderIndependentFields`.

- [ ] **Step 6: Coverage 100% for `reduce/reduce.go`**

```bash
cd /Users/karych/src/catacomb && go test -race -coverprofile=/tmp/covc.out ./reduce/ && go tool cover -func=/tmp/covc.out | grep -v '100.0%' | grep 'reduce/reduce.go' || echo "reduce.go fully covered"
```

Expected: `reduce.go fully covered`. (Ensure a test exercises the `r == fs.endRank && After` branch — `TestEndRankHigherSourceWins` covers `>`, and the two-`assistant_turn` test covers equal-rank/later-time; add a same-rank-earlier-then-later ordering if a branch is uncovered.)

- [ ] **Step 7: Commit**

```bash
cd /Users/karych/src/catacomb && git add reduce/reduce.go reduce/reduce_test.go && git commit -m "feat(reduce): stamp t_end/duration_ms on tool/mcp/assistant nodes via endRank precedence

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task B — Hybrid pricing engine → `Node.CostUSD` + session totals

**Files:**

- Create: `reduce/pricing_iface.go` (consumer-declared interface)
- Create: `pricing/pricing.go`
- Create: `pricing/pricing_test.go`
- Modify: `reduce/graph.go` (add `pricer` field + `NewGraphWithPricer`)
- Modify: `reduce/reduce.go` (attribute cost at `assistant_turn`)
- Modify: `reduce/reduce_test.go` (cost attribution + provenance, injected fake)
- Modify: `daemon/daemon.go` (wire `pricing.New()` into graph construction)

**Interfaces:**

- Produces (consumer-declared, in `reduce/pricing_iface.go`):

```go
package reduce

type PriceInputs struct {
	ModelID     string
	TokensIn    int64
	TokensOut   int64
	CacheReadIn int64
	CacheWrite  int64
	ReportedUSD *float64
}

type PriceResult struct {
	USD    float64
	Source string
}

type Pricer interface {
	Cost(in PriceInputs) (PriceResult, bool)
}
```

- Produces (provider, in `pricing/pricing.go`):

```go
package pricing

type Tier struct {
	InputPerMTok     float64
	OutputPerMTok    float64
	CacheReadPerMTok float64
	CacheWritePerMTok float64
}

type Inputs struct {
	ModelID     string
	TokensIn    int64
	TokensOut   int64
	CacheReadIn int64
	CacheWrite  int64
	ReportedUSD *float64
}

type Result struct {
	USD    float64
	Source string
}

func New() *Engine

func (e *Engine) Cost(in Inputs) (Result, bool)
```

`pricing.Engine` satisfies `reduce.Pricer` structurally **after** a tiny adapter, because the two `Inputs`/`Result` types are package-local (dependency inversion: the reducer must not import `pricing`). **Decision:** the daemon (the composition root, `New`/graph construction) wires them with a small adapter that converts `reduce.PriceInputs` → `pricing.Inputs` and `pricing.Result` → `reduce.PriceResult`. The adapter lives in `daemon` (or a thin `reduce`-side wrapper that takes a func). Simplest: `reduce.Pricer` is satisfied by a function adapter:

```go
type PricerFunc func(PriceInputs) (PriceResult, bool)

func (f PricerFunc) Cost(in PriceInputs) (PriceResult, bool) { return f(in) }
```

and the daemon constructs `reduce.NewGraphWithPricer(reduce.PricerFunc(func(in reduce.PriceInputs) (reduce.PriceResult, bool) { r, ok := eng.Cost(pricing.Inputs{...}); return reduce.PriceResult{USD: r.USD, Source: r.Source}, ok }))`. This keeps `reduce` free of any `pricing` import (true dependency inversion) and keeps the engine independently testable.

- Produces (provenance constants in `reduce`): cost source strings `"reported"` / `"estimated"` written to `n.Attrs["cost_source"]`.
- Consumes: `model.Node.CostUSD` (`model/model.go:106`), `attrs["cost_usd"]` from streamjson (`ingest/streamjson/streamjson.go:129`), `model.Node.Attrs`.

**Notes for implementer:** The cost calculation MUST be a **pure function** of (`ModelID`, token tiers, reported cost) with no I/O, no clock, no globals beyond the immutable price table. Tests inject **representative** rates (e.g. `$1/MTok` in, `$5/MTok` out) — they do **NOT** assert real Anthropic prices. The reducer change must preserve determinism: cost is attributed inside the existing `applyTokens` rank gate at the `assistant_turn` site so it converges under reordering exactly like tokens do.

### Sourcing the price table (CRITICAL — do at implementation time, not from memory)

The versioned Go price table (`pricing/pricing.go`'s `map[string]Tier`) holds the **real** model ids and per-token tier values. **Do NOT invent or hardcode prices in this plan or from memory.** When authoring the table, invoke the **`claude-api` skill** and read `shared/models.md` (input/output $/MTok per model id, e.g. `claude-opus-4-8`, `claude-sonnet-4-6`, `claude-haiku-4-5`, `claude-fable-5`) and `shared/prompt-caching.md` for the cache-tier multipliers (cache **write** = 1.25× base input for 5-minute TTL / 2× for 1-hour; cache **read** ≈ 0.1× base input). Derive `CacheReadPerMTok`/`CacheWritePerMTok` from each model's `InputPerMTok` using those documented multipliers (or the explicit cache-pricing rows if the skill provides them at author time). The table is updated by manual PR (spec §10 #2); no runtime fetch. The unit tests inject their own representative `Tier` values via a test-only constructor (`newEngineWithTable(map[string]Tier)`) so they never depend on the real numbers — the real-table values are exercised only by a thin smoke test asserting a known model id resolves and produces a positive cost, not an exact dollar figure.

### Pricing behavior (pin)

1. **Reported-first.** If `in.ReportedUSD != nil`, return `{USD: *in.ReportedUSD, Source: "reported"}, true` without consulting the table. (The reducer passes `attrs["cost_usd"]` here for the node it is attached to — see gap #6; this is the cumulative `total_cost_usd` attributed to its own turn node, never re-summed.)
2. **Estimate fallback.** Else look up `in.ModelID` in the table. If found, compute
   `USD = TokensIn/1e6*InputPerMTok + TokensOut/1e6*OutputPerMTok + CacheReadIn/1e6*CacheReadPerMTok + CacheWrite/1e6*CacheWritePerMTok`
   and return `{USD, Source: "estimated"}, true`.
3. **Unknown model id.** Return `Result{}, false` — the caller leaves `CostUSD` nil (estimate unavailable) and writes no `cost_source`. Never panic.
4. **Zero tokens, known model, no reported** → returns `{USD: 0, Source: "estimated"}, true` (a real 0-cost estimate, distinct from "unavailable").

### Reducer attribution (pin)

In `reduce.go` `assistant_turn`, after `applyTokens`, attribute cost guarded by the same token rank (so reordering converges). Add an unexported `applyCost(n, o)` that:

- builds `PriceInputs` from the node's `TokensIn`/`TokensOut` (post-`applyTokens`), `model` attr (`o.Attrs["model"]`), and reported cost (`o.Attrs["cost_usd"]` as `*float64` if present);
- calls `g.pricer.Cost(...)` (nil-safe: if `g.pricer == nil`, do nothing — `NewGraph` stays cost-free for existing tests);
- on `ok`: sets `n.CostUSD = &res.USD` and `n.Attrs["cost_source"] = res.Source` (initializing `n.Attrs` if nil);
- on `!ok`: leaves `CostUSD` nil and writes no `cost_source`.

Gate it behind the token rank: only (re)attribute when the token rank gate accepts the observation (call `applyCost` from inside/after `applyTokens` only when `applyTokens` actually updated tokens). Simplest deterministic approach: have `applyTokens` return a `bool` (updated) and call `applyCost` only when true; or recompute cost every accepted assistant_turn (idempotent because inputs are the converged token values + model). The reorder/determinism test is the guard.

- [ ] **Step 1: Write failing pricing tests in `pricing/pricing_test.go` (injected rates)**

```go
package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTable() map[string]Tier {
	return map[string]Tier{
		"model-x": {InputPerMTok: 1, OutputPerMTok: 5, CacheReadPerMTok: 0.1, CacheWritePerMTok: 1.25},
	}
}

func TestCostReportedFirst(t *testing.T) {
	e := newEngineWithTable(testTable())
	reported := 0.42
	r, ok := e.Cost(Inputs{ModelID: "model-x", TokensIn: 999999, ReportedUSD: &reported})
	require.True(t, ok)
	assert.Equal(t, "reported", r.Source)
	assert.InDelta(t, 0.42, r.USD, 1e-9)
}

func TestCostEstimateFromTiers(t *testing.T) {
	e := newEngineWithTable(testTable())
	r, ok := e.Cost(Inputs{ModelID: "model-x", TokensIn: 1_000_000, TokensOut: 1_000_000, CacheReadIn: 1_000_000, CacheWrite: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 1+5+0.1+1.25, r.USD, 1e-9)
}

func TestCostUnknownModel(t *testing.T) {
	e := newEngineWithTable(testTable())
	_, ok := e.Cost(Inputs{ModelID: "nope", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostZeroTokensKnownModel(t *testing.T) {
	e := newEngineWithTable(testTable())
	r, ok := e.Cost(Inputs{ModelID: "model-x"})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 0, r.USD, 1e-9)
}

func TestNewHasRealTableEntry(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-8", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.Greater(t, r.USD, 0.0)
}
```

(`TestNewHasRealTableEntry` asserts the real table resolves a known id to a positive cost — not an exact dollar value — so it survives price updates. The model id `claude-opus-4-8` is the current default per the `claude-api` skill; confirm at author time.)

- [ ] **Step 2: Run to verify failure (package does not exist)**

```bash
cd /Users/karych/src/catacomb && go test ./pricing/ 2>&1 | head -10
```

Expected: build failure (`no Go files` / undefined symbols).

- [ ] **Step 3: Implement `pricing/pricing.go`**

```go
package pricing

type Tier struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheReadPerMTok  float64
	CacheWritePerMTok float64
}

type Inputs struct {
	ModelID     string
	TokensIn    int64
	TokensOut   int64
	CacheReadIn int64
	CacheWrite  int64
	ReportedUSD *float64
}

type Result struct {
	USD    float64
	Source string
}

type Engine struct {
	table map[string]Tier
}

func New() *Engine {
	return newEngineWithTable(defaultTable())
}

func newEngineWithTable(t map[string]Tier) *Engine {
	return &Engine{table: t}
}

func (e *Engine) Cost(in Inputs) (Result, bool) {
	if in.ReportedUSD != nil {
		return Result{USD: *in.ReportedUSD, Source: "reported"}, true
	}
	tier, ok := e.table[in.ModelID]
	if !ok {
		return Result{}, false
	}
	usd := perMTok(in.TokensIn, tier.InputPerMTok) +
		perMTok(in.TokensOut, tier.OutputPerMTok) +
		perMTok(in.CacheReadIn, tier.CacheReadPerMTok) +
		perMTok(in.CacheWrite, tier.CacheWritePerMTok)
	return Result{USD: usd, Source: "estimated"}, true
}

func perMTok(tokens int64, perM float64) float64 {
	return float64(tokens) / 1_000_000 * perM
}

func defaultTable() map[string]Tier {
	return map[string]Tier{}
}
```

**At author time** replace `defaultTable()`'s empty literal with the real model ids and tier values **sourced from the `claude-api` skill** (see §"Sourcing the price table"). Keep it comment-free. Example shape (values are placeholders — DO NOT ship these; fetch the real ones):

```go
func defaultTable() map[string]Tier {
	return map[string]Tier{
		"claude-opus-4-8":   {InputPerMTok: 0, OutputPerMTok: 0, CacheReadPerMTok: 0, CacheWritePerMTok: 0},
		"claude-sonnet-4-6": {InputPerMTok: 0, OutputPerMTok: 0, CacheReadPerMTok: 0, CacheWritePerMTok: 0},
		"claude-haiku-4-5":  {InputPerMTok: 0, OutputPerMTok: 0, CacheReadPerMTok: 0, CacheWritePerMTok: 0},
	}
}
```

- [ ] **Step 4: Run pricing tests; 100% coverage**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./pricing/ -v && go test -race -coverprofile=/tmp/covb.out ./pricing/ && go tool cover -func=/tmp/covb.out | grep -v '100.0%' || echo "pricing fully covered"
```

Expected: all PASS, `pricing fully covered`.

- [ ] **Step 5: Create `reduce/pricing_iface.go` (consumer interface + func adapter)**

```go
package reduce

type PriceInputs struct {
	ModelID     string
	TokensIn    int64
	TokensOut   int64
	CacheReadIn int64
	CacheWrite  int64
	ReportedUSD *float64
}

type PriceResult struct {
	USD    float64
	Source string
}

type Pricer interface {
	Cost(in PriceInputs) (PriceResult, bool)
}

type PricerFunc func(PriceInputs) (PriceResult, bool)

func (f PricerFunc) Cost(in PriceInputs) (PriceResult, bool) { return f(in) }
```

- [ ] **Step 6: Add `pricer` field + `NewGraphWithPricer` to `reduce/graph.go`**

```go
type Graph struct {
	Nodes        map[string]*model.Node
	Edges        map[string]*model.Edge
	Runs         map[string]*model.Run
	spanChildren map[string]bool
	stamps       map[string]*fieldStamps
	deltas       []cdc.GraphDelta
	pricer       Pricer
}

func NewGraph() *Graph {
	return newGraph(nil)
}

func NewGraphWithPricer(p Pricer) *Graph {
	return newGraph(p)
}

func newGraph(p Pricer) *Graph {
	return &Graph{
		Nodes:        map[string]*model.Node{},
		Edges:        map[string]*model.Edge{},
		Runs:         map[string]*model.Run{},
		spanChildren: map[string]bool{},
		stamps:       map[string]*fieldStamps{},
		pricer:       p,
	}
}
```

- [ ] **Step 7: Write failing cost-attribution tests in `reduce/reduce_test.go` (injected fake Pricer)**

```go
func TestAssistantTurnCostReportedProvenance(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		if in.ReportedUSD != nil {
			return PriceResult{USD: *in.ReportedUSD, Source: "reported"}, true
		}
		return PriceResult{USD: float64(in.TokensIn+in.TokensOut) / 1000, Source: "estimated"}, true
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc1"
	o.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(10), "tokens_out": int64(5), "cost_usd": 0.25}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc1")]
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, 0.25, *n.CostUSD, 1e-9)
	assert.Equal(t, "reported", n.Attrs["cost_source"])
}

func TestAssistantTurnCostEstimatedProvenance(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{USD: float64(in.TokensIn+in.TokensOut) / 1000, Source: "estimated"}, true
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc2"
	o.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(1000), "tokens_out": int64(0)}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc2")]
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, 1.0, *n.CostUSD, 1e-9)
	assert.Equal(t, "estimated", n.Attrs["cost_source"])
}

func TestAssistantTurnCostUnavailableLeavesNil(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{}, false
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc3"
	o.Attrs = map[string]any{"model": "unknown", "tokens_in": int64(5)}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc3")]
	assert.Nil(t, n.CostUSD)
	_, has := n.Attrs["cost_source"]
	assert.False(t, has)
}

func TestCostAttributionOrderIndependent(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{USD: float64(in.TokensIn), Source: "estimated"}, true
	})
	t0 := time.Unix(0, 0).UTC()
	a := ob("assistant_turn", "", t0)
	a.Source = model.SourceStreamJSON
	a.Correlation.MessageID = "mc4"
	a.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(7)}
	b := ob("assistant_turn", "", t0.Add(time.Second))
	b.Source = model.SourceOTel
	b.Correlation.MessageID = "mc4"
	b.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(11)}

	fwd := NewGraphWithPricer(p)
	fwd.ApplyAll([]model.Observation{a, b})
	rev := NewGraphWithPricer(p)
	rev.ApplyAll([]model.Observation{b, a})

	id := model.AssistantTurnID(execID, "mc4")
	require.NotNil(t, fwd.Nodes[id].CostUSD)
	assert.Equal(t, *fwd.Nodes[id].CostUSD, *rev.Nodes[id].CostUSD)
}

func TestNewGraphNoPricerNoCost(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc5"
	o.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(5)}
	g := NewGraph()
	g.Apply(o)
	assert.Nil(t, g.Nodes[model.AssistantTurnID(execID, "mc5")].CostUSD)
}
```

(`TestCostAttributionOrderIndependent` relies on OTel out-ranking StreamJSON in `tokenRank` (`reduce.go:140`): OTel=2, StreamJSON=1. So the converged tokens are 11 regardless of order, and cost = 11 both ways.)

- [ ] **Step 8: Implement cost attribution in `reduce/reduce.go`**

Change `applyTokens` to report whether it updated, and add `applyCost`:

```go
func (g *Graph) applyTokens(n *model.Node, attrs map[string]any, src model.Source) bool {
	fs := g.stampsFor(n.ID)
	r := tokenRank(src)
	if fs.haveToken && r < fs.tokenRank {
		return false
	}
	fs.tokenRank = r
	fs.haveToken = true
	if v, ok := toInt64(attrs["tokens_in"]); ok {
		n.TokensIn = &v
	}
	if v, ok := toInt64(attrs["tokens_out"]); ok {
		n.TokensOut = &v
	}
	return true
}

func (g *Graph) applyCost(n *model.Node, attrs map[string]any) {
	if g.pricer == nil {
		return
	}
	in := PriceInputs{}
	if m, ok := attrs["model"].(string); ok {
		in.ModelID = m
	}
	if n.TokensIn != nil {
		in.TokensIn = *n.TokensIn
	}
	if n.TokensOut != nil {
		in.TokensOut = *n.TokensOut
	}
	if v, ok := toFloat64(attrs["cost_usd"]); ok {
		in.ReportedUSD = &v
	}
	res, ok := g.pricer.Cost(in)
	if !ok {
		return
	}
	usd := res.USD
	n.CostUSD = &usd
	if n.Attrs == nil {
		n.Attrs = map[string]any{}
	}
	n.Attrs["cost_source"] = res.Source
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}
```

In the `assistant_turn` case, call `applyCost` after `applyTokens` when tokens were accepted (cost recompute is idempotent on converged inputs; calling only on accept keeps a lower-rank late observation from clobbering with stale tokens):

```go
	case "assistant_turn":
		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		g.stampEnd(n, o)
		if g.applyTokens(n, o.Attrs, o.Source) {
			g.applyCost(n, o.Attrs)
		}
		g.emitNode(n, o)
```

- [ ] **Step 9: Wire `pricing.New()` into the daemon graph construction (`daemon/daemon.go`)**

The daemon constructs `reduce.NewGraph()` in five places (`Recover` `daemon.go:144`, `ingestLocked` `daemon.go:195`, `IngestOTLP` `daemon.go:248`, `IngestStreamJSON` `daemon.go:293`, `IngestTranscript` `daemon.go:336`). Add a `pricer reduce.Pricer` field on `Daemon`, construct it once in `New` from `pricing.New()` via the func adapter, and replace each `reduce.NewGraph()` with `reduce.NewGraphWithPricer(d.pricer)`. Add `"github.com/realkarych/catacomb/pricing"` to the daemon imports.

```go
func New(s store.Store) *Daemon {
	eng := pricing.New()
	return &Daemon{
		store:     s,
		newExecID: func() string { return ulid.Make().String() },
		graphs:    map[string]*reduce.Graph{},
		execBySession: map[string]string{},
		bus:           cdc.NewBus(),
		lastSeen:      map[string]time.Time{},
		reaperWindow:  defaultReaperWindow,
		maxShards:     defaultMaxShards,
		startedAt:     nowFn(),
		pricer: reduce.PricerFunc(func(in reduce.PriceInputs) (reduce.PriceResult, bool) {
			r, ok := eng.Cost(pricing.Inputs{
				ModelID:     in.ModelID,
				TokensIn:    in.TokensIn,
				TokensOut:   in.TokensOut,
				CacheReadIn: in.CacheReadIn,
				CacheWrite:  in.CacheWrite,
				ReportedUSD: in.ReportedUSD,
			})
			return reduce.PriceResult{USD: r.USD, Source: r.Source}, ok
		}),
	}
}
```

Add the `pricer reduce.Pricer` field to the `Daemon` struct.

- [ ] **Step 10: Run reduce + daemon suites with race; coverage**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./reduce/ ./pricing/ ./daemon/ 2>&1 | tail -20 && go test -race -coverprofile=/tmp/covb2.out ./reduce/ ./pricing/ && go tool cover -func=/tmp/covb2.out | grep -v '100.0%' | grep -E 'reduce/(reduce|graph|pricing_iface)\.go|pricing/' || echo "fully covered"
```

Expected: all PASS; `fully covered`. (Add a `toFloat64` table test mirroring `TestToInt64` to cover its branches, and ensure the daemon's adapter closure is exercised by an existing ingest test that feeds an `assistant_turn` with a `model` attr — e.g. a streamjson result line — so the daemon's new closure line is covered. If no such daemon test exists, add one ingesting a stream-json `result` with `total_cost_usd`.)

- [ ] **Step 11: Author the REAL price table from the `claude-api` skill**

Invoke the `claude-api` skill, read `shared/models.md` + `shared/prompt-caching.md`, and fill `defaultTable()` with real model ids + per-token tiers (input/output) and derived cache-read/write tiers. Re-run Step 10 and confirm `TestNewHasRealTableEntry` passes against a real id. Do not commit placeholder zeros.

- [ ] **Step 12: Lint + cross-platform build**

```bash
cd /Users/karych/src/catacomb && golangci-lint run ./pricing/ ./reduce/ ./daemon/ && GOOS=windows go build ./... && GOOS=linux go build ./...
```

Expected: clean. (No new deps; do not run `go mod tidy`.)

- [ ] **Step 13: Commit**

```bash
cd /Users/karych/src/catacomb && git add pricing/ reduce/pricing_iface.go reduce/graph.go reduce/reduce.go reduce/reduce_test.go daemon/daemon.go && git commit -m "feat(pricing): hybrid cost engine -> Node.CostUSD + cost_source provenance (consumer-declared interface)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task A — Session-by-hash API

**Files:**

- Modify: `daemon/daemon.go` (fix `Recover` mis-keying; add `executionsForSession`)
- Modify: `daemon/subscribe.go` (`SubFilter.SessionID`; session-scoped `matchDelta` + snapshot)
- Modify: `daemon/sse.go` (`parseSubFilter` reads `?session=`; resolve exec-set at subscribe)
- Create: `daemon/sessions.go` (`SessionSummary`, `sessionSummaries`, `sessionGraphDeltas`, `ErrSessionNotFound`)
- Modify: `daemon/server.go` (register + handle the two new bearer-gated routes)
- Create: `daemon/sessions_test.go`
- Modify: `daemon/subscribe_test.go`, `daemon/sse_test.go`, `daemon/server_test.go`

**Interfaces:**

- Produces:
  - `func (d *Daemon) executionsForSession(hash string) []string` (unexported; the single authoritative session→executions lookup, derived from each graph's `Run.SessionIDs`). Holds `d.mu` is the caller's responsibility — provide an exported-for-test wrapper if needed, but internal callers call it under the lock.
  - `SubFilter.SessionID string` (new field on `daemon/subscribe.go:10`).
  - `func (d *Daemon) sessionSummaries() []SessionSummary` (`daemon/sessions.go`).
  - `func (d *Daemon) sessionGraphDeltas(hash string) ([]sseEvent, error)` (`daemon/sessions.go`; returns `ErrSessionNotFound` for unknown hash).
  - `var ErrSessionNotFound = errors.New("daemon: session not found")`.
  - Routes: `GET /v1/sessions` and `GET /v1/sessions/{hash}/graph`, both `authedAllowQuery`-gated.
- Consumes: `reduce.Graph.RunsSnapshot()` (`reduce/graph.go:27`), `Run.SessionIDs` (`model/model.go:128`), `Graph.Snapshot()` (`reduce/graph.go:72`), `copyNode`/`copyEdge` (`daemon/copy.go`), `deltaToSSE` (`daemon/sse.go:34`), `authedAllowQuery` (`daemon/server.go:124`).

**Notes for implementer:** The authoritative lookup is the linchpin (gap #1, #2). Build `executionsForSession` from `Run.SessionIDs`, NOT from `d.execBySession`. Every session-scoped surface (the `?session=` snapshot, the live `matchDelta`, `GET /v1/sessions/{hash}/graph`) consults it. Resolve the execution set **once at subscribe time** for the live SSE filter (the requirement: "match `delta.ExecutionID ∈ executionsForSession(hash)` resolved at subscribe time") so the live stream doesn't re-scan graphs per delta and so the scope is stable for the subscription's lifetime.

### Sub-step A0 — `executionsForSession` + `Recover` fix

- [ ] **Step 1: Write failing determinism test in `daemon/sessions_test.go` (replay → recover → `?session=`)**

```go
package daemon

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/store"
)

func TestExecutionsForSessionSurvivesRecover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	d.mu.Lock()
	before := d.executionsForSession("s1")
	d.mu.Unlock()
	require.Equal(t, []string{"exec1"}, before)
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())

	d2.mu.Lock()
	after := d2.executionsForSession("s1")
	d2.mu.Unlock()
	assert.Equal(t, before, after)
}

func TestExecutionsForSessionUnknown(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	defer d.mu.Unlock()
	assert.Empty(t, d.executionsForSession("nope"))
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd /Users/karych/src/catacomb && go test -run 'TestExecutionsForSession' ./daemon/ -v 2>&1 | head -20
```

Expected: `FAIL` — `executionsForSession` undefined.

- [ ] **Step 3: Implement `executionsForSession` (in `daemon/daemon.go`) + fix `Recover`**

```go
func (d *Daemon) executionsForSession(hash string) []string {
	if hash == "" {
		return nil
	}
	var out []string
	for execID, g := range d.graphs {
		for _, r := range g.Runs {
			if slices.Contains(r.SessionIDs, hash) {
				out = append(out, execID)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
```

Add `"slices"` to the daemon imports (`sort` is already imported, `daemon.go:9`). Sorting makes the returned set deterministic (the snapshot/graph endpoints iterate it).

Fix `Recover` (`daemon.go:148`): replace `d.execBySession[o.RunID] = o.ExecutionID` with a session-hash keying. The observation carries `Correlation.SessionID`:

```go
		g.Apply(o)
		if o.Correlation.SessionID != "" {
			d.execBySession[o.Correlation.SessionID] = o.ExecutionID
		}
		d.lastSeen[o.RunID] = o.ObservedAt
```

(This keeps `execBySession` honest for its other readers, but `executionsForSession` — the source of truth — does not depend on it. Verify `Recover`'s replayed observations carry `Correlation.SessionID`: they are the same observations persisted by ingest, which set it via `sessionIDOf`→`hook.Parse`. If a stored observation lacks it, `executionsForSession` still works because it reads `Run.SessionIDs`, which `ensureRun` rebuilt during replay.)

- [ ] **Step 4: Run; confirm green**

```bash
cd /Users/karych/src/catacomb && go test -race -run 'TestExecutionsForSession|TestRecover' ./daemon/ -v 2>&1 | tail -20
```

Expected: PASS (including pre-existing `TestRecoverRebuildsGraphsAndSeq` — it asserts `d2.execBySession["s1"] == "exec1"`, which still holds because the replayed observation's `Correlation.SessionID == "s1"`).

### Sub-step A1 — `?session=` SSE filter

- [ ] **Step 5: Write failing filter tests in `daemon/subscribe_test.go` + `daemon/sse_test.go`**

In `daemon/sse_test.go`, assert `parseSubFilter` reads `?session=`:

```go
func TestParseSubFilterSession(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe?session=s1", nil)
	f := parseSubFilter(r)
	assert.Equal(t, "s1", f.SessionID)
}
```

In `daemon/subscribe_test.go`, assert the snapshot is session-scoped and the live `matchDelta` matches only in-session executions:

```go
func TestSubscribeFilteredBySession(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	for _, delta := range sub.Snapshot {
		assert.Equal(t, "exec1", delta.ExecutionID)
	}
	assert.NotEmpty(t, sub.Snapshot)
}

func TestSubscribeSessionUnknownEmptySnapshot(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	sub := d.SubscribeFiltered(SubFilter{SessionID: "ghost"}, 64)
	defer d.Unsubscribe(sub)
	assert.Empty(t, sub.Snapshot)
}
```

- [ ] **Step 6: Run to verify failure**

```bash
cd /Users/karych/src/catacomb && go test -run 'TestParseSubFilterSession|TestSubscribeFilteredBySession|TestSubscribeSessionUnknown' ./daemon/ -v 2>&1 | head -20
```

Expected: `FAIL` (`SessionID` field undefined / snapshot unscoped).

- [ ] **Step 7: Extend `SubFilter`, `Subscription`, `parseSubFilter`, snapshot, and live match**

`daemon/subscribe.go`:

```go
type SubFilter struct {
	RunID     string
	SessionID string
	NodeTypes []string
	Tiers     []string
}
```

Add a resolved exec-set to `Subscription` so the live filter is stable for the subscription's lifetime:

```go
type Subscription struct {
	Snapshot []cdc.GraphDelta
	Consumer *cdc.Consumer
	filter   SubFilter
	execSet  map[string]bool
}
```

In `SubscribeFiltered` (under `d.mu`), resolve the exec-set once and scope the snapshot:

```go
func (d *Daemon) SubscribeFiltered(f SubFilter, bufSize int) *Subscription {
	d.mu.Lock()
	defer d.mu.Unlock()
	var execSet map[string]bool
	if f.SessionID != "" {
		execSet = map[string]bool{}
		for _, e := range d.executionsForSession(f.SessionID) {
			execSet[e] = true
		}
	}
	var snapshot []cdc.GraphDelta
	for execID, g := range d.graphs {
		if execSet != nil && !execSet[execID] {
			continue
		}
		nodes, edges := g.Snapshot()
		for _, n := range nodes {
			if !matchNode(f, n) {
				continue
			}
			nc := copyNode(n)
			snapshot = append(snapshot, cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         n.Rev,
				Node:        nc,
				RunID:       n.RunID,
				ExecutionID: execID,
			})
		}
		for _, e := range edges {
			if !matchEdge(f, e) {
				continue
			}
			ec := copyEdge(e)
			snapshot = append(snapshot, cdc.GraphDelta{
				Kind:        cdc.DeltaEdgeUpsert,
				Rev:         e.Rev,
				Edge:        ec,
				RunID:       e.RunID,
				ExecutionID: execID,
			})
		}
	}
	consumer := d.bus.Subscribe(bufSize)
	return &Subscription{Snapshot: snapshot, Consumer: consumer, filter: f, execSet: execSet}
}
```

Live matching is by execution-set, not the simple `RunID` compare. The cleanest seam is to move from a free `matchDelta(f, delta)` to a subscription-bound check. Add a method on `Subscription`:

```go
func (s *Subscription) match(d cdc.GraphDelta) bool {
	if s.execSet != nil && !s.execSet[d.ExecutionID] {
		return false
	}
	return matchDelta(s.filter, d)
}
```

and in `daemon/sse.go` `streamSSE`, change `if !matchDelta(f, delta)` to `if !sub.match(delta)`. Keep `matchDelta`'s existing run/type/tier behavior for the non-session predicates. (`parseSubFilter` change below adds the `SessionID` read.)

`daemon/sse.go` `parseSubFilter` (`sse.go:52`):

```go
func parseSubFilter(r *http.Request) SubFilter {
	q := r.URL.Query()
	f := SubFilter{
		RunID:     q.Get("run"),
		SessionID: q.Get("session"),
	}
	// existing type/tier loops unchanged
	return f
}
```

(Write the type/tier loops exactly as they are now; no comment in the real code.)

Because `streamSSE` now uses `sub.match`, the `f SubFilter` parameter it receives is redundant for session scope but still used by `matchDelta` inside `sub.match` (which reads `s.filter`). Simplify: `streamSSE` can drop the `f` param and call `sub.match`; or keep the signature and ignore. To minimize churn and keep coverage tractable, keep the existing signature and have `handleSSE` pass `sub`/`f` as today; `sub.match` ignores the passed `f` and uses `s.filter`. Ensure no unused-param lint (`unparam`) fires — if it does, drop `f` from `streamSSE` and update `handleSSE` + `daemon/sse_test.go` callers accordingly.

- [ ] **Step 8: Run filter tests + full sse/subscribe suites with race**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./daemon/ -run 'TestParseSubFilter|TestSubscribe|TestMatch|TestSSE|TestStream' -v 2>&1 | tail -40
```

Expected: PASS, including pre-existing `TestMatchDelta*` and the SSE e2e tests.

### Sub-step A2 — `GET /v1/sessions` + `GET /v1/sessions/{hash}/graph`

- [ ] **Step 9: Write failing tests in `daemon/sessions_test.go` for summaries, scoped graph, 404, and bearer-gating**

```go
func TestSessionSummariesBasic(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	s := sums[0]
	assert.Equal(t, "s1", s.Session)
	assert.GreaterOrEqual(t, s.NodeCount, 2)
	assert.Equal(t, 1, s.ToolCount)
	assert.NotNil(t, s.RunIDs)
	assert.Contains(t, s.RunIDs, "s1")
}

func TestSessionGraphDeltasScoped(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))

	d.mu.Lock()
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)
	require.NotEmpty(t, evs)
	for _, ev := range evs {
		assert.Equal(t, "exec1", ev.ExecutionID)
		assert.Contains(t, []string{"node_upsert", "edge_upsert"}, ev.Kind)
		if ev.Node != nil {
			assert.Nil(t, ev.Node.Payload)
		}
	}
}

func TestSessionGraphDeltasUnknown404(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	_, err := d.sessionGraphDeltas("ghost")
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestSessionsEndpointBearerGated(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp2, err := http.Get(srv.URL + "/v1/sessions?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Contains(t, resp2.Header.Get("Content-Type"), "application/json")
}

func TestSessionGraphEndpoint(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions/s1/graph?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	assert.NotEmpty(t, evs)

	resp404, err := http.Get(srv.URL + "/v1/sessions/ghost/graph?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp404.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp404.StatusCode)

	respUnauth, err := http.Get(srv.URL + "/v1/sessions/s1/graph")
	require.NoError(t, err)
	defer func() { _ = respUnauth.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, respUnauth.StatusCode)
}
```

(Add `"encoding/json"`, `"net/http"`, `"net/http/httptest"` to `daemon/sessions_test.go` imports.)

- [ ] **Step 10: Run to verify failure**

```bash
cd /Users/karych/src/catacomb && go test -run 'TestSessionSummaries|TestSessionGraph|TestSessionsEndpoint' ./daemon/ -v 2>&1 | head -20
```

Expected: `FAIL` — `sessionSummaries`/`sessionGraphDeltas`/`ErrSessionNotFound` undefined; routes 404 from the static handler.

- [ ] **Step 11: Implement `daemon/sessions.go`**

```go
package daemon

import (
	"errors"
	"time"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

var ErrSessionNotFound = errors.New("daemon: session not found")

type SessionSummary struct {
	Session    string   `json:"session"`
	Status     string   `json:"status"`
	StartedAt  string   `json:"started_at,omitempty"`
	EndedAt    string   `json:"ended_at,omitempty"`
	DurationMS *int64   `json:"duration_ms,omitempty"`
	TokensIn   int64    `json:"tokens_in"`
	TokensOut  int64    `json:"tokens_out"`
	CostUSD    *float64 `json:"cost_usd,omitempty"`
	CostSource string   `json:"cost_source"`
	NodeCount  int      `json:"node_count"`
	ToolCount  int      `json:"tool_count"`
	ErrorCount int      `json:"error_count"`
	ModelID    string   `json:"model_id,omitempty"`
	RunIDs     []string `json:"run_ids"`
}

func (d *Daemon) sessionGraphDeltas(hash string) ([]sseEvent, error) {
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, ErrSessionNotFound
	}
	out := []sseEvent{}
	for _, execID := range execs {
		g := d.graphs[execID]
		nodes, edges := g.Snapshot()
		for _, n := range nodes {
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         n.Rev,
				Node:        copyNode(n),
				RunID:       n.RunID,
				ExecutionID: execID,
			}))
		}
		for _, e := range edges {
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaEdgeUpsert,
				Rev:         e.Rev,
				Edge:        copyEdge(e),
				RunID:       e.RunID,
				ExecutionID: execID,
			}))
		}
	}
	return out, nil
}
```

The summaries function aggregates across the session's hashes. Because one session hash can span multiple executions, group by hash: collect every distinct hash across all runs, then for each hash union its executions and aggregate. Pin the rollup rules (status lattice via `rank`, provenance precedence reported>estimated):

```go
func (d *Daemon) sessionSummaries() []SessionSummary {
	hashes := map[string]bool{}
	for _, g := range d.graphs {
		for _, r := range g.Runs {
			for _, h := range r.SessionIDs {
				hashes[h] = true
			}
		}
	}
	out := make([]SessionSummary, 0, len(hashes))
	for h := range hashes {
		out = append(out, d.summarizeSession(h))
	}
	sortSummaries(out)
	return out
}
```

`summarizeSession(hash)` walks `executionsForSession(hash)`'s graphs, summing tokens, counting nodes/tools/errors, min `TStart`/max `TEnd` across the session-scoped nodes (only nodes whose `RunID` belongs to a run containing the hash — or simplest and correct for A: all nodes in the session's executions, since an execution maps 1:1 to a session in current ingest), summing `CostUSD`, folding `cost_source`, and folding run ids + status + model id. Provide helpers `foldStatus(cur, n model.Status) string` (reuse `rank`) and an RFC3339 formatter. Keep all of it pure (no clock). Implement `sortSummaries` to sort by `Session` ascending for deterministic output (the SPA re-sorts client-side; deterministic order makes tests stable and is required for 100% determinism). Use `time.RFC3339` for timestamps.

(Implementation detail left to the implementer, but the field semantics above are binding. Ensure every branch — running session with no `TEnd`, unknown cost, zero tools — is covered by a test so coverage stays 100%.)

- [ ] **Step 12: Register + handle the two routes in `daemon/server.go`**

In `Handler` (`server.go:41`), add **before** the catch-all `mux.Handle("GET /", webui.Handler())` (longer patterns win in ServeMux regardless, but keep them grouped with the other `/v1/` routes):

```go
	mux.HandleFunc("GET /v1/sessions", d.authedAllowQuery(token, d.handleSessions))
	mux.HandleFunc("GET /v1/sessions/{hash}/graph", d.authedAllowQuery(token, d.handleSessionGraph))
```

Add the handlers (in `server.go` or `sessions.go`):

```go
func (d *Daemon) handleSessions(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sums)
}

func (d *Daemon) handleSessionGraph(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	d.mu.Lock()
	evs, err := d.sessionGraphDeltas(hash)
	d.mu.Unlock()
	if errors.Is(err, ErrSessionNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(evs)
}
```

`encoding/json` and `errors` are already imported in `server.go` (`server.go:8-9`). If the handlers live in `sessions.go`, ensure that file imports `encoding/json` and `net/http`.

- [ ] **Step 13: Run the full daemon suite with race + coverage**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./daemon/ -v 2>&1 | tail -40 && go test -race -coverprofile=/tmp/cova.out ./daemon/ && go tool cover -func=/tmp/cova.out | grep -v '100.0%' | grep -E 'daemon/(daemon|subscribe|sse|sessions|server)\.go' || echo "daemon fully covered"
```

Expected: all PASS, no race, `daemon fully covered`. Add targeted tests for any uncovered branch (e.g. a session spanning an error node for `ErrorCount`; a running session for the nil-`EndedAt`/nil-`DurationMS` path; a multi-run session for the `RunIDs` fold).

- [ ] **Step 14: Lint + cross-platform build + full suite**

```bash
cd /Users/karych/src/catacomb && golangci-lint run ./daemon/ && GOOS=windows go build ./... && GOOS=linux go build ./... && go test -race ./... 2>&1 | tail -15
```

Expected: clean; all packages PASS.

- [ ] **Step 15: Commit**

```bash
cd /Users/karych/src/catacomb && git add daemon/daemon.go daemon/subscribe.go daemon/sse.go daemon/sessions.go daemon/server.go daemon/sessions_test.go daemon/subscribe_test.go daemon/sse_test.go daemon/server_test.go && git commit -m "feat(daemon): session-by-hash API — executionsForSession, ?session= filter, /v1/sessions, /v1/sessions/{hash}/graph

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all four tasks)

- [ ] **Whole suite, race, 100% coverage gate**

```bash
cd /Users/karych/src/catacomb && make cover 2>&1 | tail -25
```

Expected: the coverage gate (`.testcoverage.yml`, 100%) passes. No file regresses below threshold.

- [ ] **No-comments policy**

```bash
cd /Users/karych/src/catacomb && go test ./internal/codepolicy/ 2>&1 | tail -5
```

Expected: PASS (only `//go:*` directives in the new/changed files; the `pricing` package has none).

- [ ] **Lint + cross-platform**

```bash
cd /Users/karych/src/catacomb && make lint && GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
```

Expected: clean on all three platforms.

- [ ] **No stray dep changes**

```bash
cd /Users/karych/src/catacomb && git diff --stat go.mod go.sum
```

Expected: **no** changes to `go.mod`/`go.sum` (pricing adds no deps; never ran `go mod tidy`).

---

## Self-Review

### Spec Coverage Table

| Spec section | Requirement | Covered by |
|---|---|---|
| §5.4 / §2.3 #4 | Rev-ordered coalesce flush in `cdc.Consumer.deliver` | Task D (Step 3); `TestDirtyFlushIsRevOrdered` |
| §5.4 | Preserve existing coalesce-drop semantics | Task D — pre-existing `TestPublishDropsAndCoalescesWhenFull` / `…ReEmitted…` / `…LifecycleDelta…` unchanged |
| §5.3 / §2.3 #2 | Stamp `t_end`/`duration_ms` on `tool_call`/`mcp_call`/`assistant_turn` end observation | Task C (Steps 3-4); `TestToolCallStampsEndAndDuration`, `TestAssistantTurnStampsDurationWhenStartAndEndKnown` |
| §5.3 | Same source-precedence discipline as `stamp`; reorder→same result | Task C — `endRank`/`stampEnd`; `TestDurationStampOrderIndependent`, `TestEndRankHigherSourceWins` |
| §5.2 / §2.3 #1 | Populate `Node.CostUSD`; hybrid reported-first then estimate | Task B; `TestAssistantTurnCostReportedProvenance` / `…Estimated…`, `pricing` tests |
| §5.2 | Versioned Go price table (input/output/cache-read/cache-write tiers × tokens) | Task B `pricing/pricing.go` `Tier`/`Cost`/`defaultTable` |
| §5.2 | Unknown model → CostUSD nil, never crash | Task B; `TestCostUnknownModel`, `TestAssistantTurnCostUnavailableLeavesNil` |
| §5.2 | Provenance on node via `attrs["cost_source"]` (no model-struct ripple) | Task B `applyCost`; `cost_source` assertions; gap #5 |
| §5.2 / §10 #2 | Pure function, 100% with injected rates; real prices from `claude-api` skill at impl time | Task B (Steps 1-4 injected; Step 11 real table); §"Sourcing the price table" |
| §5.2 | Dependency inversion — reducer depends on the abstraction | Task B `reduce/pricing_iface.go` (consumer-declared `Pricer`); daemon adapter |
| §5.2 / §5.1 | Roll up per-session cost totals | Task A `SessionSummary.CostUSD`/`CostSource` rollup |
| §5.1.1 / §2.3 (Recover) | Fix `Recover` mis-keying; one authoritative `executionsForSession` from `Run.SessionIDs` | Task A A0; `TestExecutionsForSessionSurvivesRecover` (replay→recover→`?session=` determinism) |
| §5.1 | `?session=` SSE filter — `SubFilter.SessionID`, `parseSubFilter`, snapshot scope, live `matchDelta ∈ execSet` resolved at subscribe | Task A A1; `TestSubscribeFilteredBySession`, `TestParseSubFilterSession` |
| §5.1 | `GET /v1/sessions` (bearer-gated) → `[]SessionSummary` | Task A A2; `TestSessionsEndpointBearerGated` |
| §5.1 | `GET /v1/sessions/{hash}/graph` (bearer-gated) → `[]sseEvent` node/edge upserts, payloads stripped; unknown → 404 | Task A A2; `TestSessionGraphEndpoint`, `TestSessionGraphDeltasUnknown404` |
| Wire contract | `sseEvent` JSON unchanged | Pinned (Contracts) — `daemon/sse.go:23` untouched |
| Wire contract | `SessionSummary` exact fields/tags | Pinned (Contracts) — `daemon/sessions.go` struct |
| Wire contract | `/v1/sessions/{hash}/graph` returns `[]sseEvent`, stripped | Pinned — `sessionGraphDeltas` via `deltaToSSE` |
| §4.2 | No Go comments except go:build/embed/generate | All snippets comment-free; `internal/codepolicy` gate (Final) |
| §4.2 | 100% coverage under -race, TDD-first | Per-task coverage steps + `make cover` (Final) |
| §4.2 | No `time.Sleep`; deterministic reducer | `require.Eventually`/channels; reorder tests per Task |
| §4.2 | Dependency inversion; sentinel errors via errors.Is/As | `Pricer` interface; `ErrSessionNotFound` |
| §4.2 | Bearer-gated new endpoints (ADR-0013) | `authedAllowQuery` on both routes |
| §4.2 | `GOOS=windows go build ./...` clean; no new deps; commit per task | Cross-platform + `go.mod` diff (Final); 4 commits |

### Contradictions / gaps surfaced to the caller

All in the "Contradictions / gaps found vs the code" section above. The load-bearing ones:

1. **`cdc` internals:** `deliver` drains `dirty` in Go map order (`cdc.go:102`); only the *drain order* changes — the drop/coalesce accounting and the `d`-on-full path are unchanged. The fix sorts by `Rev` then `coalesceKey`.
2. **`stamp`/precedence in `reduce`:** there is **no** end-timestamp precedence field today; `TEnd` is set unconditionally for `session_end`/`subagent_stop` only. Task C adds `endRank`/`haveEnd` + `stampEnd` mirroring `stamp`'s discipline, and routes the two existing terminal cases through it too (so they gain `DurationMS` with no `TEnd` behavior change).
3. **`Run.SessionIDs` population:** built in `ensureRun` (`reduce.go:323`) via `appendUnique`, dropping `""`. This is the canonical session↔run↔exec source — `executionsForSession` reads it, **not** `d.execBySession`. The `Recover` mis-keying (`daemon.go:148`, keyed on run id not session hash) is fixed, but the new lookup does not depend on that map; the replay→recover→`?session=` test is the guard.
4. **stream-json `total_cost_usd` is cumulative**, already landing in `attrs["cost_usd"]` (`streamjson.go:129`). The reducer treats it as the reported cost of its own turn node (provenance `reported`) and the rollup sums per-node costs — documented to avoid double-counting (gap #6).
5. **Provenance via `attrs`, not a struct field** — `model.Node.CostUSD` already exists and is consumed by grpc/otlp/postgres; populating it is additive. `cost_source` rides in `attrs` to avoid a proto/DDL/exporter ripple (gap #5).

### Placeholder scan

No TODO/TBD/"similar to" left as code. The only deliberately-deferred concrete value is the **price-table numbers** in `pricing/defaultTable()`, which the plan explicitly instructs to source from the `claude-api` skill at implementation time (Task B Step 11) and which the injected-rate tests do not depend on.

### Type consistency

- `Pricer`/`PriceInputs`/`PriceResult` (reduce) ↔ `Engine.Cost`/`Inputs`/`Result` (pricing) bridged by a func adapter in the daemon composition root — no `pricing` import in `reduce` (dependency inversion intact).
- `SessionSummary` field set/tags match the pinned contract exactly.
- `sessionGraphDeltas` returns `[]sseEvent` built via `deltaToSSE`, identical shape to the SSE snapshot, payloads stripped.
- `SubFilter.SessionID` threads `parseSubFilter` → `SubscribeFiltered` (snapshot scope) → `Subscription.execSet` → `sub.match` (live), all resolved once at subscribe time.
- `executionsForSession` returns a sorted `[]string` for deterministic snapshot/graph ordering.
