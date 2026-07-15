# PV-1: Offline Parity Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `catacomb bench --offline` runs a basket with no daemon, writes redacted evidence copies under a runs dir, and `catacomb regress --runs-dir` gates regressions from those evidence dirs — proving ADR-0026's architecture before any deletion.

**Architecture:** Additive only. A new `evidence` package owns durable run-evidence dirs (redacted transcript copies + `meta.json`). New offline helpers in `cmd/catacomb` reuse the existing `ingest/jsonl → reduce` path (`loadGraph` in `replay.go` is the model): parse main + subagent transcripts, inject synthetic `task:<id>` boundary-marker observations (mirroring `daemon/mark.go`), verify checkpoints against the in-process graph. `regress` gains `--runs-dir` resolution for `label:` selectors.

**Tech Stack:** Go 1.26, cobra, `modernc.org/sqlite` untouched (no store changes in PV-1), stdlib only otherwise.

## Global Constraints

- No comments in Go code (only `//go:build`, `//go:embed`, `//go:generate`); enforced by `internal/codepolicy`.
- 100% file/package/total coverage (`.testcoverage.yml`); every error branch needs a test.
- TDD: failing test before implementation, every task.
- `golangci-lint` bans direct `time.Sleep` calls (`forbidigo`): use a `var sleepFn = time.Sleep` indirection.
- Module path `github.com/realkarych/catacomb`. Repo commit style: `feat(scope): …` / `fix(scope): …`.
- Existing symbols this plan builds on (do not redefine): `runSetup`, `cellEnv`, `cloneLabels`, `exitInfo`, `spawnFailure`, `appendNote`, `nowFn`, `execCommand`, `lineObserver`, `newExecutionID`, `loadGraph` (all in `cmd/catacomb`), `bench.Cell`, `bench.Manifest`, `aggregate.RunGraph`, `reduce.NewGraph`, `redact.DefaultPolicy`, `redact.Redact`, `ijsonl.ParseReader`, `model.Observation`, `model.NodeMarker`.

---

### Task 1: `evidence` package — durable run-evidence dirs

**Files:**

- Create: `evidence/evidence.go`
- Test: `evidence/evidence_test.go`

**Interfaces:**

- Consumes: `redact.Redact([]byte) redact.Result` (use `.Data`).
- Produces:
  - `type Meta struct { RunID, Task, Variant string; Rep int; SessionID string; Labels map[string]string; ExitCode int; CostUSD *float64; BasketHash, MarkerName string; MarkerStart, MarkerEnd, FinishedAt time.Time }` (JSON tags snake_case, e.g. `run_id`, `marker_start`).
  - `type SourceFile struct { Src, Rel string }`
  - `func Write(dir string, m Meta, files []SourceFile) error`
  - `func ReadMeta(dir string) (Meta, error)`
  - `type Run struct { Dir string; Meta Meta }`
  - `func ScanRuns(root string) ([]Run, error)` — sorted by `Meta.RunID`; dirs without a readable `meta.json` are skipped.
  - `func MatchLabels(have, want map[string]string) bool` — subset match.

- [ ] **Step 1: Write failing tests** — roundtrip, redaction property, scan/match, error paths:

```go
package evidence_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/redact"
)

func sampleMeta(runID, variant string) evidence.Meta {
	return evidence.Meta{
		RunID: runID, Task: "t1", Variant: variant, Rep: 1,
		SessionID: "sess-1", Labels: map[string]string{"basket": "b", "variant": variant},
		ExitCode: 0, BasketHash: "h", MarkerName: "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(), MarkerEnd: time.Unix(200, 0).UTC(),
		FinishedAt: time.Unix(201, 0).UTC(),
	}
}

func TestWriteReadRoundtrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{\"a\":1}\n{\"b\":2}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-1")
	m := sampleMeta("run-1", "base")
	require.NoError(t, evidence.Write(dir, m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
	got, err := evidence.ReadMeta(dir)
	require.NoError(t, err)
	require.Equal(t, m, got)
	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, string(redact.Redact([]byte("{\"a\":1}")).Data)+"\n"+string(redact.Redact([]byte("{\"b\":2}")).Data)+"\n", string(copied))
}

func TestWriteNestedRelAndErrors(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-2")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-2", "base"), []evidence.SourceFile{{Src: src, Rel: filepath.Join("subagents", "agent-1.jsonl")}}))
	_, err := os.Stat(filepath.Join(dir, "subagents", "agent-1.jsonl"))
	require.NoError(t, err)
	require.Error(t, evidence.Write(filepath.Join(t.TempDir(), "run-3"), sampleMeta("run-3", "base"), []evidence.SourceFile{{Src: filepath.Join(t.TempDir(), "missing.jsonl"), Rel: "session.jsonl"}}))
}

func TestReadMetaErrors(t *testing.T) {
	_, err := evidence.ReadMeta(t.TempDir())
	require.Error(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{"), 0o600))
	_, err = evidence.ReadMeta(dir)
	require.Error(t, err)
}

func TestScanRunsAndMatchLabels(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"run-b", "run-a"} {
		require.NoError(t, evidence.Write(filepath.Join(root, id), sampleMeta(id, "base"), nil))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "junk"), 0o700))
	runs, err := evidence.ScanRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, "run-a", runs[0].Meta.RunID)
	require.True(t, evidence.MatchLabels(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}))
	require.False(t, evidence.MatchLabels(map[string]string{"a": "1"}, map[string]string{"a": "2"}))
	_, err = evidence.ScanRuns(filepath.Join(root, "absent"))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./evidence/ -v` — Expected: FAIL (package does not exist).
