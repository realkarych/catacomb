# PR-P: stream-json `result` finalizes the run (V-1 F6) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A hook-less `catacomb run … -- claude -p … --output-format stream-json` run finalizes the moment the child emits its terminal `result` event — run status a genuine terminal (`ok`, or `error` when the result reports `is_error`), `EndedAt` set, run-scope `duration_ms` samples flowing into `aggregate` — instead of sitting `running` for 30 minutes until the idle reaper marks it `abandoned` with no end time (V-1 finding F6, `docs/reviews/2026-07-04-dogfood-calibration.md` §6).

**Architecture:** the `result` event is the protocol's own end-of-session record (ADR-0012 §4: for a `-p` session, `result` *is* the end). `ingest/streamjson` emits one ADDITIONAL `session_end` partial from the `result` case — after the existing F7 session-total `assistant_turn` partial, which stays byte-identical (`session_total: true`, no `model` attr, PR #115) — mirroring the hook path's shape exactly (`ingest/hook/hook.go:79-80`: kind `session_end`, attrs `{"reason": …}`): `reason` is the result subtype verbatim (`success`, `error_max_turns`, `error_during_execution`, `error_max_budget_usd`, …) and `status: "error"` is added only when the envelope's `is_error` is true. The reducer's `session_end` case (`reduce/reduce.go:41-52`) is shared, not forked, with three honest adjustments: (1) node and run status resolve through the existing lattice from an `end` terminal that is `error` when the observation says so, else `ok` (a max-turns run must not end `ok`; run `error` feeds `aggregate.RunTotals.ErrorRate` at `aggregate/aggregate.go:359`); (2) run status latches via `resolveStatus` instead of unconditional assignment, so a second `session_end` can never flip `error` back to `ok` (ADR-0014: among genuine terminals, `terminalRank` keeps `error` over `ok`); (3) `Run.EndedAt` copies the session node's rank-resolved `TEnd` (just stamped by `stampEnd`, hook rank 2 > stream-json rank 0 per `sourceRank`) instead of last-write `o.EventTime`, so hook timing is authoritative in **either** arrival order (the stream `result` usually reaches the daemon before the hook forwarder's `SessionEnd` POST, but the two race; both stamps are daemon-receipt times ms apart). Nothing else moves: the idle reaper (`daemon/daemon.go` `reapIdle`) already skips non-`running` runs and stays the fallback for streams that die before `result`; `ensureRun` reawakens only `abandoned`, so a late observation after a `result`-emitted end updates `LastSeq` without reopening the run; `applyRunEnded` still refuses to abandon an `ok` run (and `resolveStatus(error, abandoned)` keeps `error`, so it now also refuses `error` runs — extend its guard accordingly); the OTLP exporter's second `DeltaSessionEnded` triggers a `FlushRun` that re-exports only nodes upserted since the first flush (the hook-retimed session span), which is correct. Bench/regress presence semantics are untouched: group membership is label-driven, presence comes from marker phases, and the F7 session-total node handling is not disturbed.

**Tech Stack:** Go 1.26, testify, table-driven tests. Repo rules: NO comments in Go (codepolicy), 100% coverage, TDD, gofumpt via `make fmt`, no wall-clock in tests, deterministic outputs, every task ends with a commit.

## Design decisions (grounded against master)

- **Hook mechanism mirrored:** `SessionEnd` → one observation `kind: "session_end"`, `Source: hook`, correlation `{SessionID}`, attrs `{"reason": e.Reason}`. The stream partial mirrors this: kind `session_end`, attrs `{"reason": e.Subtype}` (+ `"status": "error"` iff `is_error`). `Run.EndReason` stays the trigger label `"session_ended"` on both paths (asserted by existing tests); the subtype rides the durable observation attrs.
- **Partial order matters:** `[assistant_turn(session_total), session_end]` — the end must apply after the cost lands so the exporter's `DeltaSessionEnded` flush includes the session-total node, and `LastSeq`/seq stay monotone.
- **Idempotency & precedence:** hooks+stream → two `session_end` observations. Session-node `TEnd` is rank-gated (`stampEnd`: hook 2 > stream 0); `Run.EndedAt` now copies that `TEnd`, so both orders converge to the hook timestamp. Statuses go through `resolveStatus`: `ok`→`ok` no-op, `error` latches over a later `ok` and vice-versa-proof.
- **Reawaken contract intact:** `ensureRun` resets only `StatusAbandoned`; a genuine terminal is never reopened by late observations. The reaper's synthetic `run_ended{timeout}` remains reachable only for runs that never saw any end.
- **`status` attr is `"error"` or absent** — never other values; the reducer accepts exactly `"error"`, defaulting `ok`, keeping every branch coverable.

