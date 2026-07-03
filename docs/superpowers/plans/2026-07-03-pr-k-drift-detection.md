# PR-K: Capture Format Drift Detection (ADR-0025) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement ADR-0025 — each ingest parser reports counts of well-formed-but-unrecognized records keyed by `(source, reason)`; the daemon aggregates them in memory, exposes them in `/metrics`, prints a drift section in `catacomb status` only when nonzero, and logs rate-limited slog warnings; a compile-time tested Claude Code version ceiling flags newer sessions with run meta `format_watch: true` and one warning. Capture behavior never changes (fail-open); quarantine is untouched.

**Architecture:** one new leaf package `ingest/drift` (reason buckets, `Counts`, version ceiling + compare). Parsers (`ingest/hook`, `ingest/streamjson`, `ingest/jsonl`, `ingest/otel`) gain an additive middle return value `drift.Counts` — pure data out, no recorder callbacks, no global state; `jsonl.ParseReader` keeps its signature (offline replay discards counts), so `cmd/catacomb/replay.go` and `reduce/prompt_dedup_test.go` stay untouched. The daemon (`daemon/daemon.go`) owns aggregation (`map[driftKey]uint64` under the existing `d.mu`), the `*slog.Logger` (injectable via `SetLogger`, default `slog.Default()`), the deterministic warn cadence (first occurrence per key, then every 100th), the version watchlist in `applyAndPersist`, and a new `Metrics.Drift` field (flat `source/reason` keys, `omitempty`). `cmd/catacomb/status.go` fetches `/metrics` (unauthenticated, same as `/healthz`) best-effort and renders sorted `drift` rows only when the map is nonempty. `ingest/tail` is a file-tailer, not a format parser — out of scope. No reducer, schema, or store changes.

**Tech Stack:** Go 1.26, testify, table-driven tests, `log/slog` with a buffer-backed `TextHandler` in tests. Repo rules: NO comments in Go (codepolicy), 100% coverage, TDD, gofumpt via `make fmt`, no `time.Sleep` in tests, deterministic outputs, every task ends with a commit.

## Global Constraints

- Work in this worktree only; never touch the shared checkout.
- No comments in Go code — none, not even doc comments (`internal/codepolicy` fails the build otherwise).
- 100% test coverage (`make cover`); TDD: failing test first, minimal implementation, refactor under green. Do not add unreachable guard branches — they cannot be covered.
- `make lint` must pass; `make fmt` before committing.
- Deterministic behavior: warn cadence is count-based (`before == 0 || after/100 > before/100`), never time-based; map iteration for warnings and rendering is sorted; JSON map marshaling is key-sorted by `encoding/json`.
- Reason labels are the five enumerated buckets only — never echo payload content (type names, span names, event names) into counter keys or metrics.
- Counters are in-memory and reset on daemon restart (per ADR); `Recover()` does not rebuild them and this plan must not persist them.
- Version compare is dotted-numeric with the leading integer of each segment (missing/non-numeric segments count as 0); ties (including pre-release suffixes like `2.1.1-beta` vs `2.1.1`) compare equal and are NOT newer. Fail-open: an unparseable version never warns.
- Parser drift enumeration is a deliberate maintenance point (ADR-0025 consequence): known-but-ignored shapes (e.g. `stream_event`, `summary`, `thinking` blocks) must NOT count as drift.

---

### Task 1: `ingest/drift` package — reason buckets, Counts, version ceiling

**Files:**

- Create: `ingest/drift/drift.go`
- Test: `ingest/drift/drift_test.go`

**Interfaces:**

- Produces: `drift.Counts` (`map[string]uint64`) with `Bump(reason) Counts` (lazy-alloc, returns receiver) and `Merge(other) Counts`; reason constants `ReasonUnknownHookEvent`, `ReasonUnknownRecordType`, `ReasonUnknownSubtype`, `ReasonUnknownSpanName`, `ReasonUnknownContentBlock`; `TestedClaudeCodeVersion` const; `NewerThanTested(v string) bool`; `CompareVersions(a, b string) int`. Tasks 2–6 depend on these exact names.

- [ ] **Step 1: Pin the ceiling value**

Run: `claude --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1`
If it prints a version (e.g. `2.1.34`), use that string for `TestedClaudeCodeVersion` below. If the command is unavailable, keep `"2.1.0"`. Record the chosen value in the commit message.

- [ ] **Step 2: Write failing tests**

Create `ingest/drift/drift_test.go` (internal test package, matching all `ingest/*` packages):

```go
package drift

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBumpAllocatesLazily(t *testing.T) {
	var c Counts
	c = c.Bump(ReasonUnknownRecordType)
	c = c.Bump(ReasonUnknownRecordType)
	c = c.Bump(ReasonUnknownSubtype)
	require.Equal(t, Counts{ReasonUnknownRecordType: 2, ReasonUnknownSubtype: 1}, c)
}

func TestMergeNilAndNonNil(t *testing.T) {
	var c Counts
	assert.Nil(t, c.Merge(nil))
	c = c.Merge(Counts{ReasonUnknownSpanName: 3})
	c = c.Bump(ReasonUnknownSpanName)
	merged := c.Merge(Counts{ReasonUnknownHookEvent: 1})
	assert.Equal(t, Counts{ReasonUnknownSpanName: 4, ReasonUnknownHookEvent: 1}, merged)
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.0", "2.1.0", 0},
		{"2.1", "2.1.0", 0},
		{"2.1.1", "2.1.0", 1},
		{"2.0.9", "2.1.0", -1},
		{"10.0", "9.9", 1},
		{"2.1.1-beta", "2.1.1", 0},
		{"beta", "0", 0},
		{"", "0.0.0", 0},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, CompareVersions(tc.a, tc.b), "%s vs %s", tc.a, tc.b)
	}
}

func TestNewerThanTested(t *testing.T) {
	assert.False(t, NewerThanTested(TestedClaudeCodeVersion))
	assert.False(t, NewerThanTested(""))
	assert.True(t, NewerThanTested("9999.0.0"))
}
```