- [ ] **Step 3: Implement `evidence/evidence.go`:**

```go
package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/realkarych/catacomb/redact"
)

const metaFileName = "meta.json"

type Meta struct {
	RunID       string            `json:"run_id"`
	Task        string            `json:"task"`
	Variant     string            `json:"variant"`
	Rep         int               `json:"rep"`
	SessionID   string            `json:"session_id"`
	Labels      map[string]string `json:"labels,omitempty"`
	ExitCode    int               `json:"exit_code"`
	CostUSD     *float64          `json:"cost_usd,omitempty"`
	BasketHash  string            `json:"basket_hash"`
	MarkerName  string            `json:"marker_name"`
	MarkerStart time.Time         `json:"marker_start"`
	MarkerEnd   time.Time         `json:"marker_end"`
	FinishedAt  time.Time         `json:"finished_at"`
}

type SourceFile struct {
	Src string
	Rel string
}

type Run struct {
	Dir  string
	Meta Meta
}

func Write(dir string, m Meta, files []SourceFile) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("evidence.Write: %w", err)
	}
	for _, f := range files {
		if err := copyRedacted(f.Src, filepath.Join(dir, f.Rel)); err != nil {
			return fmt.Errorf("evidence.Write: %w", err)
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence.Write: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, metaFileName), data, 0o600); err != nil {
		return fmt.Errorf("evidence.Write: %w", err)
	}
	return nil
}

func copyRedacted(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	werr := redactLines(in, out)
	cerr := out.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func redactLines(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		trimmed := bytes.TrimSuffix(line, []byte{'\n'})
		if len(trimmed) > 0 {
			if _, werr := out.Write(append(redact.Redact(trimmed).Data, '\n')); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func ReadMeta(dir string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		return Meta{}, fmt.Errorf("evidence.ReadMeta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("evidence.ReadMeta: %w", err)
	}
	return m, nil
}

func ScanRuns(root string) ([]Run, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("evidence.ScanRuns: %w", err)
	}
	var out []Run
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		m, merr := ReadMeta(dir)
		if merr != nil {
			continue
		}
		out = append(out, Run{Dir: dir, Meta: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Meta.RunID < out[j].Meta.RunID })
	return out, nil
}

func MatchLabels(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
```

Note: if `redact.Redact`'s return type is not named `Result`, match the actual type from `redact/redact.go` — the call shape `redact.Redact(b).Data` is what `stepkey/stepkey.go:163` already uses.

- [ ] **Step 4: Run tests** — `go test ./evidence/ -v` — Expected: PASS. Then `go test ./evidence/ -coverprofile=/tmp/e.out && go tool cover -func=/tmp/e.out | tail -1` — Expected: 100.0%. Add missing error-branch tests if not (e.g. unwritable dst via a read-only parent dir with `t.Chmod`).
- [ ] **Step 5: Commit** — `git add evidence/ && git commit -m "feat(evidence): durable run-evidence dirs — redacted copies + meta.json"`

---

### Task 2: transcript resolver (`~/.claude/projects` → session files)

**Files:**

- Create: `cmd/catacomb/transcripts.go`
- Test: `cmd/catacomb/transcripts_test.go`

**Interfaces:**