## Global Constraints

- Work in this worktree only; never touch the shared checkout.
- No comments in Go code — none, not even doc comments (`internal/codepolicy` fails the build otherwise).
- 100% test coverage (`make cover`); TDD: failing test first, minimal implementation, refactor under green. No unreachable guard branches.
- `make lint` 0 issues; `make fmt` before committing; markdownlint on touched docs.
- Deterministic tests: observation `EventTime`s are constructed (`time.Unix(seq, 0)` style helpers), never wall-clock.
- The F7 invariants are load-bearing: the result-derived `assistant_turn` partial keeps `session_total: true`, keeps no `model` attr, and the guard test `TestParseResultNeverCarriesModelAttrElseSessionTotalGetsEstimatePricedAsReported` must stay green untouched.

---

### Task 1: `ingest/streamjson` — `result` also emits a `session_end` partial

**Files:**

- Modify: `ingest/streamjson/streamjson.go` (envelope `IsError`, `build` result case)
- Test: `ingest/streamjson/streamjson_test.go`

**Interfaces:**

- Produces: `result` lines parse to exactly two observations, `[0]` the unchanged session-total `assistant_turn`, `[1]` `session_end` with attrs `{"reason": <subtype>}` and `{"status": "error"}` iff `is_error`. Task 2's reducer tests and Task 3's daemon test depend on this shape.

- [ ] **Step 1: Write failing tests**

Add to `ingest/streamjson/streamjson_test.go` (reuse the file's `fixedNow` and `seq` helpers):

```go
func TestParseResultEmitsSessionEndAfterSessionTotal(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"session_id":"s","usage":{"input_tokens":1,"output_tokens":2},"total_cost_usd":0.5}`)
	obs, dc, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 2)
	assert.Equal(t, "assistant_turn", obs[0].Kind)
	assert.Equal(t, true, obs[0].Attrs["session_total"])
	end := obs[1]
	assert.Equal(t, "session_end", end.Kind)
	assert.Equal(t, "success", end.Attrs["reason"])
	_, hasStatus := end.Attrs["status"]
	assert.False(t, hasStatus)
	assert.Equal(t, "s", end.Correlation.SessionID)
	assert.Equal(t, model.SourceStreamJSON, end.Source)
	assert.Less(t, obs[0].Seq, obs[1].Seq)
	assert.Empty(t, dc)
}

func TestParseResultErrorSubtypesMapErrorStatus(t *testing.T) {
	fixedNow(time.Now())
	for _, subtype := range []string{"error_max_turns", "error_during_execution", "error_max_budget_usd"} {
		line := []byte(`{"type":"result","subtype":"` + subtype + `","is_error":true,"session_id":"s"}`)
		obs, _, err := Parse(line, "e", seq())
		require.NoError(t, err)
		require.Len(t, obs, 2)
		end := obs[1]
		assert.Equal(t, "session_end", end.Kind)
		assert.Equal(t, subtype, end.Attrs["reason"])
		assert.Equal(t, string(model.StatusError), end.Attrs["status"])
	}
}
```

Mechanically bump the existing result-line length assertions from `require.Len(t, obs, 1)` to `require.Len(t, obs, 2)` — the turn stays `obs[0]`, all its assertions unchanged: `TestParseResultCacheTokens`, `TestParseResultEnrichment`, `TestParseResultNoUsageNoCost`, `TestParseResultTurnNoPayload`, `TestParseResultTaggedSessionTotal` (first two `Parse` calls only; the third is an `assistant` line and stays `1`), `TestParseResultNeverCarriesModelAttrElseSessionTotalGetsEstimatePricedAsReported` (loop body). Where a test iterates result observations by index, keep it pinned to `obs[0]`.

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./ingest/streamjson/ 2>&1 | head -20`
Expected: FAIL (`Len` 1 vs 2, missing `session_end`).

