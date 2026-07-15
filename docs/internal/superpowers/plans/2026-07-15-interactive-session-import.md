# `catacomb import` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `catacomb import` subcommand that turns an already-finished session transcript (including a hand-run interactive TUI session) into a bench-cell-shaped evidence directory the gate compares, and correct the docs where `claude -p` reads as the only agent path.

**Architecture:** `import` is a thin front-end over the offline-evidence path `bench` already runs after a child exits. It loads the basket for `task`/`variant`/`verify`/`checkpoints`/labels (basket = source of truth, task `cmd` ignored), resolves the transcript by `--session-id` (under `--projects-dir`) or `--transcript` (direct path), parses it once, synthesizes the `task:<id>` marker window from the transcript's first/last record timestamps, builds the graph (pricing tokens, honoring any `mark` markers), and writes a redacted evidence dir via `evidence.Write`. The only shared-code change is splitting `loadGraphOffline` so the marker window can be computed from parsed timestamps without re-parsing (which would double-emit drift warnings). Verification stays a separate `catacomb verify` step.

**Tech Stack:** Go, cobra CLI, existing `bench`/`evidence`/`reduce`/`model` packages.

## Global Constraints

- **No comments in Go code** — none, not even doc comments. Only `//go:build`, `//go:embed`, `//go:generate` directives are allowed. Enforced by `internal/codepolicy`. Every Go snippet below is comment-free; keep it that way.
- **100% test coverage, TDD-first.** Write the failing test before the implementation. The threshold never drops.
- **Always work in the worktree** `.claude/worktrees/import-command` (branch `worktree-import-command`). Never edit the shared checkout.
- **Exit codes are uniform:** `0` success, `1` regression/verify failure, `2` operational error. Operational errors wrap through `operational(err)`.
- **Run-id scheme:** `import-<basket>-<task>-<variant>-r<rep>` (distinct from bench's `bench-…`), overridable with `--run-id`.
- **Labels are identical to a bench cell:** `basket`, `task`, `variant`, `rep` (rep via `strconv.Itoa`), so `regress`/`verify` selectors match unchanged.
- Full local check before any commit: `go build ./... && go test ./...`. The `internal/codepolicy` test fails the build on any stray comment.

---

### Task 1: Split `loadGraphOffline`; add `graphFromObservations` and `transcriptTimeBounds`

Foundation for computing the marker window from parsed timestamps without a second parse. Bench behavior must not change.

**Files:**

- Modify: `cmd/catacomb/offline.go` (refactor `loadGraphOffline` at lines 117-141; add two functions)
- Test: `cmd/catacomb/offline_test.go` (add tests)

**Interfaces:**

- Produces: `graphFromObservations(obs []model.Observation, executionID string, pricer reduce.Pricer, extra []model.Observation) *reduce.Graph`
- Produces: `transcriptTimeBounds(obs []model.Observation) (start, end time.Time, ok bool)` — min/max of non-zero `EventTime`; `ok=false` when no observation carries a non-zero `EventTime`.
- Produces (unchanged public behavior): `loadGraphOffline(main string, subs []string, executionID string, pricer reduce.Pricer, extra []model.Observation) (*reduce.Graph, error)` now delegates to `parseTranscripts` + `graphFromObservations`.
- Consumes: `parseTranscripts` (existing, `offline.go:27`), `reduce.NewGraphWithPricer`/`reduce.NewGraph`, `redact.DefaultPolicy`, `model.Observation.EventTime`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/catacomb/offline_test.go`:

```go
func TestGraphFromObservationsAppliesExtraAndPricer(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	extra := boundaryObservations("s1", "task:t1", time.Unix(0, 0).UTC(), time.Unix(10, 0).UTC())
	g := graphFromObservations(obs, "exec-1", newPricer(), extra)
	require.NotNil(t, g)
	marks := graphMarkerNames(g)
	_, ok := marks["task:t1"]
	assert.True(t, ok)
}

func TestTranscriptTimeBounds(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	start, end, ok := transcriptTimeBounds(obs)
	require.True(t, ok)
	assert.False(t, start.After(end))
}

func TestTranscriptTimeBoundsEmpty(t *testing.T) {
	_, _, ok := transcriptTimeBounds(nil)
	assert.False(t, ok)
}

func TestLoadGraphOfflineStillWorks(t *testing.T) {
	g, err := loadGraphOffline("testdata/session.jsonl", nil, "exec-1", newPricer(), nil)
	require.NoError(t, err)
	require.NotNil(t, g)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'GraphFromObservations|TranscriptTimeBounds|LoadGraphOfflineStillWorks' -v`
Expected: FAIL — `graphFromObservations`/`transcriptTimeBounds` undefined.

- [ ] **Step 3: Refactor and implement**

In `cmd/catacomb/offline.go`, replace the body of `loadGraphOffline` (lines 117-141) and add the two helpers:

```go
func loadGraphOffline(main string, subs []string, executionID string, pricer reduce.Pricer, extra []model.Observation) (*reduce.Graph, error) {
	obs, err := parseTranscripts(main, subs, executionID)
	if err != nil {
		return nil, err
	}
	return graphFromObservations(obs, executionID, pricer, extra), nil
}

func graphFromObservations(obs []model.Observation, executionID string, pricer reduce.Pricer, extra []model.Observation) *reduce.Graph {
	base := len(obs)
	for i := range extra {
		e := extra[i]
		e.ExecutionID = executionID
		e.Seq = uint64(base + i + 1)
		obs = append(obs, e)
	}
	policy := redact.DefaultPolicy()
	for i := range obs {
		obs[i] = policy.Observation(obs[i])
	}
	var g *reduce.Graph
	if pricer != nil {
		g = reduce.NewGraphWithPricer(pricer)
	} else {
		g = reduce.NewGraph()
	}
	g.ApplyAll(obs)
	return g
}

func transcriptTimeBounds(obs []model.Observation) (time.Time, time.Time, bool) {
	var start, end time.Time
	found := false
	for _, o := range obs {
		if o.EventTime.IsZero() {
			continue
		}
		if !found {
			start, end, found = o.EventTime, o.EventTime, true
			continue
		}
		if o.EventTime.Before(start) {
			start = o.EventTime
		}
		if o.EventTime.After(end) {
			end = o.EventTime
		}
	}
	return start, end, found
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'GraphFromObservations|TranscriptTimeBounds|LoadGraphOfflineStillWorks' -v`
Expected: PASS. Then `go test ./cmd/catacomb/` to confirm existing bench offline tests still pass (behavior unchanged).

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/offline.go cmd/catacomb/offline_test.go
git commit -m "refactor: split loadGraphOffline; add graphFromObservations and transcriptTimeBounds

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `import` command scaffold — flags, basket load, task/variant selection, input validation

**Files:**

- Create: `cmd/catacomb/import.go`
- Modify: `cmd/catacomb/root.go:36` (register `newImportCmd()` beside `newPackCmd()`)
- Test: `cmd/catacomb/import_test.go`

**Interfaces:**

- Consumes: `bench.LoadOffline` (`bench/basket.go:141`), `indexTasks`/`indexVariants` (`verify.go:144,152`), `operational` (existing), `benchDefaultDir` (`bench.go:64`).
- Produces: `newImportCmd() *cobra.Command`; `type importFlags struct{ task, variant, sessionID, transcript, rep, runID, projectsDir, runsDir, labels string }` (rep is `int`); `runImport(ctx context.Context, stdout, stderr io.Writer, basketPath string, f importFlags) error`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/catacomb/import_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeBasket(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`basket: checkout
reps: 1
tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item", "--output-format", "stream-json"]
    checkpoints: ["phase:cart"]
variants:
  - id: trunk
  - id: patched
`), 0o600))
	return p
}

func TestImportRequiresSessionXorTranscript(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session-id")
}

func TestImportRejectsBothInputs(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "s1", transcript: "x.jsonl", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
}

func TestImportUnknownTask(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "nope", variant: "trunk", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task")
}

func TestImportUnknownVariant(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "nope", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "variant")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run TestImport -v`
Expected: FAIL — `runImport`/`importFlags` undefined.

- [ ] **Step 3: Implement the scaffold**

Create `cmd/catacomb/import.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/bench"
)

var errImportInput = errors.New("import: exactly one of --session-id or --transcript is required")

type importFlags struct {
	task        string
	variant     string
	sessionID   string
	transcript  string
	rep         int
	runID       string
	projectsDir string
	runsDir     string
	labels      string
}

func newImportCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   "import <basket.yaml>",
		Short: "Ingest an already-finished session transcript as a bench-cell-shaped evidence dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.task, "task", "", "task id in the basket (selects verify/checkpoints/labels)")
	cmd.Flags().StringVar(&f.variant, "variant", "", "variant id in the basket")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "session UUID resolved under --projects-dir")
	cmd.Flags().StringVar(&f.transcript, "transcript", "", "direct path to a main session .jsonl")
	cmd.Flags().IntVar(&f.rep, "rep", 1, "repetition index")
	cmd.Flags().StringVar(&f.runID, "run-id", "", "evidence dir name (default: import-<basket>-<task>-<variant>-r<rep>)")
	cmd.Flags().StringVar(&f.projectsDir, "projects-dir", benchDefaultDir(home, ".claude", "projects"), "Claude projects dir holding session transcripts")
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence output dir")
	cmd.Flags().StringVar(&f.labels, "label", "", "extra ambient labels merged under cell labels (k=v, comma-separated)")
	return cmd
}

func runImport(ctx context.Context, stdout, stderr io.Writer, basketPath string, f importFlags) error {
	if (f.sessionID == "") == (f.transcript == "") {
		return operational(errImportInput)
	}
	basket, hash, err := bench.LoadOffline(basketPath)
	if err != nil {
		return operational(err)
	}
	task, ok := indexTasks(basket.Tasks)[f.task]
	if !ok {
		return operational(fmt.Errorf("import: task %q not in basket", f.task))
	}
	if _, ok := indexVariants(basket.Variants)[f.variant]; !ok {
		return operational(fmt.Errorf("import: variant %q not in basket", f.variant))
	}
	return importEvidence(ctx, stdout, stderr, basket, hash, task, f)
}
```

Note: `importEvidence` is implemented in Task 4. For this task, add a temporary stub at the bottom of `import.go` so the package compiles and the scaffold tests pass:

```go
func importEvidence(_ context.Context, _, _ io.Writer, _ bench.Basket, _ string, _ bench.Task, _ importFlags) error {
	return nil
}
```

Register in `cmd/catacomb/root.go` after `root.AddCommand(newPackCmd())`:

```go
	root.AddCommand(newImportCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run TestImport -v`
Expected: PASS (all four validation tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/import.go cmd/catacomb/import_test.go cmd/catacomb/root.go
git commit -m "feat: import command scaffold — flags, basket load, task/variant validation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Transcript resolution for `--session-id` and `--transcript`

**Files:**

- Modify: `cmd/catacomb/import.go` (add `importTranscripts`)
- Test: `cmd/catacomb/import_test.go`

**Interfaces:**

- Consumes: `resolveTranscripts` (`transcripts.go:23`), `transcriptSet` (`transcripts.go:18`), `filepath.Glob`, `sort.Strings`.
- Produces: `importTranscripts(f importFlags) (transcriptSet, string, error)` — returns the resolved transcript set and the effective session id. For `--session-id`, uses `resolveTranscripts`. For `--transcript`, main = path (must exist), session id = base name minus `.jsonl`, subagents globbed from `<dir>/<sid>/subagents/agent-*.jsonl`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/catacomb/import_test.go`:

```go
func stageTranscript(t *testing.T, projects, sid string) {
	t.Helper()
	dst := filepath.Join(projects, "proj", sid+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o600))
}

func TestImportTranscriptsBySessionID(t *testing.T) {
	projects := t.TempDir()
	stageTranscript(t, projects, "sess-123")
	ts, sid, err := importTranscripts(importFlags{sessionID: "sess-123", projectsDir: projects})
	require.NoError(t, err)
	assert.Equal(t, "sess-123", sid)
	assert.Contains(t, ts.Main, "sess-123.jsonl")
}

func TestImportTranscriptsBySessionIDNotFound(t *testing.T) {
	projects := t.TempDir()
	_, _, err := importTranscripts(importFlags{sessionID: "missing", projectsDir: projects})
	require.Error(t, err)
}

func TestImportTranscriptsByPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sess-abc.jsonl")
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(main, data, 0o600))
	ts, sid, err := importTranscripts(importFlags{transcript: main})
	require.NoError(t, err)
	assert.Equal(t, "sess-abc", sid)
	assert.Equal(t, main, ts.Main)
}

func TestImportTranscriptsByPathMissing(t *testing.T) {
	_, _, err := importTranscripts(importFlags{transcript: filepath.Join(t.TempDir(), "nope.jsonl")})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run TestImportTranscripts -v`
Expected: FAIL — `importTranscripts` undefined.

- [ ] **Step 3: Implement**

Add to `cmd/catacomb/import.go` (extend imports with `path/filepath` and `sort`):

```go
func importTranscripts(f importFlags) (transcriptSet, string, error) {
	if f.sessionID != "" {
		ts, err := resolveTranscripts(f.projectsDir, f.sessionID)
		if err != nil {
			return transcriptSet{}, "", err
		}
		return ts, f.sessionID, nil
	}
	if _, err := os.Stat(f.transcript); err != nil {
		return transcriptSet{}, "", fmt.Errorf("import: transcript: %w", err)
	}
	sid := strings.TrimSuffix(filepath.Base(f.transcript), ".jsonl")
	subs, err := filepath.Glob(filepath.Join(filepath.Dir(f.transcript), sid, "subagents", "agent-*.jsonl"))
	if err != nil {
		return transcriptSet{}, "", fmt.Errorf("import: subagents: %w", err)
	}
	sort.Strings(subs)
	return transcriptSet{Main: f.transcript, Subagents: subs}, sid, nil
}
```

Add `"path/filepath"`, `"sort"`, and `"strings"` to the import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run TestImportTranscripts -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/import.go cmd/catacomb/import_test.go
git commit -m "feat: import transcript resolution by session-id and direct path

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Evidence production — parse, window, graph, marks, meta, write

Replaces the Task 2 stub with the real `importEvidence`.

**Files:**

- Modify: `cmd/catacomb/import.go` (replace stub `importEvidence`; add `importMeta`, `importLabels`, `importRunID`)
- Test: `cmd/catacomb/import_test.go`

**Interfaces:**

- Consumes: `parseTranscripts`, `transcriptTimeBounds` (Task 1), `boundaryObservations`, `graphFromObservations` (Task 1), `graphMarkerNames`, `benchEnvStamps` (`bench.go:386`), `offlineFiles` (`bench.go:406`), `newPricer` (`storeread.go:34`), `newExecutionID` (`replay.go:43`), `evidence.Write`/`evidence.Meta`, `model.ParseLabels`/`model.MergeLabels`, `nowFn` (`baseline.go:21`).
- Produces: `importEvidence(...)` writes `<runs-dir>/<run-id>/{session.jsonl, subagents/…, meta.json}` and returns nil on success; `importRunID(f, basketName) string`; `importLabels(f, basketName) map[string]string`; `importMeta(runID, task, variant string, rep int, sessionID, hash string, labels map[string]string, start, end time.Time, env *evidence.EnvStamps) evidence.Meta`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/catacomb/import_test.go`:

```go
func TestImportWritesEvidence(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	})
	require.NoError(t, err)
	metaPath := filepath.Join(runs, "import-checkout-add-item-trunk-r1", "meta.json")
	require.FileExists(t, metaPath)
	require.FileExists(t, filepath.Join(runs, "import-checkout-add-item-trunk-r1", "session.jsonl"))
}

func TestImportMetaShape(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "patched", rep: 2, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	}))
	m, err := evidence.ReadMeta(filepath.Join(runs, "import-checkout-add-item-patched-r2"))
	require.NoError(t, err)
	assert.Equal(t, "task:add-item", m.MarkerName)
	assert.Equal(t, "patched", m.Labels["variant"])
	assert.Equal(t, "checkout", m.Labels["basket"])
	assert.Equal(t, "2", m.Labels["rep"])
	assert.Nil(t, m.CostUSD)
	assert.False(t, m.MarkerStart.After(m.MarkerEnd))
}

func TestImportRunIDOverride(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz", runID: "manual-1",
		projectsDir: projects, runsDir: runs,
	}))
	require.FileExists(t, filepath.Join(runs, "manual-1", "meta.json"))
}

func TestImportWarnsMissingCheckpoint(t *testing.T) {
	dir := t.TempDir()
	basket := writeBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	}))
	assert.Contains(t, errb.String(), "missing checkpoints")
	assert.Contains(t, errb.String(), "phase:cart")
}

func TestImportLabelsMergeAmbient(t *testing.T) {
	got := importLabels(importFlags{task: "t", variant: "v", rep: 3, labels: "env=ci,variant=SHOULD_LOSE"}, "b")
	assert.Equal(t, "v", got["variant"])
	assert.Equal(t, "ci", got["env"])
	assert.Equal(t, "3", got["rep"])
	assert.Equal(t, "b", got["basket"])
}
```

Add `"github.com/realkarych/catacomb/evidence"` to the test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestImportWrites|TestImportMeta|TestImportRunID|TestImportWarns|TestImportLabels' -v`
Expected: FAIL — stub returns nil, writes nothing; `importLabels` undefined.

- [ ] **Step 3: Implement**

Replace the stub `importEvidence` in `cmd/catacomb/import.go` and add helpers:

```go
func importEvidence(_ context.Context, stdout, stderr io.Writer, basket bench.Basket, hash string, task bench.Task, f importFlags) error {
	ts, sessionID, err := importTranscripts(f)
	if err != nil {
		return operational(fmt.Errorf("import: %w", err))
	}
	execID := newExecutionID()
	obs, err := parseTranscripts(ts.Main, ts.Subagents, execID)
	if err != nil {
		return operational(fmt.Errorf("import: %w", err))
	}
	start, end, ok := transcriptTimeBounds(obs)
	if !ok {
		return operational(fmt.Errorf("import: transcript %s has no timestamped records", ts.Main))
	}
	boundary := boundaryObservations(sessionID, "task:"+task.ID, start, end)
	g := graphFromObservations(obs, execID, newPricer(), boundary)
	marks := graphMarkerNames(g)
	warnMissingCheckpoints(stderr, task, marks, importRunID(f, basket.Name))
	env := benchEnvStamps(g.RunsSnapshot(), sessionID, nil)
	runID := importRunID(f, basket.Name)
	meta := importMeta(runID, task.ID, f.variant, f.rep, sessionID, hash, importLabels(f, basket.Name), start, end, env)
	dir := filepath.Join(f.runsDir, runID)
	if err := evidence.Write(dir, meta, offlineFiles(ts)); err != nil {
		return operational(fmt.Errorf("import: evidence write: %w", err))
	}
	fmt.Fprintf(stdout, "import %s: %s\n", runID, dir)
	return nil
}

func warnMissingCheckpoints(stderr io.Writer, task bench.Task, marks map[string]struct{}, runID string) {
	var missing []string
	for _, name := range task.Checkpoints {
		if _, ok := marks[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "import %s: missing checkpoints: %s\n", runID, strings.Join(missing, ", "))
	}
}

func importRunID(f importFlags, basketName string) string {
	if f.runID != "" {
		return f.runID
	}
	return fmt.Sprintf("import-%s-%s-%s-r%d", basketName, f.task, f.variant, f.rep)
}

func importLabels(f importFlags, basketName string) map[string]string {
	cell := map[string]string{
		"basket":  basketName,
		"task":    f.task,
		"variant": f.variant,
		"rep":     strconv.Itoa(f.rep),
	}
	return model.MergeLabels(model.ParseLabels(f.labels), cell)
}

func importMeta(runID, task, variant string, rep int, sessionID, hash string, labels map[string]string, start, end time.Time, env *evidence.EnvStamps) evidence.Meta {
	return evidence.Meta{
		RunID:       runID,
		Task:        task,
		Variant:     variant,
		Rep:         rep,
		SessionID:   sessionID,
		Labels:      labels,
		ExitCode:    0,
		CostUSD:     nil,
		BasketHash:  hash,
		MarkerName:  "task:" + task,
		MarkerStart: start.UTC(),
		MarkerEnd:   end.UTC(),
		FinishedAt:  nowFn().UTC(),
		Env:         env,
	}
}
```

Extend the import block with `"strconv"`, `"time"`, `"github.com/realkarych/catacomb/evidence"`, and `"github.com/realkarych/catacomb/model"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run TestImport -v`
Expected: PASS (all import tests). Then `go test ./cmd/catacomb/` for the whole package.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/import.go cmd/catacomb/import_test.go
git commit -m "feat: import evidence production — window synthesis, marks, meta, write

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Integration test — `import → verify → regress` with no `claude -p`

Proves the imported evidence dir is indistinguishable from a bench cell across the downstream pipeline.

**Files:**

- Test: `cmd/catacomb/import_integration_test.go` (create)

**Interfaces:**

- Consumes: `runImport`, `runVerify` (`verify.go:44`), `runRegress` (the regress entry — confirm exact signature by reading `regress.go`; call it the same way `regress_test.go` does), the `session_marked.jsonl` fixture (carries a `mark` marker).

- [ ] **Step 1: Write the failing test**

Read `cmd/catacomb/regress_test.go` first to copy the exact `runRegress`/selector invocation pattern and its flags struct. Then create `cmd/catacomb/import_integration_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImportThenVerifyThenRegress(t *testing.T) {
	dir := t.TempDir()
	basket := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(basket, []byte(`basket: checkout
reps: 1
tasks:
  - id: add-item
    cmd: ["claude", "-p", "x", "--output-format", "stream-json"]
variants:
  - id: trunk
  - id: patched
`), 0o600))
	projects := filepath.Join(dir, "projects")
	runs := filepath.Join(dir, "runs")
	for _, v := range []string{"trunk", "patched"} {
		sid := "sess-" + v
		dst := filepath.Join(projects, "p", sid+".jsonl")
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		data, err := os.ReadFile("testdata/session_marked.jsonl")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(dst, data, 0o600))
		var out, errb bytes.Buffer
		require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
			task: "add-item", variant: v, rep: 1, sessionID: sid, projectsDir: projects, runsDir: runs,
		}))
	}
	var vout, verrb bytes.Buffer
	require.NoError(t, runVerify(context.Background(), &vout, &verrb, basket, verifyFlags{runsDir: runs}))
}
```

Extend this test with a `runRegress` call over `--runs-dir runs`, `--baseline label:variant=trunk`, `--candidate label:variant=patched`, matching the signature you read from `regress_test.go`, asserting a verdict is produced (no error, or the expected gate exit). Keep the basket verifier-free so `verify` matches zero cells cleanly, or add a trivial `verify: { cmd: ["true"] }` and assert `verify` passes — pick whichever `regress_test.go`'s patterns make simplest.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/catacomb/ -run TestImportThenVerifyThenRegress -v`
Expected: FAIL initially if selectors/flags mismatch — adjust to the real `runRegress` signature until it drives a verdict.

- [ ] **Step 3: Make it pass**

Adjust selectors/flags to the real `runRegress` API. No production code changes should be needed; if a gap surfaces, stop and report it rather than widening scope here.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/catacomb/ -run TestImportThenVerifyThenRegress -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/import_integration_test.go
git commit -m "test: import to verify to regress end-to-end with no claude -p

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Documentation corrections (parallelizable — disjoint from code and from Task 7)

Correct every place `claude -p` reads as the only way to feed the gate, and document `import`.

**Files:**

- Modify: `docs/guide/cli.md`, `docs/guide/basket.md`, `docs/guide/troubleshooting.md`, `docs/guide/workflows.md`, `AGENTS.md`, `README.md`

- [ ] **Step 1: `docs/guide/cli.md`**
  - Add a row to the command table: `| [`import`](#import) | Ingest an already-finished session transcript as an evidence dir |`.
  - Add `import` to the "commands that parse transcripts" advisory line alongside `bench, regress, diff, subgraph, export, replay`.
  - Add a `## import` section after `## verify`: the flag table from the spec, the two input modes (`--session-id` vs `--transcript`), the run-id scheme, the note that the task `cmd` is ignored, that `CostUSD` is null while the token-derived `cost_usd` metric still works, and the recommended `--session-id $(uuidgen)` workflow. Cross-link `verify` and `regress`.
  - In the `## bench` section, change the parenthetical implying stream-json is universal to scope it to bench-driven cells, and add: "For a session run by hand (interactive TUI), record it with `import` instead."

- [ ] **Step 2: `docs/guide/basket.md`**
  - By the `cmd` field description (line ~40) and the `claude -p` examples (lines ~177, ~184), add a note: "`cmd` drives `bench` only; `catacomb import` ingests a session you ran yourself and ignores `cmd`."

- [ ] **Step 3: `docs/guide/troubleshooting.md`**
  - On the `no session id observed` and `transcripts not found` rows, add: "For a hand-run interactive session, use [`catacomb import`](cli.md#import) — it does not need stream-json on stdout."

- [ ] **Step 4: `docs/guide/workflows.md`**
  - Add an "Importing a hand-run interactive session" subsection: pin the id (`SID=$(uuidgen); claude --session-id "$SID" --mcp-config catacomb-mcp.json`), do the task and mark phases, then `catacomb import … --session-id "$SID"`, `catacomb verify`, `catacomb regress`. Note the `--transcript` fallback (newest file under `~/.claude/projects/<encoded-cwd>/`).

- [ ] **Step 5: `AGENTS.md`**
  - In the one-line pipeline description, note `import` as a second entry point beside `bench` (a session run by hand can enter the same `transcript JSONL → reduce → … → regress` pipeline).

- [ ] **Step 6: `README.md`**
  - Where the tutorial presents `claude -p`, add one line that an interactive session can be ingested with `catacomb import`, linking `docs/guide/cli.md#import`.

- [ ] **Step 7: Verify links and commit**

Run the repo's docs-link check if one exists (grep `Makefile` / `.github/workflows` for a link checker); otherwise eyeball the new anchors resolve.

```bash
git add docs/guide/cli.md docs/guide/basket.md docs/guide/troubleshooting.md docs/guide/workflows.md AGENTS.md README.md
git commit -m "docs: document import; stop presenting claude -p as the only agent path

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: ADR-0030 + index (parallelizable — disjoint from Task 6)

**Files:**

- Create: `docs/adr/0030-interactive-session-import.md`
- Modify: `docs/adr/README.md` (index the new ADR)

- [ ] **Step 1: Write the ADR**

Match the house format of `docs/adr/0029-basket-relative-path-resolution.md` (read it first). Record: context (single `bench` entry point; interactive sessions locked out; stream-json is headless-only), decision (`import` subcommand, basket-anchored, evidence-only, reuse of the offline path, synth+honor markers, run-id prefix), the cost semantics (`meta.CostUSD` null, token-derived `cost_usd` metric works), consequences (second entry point into the pipeline; JSONL format remains internal/undocumented risk shared with all transcript commands), and alternatives rejected (bench attach-mode; freeform flags).

- [ ] **Step 2: Index it**

Add ADR-0030 to `docs/adr/README.md` following the existing row style.

- [ ] **Step 3: Commit**

```bash
git add docs/adr/0030-interactive-session-import.md docs/adr/README.md
git commit -m "docs: ADR-0030 interactive-session import

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: Hermetic E2E leg — import path (after Task 5; parallelizable with 6/7)

Add an import leg to the hermetic E2E so CI exercises the full cycle with zero agent spawn.

**Files:**

- Read first: `e2e/hermetic/run.sh`, `e2e/run.sh`, `e2e/basket-sql.yaml` (understand how the hermetic run builds the binary, stages a fake projects-dir, and asserts).
- Modify: `e2e/hermetic/run.sh` (add an import-path assertion block) and any basket/fixture it needs.

- [ ] **Step 1: Add the import leg**

After the existing bench/verify/regress assertions, add a block that: generates a session id, copies a recorded transcript fixture into a staged `--projects-dir` as `<sid>.jsonl`, runs `catacomb import <basket> --task … --variant … --session-id <sid> --projects-dir … --runs-dir …`, then runs `catacomb verify` and `catacomb regress` over that runs-dir, asserting the evidence dir and a verdict — all without invoking `claude`.

- [ ] **Step 2: Run the hermetic E2E locally**

Run: `bash e2e/hermetic/run.sh` (or the Make target it is wired to; check `Makefile`).
Expected: PASS including the new import leg.

- [ ] **Step 3: Commit**

```bash
git add e2e/hermetic/run.sh
git commit -m "test(e2e): hermetic import leg — full cycle with no claude spawn

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**

- Command surface + flags → Task 2. ✓
- Basket-anchored, `cmd` ignored → Task 2 (`LoadOffline`, selection). ✓
- `--session-id` + `--transcript` → Task 3. ✓
- Marker window synth from timestamps + honor `mark` → Task 1 (`transcriptTimeBounds`) + Task 4 (`boundaryObservations`, `graphMarkerNames`). ✓
- Run-id `import-…` + override → Task 4 (`importRunID`). ✓
- Labels identical to bench cell + ambient merge → Task 4 (`importLabels`). ✓
- Cost: `meta.CostUSD` null, token-derived metric via pricer → Task 4 (`CostUSD: nil`, `newPricer()`). ✓
- Env stamps (model/version from transcript, workspace nil) → Task 4 (`benchEnvStamps(…, nil)`). ✓
- Evidence-only (verify separate) → no verify in `importEvidence`; Task 5 runs `verify` separately. ✓
- Checkpoint warnings, never gate → Task 4 (`warnMissingCheckpoints`). ✓
- Redaction free → `evidence.Write` (Task 4). ✓
- `import → verify → regress` integration + hermetic E2E → Tasks 5, 8. ✓
- Docs corrections everywhere `claude -p` is sole path → Task 6. ✓
- ADR-0030 → Task 7. ✓

**Placeholder scan:** Task 5 leaves the exact `runRegress` call to be matched against `regress_test.go` (signature not hand-guessed on purpose) and Task 8 reads `run.sh` before editing — both are "read the real API/harness first" instructions, not code placeholders. All production code steps carry complete, comment-free code.

**Type consistency:** `importFlags` fields, `importTranscripts`/`importEvidence`/`importMeta`/`importLabels`/`importRunID` signatures, and the `graphFromObservations`/`transcriptTimeBounds` signatures are consistent across Tasks 1-5. `MergeLabels(dst, src)` with `src` (cell) winning matches bench usage. Run-id string matches the asserted `import-checkout-add-item-trunk-r1`.

## Parallelization

- **Serial chain (shared `cmd/catacomb` files):** Task 1 → 2 → 3 → 4 → 5.
- **Parallel after Task 5 lands:** Task 6 (docs), Task 7 (ADR), Task 8 (e2e) touch disjoint files — dispatch each in its own worktree.
