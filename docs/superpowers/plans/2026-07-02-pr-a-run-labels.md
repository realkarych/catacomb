# PR-A: Run Labels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every run can carry open `k=v` labels — set via `CATACOMB_LABELS` env or `catacomb run --label` — persisted deterministically through the observation log and selectable in `catacomb runs` (ADR-0022 §1).

**Architecture:** labels enter as an attribute on ingested observations (`attrs["catacomb.labels"]`, canonical string), carried from client env by the hook/stream forwarders as an `X-Catacomb-Labels` header; the reducer unions them into `Run.Labels` in `ensureRun`, so rebuild-from-log reproduces them and determinism holds. Store round-trip is free (`runs.body` serializes the whole `model.Run`).

**Tech Stack:** Go 1.26, stdlib only (no new deps), testify require/assert, table-driven tests.

## Global Constraints

- No comments in Go code (only `//go:build`/`//go:embed`/`//go:generate`); enforced by `internal/codepolicy`.
- 100% test coverage (`make cover`); TDD — failing test first.
- `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`); `make lint` clean.
- Errors: sentinels + wrapping `fmt.Errorf("pkg.Op: %w", err)`; no error-string parsing.
- No `time.Sleep` in tests; table-driven by default; mock through the caller's interface.
- Labels are metadata only — never identity (ADR-0011 untouched); caps: ≤32 pairs, key `[a-z0-9_.-]{1,64}`, value ≤256 chars.
- Follow the style of neighboring tests in each package (read them first).

---

### Task 1: `model` label primitives + `Run.Labels`

**Files:**

- Create: `model/labels.go`
- Create: `model/labels_test.go`
- Modify: `model/model.go` (Run struct, after `Meta`)

**Interfaces:**

- Produces: `model.ParseLabels(s string) map[string]string`, `model.FormatLabels(l map[string]string) string`, `model.MergeLabels(dst, src map[string]string) map[string]string`, `model.MatchLabels(labels, want map[string]string) bool`, field `Run.Labels map[string]string`.

- [ ] **Step 1: Write failing tests** in `model/labels_test.go`:

```go
package model_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"single", "basket=checkout", map[string]string{"basket": "checkout"}},
		{"multi", "a=1,b=2", map[string]string{"a": "1", "b": "2"}},
		{"last write wins", "a=1,a=2", map[string]string{"a": "2"}},
		{"spaces trimmed", " a = 1 , b = 2 ", map[string]string{"a": "1", "b": "2"}},
		{"invalid key dropped", "A!=x,ok=1", map[string]string{"ok": "1"}},
		{"missing value dropped", "a,b=2", map[string]string{"b": "2"}},
		{"empty value kept", "a=,b=2", map[string]string{"a": "", "b": "2"}},
		{"value cap dropped", "a=" + strings.Repeat("x", 257) + ",b=2", map[string]string{"b": "2"}},
		{"key cap dropped", strings.Repeat("k", 65) + "=1,b=2", map[string]string{"b": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, model.ParseLabels(tt.in))
		})
	}
}

func TestParseLabelsPairCap(t *testing.T) {
	var b strings.Builder
	for i := range 40 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(string(rune('a'+i%26)) + string(rune('0'+i/26)) + "=v")
	}
	assert.Len(t, model.ParseLabels(b.String()), 32)
}

func TestFormatLabelsCanonical(t *testing.T) {
	assert.Equal(t, "a=1,b=2", model.FormatLabels(map[string]string{"b": "2", "a": "1"}))
	assert.Equal(t, "", model.FormatLabels(nil))
}

func TestMergeLabels(t *testing.T) {
	dst := map[string]string{"a": "1"}
	got := model.MergeLabels(dst, map[string]string{"a": "2", "b": "3"})
	assert.Equal(t, map[string]string{"a": "2", "b": "3"}, got)
	assert.Equal(t, map[string]string{"a": "2", "b": "3"}, dst)
	assert.Equal(t, map[string]string{"x": "1"}, model.MergeLabels(nil, map[string]string{"x": "1"}))
}

func TestMatchLabels(t *testing.T) {
	labels := map[string]string{"basket": "checkout", "variant": "v2"}
	assert.True(t, model.MatchLabels(labels, map[string]string{"basket": "checkout"}))
	assert.True(t, model.MatchLabels(labels, nil))
	assert.False(t, model.MatchLabels(labels, map[string]string{"basket": "other"}))
	assert.False(t, model.MatchLabels(labels, map[string]string{"missing": "x"}))
	assert.False(t, model.MatchLabels(nil, map[string]string{"a": "1"}))
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./model/ -run 'TestParseLabels|TestFormatLabels|TestMergeLabels|TestMatchLabels' -v`
Expected: FAIL — undefined: `model.ParseLabels` etc.