- [ ] **Step 3: Implement**

In `ingest/streamjson/streamjson.go`, add to `envelope` after `TotalCostUSD`:

```go
	IsError bool `json:"is_error"`
```

Replace the `case "result":` return with:

```go
	case "result":
		attrs := map[string]any{"session_total": true}
		if e.Usage != nil {
			attrs["tokens_in"] = e.Usage.InputTokens
			attrs["tokens_out"] = e.Usage.OutputTokens
			attrs["cache_read_in"] = e.Usage.CacheReadInputTokens
			attrs["cache_write"] = e.Usage.CacheCreationInputTokens
		}
		if e.TotalCostUSD != nil {
			attrs["cost_usd"] = *e.TotalCostUSD
		}
		endAttrs := map[string]any{"reason": e.Subtype}
		if e.IsError {
			endAttrs["status"] = string(model.StatusError)
		}
		return []partial{
			{kind: "assistant_turn", correlation: base, attrs: attrs},
			{kind: "session_end", correlation: base, attrs: endAttrs},
		}, nil, nil
```

- [ ] **Step 4: Green + commit**

Run: `go test -race ./ingest/streamjson/ ./redact/ && go test -race ./ingest/streamjson/ -cover`
Expected: PASS, coverage 100.0% (`redact` guard test consumes the new observation's scalar attrs — strings and absent/`"error"` only — and must stay green).

```bash
git add ingest/streamjson/
git commit -m "feat(ingest/streamjson): result event emits session_end partial (V-1 F6)"
```

---

### Task 2: `reduce` — honest end status, rank-authoritative run `EndedAt`, idempotent double-end

**Files:**

- Modify: `reduce/reduce.go` (`case "session_end"`, `applyRunEnded` guard)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Produces: `session_end` honors optional attr `status: "error"`; `Run.Status` latches through `resolveStatus`; `Run.EndedAt` equals the session node's rank-resolved `TEnd`. `applyRunEnded` returns early for any genuine-terminal run status (`rank == 3`), not just `ok`. Tasks 3 and 5 depend on a stream-only run reaching `ok`/`error` with `EndedAt` set.

- [ ] **Step 1: Write failing tests**

Add to `reduce/reduce_test.go` (beside `sessionEndObs`):

```go
func streamSessionEndObs(exec, runID, reason, status string, seq uint64) model.Observation {
	attrs := map[string]any{"reason": reason}
	if status != "" {
		attrs["status"] = status
	}
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceStreamJSON, Kind: "session_end",
		Correlation: model.Correlation{SessionID: runID}, Attrs: attrs,
		EventTime: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestStreamSessionEndFinalizesRunOK(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		streamSessionEndObs("e1", "s1", "success", "", 2),
	})
	r := g.Runs["s1"]
	assert.Equal(t, model.StatusOK, r.Status)
	assert.Equal(t, "session_ended", r.EndReason)
	require.NotNil(t, r.EndedAt)
	assert.Equal(t, time.Unix(2, 0).UTC(), *r.EndedAt)
	n := g.Nodes[model.SessionNodeID("e1")]
	assert.Equal(t, model.StatusOK, n.Status)
	require.NotNil(t, n.TEnd)
}

func TestStreamSessionEndErrorEndsRunErrorAndLatches(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		streamSessionEndObs("e1", "s1", "error_max_turns", string(model.StatusError), 2),
		sessionEndObs("e1", "s1", 3),
	})
	r := g.Runs["s1"]
	assert.Equal(t, model.StatusError, r.Status)
	assert.Equal(t, model.StatusError, g.Nodes[model.SessionNodeID("e1")].Status)
	require.NotNil(t, r.EndedAt)
	assert.Equal(t, time.Unix(3, 0).UTC(), *r.EndedAt)
}

func TestHookOKThenStreamErrorEndsRunError(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		sessionEndObs("e1", "s1", 2),
		streamSessionEndObs("e1", "s1", "error_during_execution", string(model.StatusError), 3),
	})
	assert.Equal(t, model.StatusError, g.Runs["s1"].Status)
}

func TestRunEndedAtConvergesToHookTimingBothOrders(t *testing.T) {
	hook := sessionEndObs("e1", "s1", 6)
	stream := streamSessionEndObs("e1", "s1", "success", "", 5)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{sessionStartObs("e1", "s1", 1), stream, hook})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sessionStartObs("e1", "s1", 1), hook, stream})
	require.NotNil(t, fwd.Runs["s1"].EndedAt)
	require.NotNil(t, rev.Runs["s1"].EndedAt)
	assert.Equal(t, time.Unix(6, 0).UTC(), *fwd.Runs["s1"].EndedAt)
	assert.Equal(t, time.Unix(6, 0).UTC(), *rev.Runs["s1"].EndedAt)
}

func TestLateObservationAfterStreamEndDoesNotReopenRun(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		streamSessionEndObs("e1", "s1", "success", "", 2),
		toolObs("e1", "s1", "t9", "Bash", "running", 3),
	})
	r := g.Runs["s1"]
	assert.Equal(t, model.StatusOK, r.Status)
	require.NotNil(t, r.EndedAt)
	assert.Equal(t, time.Unix(2, 0).UTC(), *r.EndedAt)
	assert.Equal(t, uint64(3), r.LastSeq)
}

func TestReaperRunEndedCannotAbandonErrorRun(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		streamSessionEndObs("e1", "s1", "error_max_turns", string(model.StatusError), 2),
		runEndedObs("e1", "s1", "timeout", 3),
	})
	assert.Equal(t, model.StatusError, g.Runs["s1"].Status)
	assert.Equal(t, "session_ended", g.Runs["s1"].EndReason)
}

func TestStreamEndYieldsRunDurationSample(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		streamSessionEndObs("e1", "s1", "success", "", 2),
	})
	rep := aggregate.Aggregate([]aggregate.RunGraph{{Run: *g.Runs["s1"]}}, aggregate.Options{})
	assert.Equal(t, 1, rep.Totals.DurationMS.N)
	assert.Equal(t, float64(1000), rep.Totals.DurationMS.Median)
}
```

Add `"github.com/realkarych/catacomb/aggregate"` to the test file imports (no cycle: `aggregate` imports `model`/`stepkey`/`phasekey` only). `toolObs`, `sessionStartObs`, `sessionEndObs`, `runEndedObs` already exist. Existing tests `TestSessionEndEndsRunOK`, `TestSessionEnd`, and `TestRunStatusGenuineSessionEndLatchesOverRunEnded` must stay green unmodified — they pin the hook path.

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./reduce/ -run 'TestStreamSessionEnd|TestHookOKThen|TestRunEndedAtConverges|TestLateObservation|TestReaperRunEnded|TestStreamEndYields' -v 2>&1 | head -30`
Expected: FAIL (status stays `ok`, `EndedAt` last-write, `run_ended` flips `error` to `abandoned`).

- [ ] **Step 3: Implement**

In `reduce/reduce.go`, replace the `case "session_end":` body:

```go
	case "session_end":
		n := g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
		g.stamp(n, o)
		g.stampEnd(n, o)
		end := model.StatusOK
		if s, ok := o.Attrs["status"].(string); ok && s == string(model.StatusError) {
			end = model.StatusError
		}
		n.Status = resolveStatus(n.Status, end)
		g.emitNode(n, o)
		g.cascadeStatus(n.ID, model.StatusUnknown, o.Seq)
		r := g.Runs[o.RunID]
		r.Status = resolveStatus(r.Status, end)
		ended := *n.TEnd
		r.EndedAt = &ended
		r.EndReason = "session_ended"
		g.emit(cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: o.Seq, RunID: o.RunID, ExecutionID: o.ExecutionID})