- [ ] **Step 3: Run tests, verify they fail**

Run: `go test ./ingest/drift/`
Expected: FAIL (package does not exist / undefined identifiers).

- [ ] **Step 4: Implement `ingest/drift/drift.go`**

```go
package drift

import (
	"strconv"
	"strings"
)

const TestedClaudeCodeVersion = "2.1.0"

const (
	ReasonUnknownHookEvent    = "unknown_hook_event"
	ReasonUnknownRecordType   = "unknown_record_type"
	ReasonUnknownSubtype      = "unknown_subtype"
	ReasonUnknownSpanName     = "unknown_span_name"
	ReasonUnknownContentBlock = "unknown_content_block"
)

type Counts map[string]uint64

func (c Counts) Bump(reason string) Counts {
	if c == nil {
		c = Counts{}
	}
	c[reason]++
	return c
}

func (c Counts) Merge(other Counts) Counts {
	if len(other) == 0 {
		return c
	}
	if c == nil {
		c = Counts{}
	}
	for reason, n := range other {
		c[reason] += n
	}
	return c
}

func NewerThanTested(v string) bool {
	return CompareVersions(v, TestedClaudeCodeVersion) > 0
}

func CompareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := segment(as, i), segment(bs, i)
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func segment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	s := parts[i]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	v, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return v
}
```

Substitute the ceiling value pinned in Step 1.

- [ ] **Step 5: Green + commit**

Run: `go test -race ./ingest/drift/ && go test -race ./ingest/drift/ -cover`
Expected: PASS, coverage 100.0%.

```bash
git add ingest/drift/
git commit -m "feat(ingest/drift): drift reason buckets, counts, tested Claude Code version ceiling (ADR-0025)"
```

---

### Task 2: Daemon aggregation, rate-limited slog warnings, `/metrics` drift field

**Files:**

- Modify: `daemon/daemon.go` (struct fields, `New`, `SetLogger`, `driftKey`, `recordDrift`, `Metrics`, `metricsSnapshot`)
- Test: `daemon/daemon_test.go`

**Interfaces:**

- Consumes: `drift.Counts` (Task 1).
- Produces: `(*Daemon).recordDrift(source model.Source, dc drift.Counts)` — MUST be called with `d.mu` held (all `Ingest*` methods hold it); `(*Daemon).SetLogger(*slog.Logger)`; `Metrics.Drift map[string]uint64` json `"drift,omitempty"` keyed `source + "/" + reason`. Tasks 3–6 rely on these.

- [ ] **Step 1: Write failing tests**

Add to `daemon/daemon_test.go` (internal `package daemon`; reuse the existing `tempStore(t)` helper; add imports `log/slog`, `strings`, and `"github.com/realkarych/catacomb/ingest/drift"` as needed):

```go
func driftLogBuffer(d *Daemon) *bytes.Buffer {
	var buf bytes.Buffer
	d.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	return &buf
}

func TestRecordDriftAggregatesAndExposesMetrics(t *testing.T) {
	d := New(tempStore(t))
	buf := driftLogBuffer(d)
	d.mu.Lock()
	d.recordDrift(model.SourceStreamJSON, drift.Counts{drift.ReasonUnknownRecordType: 2})
	d.recordDrift(model.SourceStreamJSON, drift.Counts{drift.ReasonUnknownRecordType: 1})
	d.recordDrift(model.SourceStreamJSON, nil)
	d.mu.Unlock()
	m := d.metricsSnapshot()
	require.Equal(t, uint64(3), m.Drift["stream_json/unknown_record_type"])
	assert.Equal(t, 1, strings.Count(buf.String(), "format drift"))
}

func TestRecordDriftWarnsFirstThenEveryNth(t *testing.T) {
	d := New(tempStore(t))
	buf := driftLogBuffer(d)
	d.mu.Lock()
	for range 99 {
		d.recordDrift(model.SourceHook, drift.Counts{drift.ReasonUnknownHookEvent: 1})
	}
	d.mu.Unlock()
	assert.Equal(t, 1, strings.Count(buf.String(), "format drift"))
	d.mu.Lock()
	d.recordDrift(model.SourceHook, drift.Counts{drift.ReasonUnknownHookEvent: 1})
	d.mu.Unlock()
	assert.Equal(t, 2, strings.Count(buf.String(), "format drift"))
}

func TestRecordDriftBatchCrossingWarnsOnce(t *testing.T) {
	d := New(tempStore(t))
	buf := driftLogBuffer(d)
	d.mu.Lock()
	d.recordDrift(model.SourceOTel, drift.Counts{drift.ReasonUnknownSpanName: 1})
	d.recordDrift(model.SourceOTel, drift.Counts{drift.ReasonUnknownSpanName: 250})
	d.mu.Unlock()
	assert.Equal(t, 2, strings.Count(buf.String(), "format drift"))
	assert.Equal(t, uint64(251), d.metricsSnapshot().Drift["otel/unknown_span_name"])
}

func TestMetricsJSONOmitsDriftWhenEmpty(t *testing.T) {
	d := New(tempStore(t))
	raw, err := json.Marshal(d.metricsSnapshot())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"drift"`)
}