- [ ] **Step 3: Implement** `model/labels.go`:

```go
package model

import (
	"regexp"
	"sort"
	"strings"
)

const (
	maxLabelPairs    = 32
	maxLabelValueLen = 256
)

var labelKeyRe = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)

func ParseLabels(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !labelKeyRe.MatchString(k) || len(v) > maxLabelValueLen {
			continue
		}
		if _, seen := out[k]; !seen && len(out) >= maxLabelPairs {
			continue
		}
		out[k] = v
	}
	return out
}

func FormatLabels(l map[string]string) string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+l[k])
	}
	return strings.Join(parts, ",")
}

func MergeLabels(dst, src map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func MatchLabels(labels, want map[string]string) bool {
	for k, v := range want {
		if labels[k] != v {
			return false
		}
	}
	return true
}
```

Add to `Run` in `model/model.go` after the `Meta` field:

```go
	Labels     map[string]string `json:"labels,omitempty"`
```

- [ ] **Step 4: Run tests** — `go test ./model/ -v` → PASS. Note: the pair-cap test may need adjusting key generation to satisfy the key regexp; keep keys like `k0`..`k39`.
- [ ] **Step 5: Commit** — `git add model/ && git commit -m "feat(model): run label primitives (parse/format/merge/match) + Run.Labels"`

### Task 2: reducer merges label attrs into `Run.Labels`; deep copy

**Files:**

- Modify: `reduce/reduce.go` (`ensureRun`, ~line 564)
- Modify: `daemon/copy.go` (`copyRun`, ~line 51)
- Test: `reduce/reduce_labels_test.go` (new), extend `daemon/copy_test.go`

**Interfaces:**

- Consumes: `model.ParseLabels`, `model.MergeLabels` (Task 1).
- Produces: any observation with `Attrs["catacomb.labels"]` (canonical string) unions those labels into its run's `Labels`.

- [ ] **Step 1: Write failing test** `reduce/reduce_labels_test.go`. Read `reduce/reduce_test.go` first and reuse its observation-builder helpers/fixture style. The test must assert:
  - an observation carrying `Attrs: map[string]any{"catacomb.labels": "basket=checkout,rep=1"}` produces `g.Runs[runID].Labels == map[string]string{"basket":"checkout","rep":"1"}`;
  - a later observation with `"catacomb.labels": "rep=2"` overrides to `rep=2` keeping `basket`;
  - an observation without the attr leaves labels untouched;
  - a non-string attr value is ignored.
- [ ] **Step 2: Run** `go test ./reduce/ -run Labels -v` → FAIL.
- [ ] **Step 3: Implement** — append to `ensureRun` (after the `cwd` repro block):

```go
	if raw, ok := o.Attrs["catacomb.labels"].(string); ok && raw != "" {
		r.Labels = model.MergeLabels(r.Labels, model.ParseLabels(raw))
	}
```

And in `daemon/copy.go` `copyRun`, after the `Meta` copy block:

```go
	if r.Labels != nil {
		rc.Labels = make(map[string]string, len(r.Labels))
		for k, v := range r.Labels {
			rc.Labels[k] = v
		}
	}
```

- [ ] **Step 4: Run** `go test ./reduce/ ./daemon/ -v` → PASS (extend `daemon/copy_test.go` to assert the Labels map is deep-copied — mutate original, copy unchanged).
- [ ] **Step 5: Store round-trip test** — in the store test file covering `UpsertRun`/run loading (find via `grep -rn "UpsertRun" store/*_test.go`), add a case asserting a `Run{Labels: ...}` survives upsert → reopen → load with labels intact.
- [ ] **Step 6: Commit** — `git commit -m "feat(reduce): merge catacomb.labels observation attr into Run.Labels"`

### Task 3: daemon ingress accepts `X-Catacomb-Labels`

**Files:**