```

(`n.TEnd` is non-nil here: `stampEnd` just ran; copy it — do not alias the pointer.)

In `applyRunEnded`, widen the guard from `ok`-only to any genuine terminal:

```go
	if rank(r.Status) == 3 {
		return
	}
```

- [ ] **Step 4: Green + commit**

Run: `go test -race ./reduce/ && go test -race ./reduce/ -cover`
Expected: PASS, coverage 100.0%.

```bash
git add reduce/
git commit -m "feat(reduce): session_end honors error status, run EndedAt takes rank-resolved node end (V-1 F6)"
```

---

### Task 3: daemon end-to-end — stream-only run finalizes, reaper skips it

**Files:**

- Test: `daemon/daemon_test.go` (plus mechanical fallout anywhere `go test ./...` surfaces it)

**Interfaces:**

- Consumes: Tasks 1–2. No daemon source change expected — `IngestStreamJSONWithLabels` already applies every parsed observation and `reapIdle` already skips non-`running` runs; this task proves it end-to-end and sweeps the suite.

- [ ] **Step 1: Write failing-or-passing tests (they must fail before Tasks 1–2, pass after)**

Add to `daemon/daemon_test.go`:

```go
func TestStreamJSONOnlyRunFinalizesAndReaperSkipsIt(t *testing.T) {
	d := New(tempStore(t))
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"f6","model":"m"}`,
		`{"type":"assistant","session_id":"f6","message":{"id":"m1","model":"m","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"f6","usage":{"input_tokens":10,"output_tokens":5},"total_cost_usd":0.01}`,
	}
	for _, l := range lines {
		require.NoError(t, d.IngestStreamJSON([]byte(l), "f6"))
	}
	g := d.GraphsForTest()[d.execForTest("f6")]
	require.NotNil(t, g)
	r := g.Runs["f6"]
	require.NotNil(t, r)
	assert.Equal(t, model.StatusOK, r.Status)
	require.NotNil(t, r.EndedAt)
	assert.Equal(t, "session_ended", r.EndReason)
	require.NoError(t, d.reapIdle(time.Now().Add(24*time.Hour)))
	r = d.GraphsForTest()[d.execForTest("f6")].Runs["f6"]
	assert.Equal(t, model.StatusOK, r.Status)
	assert.Equal(t, "session_ended", r.EndReason)
}