func TestSetLoggerNilIsIgnored(t *testing.T) {
	d := New(tempStore(t))
	d.SetLogger(nil)
	d.mu.Lock()
	d.recordDrift(model.SourceHook, drift.Counts{drift.ReasonUnknownHookEvent: 1})
	d.mu.Unlock()
	assert.Equal(t, uint64(1), d.metricsSnapshot().Drift["hook/unknown_hook_event"])
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./daemon/ -run 'TestRecordDrift|TestMetricsJSONOmits|TestSetLogger' -v`
Expected: FAIL (undefined `recordDrift`, `SetLogger`, `Drift`).

- [ ] **Step 3: Implement in `daemon/daemon.go`**

Add `"log/slog"` and `"maps"` to imports and `"github.com/realkarych/catacomb/ingest/drift"` (`slices` is already imported). Add fields to the `Daemon` struct after `reproCaptured`:

```go
	drift  map[driftKey]uint64
	logger *slog.Logger
```

In `New`, add to the literal:

```go
		drift:  map[driftKey]uint64{},
		logger: slog.Default(),
```

Add alongside the other setters:

```go
func (d *Daemon) SetLogger(l *slog.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if l == nil {
		return
	}
	d.logger = l
}
```

Add near `quarantine`:

```go
type driftKey struct {
	source string
	reason string
}

const driftWarnEvery uint64 = 100

func (d *Daemon) recordDrift(source model.Source, dc drift.Counts) {
	for _, reason := range slices.Sorted(maps.Keys(dc)) {
		k := driftKey{source: string(source), reason: reason}
		before := d.drift[k]
		after := before + dc[reason]
		d.drift[k] = after
		if before == 0 || after/driftWarnEvery > before/driftWarnEvery {
			d.logger.Warn("format drift: well-formed input matched no known shape",
				"source", k.source, "reason", reason, "count", after)
		}
	}
}
```

Extend `Metrics` (after `LossyRuns`):

```go
	Drift               map[string]uint64 `json:"drift,omitempty"`
```

In `metricsSnapshot`, before the `return`:

```go
	var driftCounts map[string]uint64
	if len(d.drift) > 0 {
		driftCounts = make(map[string]uint64, len(d.drift))
		for k, v := range d.drift {
			driftCounts[k.source+"/"+k.reason] = v
		}
	}
```

and add `Drift: driftCounts,` to the returned `Metrics` literal.

- [ ] **Step 4: Green + commit**

Run: `go test -race ./daemon/`
Expected: PASS.

```bash
git add daemon/
git commit -m "feat(daemon): drift counter aggregation, rate-limited warnings, /metrics drift field (ADR-0025)"
```

---

### Task 3: hook + stream-json parsers report drift; daemon wires them

**Files:**

- Modify: `ingest/hook/hook.go` (Parse signature), `ingest/streamjson/streamjson.go` (Parse, build, block helpers)
- Modify: `daemon/daemon.go` (`ingestLocked`, `IngestStreamJSONWithLabels`)
- Test: `ingest/hook/hook_test.go`, `ingest/streamjson/streamjson_test.go`, `daemon/daemon_test.go`

**Interfaces:**

- Produces: `hook.Parse(...) ([]model.Observation, drift.Counts, error)`; `streamjson.Parse(...) ([]model.Observation, drift.Counts, error)`. The daemon's `streamParseFn` package var picks up the new type by assignment.
- Consumes: `recordDrift` (Task 2), `drift` package (Task 1).

- [ ] **Step 1: Write failing tests**

`ingest/hook/hook_test.go` (import the drift package; then mechanically update every existing `Parse(` call in the file to receive three values, discarding the middle with `_`):

```go
func TestParseUnknownHookEventCountsDrift(t *testing.T) {
	obs, dc, err := Parse("BrandNewHook", []byte(`{"session_id":"s1"}`), "exec-1", seqGen())
	require.NoError(t, err)
	assert.Nil(t, obs)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownHookEvent: 1}, dc)
}

func TestParseKnownHookEventNoDrift(t *testing.T) {
	obs, dc, err := Parse("Stop", []byte(`{"session_id":"s1"}`), "exec-1", seqGen())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Empty(t, dc)
}
```

`ingest/streamjson/streamjson_test.go` (same mechanical three-value update for existing calls):

```go
func TestParseUnknownRecordTypeCountsDrift(t *testing.T) {
	obs, dc, err := Parse([]byte(`{"type":"telemetry_v2","session_id":"s1"}`), "exec-1", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownRecordType: 1}, dc)
}

func TestParseUnknownSystemSubtypeCountsDrift(t *testing.T) {
	_, dc, err := Parse([]byte(`{"type":"system","subtype":"warmup","session_id":"s1"}`), "exec-1", seq())
	require.NoError(t, err)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownSubtype: 1}, dc)
}

func TestParseCompactBoundarySubtypeNoDrift(t *testing.T) {
	obs, dc, err := Parse([]byte(`{"type":"system","subtype":"compact_boundary","session_id":"s1"}`), "exec-1", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
	assert.Empty(t, dc)
}

func TestParseUnknownContentBlockCountsDrift(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"id":"m1","model":"m","content":[{"type":"hologram"},{"type":"text","text":"hi"}]}}`
	obs, dc, err := Parse([]byte(line), "exec-1", seq())
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownContentBlock: 1}, dc)
}

func TestParseKnownIgnoredShapesNoDrift(t *testing.T) {
	_, dc, err := Parse([]byte(`{"type":"stream_event","session_id":"s1"}`), "exec-1", seq())
	require.NoError(t, err)
	assert.Empty(t, dc)
	_, dc, err = Parse([]byte(`{"type":"assistant","session_id":"s1","message":{"content":[{"type":"thinking"}]}}`), "exec-1", seq())
	require.NoError(t, err)
	assert.Empty(t, dc)
	_, dc, err = Parse([]byte(`{"type":"user","session_id":"s1","message":{"content":[{"type":"image"}]}}`), "exec-1", seq())
	require.NoError(t, err)
	assert.Empty(t, dc)
}
```