- Produces:
  - `type transcriptSet struct { Main string; Subagents []string }`
  - `func resolveTranscripts(root, sessionID string) (transcriptSet, error)` — `Main` = unique glob match of `<root>/*/<sessionID>.jsonl` (error when 0 or >1); `Subagents` = sorted glob of `<root>/*/<sessionID>/subagents/agent-*.jsonl`.
  - `func resolveTranscriptsRetry(root, sessionID string, attempts int, delay time.Duration) (transcriptSet, error)`
  - `var sleepFn = time.Sleep`

- [ ] **Step 1: Write failing tests:**

```go
func TestResolveTranscripts(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-Users-x-proj")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "sess-1", "subagents"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "sess-1.jsonl"), []byte("{}\n"), 0o600))
	for _, a := range []string{"agent-b.jsonl", "agent-a.jsonl"} {
		require.NoError(t, os.WriteFile(filepath.Join(proj, "sess-1", "subagents", a), []byte("{}\n"), 0o600))
	}
	ts, err := resolveTranscripts(root, "sess-1")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(proj, "sess-1.jsonl"), ts.Main)
	require.Equal(t, []string{
		filepath.Join(proj, "sess-1", "subagents", "agent-a.jsonl"),
		filepath.Join(proj, "sess-1", "subagents", "agent-b.jsonl"),
	}, ts.Subagents)
}

func TestResolveTranscriptsNotFoundAndAmbiguous(t *testing.T) {
	root := t.TempDir()
	_, err := resolveTranscripts(root, "sess-x")
	require.ErrorContains(t, err, "no transcript")
	for _, p := range []string{"p1", "p2"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, p), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, p, "dup.jsonl"), []byte("{}\n"), 0o600))
	}
	_, err = resolveTranscripts(root, "dup")
	require.ErrorContains(t, err, "ambiguous")
}

func TestResolveTranscriptsRetry(t *testing.T) {
	root := t.TempDir()
	calls := 0
	old := sleepFn
	sleepFn = func(time.Duration) {
		calls++
		if calls == 2 {
			proj := filepath.Join(root, "p")
			require.NoError(t, os.MkdirAll(proj, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(proj, "late.jsonl"), []byte("{}\n"), 0o600))
		}
	}
	defer func() { sleepFn = old }()
	ts, err := resolveTranscriptsRetry(root, "late", 5, time.Millisecond)
	require.NoError(t, err)
	require.NotEmpty(t, ts.Main)
	_, err = resolveTranscriptsRetry(t.TempDir(), "never", 2, time.Millisecond)
	require.Error(t, err)
}
```

- [ ] **Step 2: Verify failure** — `go test ./cmd/catacomb/ -run TestResolveTranscripts -v` — Expected: FAIL (undefined symbols).
- [ ] **Step 3: Implement:**

```go
package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

var sleepFn = time.Sleep

type transcriptSet struct {
	Main      string
	Subagents []string
}

func resolveTranscripts(root, sessionID string) (transcriptSet, error) {
	mains, err := filepath.Glob(filepath.Join(root, "*", sessionID+".jsonl"))
	if err != nil {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: %w", err)
	}
	if len(mains) == 0 {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: no transcript for session %s under %s", sessionID, root)
	}
	if len(mains) > 1 {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: ambiguous session %s: %d matches", sessionID, len(mains))
	}
	subs, err := filepath.Glob(filepath.Join(root, "*", sessionID, "subagents", "agent-*.jsonl"))
	if err != nil {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: %w", err)
	}
	sort.Strings(subs)
	return transcriptSet{Main: mains[0], Subagents: subs}, nil
}

func resolveTranscriptsRetry(root, sessionID string, attempts int, delay time.Duration) (transcriptSet, error) {
	var last error
	for i := 0; i < attempts; i++ {
		ts, err := resolveTranscripts(root, sessionID)
		if err == nil {
			return ts, nil
		}
		last = err
		sleepFn(delay)
	}
	return transcriptSet{}, last
}
```

- [ ] **Step 4: Run tests** — `go test ./cmd/catacomb/ -run TestResolveTranscripts -v` — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(bench): transcript resolution from claude projects dir"`

---

### Task 3: offline loader + synthetic boundary markers

**Files:**

- Create: `cmd/catacomb/offline.go`
- Test: `cmd/catacomb/offline_test.go`

**Interfaces:**