- Modify: `daemon/daemon.go` (`Ingest` ~181, `ingestLocked` ~196, `IngestStreamJSON`)
- Modify: `daemon/server.go` (`handleHook` ~155, `handleStreamJSON` ~95)
- Test: extend `daemon/server_test.go` / `daemon/daemon_test.go` (follow existing handler-test style)

**Interfaces:**

- Produces: `(d *Daemon) IngestWithLabels(hookType string, payload []byte, labels string) error` and `(d *Daemon) IngestStreamJSONWithLabels(line []byte, sessionID, labels string) error`; existing `Ingest`/`IngestStreamJSON` delegate with `labels = ""`. The `labels` argument is the RAW header value; it is canonicalized via `model.FormatLabels(model.ParseLabels(raw))` before attaching, and attached as `Attrs["catacomb.labels"]` on every observation parsed from that request iff non-empty.

- [ ] **Step 1: Write failing tests**: POST `/hook/PostToolUse` with header `X-Catacomb-Labels: basket=b1,rep=1` (reuse an existing hook-payload fixture from server tests) → after ingest, the daemon's run for that session has `Labels == {"basket":"b1","rep":"1"}`; malformed header (`X-Catacomb-Labels: !!!`) → empty labels, ingest still 204; stream-json POST with the header labels the run likewise.
- [ ] **Step 2: Run** → FAIL (methods undefined).
- [ ] **Step 3: Implement**: rename the internals so labels thread through: `ingestLocked(hookType, payload, labels string)`; after `hook.Parse` returns observations, if canonical labels string non-empty, for each obs do `if obs[i].Attrs == nil { obs[i].Attrs = map[string]any{} }` then `obs[i].Attrs["catacomb.labels"] = canonical` (adapt to the actual parse-return shape — read the code; if `hook.Parse` returns a single obs, adjust). Wire `handleHook`: `_ = d.IngestWithLabels(r.PathValue("type"), payload, r.Header.Get("X-Catacomb-Labels"))`; `handleStreamJSON` reads the header once before the scan loop and passes it per line.
- [ ] **Step 4: Run** `go test ./daemon/ -v` → PASS; `make lint` clean.
- [ ] **Step 5: Commit** — `git commit -m "feat(daemon): X-Catacomb-Labels ingress header labels the run"`

### Task 4: forwarders carry `CATACOMB_LABELS` env as the header

**Files:**

- Modify: `cmd/catacomb/hook.go` (`forward`, ~line 28)
- Modify: `cmd/catacomb/streamjson.go` (`streamForward` ~46, `newIngestCmd` ~70, `runChild` ~101)
- Test: extend `cmd/catacomb/hook_test.go`, `cmd/catacomb/streamjson_test.go`

**Interfaces:**

- Consumes: daemon header contract from Task 3.
- Produces: `forward(warn, discoveryPath, hookType string, stdin io.Reader, labels string)` and `streamForward(warn, discoveryPath string, body io.Reader, labels string)` — both set `X-Catacomb-Labels: <labels>` iff labels non-empty. Call sites read `os.Getenv("CATACOMB_LABELS")` in the cobra `RunE` and pass it down (keeps the funcs testable without env).

- [ ] **Step 1: Write failing tests**: in the existing httptest-based forwarder tests, assert the received request carries the header when labels are passed, and no header when empty.
- [ ] **Step 2: Run** → FAIL (signature mismatch).
- [ ] **Step 3: Implement** the two-line change per func:

```go
	if labels != "" {
		req.Header.Set("X-Catacomb-Labels", labels)
	}
```