`daemon/daemon_test.go` end-to-end (quarantine must NOT grow — drift is the complementary class):

```go
func TestIngestUnknownHookEventRecordsDriftNotQuarantine(t *testing.T) {
	d := New(tempStore(t))
	require.NoError(t, d.Ingest("BrandNewHook", []byte(`{"session_id":"s1"}`)))
	assert.Equal(t, uint64(1), d.metricsSnapshot().Drift["hook/unknown_hook_event"])
	assert.Equal(t, int64(0), d.metricsSnapshot().Quarantined)
}

func TestIngestStreamJSONUnknownTypeRecordsDrift(t *testing.T) {
	d := New(tempStore(t))
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"telemetry_v2","session_id":"s1"}`), "s1"))
	assert.Equal(t, uint64(1), d.metricsSnapshot().Drift["stream_json/unknown_record_type"])
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./ingest/hook/ ./ingest/streamjson/ ./daemon/ 2>&1 | head -20`
Expected: compile errors / FAIL.

- [ ] **Step 3: Implement `ingest/hook/hook.go`**

Change `Parse` (add `"github.com/realkarych/catacomb/ingest/drift"` import):

```go
func Parse(hookType string, payload []byte, executionID string, nextSeq func() uint64) ([]model.Observation, drift.Counts, error) {
	var e envelope
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, nil, fmt.Errorf("hook.Parse: %w", err)
	}
	p := build(hookType, e)
	if p == nil {
		return nil, drift.Counts{drift.ReasonUnknownHookEvent: 1}, nil
	}
```

The function's final return becomes `}}, nil, nil` (observations, nil counts, nil error). `build` is unchanged.

- [ ] **Step 4: Implement `ingest/streamjson/streamjson.go`**

`Parse` threads counts through:

```go
func Parse(line []byte, executionID string, nextSeq func() uint64) ([]model.Observation, drift.Counts, error) {
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, nil, fmt.Errorf("streamjson.Parse: %w", err)
	}
	parts, dc, err := build(e)
	if err != nil {
		return nil, nil, err
	}
	if len(parts) == 0 {
		return []model.Observation{}, dc, nil
	}
```

(final return `return out, dc, nil`). `build` becomes `func build(e envelope) ([]partial, drift.Counts, error)`; every existing `return X, err`/`return X, nil` gains a middle value:

- `case "system"`: `init` returns `[]partial{{...}}, nil, nil`; then:

```go
		if e.Subtype == "compact_boundary" {
			return nil, nil, nil
		}
		return nil, drift.Counts{drift.ReasonUnknownSubtype: 1}, nil
```

(restructure the case so `init` is handled first, `compact_boundary` is known-ignored, everything else counts).

- `case "assistant"`: `return assistantParts(base, msg, text, blocks), unknownBlockCounts(blocks, knownAssistantBlock), nil` (error returns become `nil, nil, err`).
- `case "user"`: `return userParts(base, text, blocks), unknownBlockCounts(blocks, knownUserBlock), nil`.
- `case "stream_event"`: `return nil, nil, nil`.
- `case "result"`: `return []partial{{...}}, nil, nil`.
- `default`: `return nil, drift.Counts{drift.ReasonUnknownRecordType: 1}, nil`.

Add the helpers at the bottom of the file:

```go
func unknownBlockCounts(blocks []block, known func(string) bool) drift.Counts {
	var dc drift.Counts
	for _, b := range blocks {
		if !known(b.Type) {
			dc = dc.Bump(drift.ReasonUnknownContentBlock)
		}
	}
	return dc
}

func knownAssistantBlock(t string) bool {
	switch t {
	case "text", "tool_use", "thinking", "redacted_thinking":
		return true
	default:
		return false
	}
}