- Consumes: `ijsonl.ParseReader(io.Reader, string) ([]model.Observation, error)`; `redact.DefaultPolicy()`; `reduce.NewGraphWithPricer(reduce.Pricer)`; the pricer instance the CLI already constructs for `regress` (reuse the same constructor the `regress` command uses — see `newRegressCmd` wiring in `cmd/catacomb/regress.go`).
- Produces:
  - `func parseTranscripts(main string, subs []string, executionID string) ([]model.Observation, error)` — concatenated observations, `Seq` renumbered 1..n.
  - `func boundaryObservations(sessionID, name string, start, end time.Time) []model.Observation` — two `Kind:"marker"` observations mirroring `daemon/mark.go:64-86` (`Attrs: {name, boundary}`, `Source: model.SourceHook`, `Correlation.SessionID`).
  - `func loadGraphOffline(main string, subs []string, executionID string, pricer reduce.Pricer, extra []model.Observation) (*reduce.Graph, error)`
  - `func graphMarkerNames(g *reduce.Graph) map[string]struct{}`

- [ ] **Step 1: Write failing tests** (fixture: existing `cmd/catacomb/testdata/session.jsonl`; a synthetic subagent file made by copying it):

```go
func TestParseTranscriptsRenumbersSeq(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	sub := filepath.Join(t.TempDir(), "agent-a.jsonl")
	data, err := os.ReadFile(main)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sub, data, 0o600))
	obs, err := parseTranscripts(main, []string{sub}, "exec-1")
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	for i, o := range obs {
		require.Equal(t, uint64(i+1), o.Seq)
		require.Equal(t, "exec-1", o.ExecutionID)
	}
	_, err = parseTranscripts(filepath.Join(t.TempDir(), "absent.jsonl"), nil, "exec-1")
	require.Error(t, err)
}

func TestBoundaryObservationsShape(t *testing.T) {
	start, end := time.Unix(10, 0), time.Unix(20, 0)
	obs := boundaryObservations("sess-9", "task:t1", start, end)
	require.Len(t, obs, 2)
	require.Equal(t, "marker", obs[0].Kind)
	require.Equal(t, "task:t1", obs[0].Attrs["name"])
	require.Equal(t, "start", obs[0].Attrs["boundary"])
	require.Equal(t, "end", obs[1].Attrs["boundary"])
	require.Equal(t, "sess-9", obs[0].Correlation.SessionID)
	require.True(t, obs[0].EventTime.Equal(start.UTC()))
}

func TestLoadGraphOfflineInjectsMarkers(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	boundary := boundaryObservations("s", "task:demo", time.Unix(1, 0), time.Unix(2, 0))
	g, err := loadGraphOffline(main, nil, "exec-2", nil, boundary)
	require.NoError(t, err)
	names := graphMarkerNames(g)
	_, ok := names["task:demo"]
	require.True(t, ok)
	g2, err := loadGraphOffline(main, nil, "exec-2", nil, boundaryObservations("s", "task:demo", time.Unix(1, 0), time.Unix(2, 0)))
	require.NoError(t, err)
	n1, e1 := g.Snapshot()
	n2, e2 := g2.Snapshot()
	require.Equal(t, len(n1), len(n2))
	require.Equal(t, len(e1), len(e2))
}
```

If `reduce.NewGraphWithPricer(nil)` panics on nil, pass `reduce.NewGraph()` when pricer is nil inside `loadGraphOffline` (branch + test both ways).

- [ ] **Step 2: Verify failure** — `go test ./cmd/catacomb/ -run 'TestParseTranscripts|TestBoundary|TestLoadGraphOffline' -v` — Expected: FAIL.
- [ ] **Step 3: Implement `cmd/catacomb/offline.go`:**