Update all call sites (`newHookCmd`, `newIngestCmd`, `newRunCmd`→`runChild`'s forwarding goroutine) to pass `os.Getenv("CATACOMB_LABELS")`.

- [ ] **Step 4: Run** `go test ./cmd/... -v` → PASS.
- [ ] **Step 5: Commit** — `git commit -m "feat(cmd): forwarders carry CATACOMB_LABELS as X-Catacomb-Labels"`

### Task 5: `catacomb run --label`

**Files:**

- Modify: `cmd/catacomb/streamjson.go` (`newRunCmd` ~87, `runChild` ~101)
- Test: extend `cmd/catacomb/streamjson_test.go` (uses the `execCommand` seam, var at line 21)

**Interfaces:**

- Consumes: `model.ParseLabels`/`FormatLabels`/`MergeLabels`.
- Produces: `runChild(stdout, stderr io.Writer, discoveryPath, runID string, labels []string, args []string) error` — merges env `CATACOMB_LABELS` (base) with `--label k=v` flags (flags win, in flag order) and sets the canonical merged value as `CATACOMB_LABELS` in the child env; also passes the merged canonical string to its own `streamForward` (so the teed stream is labeled even if hooks are not installed).

- [ ] **Step 1: Write failing test**: with `execCommand` stubbed (existing pattern), run `runChild` with `labels = []string{"variant=v2", "rep=3"}` and env `CATACOMB_LABELS=basket=b1,variant=v1` (use `t.Setenv`) → child env contains exactly one `CATACOMB_LABELS=basket=b1,rep=3,variant=v2` entry (canonical, flag wins for `variant`).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**: add `cmd.Flags().StringArrayVar(&labels, "label", nil, "k=v label recorded on the run (repeatable; adds to CATACOMB_LABELS)")`; in `runChild` compute:

```go
	merged := model.MergeLabels(model.ParseLabels(os.Getenv("CATACOMB_LABELS")), model.ParseLabels(strings.Join(labels, ",")))
	canonical := model.FormatLabels(merged)
	if canonical != "" {
		child.Env = append(child.Env, "CATACOMB_LABELS="+canonical)
	}
```

(Env append after `os.Environ()` — last entry wins for duplicate names in `exec`; assert that in the test rather than filtering.)

- [ ] **Step 4: Run** `go test ./cmd/... -v` → PASS; `make lint`.
- [ ] **Step 5: Commit** — `git commit -m "feat(cmd): catacomb run --label sets CATACOMB_LABELS for the child"`

### Task 6: `catacomb runs --label` filter + labels in output; docs

**Files:**

- Modify: `cmd/catacomb/runs.go` (flag + filter, ~line 16)
- Modify: `daemon/sessions.go` (`SessionSummary` + `summarizeGraphs` populate `Labels`, ~line 19/250)
- Modify: `docs/guide/cli.md`, `docs/guide/ingestion.md` (a "Run labels" subsection: env var, `--label`, header, selector)
- Test: extend `cmd/catacomb/runs_test.go`, `daemon/sessions_test.go`

**Interfaces:**

- Consumes: `model.MatchLabels`, `Run.Labels`.
- Produces: `SessionSummary.Labels map[string]string \`json:"labels,omitempty"\`` populated from the run; `runs --label k=v` (repeatable, AND) filters the listing.

- [ ] **Step 1: Write failing tests**: `daemon/sessions_test.go` — a graph whose run has labels yields a summary carrying them; `cmd/catacomb/runs_test.go` — with two stored runs (labels `basket=b1` vs `basket=b2`), `runs --label basket=b1 --json` lists exactly the matching run and its `labels` object.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**: add the field; populate where `summarizeGraphs` reads the matched `model.Run` (deep-copy not needed — summaries are serialized immediately; follow how `ModelID`/`Status` are read there). In `runs.go`: parse repeated `--label` terms via `model.ParseLabels(strings.Join(terms, ","))` into the selector and skip runs failing `model.MatchLabels(r.Labels, selector)` before summarizing.
- [ ] **Step 4: Run** `go test ./...` → PASS; `make cover` → 100%; `make lint` + `npx -y markdownlint-cli@0.49.0 docs/guide/cli.md docs/guide/ingestion.md`.
- [ ] **Step 5: Commit** — `git commit -m "feat(cmd,daemon): runs --label selector + labels in summaries; docs"`

### Task 7: live-verify + PR

- [ ] **Step 1:** `make build`; start `bin/catacomb daemon` with a temp `--db`/`--discovery`; POST a demo hook payload with `X-Catacomb-Labels` (or run `bin/catacomb demo` then a labeled `bin/catacomb run --label smoke=1 -- /bin/echo '{}'`); assert `bin/catacomb runs --db <tmp> --json` shows the labels and `--label smoke=1` filters. Record the exact commands + output in the PR body.
- [ ] **Step 2:** `make cover && make lint && go test ./internal/codepolicy/`.
- [ ] **Step 3:** Push `feat/run-labels`, open PR titled `feat: run labels (ADR-0022 §1) — CATACOMB_LABELS, run --label, runs --label selector`, body links ADR-0022 + roadmap PR-A and includes the live-verify transcript.