func knownUserBlock(t string) bool {
	switch t {
	case "text", "tool_result", "image":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 5: Wire the daemon**

In `daemon/daemon.go` `ingestLocked`:

```go
	obs, dc, err := hook.Parse(hookType, payload, execID, d.next)
	if err != nil {
		return err
	}
	d.recordDrift(model.SourceHook, dc)
```

In `IngestStreamJSONWithLabels`:

```go
	obs, dc, err := streamParseFn(line, execID, d.next)
	if err != nil {
		d.quarantine("stream-json", line, err.Error())
		return nil
	}
	d.recordDrift(model.SourceStreamJSON, dc)
```

- [ ] **Step 6: Green + commit**

Run: `go test -race ./ingest/hook/ ./ingest/streamjson/ ./daemon/ ./cmd/...`
Expected: PASS (fix any remaining mechanical call-site updates in tests first).

```bash
git add ingest/hook/ ingest/streamjson/ daemon/
git commit -m "feat(ingest): hook + stream-json parsers report drift counts (ADR-0025)"
```

---

### Task 4: jsonl + otel parsers report drift; daemon wires them

**Files:**

- Modify: `ingest/jsonl/jsonl.go` (Parse, ParseReader body, decodeLine, block helpers), `ingest/otel/otel.go` (Parse)
- Modify: `daemon/daemon.go` (`IngestOTLP`, `IngestTranscript`)
- Test: `ingest/jsonl/jsonl_test.go`, `ingest/otel/otel_test.go`, `daemon/daemon_test.go`

**Interfaces:**

- Produces: `jsonl.Parse(...) ([]model.Observation, drift.Counts, error)`; `otel.Parse(...) ([]model.Observation, drift.Counts, error)`. `jsonl.ParseReader` keeps `([]model.Observation, error)` — offline replay (`cmd/catacomb/replay.go`, `reduce/prompt_dedup_test.go`) is untouched.
- Consumes: `recordDrift` (Task 2), `drift.Counts.Merge` (Task 1).

- [ ] **Step 1: Ground the known-ignored list against real transcripts**

Run: `cat ~/.claude/projects/*/*.jsonl 2>/dev/null | grep -oE '"type":"[a-z-]+"' | sort | uniq -c | sort -rn | head -12`
Expected: a frequency table of top-level record types. The `knownLineType` switch below starts from `user`, `assistant`, `summary`, `system`, `file-history-snapshot`; add any additional type that appears in this output (they are shipping today, so they are known-ignored, not drift). Record the final list in the commit message.

- [ ] **Step 2: Write failing tests**

`ingest/jsonl/jsonl_test.go` (import drift; mechanically update existing direct `Parse(` callers — NOT `ParseReader` callers — to three values):

```go
func TestParseUnknownRecordTypeCountsDrift(t *testing.T) {
	in := `{"type":"checkpoint_v9","sessionId":"s1"}` + "\n" + `{"type":"summary","summary":"s"}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	assert.Empty(t, obs)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownRecordType: 1}, dc)
}

func TestParseUnknownContentBlockCountsDrift(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"video_frame"}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownContentBlock: 1}, dc)
}