```go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/oklog/ulid/v2"

	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/reduce"
)

func parseTranscripts(main string, subs []string, executionID string) ([]model.Observation, error) {
	var all []model.Observation
	for _, p := range append([]string{main}, subs...) {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", p, err)
		}
		obs, perr := ijsonl.ParseReader(f, executionID)
		cerr := f.Close()
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", p, perr)
		}
		if cerr != nil {
			return nil, fmt.Errorf("close %s: %w", p, cerr)
		}
		all = append(all, obs...)
	}
	for i := range all {
		all[i].Seq = uint64(i + 1)
	}
	return all, nil
}

func boundaryObservations(sessionID, name string, start, end time.Time) []model.Observation {
	return []model.Observation{
		markerObservation(sessionID, name, "start", start),
		markerObservation(sessionID, name, "end", end),
	}
}

func markerObservation(sessionID, name, boundary string, at time.Time) model.Observation {
	return model.Observation{
		ObsID:       ulid.Make().String(),
		RunID:       sessionID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: sessionID},
		Attrs:       map[string]any{"name": name, "boundary": boundary},
		EventTime:   at.UTC(),
		ObservedAt:  at.UTC(),
	}
}

func loadGraphOffline(main string, subs []string, executionID string, pricer reduce.Pricer, extra []model.Observation) (*reduce.Graph, error) {
	obs, err := parseTranscripts(main, subs, executionID)
	if err != nil {
		return nil, err
	}
	for i := range extra {
		extra[i].ExecutionID = executionID
		extra[i].Seq = uint64(len(obs) + i + 1)
		obs = append(obs, extra[i])
	}
	policy := redact.DefaultPolicy()
	for i := range obs {
		obs[i] = policy.Observation(obs[i])
	}
	g := reduce.NewGraph()
	if pricer != nil {
		g = reduce.NewGraphWithPricer(pricer)
	}
	g.ApplyAll(obs)
	return g, nil
}

func graphMarkerNames(g *reduce.Graph) map[string]struct{} {
	nodes, _ := g.Snapshot()
	out := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		if n.Type == model.NodeMarker {
			out[n.Name] = struct{}{}
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests** — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(bench): offline transcript loader + synthetic boundary markers"`

---

### Task 4: local child runner + stream peek

**Files:**

- Create: `cmd/catacomb/childlocal.go`
- Test: `cmd/catacomb/childlocal_test.go`

**Interfaces:**

- Consumes: `lineObserver` and `execCommand` (both in `cmd/catacomb/streamjson.go`).
- Produces:
  - `type streamPeek struct { sessionID string; costUSD *float64 }` with `func (p *streamPeek) onLine(line []byte)` — captures first `session_id`, and `total_cost_usd` from the `"type":"result"` event.
  - `func runChildLocal(stdout, stderr io.Writer, args []string, dir string, extraEnv []string, observe func(line []byte)) error` — like `runChildObserved` minus daemon forwarding.

- [ ] **Step 1: Write failing tests** using the `TestHelperProcess` pattern (cross-platform, no shell):

```go
func TestStreamPeek(t *testing.T) {
	p := &streamPeek{}
	p.onLine([]byte("not json"))
	p.onLine([]byte(`{"type":"system","session_id":"s-1"}`))
	p.onLine([]byte(`{"type":"system","session_id":"s-2"}`))
	p.onLine([]byte(`{"type":"result","total_cost_usd":0.5}`))
	require.Equal(t, "s-1", p.sessionID)
	require.NotNil(t, p.costUSD)
	require.InDelta(t, 0.5, *p.costUSD, 1e-9)
}

func TestRunChildLocal(t *testing.T) {
	old := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperOfflineChild")
		cmd.Env = append(os.Environ(), "GO_HELPER_OFFLINE=1")
		return cmd
	}
	defer func() { execCommand = old }()
	var out bytes.Buffer
	peek := &streamPeek{}
	err := runChildLocal(&out, io.Discard, []string{"claude"}, "", []string{"X=1"}, peek.onLine)
	require.NoError(t, err)
	require.Equal(t, "sess-h", peek.sessionID)
	require.Contains(t, out.String(), "sess-h")
}

func TestHelperOfflineChild(t *testing.T) {
	if os.Getenv("GO_HELPER_OFFLINE") != "1" {
		return
	}
	fmt.Println(`{"type":"system","session_id":"sess-h"}`)
	fmt.Println(`{"type":"result","total_cost_usd":0.25}`)
	os.Exit(0)
}
```

- [ ] **Step 2: Verify failure** — `go test ./cmd/catacomb/ -run 'TestStreamPeek|TestRunChildLocal' -v` — Expected: FAIL.
- [ ] **Step 3: Implement:**

```go
package main

import (
	"encoding/json"
	"io"
	"os"
)

type streamPeek struct {
	sessionID string
	costUSD   *float64
}

func (p *streamPeek) onLine(line []byte) {
	var e struct {
		Type         string   `json:"type"`
		SessionID    string   `json:"session_id"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
	}
	if json.Unmarshal(line, &e) != nil {
		return
	}
	if p.sessionID == "" && e.SessionID != "" {
		p.sessionID = e.SessionID
	}
	if e.Type == "result" && e.TotalCostUSD != nil {
		p.costUSD = e.TotalCostUSD
	}
}