func TestStreamJSONErrorResultFinalizesRunError(t *testing.T) {
	d := New(tempStore(t))
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"f6e","model":"m"}`), "f6e"))
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"session_id":"f6e"}`), "f6e"))
	r := d.GraphsForTest()[d.execForTest("f6e")].Runs["f6e"]
	require.NotNil(t, r)
	assert.Equal(t, model.StatusError, r.Status)
	require.NotNil(t, r.EndedAt)
}
```

(If `d.reapIdle` uses a different receiver spelling, match it — the test file is internal `package daemon`.)

- [ ] **Step 2: Full-suite mechanical sweep**

Run: `go test ./... 2>&1 | grep -v '^ok' | head -40`
Result lines now also finalize runs in every fixture that ingests one (`daemon/server_test.go`, `cmd/catacomb/bench_test.go`, `redact/scalarattrs_guard_test.go`, `reduce/reduce_test.go` F7 tests). Any assertion that expected a run to stay `running`/`abandoned` after a `result` line must flip to the new truthful terminal — update the assertion, never the mechanism. `TestCumulativeCostNoDoubleCount` (two result lines, same session) exercises the double-end path and must pass unmodified.

- [ ] **Step 3: Green + commit**

Run: `go test -race ./...`
Expected: PASS.

```bash
git add -A
git commit -m "test(daemon): stream-json-only run finalizes ok/error, reaper skips it (V-1 F6)"
```

---

### Task 4: docs — hooks are richer capture, no longer required for finalization

**Files:**

- Modify: `docs/guide/cli.md` (`### run`, `### bench`)

- [ ] **Step 1: `### run`**

Append to the "Exits with the child's exit code…" paragraph:

```markdown
The child's terminal stream-json `result` event finalizes the run as the child exits: run
status becomes `ok` (or `error` when the result reports `is_error`), `ended_at` is set, and
run-scope `duration_ms` flows into `runs`/`regress` — no hooks required. Hooks are still worth
installing for richer capture: authoritative start/end timing (hook observations outrank
stream-json ones), tool payload precedence, permission-deny (`blocked`) statuses, and
compaction/notification markers. When both fire, the run ends once — hook timing wins and the
second end signal is a no-op. The idle reaper remains the fallback for streams that die before
`result` (such runs end `abandoned` after the quiescence window).
```

- [ ] **Step 2: `### bench`**

After the "`bench` requires a running daemon…" paragraph, add:

```markdown
Cells finalize from the child's stream `result` event, so run status, `ended_at`, and run-scope
`duration_ms` work without hooks. Installing hooks (project-local is enough) still sharpens end
timing and adds permission and notification capture.
```

- [ ] **Step 3: Lint + commit**

Run: `npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`
Expected: 0 errors.

```bash
git add docs/guide/cli.md
git commit -m "docs(guide): run/bench finalize from the stream result event; hooks add precision (V-1 F6)"
```

---

### Task 5: full gates + live verify (hook-less and hooks+stream)

- [ ] **Step 1: Full gates**

Run: `make fmt && make lint && make cover`
Expected: fmt clean, lint 0, coverage 100%.

- [ ] **Step 2: Live verify — hook-less run finalizes promptly**

```bash
make build
TMP=$(mktemp -d)
BIN=$(pwd)/bin/catacomb
"$BIN" daemon --db "$TMP/cat.db" --discovery "$TMP/disc.json" > "$TMP/daemon.log" 2>&1 &
DPID=$!
until [ -f "$TMP/disc.json" ]; do sleep 0.2; done
mkdir "$TMP/proj"
cd "$TMP/proj"   # fresh dir: NO hooks installed
CATACOMB_DISCOVERY="$TMP/disc.json" "$BIN" run --label f6=nohooks -- \
  claude -p "reply with exactly: ok" --model haiku --output-format stream-json --verbose
kill $DPID; wait $DPID 2>/dev/null
"$BIN" runs --db "$TMP/cat.db" --json
sqlite3 "$TMP/cat.db" "select json_extract(body,'\$.source') from observations where json_extract(body,'\$.kind')='session_end';"
sqlite3 "$TMP/cat.db" "select status from runs;"
```

Expected, immediately after the child exits (no 30-minute wait):

- `runs --json`: the run shows `"status": "ok"`, a nonempty `"ended_at"`, and a positive `"duration_ms"`.
- `session_end` observations: exactly one, source `stream_json`.
- `runs` table status: `ok`.

- [ ] **Step 3: Live verify — hooks+stream run, no regression, hook owns end timing**

```bash
"$BIN" daemon --db "$TMP/cat2.db" --discovery "$TMP/disc2.json" > "$TMP/daemon2.log" 2>&1 &
DPID=$!
until [ -f "$TMP/disc2.json" ]; do sleep 0.2; done
cd "$TMP/proj"
CATACOMB_DISCOVERY="$TMP/disc2.json" "$BIN" install-hooks --project
CATACOMB_DISCOVERY="$TMP/disc2.json" "$BIN" run --label f6=hooks -- \
  claude -p "reply with exactly: ok" --model haiku --output-format stream-json --verbose
kill $DPID; wait $DPID 2>/dev/null
"$BIN" runs --db "$TMP/cat2.db" --json
sqlite3 "$TMP/cat2.db" "select json_extract(body,'\$.source') from observations where json_extract(body,'\$.kind')='session_end' order by 1;"
```

Expected:

- Run `ok` with `ended_at` and `duration_ms` — no double-end anomaly, no `abandoned`.
- Two `session_end` observations: `hook` and `stream_json`; the run's `ended_at` matches the hook-side end (rank-resolved), verified by comparing against `select json_extract(body,'$.event_time') …` per source if the timestamps differ visibly.

Capture both `runs --json` bodies and the two sqlite outputs in the PR description.

- [ ] **Step 4: Commit any live-verify fallout**

If live verify surfaced fixes, land them TDD-style (failing test first) before this final commit; otherwise nothing to commit here.