func TestParseReaderDiscardsDriftCounts(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(`{"type":"checkpoint_v9","sessionId":"s1"}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}
```

If the file has no seq-helper for direct `Parse` calls, follow its existing pattern (several tests construct `nextSeq` inline); name the helper `seqFor(t)` only if one does not already exist — reuse what is there.

`ingest/otel/otel_test.go` (mechanical three-value update for existing `Parse(` calls; reuse the file's `strAttr`/span builders):

```go
func TestParseUnknownSpanNameCountsDrift(t *testing.T) {
	req := requestWithSpan(&tracev1.Span{Name: "claude_code.quantum"})
	obs, dc, err := Parse(req, "exec-1", seqGen())
	require.NoError(t, err)
	assert.Empty(t, obs)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownSpanName: 1}, dc)
}

func TestParseKnownSpanNoDrift(t *testing.T) {
	req := requestWithSpan(&tracev1.Span{Name: "claude_code.tool"})
	_, dc, err := Parse(req, "exec-1", seqGen())
	require.NoError(t, err)
	assert.Empty(t, dc)
}
```

Adapt `requestWithSpan`/`seqGen` to the builders and seq helper the file already defines (do not invent parallel helpers).

`daemon/daemon_test.go`:

```go
func TestIngestTranscriptUnknownTypeRecordsDrift(t *testing.T) {
	d := New(tempStore(t))
	require.NoError(t, d.IngestTranscript([]byte(`{"type":"checkpoint_v9","sessionId":"s1"}`), "s1"))
	assert.Equal(t, uint64(1), d.metricsSnapshot().Drift["jsonl/unknown_record_type"])
}

func TestIngestOTLPUnknownSpanRecordsDrift(t *testing.T) {
	d := New(tempStore(t))
	req := otlpRequestWithSpanName(t, "claude_code.quantum")
	require.NoError(t, d.IngestOTLP(req))
	assert.Equal(t, uint64(1), d.metricsSnapshot().Drift["otel/unknown_span_name"])
}
```

Build `otlpRequestWithSpanName` from the OTLP request builders already present in `daemon/daemon_test.go` (it imports the otlp proto packages).

- [ ] **Step 3: Run, verify fail** — `go test ./ingest/jsonl/ ./ingest/otel/ ./daemon/ 2>&1 | head -20` → compile errors / FAIL.

- [ ] **Step 4: Implement `ingest/jsonl/jsonl.go`**

`ParseReader` keeps its signature, discarding counts:

```go
func ParseReader(r io.Reader, executionID string) ([]model.Observation, error) {
	var seq uint64
	next := func() uint64 {
		s := seq
		seq++
		return s
	}
	obs, _, err := Parse(r, executionID, next, func(eventTime time.Time) time.Time { return eventTime })
	return obs, err
}
```

`Parse` returns counts and merges per line:

```go
func Parse(r io.Reader, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) ([]model.Observation, drift.Counts, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var out []model.Observation
	var dc drift.Counts
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		ln, parts, lineCounts, err := decodeLine([]byte(raw))
		if err != nil {
			return nil, nil, err
		}
		dc = dc.Merge(lineCounts)
```

(the rest of the loop body is unchanged; the scanner-error return becomes `return nil, nil, fmt.Errorf(...)`; final `return out, dc, nil`).

`decodeLine` classifies the type before the empty-message early return and scans blocks per role:

```go
func decodeLine(raw []byte) (line, []partial, drift.Counts, error) {
	var ln line
	if err := json.Unmarshal(raw, &ln); err != nil {
		return ln, nil, nil, fmt.Errorf("jsonl.decodeLine: %w", err)
	}
	var dc drift.Counts
	if !knownLineType(ln.Type) {
		dc = dc.Bump(drift.ReasonUnknownRecordType)
	}
	if len(ln.Message) == 0 {
		return ln, nil, dc, nil
	}
```

The message-unmarshal and content errors become `return ln, nil, nil, ...`. In the type switch:

```go
	switch ln.Type {
	case "user":
		parts = userParts(base, text, blocks)
		dc = dc.Merge(unknownBlockCounts(blocks, knownUserBlock))
	case "assistant":
		parts = assistantParts(base, msg, text, blocks)
		dc = dc.Merge(unknownBlockCounts(blocks, knownAssistantBlock))
	}
```

Final return `return ln, parts, dc, nil`. Add helpers (deliberately duplicated from streamjson — each parser owns its enumeration per ADR-0025's consequences):

```go
func knownLineType(t string) bool {
	switch t {
	case "user", "assistant", "summary", "system", "file-history-snapshot":
		return true
	default:
		return false
	}
}

func unknownBlockCounts(blocks []block, known func(string) bool) drift.Counts {
	var dc drift.Counts
	for _, b := range blocks {
		if !known(b.Type) {
			dc = dc.Bump(drift.ReasonUnknownContentBlock)
		}
	}
	return dc
}

func knownAssistantBlock(t string) bool {
	switch t {
	case "text", "tool_use", "thinking", "redacted_thinking":
		return true
	default:
		return false
	}
}

func knownUserBlock(t string) bool {
	switch t {
	case "text", "tool_result", "image":
		return true
	default:
		return false
	}
}
```

Extend `knownLineType` with any extra types found in Step 1.

- [ ] **Step 5: Implement `ingest/otel/otel.go`**

```go
func Parse(req *collectorv1.ExportTraceServiceRequest, executionID string, nextSeq func() uint64) ([]model.Observation, drift.Counts, error) {
	var out []model.Observation
	var dc drift.Counts
	for _, rs := range req.GetResourceSpans() {
		sessionID := sessionFromAttrs(rs.GetResource().GetAttributes())
		resourceAttrs := rs.GetResource().GetAttributes()
		for _, ss := range rs.GetScopeSpans() {
			for _, span := range ss.GetSpans() {
				obs, ok := spanToObservation(span, executionID, sessionID, resourceAttrs, nextSeq)
				if !ok {
					dc = dc.Bump(drift.ReasonUnknownSpanName)
					continue
				}
				out = append(out, obs)
			}
		}
	}
	if out == nil {
		out = []model.Observation{}
	}
	return out, dc, nil
}
```

- [ ] **Step 6: Wire the daemon**

`IngestOTLP`:

```go
	obs, dc, err := parseFn(req, execID, d.next)
	if err != nil {
		var raw []byte
		raw, _ = proto.Marshal(req)
		d.quarantine("otel", raw, err.Error())
		return nil
	}
	d.recordDrift(model.SourceOTel, dc)
```

`IngestTranscript`:

```go
	obs, dc, err := tailParseFn(bytes.NewReader(line), execID, d.next, func(time.Time) time.Time { return nowFn().UTC() })
	if err != nil {
		d.quarantine("jsonl", line, err.Error())
		return nil
	}
	d.recordDrift(model.SourceJSONL, dc)
```

- [ ] **Step 7: Green + commit**

Run: `go test -race ./...`
Expected: PASS across the repo (this catches any remaining call-site drift in other packages).

```bash
git add ingest/jsonl/ ingest/otel/ daemon/
git commit -m "feat(ingest): jsonl + otel parsers report drift counts (ADR-0025)"
```

---

### Task 5: Version watchlist — `format_watch` run meta + one warning

**Files:**

- Modify: `daemon/daemon.go` (field, `watchVersion`, call in `applyAndPersist`)
- Test: `daemon/daemon_test.go`

**Interfaces:**

- Consumes: `drift.NewerThanTested`, `drift.TestedClaudeCodeVersion` (Task 1), `d.logger` (Task 2).
- Produces: run meta key `"format_watch"` (bool), persisted via the existing `UpsertRun` call in `applyAndPersist`.

- [ ] **Step 1: Write failing tests**

```go
func runForSession(t *testing.T, d *Daemon, sessionID string) *model.Run {
	t.Helper()
	for _, g := range d.GraphsForTest() {
		if r, ok := g.Runs[sessionID]; ok {
			return r
		}
	}
	t.Fatalf("run %s not found", sessionID)
	return nil
}

func TestWatchVersionFlagsRunAndWarnsOnce(t *testing.T) {
	d := New(tempStore(t))
	buf := driftLogBuffer(d)
	line := `{"type":"assistant","sessionId":"s1","version":"9999.0.0","message":{"id":"%s","model":"m","role":"assistant","content":[{"type":"text","text":"a"}]}}`
	require.NoError(t, d.IngestTranscript([]byte(fmt.Sprintf(line, "m1")), "s1"))
	require.NoError(t, d.IngestTranscript([]byte(fmt.Sprintf(line, "m2")), "s1"))
	r := runForSession(t, d, "s1")
	assert.Equal(t, true, r.Meta["format_watch"])
	assert.Equal(t, 1, strings.Count(buf.String(), "tested ceiling"))
}

func TestWatchVersionAtCeilingDoesNothing(t *testing.T) {
	d := New(tempStore(t))
	buf := driftLogBuffer(d)
	line := `{"type":"assistant","sessionId":"s2","version":"` + drift.TestedClaudeCodeVersion + `","message":{"id":"m1","model":"m","role":"assistant","content":[{"type":"text","text":"a"}]}}`
	require.NoError(t, d.IngestTranscript([]byte(line), "s2"))
	r := runForSession(t, d, "s2")
	assert.NotContains(t, r.Meta, "format_watch")
	assert.NotContains(t, buf.String(), "tested ceiling")
}
```

- [ ] **Step 2: Run, verify fail** — `go test ./daemon/ -run TestWatchVersion -v` → FAIL.

- [ ] **Step 3: Implement**

Add field `formatWatchWarned bool` to the `Daemon` struct (next to `drift`). In `applyAndPersist`, insert the call directly after `applyFn(g, o)`:

```go
func (d *Daemon) applyAndPersist(g *reduce.Graph, o model.Observation) error {
	applyFn(g, o)
	d.watchVersion(g, o)
	deltas := drainFn(g)
```

Add:

```go
func (d *Daemon) watchVersion(g *reduce.Graph, o model.Observation) {
	v, ok := o.Attrs["claude_code_version"].(string)
	if !ok || !drift.NewerThanTested(v) {
		return
	}
	r := g.Runs[o.RunID]
	if r.Meta == nil {
		r.Meta = map[string]any{}
	}
	r.Meta["format_watch"] = true
	if d.formatWatchWarned {
		return
	}
	d.formatWatchWarned = true
	d.logger.Warn("claude_code_version exceeds tested ceiling; capture continues unchanged",
		"observed", v, "tested", drift.TestedClaudeCodeVersion, "run", o.RunID)
}
```

Do NOT add a nil-run guard: `applyAndPersist` already dereferences `g.Runs[o.RunID]` unconditionally two lines later, so the run always exists after `applyFn` and a guard branch would be uncoverable. `NewerThanTested("")` is false, so no separate empty-string branch either. The meta is persisted by the `UpsertRun` already inside `applyAndPersist`; like `lossy`, `format_watch` is re-derived from live observations rather than rebuilt by `Recover` (consistent with in-memory drift state per ADR).

- [ ] **Step 4: Green + commit**

Run: `go test -race ./daemon/`
Expected: PASS.

```bash
git add daemon/
git commit -m "feat(daemon): claude_code_version watchlist sets format_watch run meta, warns once (ADR-0025)"
```

---

### Task 6: `catacomb status` drift section (only when nonzero)

**Files:**

- Modify: `cmd/catacomb/status.go` (`statusReport`, `runStatus`, `fetchDrift`, human rendering)
- Test: `cmd/catacomb/status_test.go`

**Interfaces:**

- Consumes: `daemon.Metrics.Drift` (Task 2) via `GET /metrics` (unauthenticated, like `/healthz`).
- Produces: `statusReport.Drift map[string]uint64` json `"drift,omitempty"`; human rows `drift\t<source>/<reason>=<n>` sorted by key, printed only when the map is nonempty.

- [ ] **Step 1: Write failing tests**

Add to `cmd/catacomb/status_test.go` (the multi-path handler mirrors the file's existing httptest style):

```go
func driftStatusServer(t *testing.T, metricsBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions":
			_, _ = w.Write([]byte(`[{"session":"s1","node_count":3}]`))
		case "/metrics":
			_, _ = w.Write([]byte(metricsBody))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunStatusShowsDriftWhenNonzero(t *testing.T) {
	srv := driftStatusServer(t, `{"uptime_seconds":1,"drift":{"stream_json/unknown_record_type":4,"hook/unknown_hook_event":1}}`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	output := out.String()
	assert.Contains(t, output, "hook/unknown_hook_event=1")
	assert.Contains(t, output, "stream_json/unknown_record_type=4")
	assert.Less(t, strings.Index(output, "hook/"), strings.Index(output, "stream_json/"))
}

func TestRunStatusNoDriftSectionWhenHealthy(t *testing.T) {
	srv := driftStatusServer(t, `{"uptime_seconds":1}`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "drift")
}

func TestRunStatusJSONIncludesDrift(t *testing.T) {
	srv := driftStatusServer(t, `{"drift":{"otel/unknown_span_name":2}}`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now, asJSON: true}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, uint64(2), rep.Drift["otel/unknown_span_name"])
}

func TestRunStatusDriftFetchFailureIsSilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "drift")
}

func TestRunStatusDriftDecodeErrorIsSilent(t *testing.T) {
	srv := driftStatusServer(t, `{"drift":`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "drift")
}
```

Add `writeStatusDiscovery(t, srv)` as a small helper wrapping the `daemon.WriteDiscovery` boilerplate the existing tests repeat (Addr from `srv.URL`, Token `"tok"`, Pid, StartedAt), or inline the boilerplate if extracting it disturbs the file's style.

**Pre-existing test fix:** `TestRunStatusHealthy` asserts `r.URL.Path == "/v1/sessions"` inside its handler; the new `/metrics` fetch will now also hit that server. Change that handler to switch on `r.URL.Path` and serve `{}` for `/metrics` (keep the sessions body as is). Audit the other status tests with httptest servers the same way — handlers must tolerate a `/metrics` request.

- [ ] **Step 2: Run, verify fail** — `go test ./cmd/catacomb/ -run TestRunStatus 2>&1 | head -20` → FAIL.

- [ ] **Step 3: Implement in `cmd/catacomb/status.go`**

Add `"maps"` and `"slices"` imports. `statusReport` gains, between `Nodes` and `Healthy`:

```go
	Drift          map[string]uint64 `json:"drift,omitempty"`
```

In `runStatus`, directly after the `fetchSessionCounts` line:

```go
	rep.Drift = fetchDrift(ctx, disc, deps.httpClient)
```

(assign onto `rep` after the literal is built, or add `Drift: fetchDrift(ctx, disc, deps.httpClient),` to the literal — match the surrounding style; the fetch must run for JSON mode too). In the human path, after the `nodes` row and before `return w.Flush()`:

```go
	for _, k := range slices.Sorted(maps.Keys(rep.Drift)) {
		_, _ = fmt.Fprintf(w, "drift\t%s=%d\n", k, rep.Drift[k])
	}
```

Add:

```go
func fetchDrift(ctx context.Context, disc daemon.Discovery, client *http.Client) map[string]uint64 {
	u := &url.URL{Scheme: "http", Host: disc.Addr, Path: "/metrics"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m daemon.Metrics
	if decErr := json.NewDecoder(resp.Body).Decode(&m); decErr != nil {
		return nil
	}
	return m.Drift
}
```

The `NewRequestWithContext` error branch is covered by the existing `TestRunStatusFetchSessionsNewRequestError` fixture (`Addr: "host with spaces:99"`), which now exercises `fetchDrift` too. Drift is best-effort: any failure yields `nil`, which renders nothing — silence-when-healthy also means silence-when-unknown; `Healthy` continues to come from the sessions fetch alone.

- [ ] **Step 4: Green + commit**

Run: `go test -race ./cmd/catacomb/`
Expected: PASS.

```bash
git add cmd/catacomb/
git commit -m "feat(cmd): status drift section, shown only when nonzero (ADR-0025)"
```

---

### Task 7: Docs, full gates, live verify

**Files:**

- Modify: `docs/guide/privacy-and-operations.md` (metrics table row, "Format drift" subsection, status paragraph)

- [ ] **Step 1: Docs**

In `docs/guide/privacy-and-operations.md`, add to the `/metrics` response fields table:

```markdown
| `drift` | Well-formed but unrecognized records per `source/reason` key (omitted when zero) |
```

Add a new subsection between "Health and metrics" and "Daemon status":

```markdown
### Format drift

Catacomb watches the upstream Claude Code formats it parses
([ADR-0025](../adr/0025-capture-format-drift-detection.md)). Records that parse
cleanly but match no known shape are counted per `(source, reason)`; reasons are
coarse buckets (`unknown_record_type`, `unknown_subtype`, `unknown_span_name`,
`unknown_hook_event`, `unknown_content_block`) and never contain payload
content. Counters live in daemon memory (reset on restart), surface in
`/metrics` under `drift`, and `catacomb status` prints `drift` rows only when at
least one counter is nonzero. The daemon logs a warning on the first occurrence
per key and every 100th thereafter. Malformed input still goes to quarantine;
drift covers the complementary healthy-but-unknown class.

The binary also carries a tested Claude Code version ceiling. The first session
observed with a newer `claude_code_version` logs one warning and sets
`format_watch: true` in the run's meta; capture proceeds identically. Bump
`TestedClaudeCodeVersion` in `ingest/drift` after verifying a new Claude Code
release (release-checklist item).
```

Extend the "Daemon status" paragraph's field list sentence with: `..., session and node counts, drift counters (when nonzero), and overall health.`

- [ ] **Step 2: Full gates**

Run: `make fmt && make lint && make cover && npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`
Expected: fmt clean, lint 0 issues, coverage total/package/file 100%, markdownlint 0 errors.

- [ ] **Step 3: Live verify**

```bash
make build
TMP=$(mktemp -d)
bin/catacomb daemon --db "$TMP/cat.db" --discovery "$TMP/disc.json" > "$TMP/daemon.log" 2>&1 &
DPID=$!
until [ -f "$TMP/disc.json" ]; do sleep 0.2; done
ADDR=$(python3 -c "import json;print(json.load(open('$TMP/disc.json'))['addr'])")
TOKEN=$(python3 -c "import json;print(json.load(open('$TMP/disc.json'))['token'])")
printf '%s\n%s\n%s\n' \
  '{"type":"telemetry_v2","session_id":"drift-live"}' \
  '{"type":"telemetry_v2","session_id":"drift-live"}' \
  '{"type":"system","subtype":"warmup","session_id":"drift-live"}' \
  | curl -s -X POST "http://$ADDR/v1/stream-json" -H "Authorization: Bearer $TOKEN" --data-binary @-
printf '%s\n' \
  '{"type":"assistant","sessionId":"drift-live-2","version":"9999.0.0","message":{"id":"m1","model":"m","role":"assistant","content":[{"type":"text","text":"hi"}]}}' \
  | curl -s -X POST "http://$ADDR/v1/transcript" -H "Authorization: Bearer $TOKEN" --data-binary @-
curl -s "http://$ADDR/metrics"
CATACOMB_DISCOVERY="$TMP/disc.json" bin/catacomb status
grep -E "format drift|tested ceiling" "$TMP/daemon.log"
kill $DPID
```

Expected:

- `/metrics` JSON contains `"drift":{"stream_json/unknown_record_type":2,"stream_json/unknown_subtype":1}`.
- `status` output contains the rows `drift  stream_json/unknown_record_type=2` and `drift  stream_json/unknown_subtype=1` (tab-aligned) after `nodes`.
- `daemon.log` contains exactly one `format drift` WARN line per `(source, reason)` key (two total) and exactly one `tested ceiling` WARN line.
- Rerun `bin/catacomb daemon` fresh (or restart) and `status` against the new daemon shows NO drift rows — counters reset with the process.

Capture the `/metrics` body, the `status` output, and the two grep lines in the PR description.

- [ ] **Step 4: Commit docs**

```bash
git add docs/guide/privacy-and-operations.md
git commit -m "docs(guide): format drift metrics, status section, version ceiling (ADR-0025)"
```