func runChildLocal(stdout, stderr io.Writer, args []string, dir string, extraEnv []string, observe func(line []byte)) error {
	child := execCommand(args[0], args[1:]...)
	child.Stdin = os.Stdin
	child.Dir = dir
	child.Env = append(os.Environ(), extraEnv...)
	obs := &lineObserver{observe: observe}
	child.Stdout = io.MultiWriter(stdout, obs)
	child.Stderr = stderr
	err := child.Run()
	obs.flush()
	return err
}
```

- [ ] **Step 4: Run tests** — Expected: PASS (exit-code path: add a helper variant exiting 3 and assert `exitInfo` sees it).
- [ ] **Step 5: Commit** — `git commit -m "feat(bench): local child runner with stream peek (session id, result cost)"`

---

### Task 5: `bench --offline` wiring + manifest fields

**Files:**

- Modify: `cmd/catacomb/bench.go` (flags, offline cell path), `bench/manifest.go` (two new fields)
- Test: `cmd/catacomb/bench_offline_test.go`, extend `bench/manifest_test.go`

**Interfaces:**

- Consumes: Tasks 1–4 symbols; existing `runSetup`, `cellEnv`, `cloneLabels`, `exitInfo`, `spawnFailure`, `appendNote`, `nowFn`, `newExecutionID`, `bench.Manifest`.
- Produces:
  - `bench.ManifestEntry` gains `CostUSD *float64 \`json:"cost_usd,omitempty"\`` and `EvidenceDir string \`json:"evidence_dir,omitempty"\``.
  - `benchFlags` gains `offline bool`, `projectsDir string`, `runsDir string`; flags `--offline`, `--projects-dir` (default `~/.claude/projects`), `--runs-dir` (default `~/.catacomb/runs`); defaults via `os.UserHomeDir()`.
  - `func runBenchCellOffline(stdout, stderr io.Writer, cell bench.Cell, hash string, ambient map[string]string, o offlineOpts) (bench.ManifestEntry, bool, bool)` with `type offlineOpts struct { projectsDir, runsDir string; pricer reduce.Pricer }`.
  - Offline mode skips `benchPreflight`; `--offline` cell flow: setup → `runChildLocal` (with `CATACOMB_LABELS` env) around `nowFn()` boundary timestamps → `resolveTranscriptsRetry(projectsDir, sid, 6, 500*time.Millisecond)` → `boundaryObservations` → `loadGraphOffline` → `graphMarkerNames`: `entry.Marked` = boundary name present; `entry.MissingCheckpoints` from declared checkpoints; `evidence.Write` into `<runsDir>/<RunID>` with `session.jsonl` + `subagents/agent-*.jsonl` rel names → `entry.EvidenceDir`, `entry.CostUSD`.
  - Epilogue (offline): print `catacomb regress --runs-dir <runsDir> --baseline label:basket=<b>,variant=<v1> --candidate label:basket=<b>,variant=<v2>`.

- [ ] **Step 1: Write failing end-to-end test** — helper child copies a fixture transcript into a fake projects root (env-passed) and emits NDJSON; assert manifest entries, evidence dirs, missing-checkpoint reporting:

```go
func TestBenchOfflineEndToEnd(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("testdata", "session_marked.jsonl"))
	require.NoError(t, err)
	old := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperBenchChild")
		cmd.Env = append(os.Environ(),
			"GO_HELPER_BENCH=1",
			"HELPER_PROJECTS="+projects,
			"HELPER_FIXTURE="+fixture,
			"HELPER_SESSION="+name,
		)
		return cmd
	}
	defer func() { execCommand = old }()
	basket := filepath.Join(t.TempDir(), "b.yaml")
	require.NoError(t, os.WriteFile(basket, []byte(
		"basket: bx\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"sess-a\"]\n    checkpoints:\n      - missing.cp\nvariants:\n  - id: base\n"), 0o600))
	manifestPath := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errb bytes.Buffer
	err = runBench(context.Background(), &out, &errb, "", basket, benchFlags{
		offline: true, projectsDir: projects, runsDir: runs, manifest: manifestPath,
	})
	require.NoError(t, err)
	entries, err := bench.Manifest{Path: manifestPath}.Completed()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	entry := entries["bench-bx-t1-base-r1"]
	require.Equal(t, []string{"missing.cp"}, entry.MissingCheckpoints)
	require.True(t, entry.Marked)
	require.NotNil(t, entry.CostUSD)
	require.Contains(t, errb.String(), "missing checkpoints: missing.cp")
	got, err := evidence.ScanRuns(runs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "bench-bx-t1-base-r1", got[0].Meta.RunID)
	_, statErr := os.Stat(filepath.Join(got[0].Dir, "session.jsonl"))
	require.NoError(t, statErr)
}

func TestHelperBenchChild(t *testing.T) {
	if os.Getenv("GO_HELPER_BENCH") != "1" {
		return
	}
	sid := os.Getenv("HELPER_SESSION")
	proj := filepath.Join(os.Getenv("HELPER_PROJECTS"), "-tmp-proj")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		os.Exit(1)
	}
	data, err := os.ReadFile(os.Getenv("HELPER_FIXTURE"))
	if err != nil {
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(proj, sid+".jsonl"), data, 0o600); err != nil {
		os.Exit(1)
	}
	fmt.Printf("{\"type\":\"system\",\"session_id\":%q}\n", sid)
	fmt.Println(`{"type":"result","total_cost_usd":0.01}`)
	os.Exit(0)
}
```

The basket `cmd` abuses `args[0]` as the session id handed to the helper — this keeps the fake child deterministic per cell.

- [ ] **Step 2: Verify failure** — `go test ./cmd/catacomb/ -run TestBenchOffline -v` — Expected: FAIL (unknown fields/flags).
- [ ] **Step 3: Implement** — add flags and `offlineOpts`; branch in `runBench` (`if f.offline` → skip `benchPreflight`, per cell call `runBenchCellOffline`); implement the cell flow per the Interfaces block, mirroring `runBenchCell`'s structure and reusing `checkpointStats` for the summary; add the two `ManifestEntry` fields.
- [ ] **Step 4: Cover error branches** — table tests: no session id observed; transcript resolution timeout (empty projects root, `sleepFn` stubbed); setup failure; `evidence.Write` failure (unwritable runs dir). Each asserts the manifest `Note`.
- [ ] **Step 5: Run** — `go test ./cmd/catacomb/ -run 'TestBench' -v` and `go test ./bench/ -v` — Expected: PASS.
- [ ] **Step 6: Commit** — `git commit -m "feat(bench): --offline mode — daemonless cells, evidence dirs, in-process checkpoint verify"`

---

### Task 6: `regress --runs-dir` — offline `label:` resolution

**Files:**

- Modify: `cmd/catacomb/regress.go` (flag + resolver branch)
- Create: `cmd/catacomb/runsdir.go`
- Test: `cmd/catacomb/runsdir_test.go`

**Interfaces:**

- Consumes: `evidence.ScanRuns`, `evidence.MatchLabels`, `loadGraphOffline`, `boundaryObservations`, `parseSelector`, `model.ParseLabels`, `validateLabelTerms`, `aggregate.RunGraph`.
- Produces:
  - `regressFlags` gains `runsDir string` (flag `--runs-dir`, default "").
  - `func resolveSelectorRunsDir(runsDir string, pricer reduce.Pricer, sel string) ([]aggregate.RunGraph, model.Baseline, error)` — `label:` only; `name:` returns an error mentioning PV-2; empty match set returns an error naming the selector.
  - `func evidenceRunGraph(dir string, m evidence.Meta, pricer reduce.Pricer) (aggregate.RunGraph, error)` — main = `<dir>/session.jsonl`, subs = sorted `<dir>/subagents/agent-*.jsonl`, boundary re-synthesized from `m.MarkerName/MarkerStart/MarkerEnd`, `Run: model.Run{ID: m.RunID, SessionIDs: []string{m.SessionID}}` (extend with `Labels` if `model.Run` has that field — check `model/model.go:135-150`).
  - In `runRegress`: when `f.runsDir != ""`, resolve both selectors via `resolveSelectorRunsDir` and skip opening the store; `--record` combined with `--runs-dir` errors ("regress --record requires the store; offline baselines land in PV-2").

- [ ] **Step 1: Write failing tests** — build two evidence groups from fixtures (`session_marked.jsonl` ×2 as `variant=base`, `session.jsonl` ×2 as `variant=cand`) via `evidence.Write`, then assert group sizes, label filtering, `name:` error, empty-match error, `--record` conflict error.
- [ ] **Step 2: Verify failure** — Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces. Keep `resolveSelector` (store path) untouched.
- [ ] **Step 4: Run** — `go test ./cmd/catacomb/ -run 'TestRunsDir|TestRegress' -v` — Expected: PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(regress): --runs-dir — resolve label selectors from evidence dirs"`

---

### Task 7: parity gate test (PV-1 acceptance)

**Files:**

- Create: `cmd/catacomb/offline_parity_test.go`

**Interfaces:**

- Consumes: everything above; `runRegress` (or the cobra command via `newRegressCmd().Execute()` with args — follow the invocation style `cmd/catacomb/regress_test.go` already uses).

- [ ] **Step 1: Assert the fixture carries markers** — a test loading `testdata/session_marked.jsonl` via `loadGraphOffline` and requiring `len(graphMarkerNames(g)) > 0` (the parity gate itself only needs the overall verdict, not the marker's name); if the fixture turns out unmarked, extend it by appending a `mcp__catacomb__mark` tool-call pair copied from the shape in `reduce/marker_test.go` fixtures.
- [ ] **Step 2: Write the parity test:**

```go
func TestOfflineParityGate(t *testing.T) {
	runs := t.TempDir()
	marked := filepath.Join("testdata", "session_marked.jsonl")
	plain := filepath.Join("testdata", "session.jsonl")
	for i := 0; i < 3; i++ {
		writeEvidenceFixture(t, runs, fmt.Sprintf("base-%d", i), "base", marked)
		writeEvidenceFixture(t, runs, fmt.Sprintf("cand-%d", i), "cand", plain)
		writeEvidenceFixture(t, runs, fmt.Sprintf("ctrl-%d", i), "ctrl", marked)
	}
	report := runRegressJSON(t, runs, "label:variant=base", "label:variant=cand")
	require.Equal(t, "regression", report.Overall)
	control := runRegressJSON(t, runs, "label:variant=base", "label:variant=ctrl")
	require.Equal(t, "ok", control.Overall)
	require.Empty(t, regressionsIn(control))
}
```

`writeEvidenceFixture` builds `evidence.Meta` (labels `variant=<v>`, distinct `MarkerStart/End` covering the fixture's timestamps) and calls `evidence.Write`. `runRegressJSON` invokes the regress command with `--runs-dir`, `--json`, captures stdout, unmarshals into the report type `regress.Report` (see `regress/render.go` / `regress/testdata/golden_report.json` for the exact field names — `Overall` may be named differently; match the golden file). `regressionsIn` counts findings with `Verdict == regress.VerdictRegression`.

- [ ] **Step 3: Verify the gate exit code** — invoke the command path that maps `regression` → exit 1 (`overallVerdict` handling in `cmd/catacomb/regress.go`) and assert the returned error is the gate error, mirroring how `regress_test.go` asserts it today.
- [ ] **Step 4: Run** — `go test ./cmd/catacomb/ -run TestOfflineParity -v` — Expected: PASS: baseline-vs-degraded gates `regression`; A-vs-A yields zero regression findings.
- [ ] **Step 5: Commit** — `git commit -m "test(regress): offline parity gate — degraded variant gates, A-vs-A clean"`

---

### Task 8: docs, lint, coverage gate

**Files:**

- Modify: `docs/guide/workflows.md` (offline bench + regress section), `docs/guide/cli.md` (`--offline`, `--projects-dir`, `--runs-dir`)
- Test: full repo gates

- [ ] **Step 1: Document the offline flow** — a "Daemonless benching (ADR-0026)" subsection in `workflows.md`: the two commands (`catacomb bench --offline b.yaml`, `catacomb regress --runs-dir ~/.catacomb/runs --baseline label:… --candidate label:…`), evidence-dir layout (`session.jsonl`, `subagents/`, `meta.json`), and the note that in-task checkpoints still ride the `mark` MCP tool.
- [ ] **Step 2: `markdownlint '**/*.md' --ignore node_modules`** — Expected: clean.
- [ ] **Step 3: Full gates** — `go build ./... && go test ./... && make cover && golangci-lint run --timeout=5m` — Expected: all green, coverage 100%.
- [ ] **Step 4: Commit** — `git commit -m "docs(guide): daemonless bench/regress workflow (PV-1)"`

---

## Manual validation (not CI; record in the PR description)

1. `catacomb bench --offline` over a live 1-task × 2-variant × 3-rep haiku basket (the V-1 calibration prompt), Phoenix plugin optional.
2. `catacomb regress --runs-dir …` — confirm the degraded variant gates and A-vs-A stays clean on live data.
3. Attach the manifest + regress output to the PR.

## Self-review checklist (run after writing code, before PR)

- Spec coverage: ADR-0026 §4 (bench mechanics), §5 (selector compat) — Tasks 5–6; parity gate — Task 7.
- No placeholder steps; every symbol referenced is defined in a task or listed in Global Constraints.
- Type consistency: `transcriptSet`, `streamPeek`, `offlineOpts`, `evidence.Meta` field names match across Tasks 2–7.
