# SP-W Per-Cell Workspace Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `bench` materializes a fresh working directory per cell from a user-supplied command (optional patch handover, optional teardown, deterministic cleanup, descriptive stamps), per [ADR-0028](../../../adr/0028-per-cell-workspace-isolation.md) and the [SP-W design spec](../../specs/2026-07-13-spw-workspace-isolation-design.md).

**Architecture:** A `workspace` block on tasks (wholesale per-variant override) is validated and patch-hashed at basket load (`bench/`); a new lifecycle pair in `cmd/catacomb/workspace.go` (`setupWorkspace`/`cleanupWorkspace`) brackets the existing cell body, which is re-rooted on a `workdir` variable; env stamps gain a descriptive `workspace` block (`evidence/`). Cells without a workspace are byte-identical to today.

**Tech Stack:** Go stdlib only (no new deps), testify, existing seams (`execCommandContext`, `nowFn`); bash for E2E.

## Global Constraints

- **No comments in Go code** — none; enforced by `internal/codepolicy`. Every Go snippet below is comment-free by construction; keep it that way.
- **TDD**: failing test first, minimal implementation, refactor under green.
- **Coverage is 100%** (`make cover`); every new branch needs a test. The threshold never goes down.
- `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`); `make lint` clean.
- Errors: sentinel `errors.New` checked with `errors.Is`; wrap as `fmt.Errorf("bench.Load: …: %w", err)`.
- No `time.Sleep` in tests; table-driven tests, `require` for fatal assertions.
- **Unit tests must be cross-platform** (CI includes `windows-latest`): never exec `sh`, `true`, or other unix binaries from Go tests. Use the repo's helper-process pattern (`bench_offline_test.go`'s `stubBenchChild`: override `execCommandContext` to return `exec.CommandContext(ctx, os.Args[0], "-test.run=<Helper>")`). Real-shell behavior is proven by the hermetic E2E (linux-only workflow).
- Work on a fresh branch `feat/spw-workspace-isolation` from current `origin/master`, in an isolated worktree. One squash PR.
- Execution: fresh subagent per task, review after each task. **Parallelism (per CLAUDE.md):** wave A = Tasks 1 ∥ 2 (disjoint packages); wave B = Task 3; wave C = Task 4; wave D = Tasks 5 ∥ 6 ∥ 7 (disjoint files).

---

### Task 1: Basket schema — `workspace` block, validation, patch hashing at load

**Files:**

- Modify: `bench/basket.go`
- Test: `bench/basket_test.go`

**Interfaces:**

- Consumes: nothing new.
- Produces (Tasks 3, 4, 6 rely on these exact names):
  - `type Workspace struct { Cmd []string; Patch, Rev string; Teardown []string; PatchAbs, PatchSHA256 string }` (last two populated by `Load`, excluded from YAML/JSON).
  - `Task.Workspace *Workspace`, `Variant.Workspace *Workspace`.
  - `func (c Cell) EffectiveWorkspace() *Workspace` — variant wins, else task, else nil.
  - Sentinels: `ErrWorkspaceCmd`, `ErrWorkspaceDir`, `ErrWorkspacePatch`.

- [ ] **Step 1: Write the failing tests** (append to `bench/basket_test.go`; the file already uses `writeBasket`-style temp-YAML helpers — reuse the local pattern of writing a YAML string to `t.TempDir()` and calling `bench.Load`):

```go
func TestLoadWorkspaceValidation(t *testing.T) {
	patch := filepath.Join(t.TempDir(), "fix.patch")
	require.NoError(t, os.WriteFile(patch, []byte("delta\n"), 0o600))
	cases := []struct {
		name string
		yaml string
		want error
	}{
		{"empty workspace cmd on task", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: []}
variants:
  - id: v1
`, bench.ErrWorkspaceCmd},
		{"empty workspace cmd on variant", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
variants:
  - id: v1
    workspace: {cmd: []}
`, bench.ErrWorkspaceCmd},
		{"task dir with task workspace", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    dir: /tmp
    workspace: {cmd: ["true"]}
variants:
  - id: v1
`, bench.ErrWorkspaceDir},
		{"task dir with variant workspace", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    dir: /tmp
variants:
  - id: v1
    workspace: {cmd: ["true"]}
`, bench.ErrWorkspaceDir},
		{"missing patch", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: nope.patch}
variants:
  - id: v1
`, bench.ErrWorkspacePatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "basket.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))
			_, _, err := bench.Load(path)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestLoadWorkspacePatchHashedAndResolved(t *testing.T) {
	dir := t.TempDir()
	content := []byte("patch-bytes\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.patch"), content, 0o600))
	yaml := `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: fix.patch, rev: r42}
variants:
  - id: v1
  - id: v2
    workspace: {cmd: ["sh", "x.sh"], patch: fix.patch}
`
	path := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	b, _, err := bench.Load(path)
	require.NoError(t, err)
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	tw := b.Tasks[0].Workspace
	require.Equal(t, filepath.Join(dir, "fix.patch"), tw.PatchAbs)
	require.Equal(t, want, tw.PatchSHA256)
	require.Equal(t, "r42", tw.Rev)
	vw := b.Variants[1].Workspace
	require.Equal(t, want, vw.PatchSHA256)
}

func TestEffectiveWorkspace(t *testing.T) {
	taskWS := &bench.Workspace{Cmd: []string{"t"}}
	varWS := &bench.Workspace{Cmd: []string{"v"}}
	cases := []struct {
		name string
		cell bench.Cell
		want *bench.Workspace
	}{
		{"variant wins", bench.Cell{Task: bench.Task{Workspace: taskWS}, Variant: bench.Variant{Workspace: varWS}}, varWS},
		{"task fallback", bench.Cell{Task: bench.Task{Workspace: taskWS}}, taskWS},
		{"none", bench.Cell{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Same(t, tc.want, tc.cell.EffectiveWorkspace())
		})
	}
}
```

Note: `require.Same` on nil wants `require.Nil` — for the `"none"` case assert `require.Nil(t, tc.cell.EffectiveWorkspace())` instead (split it out of the table or branch on `tc.want == nil`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./bench/ -run 'TestLoadWorkspace|TestEffectiveWorkspace' -race`
Expected: compile FAIL (`Workspace` undefined).

- [ ] **Step 3: Implement** in `bench/basket.go`:

Add the type and sentinels:

```go
type Workspace struct {
	Cmd         []string `yaml:"cmd" json:"cmd"`
	Patch       string   `yaml:"patch,omitempty" json:"patch,omitempty"`
	Rev         string   `yaml:"rev,omitempty" json:"rev,omitempty"`
	Teardown    []string `yaml:"teardown,omitempty" json:"teardown,omitempty"`
	PatchAbs    string   `yaml:"-" json:"-"`
	PatchSHA256 string   `yaml:"-" json:"-"`
}
```

```go
	ErrWorkspaceCmd   = errors.New("bench: workspace cmd is empty")
	ErrWorkspaceDir   = errors.New("bench: dir and workspace are mutually exclusive")
	ErrWorkspacePatch = errors.New("bench: workspace patch unreadable")
```

Add `Workspace *Workspace` with tag `yaml:"workspace,omitempty" json:"workspace,omitempty"` to both `Task` and `Variant`. Add `EffectiveWorkspace`:

```go
func (c Cell) EffectiveWorkspace() *Workspace {
	if c.Variant.Workspace != nil {
		return c.Variant.Workspace
	}
	return c.Task.Workspace
}
```

In `validate`: per-task, reject `t.Workspace != nil && len(t.Workspace.Cmd) == 0` (`ErrWorkspaceCmd`) and `t.Workspace != nil && t.Dir != ""` (`ErrWorkspaceDir`); per-variant, reject empty `Cmd` the same way; then cross-check — if any variant has a workspace, every task must have `Dir == ""` (`ErrWorkspaceDir`, naming the task and variant ids in the wrapped message).

In `Load`, after `validate(b)` succeeds, resolve patches against `filepath.Dir(path)`:

```go
func resolvePatches(b *Basket, baseDir string) error {
	for i := range b.Tasks {
		if err := resolvePatch(b.Tasks[i].Workspace, baseDir); err != nil {
			return fmt.Errorf("bench.Load: task[%d].workspace.patch: %w", i, err)
		}
	}
	for i := range b.Variants {
		if err := resolvePatch(b.Variants[i].Workspace, baseDir); err != nil {
			return fmt.Errorf("bench.Load: variant[%d].workspace.patch: %w", i, err)
		}
	}
	return nil
}

func resolvePatch(w *Workspace, baseDir string) error {
	if w == nil || w.Patch == "" {
		return nil
	}
	abs := w.Patch
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(baseDir, abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkspacePatch, err)
	}
	sum := sha256.Sum256(data)
	w.PatchAbs = abs
	w.PatchSHA256 = hex.EncodeToString(sum[:])
	return nil
}
```

`Load` becomes: decode → `validate` → `resolvePatches(&b, filepath.Dir(path))` → hash+return.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./bench/ -race`
Expected: PASS (all, including pre-existing).

- [ ] **Step 5: Commit**

```bash
git add bench/basket.go bench/basket_test.go
git commit -m "feat(bench): workspace block — schema, validation, patch hashing at load"
```

---

### Task 2: Evidence — `WorkspaceStamp` on env stamps

**Files:**

- Modify: `evidence/evidence.go`
- Test: `evidence/evidence_test.go`

**Interfaces:**

- Produces (Task 4 relies on): `type WorkspaceStamp struct { Rev string; PatchSHA256 string }`; `EnvStamps.Workspace *WorkspaceStamp` serialized as `"workspace"`, omitted when nil.

- [ ] **Step 1: Write the failing test** (append; mirror the existing EnvStamps marshaling tests in this file):

```go
func TestEnvStampsWorkspaceSerialization(t *testing.T) {
	with := evidence.EnvStamps{
		CatacombVersion: "v",
		Workspace:       &evidence.WorkspaceStamp{Rev: "r42", PatchSHA256: "ab34"},
	}
	data, err := json.Marshal(with)
	require.NoError(t, err)
	require.Contains(t, string(data), `"workspace":{"rev":"r42","patch_sha256":"ab34"}`)

	without := evidence.EnvStamps{CatacombVersion: "v"}
	data, err = json.Marshal(without)
	require.NoError(t, err)
	require.NotContains(t, string(data), "workspace")

	partial := evidence.EnvStamps{Workspace: &evidence.WorkspaceStamp{Rev: "r42"}}
	data, err = json.Marshal(partial)
	require.NoError(t, err)
	require.Contains(t, string(data), `"workspace":{"rev":"r42"}`)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./evidence/ -run TestEnvStampsWorkspace -race`
Expected: compile FAIL (`WorkspaceStamp` undefined).

- [ ] **Step 3: Implement** in `evidence/evidence.go`:

```go
type WorkspaceStamp struct {
	Rev         string `json:"rev,omitempty"`
	PatchSHA256 string `json:"patch_sha256,omitempty"`
}
```

Add to `EnvStamps`: `Workspace *WorkspaceStamp \`json:"workspace,omitempty"\``.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./evidence/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add evidence/evidence.go evidence/evidence_test.go
git commit -m "feat(evidence): descriptive workspace stamp on env stamps"
```

---

### Task 3: Workspace lifecycle executor (`cmd/catacomb/workspace.go`)

**Files:**

- Create: `cmd/catacomb/workspace.go`
- Test: `cmd/catacomb/workspace_test.go`

**Interfaces:**

- Consumes: `bench.Cell.EffectiveWorkspace()`, `bench.Workspace` (Task 1); existing seams `execCommandContext` (`childlocal.go:13`), `exitInfo` (`bench.go`).
- Produces (Task 4 relies on these exact signatures):
  - `type workspaceOpts struct { baseDir string; keep bool }`
  - `func setupWorkspace(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, o workspaceOpts) (dir string, code int, ok bool)` — MkdirTemp under `o.baseDir` (empty = OS temp) with pattern `cell.RunID+"-*"`, then runs `ws.Cmd` argv with cwd=dir, `WaitDelay` 10s, env = `os.Environ()` + `CATACOMB_PATCH=<PatchAbs>` when a patch is declared. On MkdirTemp failure returns `("", -1, false)`; on cmd failure returns `(dir, exitCode, false)` — dir is returned so cleanup still runs.
  - `func cleanupWorkspace(stderr io.Writer, cell bench.Cell, dir string, keep bool) []string` — no-op on `dir == ""`; runs `ws.Teardown` (when declared) with cwd=dir on a **fresh** `context.Background()` bounded by `teardownTimeout` (package var, `time.Minute`); then `removeAllFn(dir)` unless `keep` (then prints `workspace kept: <dir>` to stderr). Returns human notes for the manifest (`"workspace teardown: <err>"`, `"workspace remove: <err>"`), empty slice when clean.
  - Package seam vars: `mkdirTempFn = os.MkdirTemp`, `removeAllFn = os.RemoveAll`, `teardownTimeout = time.Minute`.

- [ ] **Step 1: Write the failing tests** (`cmd/catacomb/workspace_test.go`, package `main`). Cross-platform: all exec goes through a routing capture-fake over the `execCommandContext` seam plus one exit-code helper process (no `sh`). The fake records every returned `*exec.Cmd` — production code sets `.Dir`/`.Env` on those pointers, so assertions read them after the call:

```go
type execCapture struct {
	names []string
	cmds  []*exec.Cmd
}

func stubWorkspaceExec(t *testing.T, exits map[string]int) *execCapture {
	t.Helper()
	t.Setenv("GO_HELPER_WS", "1")
	cap := &execCapture{}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperWorkspaceExit", "--", strconv.Itoa(exits[name]))
		cap.names = append(cap.names, name)
		cap.cmds = append(cap.cmds, c)
		return c
	}
	t.Cleanup(func() { execCommandContext = orig })
	return cap
}

func TestHelperWorkspaceExit(t *testing.T) {
	if os.Getenv("GO_HELPER_WS") != "1" {
		t.Skip("helper process")
	}
	code := 0
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			code, _ = strconv.Atoi(os.Args[i+1])
		}
	}
	os.Exit(code)
}

func wsCell(ws *bench.Workspace) bench.Cell {
	return bench.Cell{RunID: "bench-b-t-v-r1", Task: bench.Task{ID: "t", Workspace: ws}}
}

func TestSetupWorkspaceFreshDirPerCall(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}})
	dir1, code, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	require.Zero(t, code)
	dir2, _, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	require.NotEqual(t, dir1, dir2)
	for _, d := range []string{dir1, dir2} {
		require.DirExists(t, d)
		require.Equal(t, base, filepath.Dir(d))
		require.True(t, strings.HasPrefix(filepath.Base(d), "bench-b-t-v-r1-"))
	}
	require.Equal(t, []string{"ws-cmd", "ws-cmd"}, cap.names)
	require.Equal(t, dir1, cap.cmds[0].Dir)
	require.Equal(t, dir2, cap.cmds[1].Dir)
}

func TestSetupWorkspacePatchEnvOnlyWhenDeclared(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	with := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Patch: "fix.patch", PatchAbs: "/abs/fix.patch"})
	_, _, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, with, workspaceOpts{baseDir: base})
	require.True(t, ok)
	require.Contains(t, cap.cmds[0].Env, "CATACOMB_PATCH=/abs/fix.patch")
	without := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}})
	_, _, ok = setupWorkspace(context.Background(), io.Discard, io.Discard, without, workspaceOpts{baseDir: base})
	require.True(t, ok)
	for _, kv := range cap.cmds[1].Env {
		require.False(t, strings.HasPrefix(kv, "CATACOMB_PATCH="))
	}
}

func TestSetupWorkspaceCmdFailureReturnsDirForCleanup(t *testing.T) {
	base := t.TempDir()
	stubWorkspaceExec(t, map[string]int{"ws-cmd": 3})
	dir, code, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}}), workspaceOpts{baseDir: base})
	require.False(t, ok)
	require.Equal(t, 3, code)
	require.NotEmpty(t, dir)
}

func TestSetupWorkspaceMkdirTempFailure(t *testing.T) {
	stubWorkspaceExec(t, nil)
	orig := mkdirTempFn
	mkdirTempFn = func(string, string) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { mkdirTempFn = orig })
	var errBuf bytes.Buffer
	dir, code, ok := setupWorkspace(context.Background(), io.Discard, &errBuf, wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}}), workspaceOpts{})
	require.False(t, ok)
	require.Empty(t, dir)
	require.Equal(t, -1, code)
	require.Contains(t, errBuf.String(), "workspace")
}

func TestCleanupWorkspaceTeardownRunsAndDirRemoved(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd", "arg1"}})
	dir, _, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	notes := cleanupWorkspace(io.Discard, cell, dir, false)
	require.Empty(t, notes)
	require.Equal(t, []string{"ws-cmd", "td-cmd"}, cap.names)
	require.Equal(t, dir, cap.cmds[1].Dir)
	require.NoDirExists(t, dir)
}

func TestCleanupWorkspaceKeepSkipsRemovalTeardownStillRuns(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}})
	dir, _, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base, keep: true})
	require.True(t, ok)
	var errBuf bytes.Buffer
	notes := cleanupWorkspace(&errBuf, cell, dir, true)
	require.Empty(t, notes)
	require.DirExists(t, dir)
	require.Len(t, cap.names, 2)
	require.Contains(t, errBuf.String(), "workspace kept: "+dir)
}

func TestCleanupWorkspaceFailureNotes(t *testing.T) {
	base := t.TempDir()
	stubWorkspaceExec(t, map[string]int{"td-cmd": 5})
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}})
	dir, _, ok := setupWorkspace(context.Background(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	origRM := removeAllFn
	removeAllFn = func(string) error { return errors.New("busy mount") }
	t.Cleanup(func() { removeAllFn = origRM })
	notes := cleanupWorkspace(io.Discard, cell, dir, false)
	require.Len(t, notes, 2)
	require.Contains(t, notes[0], "workspace teardown")
	require.Contains(t, notes[1], "workspace remove")
}

func TestCleanupWorkspaceEmptyDirNoop(t *testing.T) {
	require.Empty(t, cleanupWorkspace(io.Discard, bench.Cell{}, "", false))
}
```

(`TestHelperWorkspaceExit` inherits `GO_HELPER_WS` through the process environment: `setupWorkspace` builds `c.Env` from `os.Environ()`, and the teardown cmd leaves `Env` nil, which inherits — both paths see the variable set by `t.Setenv`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestSetupWorkspace|TestCleanupWorkspace' -race`
Expected: compile FAIL (`setupWorkspace` undefined).

- [ ] **Step 3: Implement** `cmd/catacomb/workspace.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/realkarych/catacomb/bench"
)

var (
	mkdirTempFn     = os.MkdirTemp
	removeAllFn     = os.RemoveAll
	teardownTimeout = time.Minute
)

type workspaceOpts struct {
	baseDir string
	keep    bool
}

func setupWorkspace(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, o workspaceOpts) (string, int, bool) {
	ws := cell.EffectiveWorkspace()
	dir, err := mkdirTempFn(o.baseDir, cell.RunID+"-*")
	if err != nil {
		fmt.Fprintf(stderr, "bench %s: workspace: %s\n", cell.RunID, err)
		return "", -1, false
	}
	c := execCommandContext(ctx, ws.Cmd[0], ws.Cmd[1:]...)
	c.Dir = dir
	c.Stdout = stdout
	c.Stderr = stderr
	c.WaitDelay = 10 * time.Second
	c.Env = workspaceEnv(ws)
	if code, ok := exitInfo(c.Run()); !ok {
		return dir, code, false
	}
	return dir, 0, true
}

func workspaceEnv(ws *bench.Workspace) []string {
	env := os.Environ()
	if ws.PatchAbs != "" {
		env = append(env, "CATACOMB_PATCH="+ws.PatchAbs)
	}
	return env
}

func cleanupWorkspace(stderr io.Writer, cell bench.Cell, dir string, keep bool) []string {
	if dir == "" {
		return nil
	}
	var notes []string
	if note := runTeardown(stderr, cell, dir); note != "" {
		notes = append(notes, note)
	}
	if keep {
		fmt.Fprintf(stderr, "bench %s: workspace kept: %s\n", cell.RunID, dir)
		return notes
	}
	if err := removeAllFn(dir); err != nil {
		note := "workspace remove: " + err.Error()
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
		notes = append(notes, note)
	}
	return notes
}

func runTeardown(stderr io.Writer, cell bench.Cell, dir string) string {
	ws := cell.EffectiveWorkspace()
	if ws == nil || len(ws.Teardown) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
	defer cancel()
	c := execCommandContext(ctx, ws.Teardown[0], ws.Teardown[1:]...)
	c.Dir = dir
	c.Stderr = stderr
	c.WaitDelay = 10 * time.Second
	if err := c.Run(); err != nil {
		note := "workspace teardown: " + err.Error()
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
		return note
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestSetupWorkspace|TestCleanupWorkspace' -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/workspace.go cmd/catacomb/workspace_test.go
git commit -m "feat(bench): workspace lifecycle executor — setup, teardown on fresh context, cleanup notes"
```

---

### Task 4: Wire workspace into the bench cell path

**Files:**

- Modify: `cmd/catacomb/bench.go`
- Test: `cmd/catacomb/bench_offline_test.go` (cell-path behavior), `cmd/catacomb/bench_test.go` (flags)

**Interfaces:**

- Consumes: Task 1 (`EffectiveWorkspace`, `Workspace.Rev/PatchSHA256`), Task 2 (`evidence.WorkspaceStamp`), Task 3 (`setupWorkspace`, `cleanupWorkspace`, `workspaceOpts`).
- Produces: `bench` flags `--workspaces-dir`, `--keep-workspaces`; `offlineOpts.workspace workspaceOpts`; manifest note `workspace failed`; env stamps carry the workspace block. Dormancy: no effective workspace ⇒ behavior byte-identical to master.

- [ ] **Step 1: Write the failing tests.** In `bench_offline_test.go`, extend the file's existing harness. Add a routed stub beside `stubBenchChild` that dispatches on the argv name: the task's cmd name goes to the existing `TestHelperBenchChild` (which emits the session line and stages the fixture transcript from `HELPER_*` env), everything else (workspace/setup/teardown/verify cmds) goes to `TestHelperWorkspaceExit` from Task 3 with a per-name exit code. It reuses `execCapture` from Task 3 (same package):

```go
func stubBenchExecRouted(t *testing.T, childCmd string, exits map[string]int, env ...string) *execCapture {
	t.Helper()
	t.Setenv("GO_HELPER_BENCH", "1")
	t.Setenv("GO_HELPER_WS", "1")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	cap := &execCapture{}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		var c *exec.Cmd
		if name == childCmd {
			c = exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperBenchChild")
		} else {
			c = exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperWorkspaceExit", "--", strconv.Itoa(exits[name]))
		}
		cap.names = append(cap.names, name)
		cap.cmds = append(cap.cmds, c)
		return c
	}
	t.Cleanup(func() { execCommandContext = orig })
	return cap
}
```

Tests to add (each drives the real cell path; the fresh-dir-per-rep proof with a real shell lives in the hermetic E2E, Task 6):

```go
func TestBenchWorkspaceCellOrderingAndWorkdirThreading(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1",
		bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}}},
		bench.Variant{ID: "v", Setup: []string{"setup-cmd"}})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	require.Equal(t, []string{"ws-cmd", "setup-cmd", "sess-a", "td-cmd"}, cap.names)
	wsDir := cap.cmds[0].Dir
	require.Equal(t, base, filepath.Dir(wsDir))
	require.Equal(t, wsDir, cap.cmds[1].Dir)
	require.Equal(t, wsDir, cap.cmds[2].Dir)
	require.Equal(t, wsDir, cap.cmds[3].Dir)
	require.NoDirExists(t, wsDir)
	require.NotEmpty(t, entry.EvidenceDir)
	for _, kv := range cap.cmds[2].Env {
		require.False(t, strings.HasPrefix(kv, "CATACOMB_PATCH="))
	}
}

func TestBenchWorkspaceStampsInMeta(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	ws := &bench.Workspace{Cmd: []string{"ws-cmd"}, Rev: "r42", PatchSHA256: "ab34"}
	cell := offlineCell("bench-b-t1-v-r1", bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: ws}, bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	got, err := evidence.ScanRuns(runs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Meta.Env)
	require.NotNil(t, got[0].Meta.Env.Workspace)
	require.Equal(t, "r42", got[0].Meta.Env.Workspace.Rev)
	require.Equal(t, "ab34", got[0].Meta.Env.Workspace.PatchSHA256)
	require.NotEmpty(t, entry.EvidenceDir)
}

func TestBenchWorkspaceFailureNoteAndNoEvidence(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", map[string]int{"ws-cmd": 3},
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1",
		bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}}},
		bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.True(t, failed)
	require.False(t, verified)
	require.Equal(t, 3, entry.ExitCode)
	require.Contains(t, entry.Note, "workspace failed")
	require.Empty(t, entry.EvidenceDir)
	require.Equal(t, []string{"ws-cmd", "td-cmd"}, cap.names)
	require.False(t, entry.FinishedAt.IsZero())
}

func TestBenchWorkspaceKeepFlag(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1", bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}}}, bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base, keep: true}}
	var errb bytes.Buffer
	_, failed, _ := runBenchCellOffline(t.Context(), io.Discard, &errb, cell, "h", nil, o)
	require.False(t, failed)
	require.DirExists(t, cap.cmds[0].Dir)
	require.Contains(t, errb.String(), "workspace kept: "+cap.cmds[0].Dir)
}

func TestBenchWorkspaceDormancy(t *testing.T) {
	projects, runs := t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1", bench.Task{ID: "t1", Cmd: []string{"sess-a"}}, bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer()}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	require.Equal(t, []string{"sess-a"}, cap.names)
	got, err := evidence.ScanRuns(runs)
	require.NoError(t, err)
	require.Nil(t, got[0].Meta.Env.Workspace)
	require.NotEmpty(t, entry.EvidenceDir)
}
```

Also: a `runBench`-level test (YAML basket on disk with a failing `ws-cmd` workspace, `benchFlags{failFast: true, workspacesDir: ..., ...}`) asserting the run stops with `errBenchFailFast`; and in `bench_test.go` extend the flag-registration test to assert `--workspaces-dir` (default `""`) and `--keep-workspaces` (default `false`) exist on `newBenchCmd()`. If the verifier path needs workdir-threading coverage beyond the hermetic E2E, add a `verify:` cmd name to the routed stub and assert its captured `.Dir` equals the workspace dir.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run TestBenchWorkspace -race`
Expected: FAIL (flags unknown / no workspace behavior; basket loads but cells run in `sh`'s cwd and the marker guard trips on rep 2).

- [ ] **Step 3: Implement** in `cmd/catacomb/bench.go`:

1. `benchFlags` gains `workspacesDir string`, `keepWorkspaces bool`; register in `newBenchCmd()`:

```go
	cmd.Flags().StringVar(&f.workspacesDir, "workspaces-dir", "", "base dir for per-cell workspace dirs (default: OS temp dir)")
	cmd.Flags().BoolVar(&f.keepWorkspaces, "keep-workspaces", false, "keep per-cell workspace dirs after teardown (paths printed to stderr)")
```

2. `offlineOpts` gains `workspace workspaceOpts`; `benchCellFunc` fills it from the flags.

3. Restructure `runBenchCellOffline` so cleanup runs on every exit path — setup first, then the existing body extracted into `runBenchCellInWorkdir` operating on an explicit `workdir string`, then cleanup + note merge at the single exit:

```go
func runBenchCellOffline(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, hash string, ambient map[string]string, o offlineOpts) (bench.ManifestEntry, bool, bool) {
	entry := bench.ManifestEntry{
		RunID:      cell.RunID,
		Task:       cell.Task.ID,
		Variant:    cell.Variant.ID,
		Rep:        cell.Rep,
		BasketHash: hash,
	}
	if d, _ := cell.Task.TimeoutDuration(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	workdir := cell.Task.Dir
	wsDir := ""
	failed, verified := false, false
	if ws := cell.EffectiveWorkspace(); ws != nil {
		dir, code, ok := setupWorkspace(ctx, stdout, stderr, cell, o.workspace)
		wsDir = dir
		if !ok {
			entry.ExitCode = code
			entry.Note = "workspace failed"
			if ctxErr := ctx.Err(); ctxErr != nil {
				entry.Note = appendNote(entry.Note, ctxNote(ctxErr))
			}
			entry.FinishedAt = nowFn()
			mergeCleanupNotes(&entry, cleanupWorkspace(stderr, cell, wsDir, o.workspace.keep))
			return entry, true, false
		}
		workdir = dir
	}
	failed, verified = runBenchCellInWorkdir(ctx, stdout, stderr, cell, ambient, o, workdir, &entry)
	mergeCleanupNotes(&entry, cleanupWorkspace(stderr, cell, wsDir, o.workspace.keep))
	return entry, failed, verified
}

func mergeCleanupNotes(entry *bench.ManifestEntry, notes []string) {
	for _, n := range notes {
		entry.Note = appendNote(entry.Note, n)
	}
}
```

`runBenchCellInWorkdir` is the current body from the `runSetup` call onward, with every `cell.Task.Dir` replaced by the `workdir` parameter: `runSetup(ctx, stdout, stderr, cell, workdir)` (add the `dir string` parameter to `runSetup`, using it for `c.Dir`), `runChildLocal(..., workdir, ...)`, and it passes `workdir` through to `recordOfflineEvidence` (add a `workdir string` parameter) so `evidence.CaptureArtifacts(dir, workdir, ...)` and `verifyCellOffline`'s `verifySpec.Workdir` use it.

4. Stamps: `benchEnvStamps` gains the workspace param and `recordOfflineEvidence` passes it:

```go
func benchEnvStamps(runs []model.Run, sessionID string, ws *bench.Workspace) *evidence.EnvStamps {
	env := &evidence.EnvStamps{
		CatacombVersion: Version,
		Resources:       evidence.Resources{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()},
	}
	if ws != nil {
		env.Workspace = &evidence.WorkspaceStamp{Rev: ws.Rev, PatchSHA256: ws.PatchSHA256}
	}
	for _, r := range runs {
		if r.ID != sessionID {
			continue
		}
		env.ModelID = r.ModelID
		if r.Repro != nil {
			env.ClaudeCodeVersion = r.Repro.ClaudeCodeVersion
		}
	}
	return env
}
```

Call site: `env := benchEnvStamps(g.RunsSnapshot(), entry.SessionID, cell.EffectiveWorkspace())`.

- [ ] **Step 4: Run the full package and the coverage gate**

Run: `go test ./cmd/catacomb/ -race && make cover`
Expected: PASS, coverage 100%. Existing offline tests must pass untouched (dormancy).

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/bench.go cmd/catacomb/bench_test.go cmd/catacomb/bench_offline_test.go
git commit -m "feat(bench): per-cell workspace wiring — flags, lifecycle bracket, workdir re-rooting, stamps"
```

---

### Task 5: CLI guide — workspace section

**Files:**

- Modify: `docs/guide/cli.md` (the `bench` section)

**Interfaces:** consumes final flag names from Task 4; no code.

- [ ] **Step 1: Write the docs.** In the `bench` section of `docs/guide/cli.md`: add `--workspaces-dir` and `--keep-workspaces` to the flags list; add a `Workspace isolation` subsection documenting: the `workspace: {cmd, patch, rev, teardown}` block on tasks with wholesale per-variant override; fresh dir per cell as cwd for setup/agent/artifacts/verifier; `CATACOMB_PATCH` visible only to `workspace.cmd`; patch path relative to the basket file, hashed at load; teardown on a fresh 1-minute context, always running; `dir` and `workspace` mutually exclusive; stamps `env.workspace{rev, patch_sha256}` descriptive-only; offline `catacomb verify` unchanged (workdir is ephemeral — capture verifier inputs via `artifacts:`); materialization shares the task timeout. Include one YAML example (the trunk vs patched pair from the design spec §1).

- [ ] **Step 2: Lint**

Run: `npx --yes markdownlint-cli2 docs/guide/cli.md`
Expected: 0 errors.

- [ ] **Step 3: Commit**

```bash
git add docs/guide/cli.md
git commit -m "docs(guide): bench workspace isolation section"
```

---

### Task 6: Hermetic E2E — workspace step

**Files:**

- Create: `e2e/hermetic/ws-basket.yaml.tmpl`, `e2e/hermetic/agent_ws.sh`, `e2e/hermetic/verify_ws.py`, `e2e/hermetic/fix.patch`
- Modify: `e2e/hermetic/run.sh` (append as step 20, after step 19; update the header comment's step map)

**Interfaces:** consumes the shipped binary's behavior from Tasks 1–4; uses the script's existing helpers (`run_expect`, `run_json`, `pass`/`failrec`, `$work`, `$CATACOMB_BIN`).

- [ ] **Step 1: Add fixtures.**

`e2e/hermetic/fix.patch` (fixed bytes; the sha assertion depends on them):

```text
patched-line-1
```

`e2e/hermetic/ws-basket.yaml.tmpl`:

```yaml
basket: hermetic-ws
reps: 3
tasks:
  - id: ws
    cmd: ["./agent_ws.sh"]
    timeout: 60s
    artifacts: ["applied.txt"]
    workspace:
      cmd: ["sh", "-c", "cp __WORK__/agent_ws.sh . && cat \"$CATACOMB_PATCH\" > applied.txt && echo setup >> __WORK__/wslog/setup.log"]
      patch: fix.patch
      rev: seed-r1
      teardown: ["sh", "-c", "echo torn >> __WORK__/wslog/teardown.log"]
    verify:
      cmd: ["python3", "__WORK__/verify_ws.py"]
      timeout: 30s
variants:
  - id: only
```

(The rendered basket lands in `$work`, next to a copy of `fix.patch` — patch paths resolve against the basket file's directory.)

`e2e/hermetic/agent_ws.sh` — same transcript synthesis as `agent.sh`, plus the isolation guard:

```bash
#!/usr/bin/env bash
set -euo pipefail
[ ! -f marker ] || exit 7
touch marker
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p "$HERMETIC_PROJECTS/hermetic"
sed "s/__SESSION_ID__/$sid/g" "$HERMETIC_TDIR/transcript.jsonl.tmpl" \
  > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
```

`e2e/hermetic/verify_ws.py` — scores the patch handover (reads the artifact through the SDK so bench and offline re-verify read identical bytes):

```python
import sys
from catacomb_verifier import Cell

cell = Cell.from_env()
data = cell.artifact("applied.txt").read_bytes()
cell.emit(passed=(data == b"patched-line-1\n"))
```

- [ ] **Step 2: Add step 20 to `run.sh`** (after step 19; stage fixtures in step 1's workspace section: render `ws-basket.yaml.tmpl` with the same `__WORK__` substitution, copy `fix.patch`, `agent_ws.sh` (chmod +x), `verify_ws.py` into `$work`, `mkdir -p "$work/wslog"`):

```bash
echo "== step 20: SP-W workspace isolation =="
wsruns="$work/wsruns"
wsroot="$work/wsroot"
mkdir -p "$wsroot"
run_expect 0 "ws bench (3 reps, fresh dir each)" -- \
  catacomb bench "$work/ws-basket.yaml" --runs-dir "$wsruns" \
  --manifest "$work/ws.manifest.jsonl" --workspaces-dir "$wsroot"
grep -q '"exit_code":0' "$work/ws.manifest.jsonl" &&
  ! grep -q '"exit_code":7' "$work/ws.manifest.jsonl"
record $? "isolation guard never tripped (no exit 7 in manifest)"
[ "$(grep -c torn "$work/wslog/teardown.log")" -eq 3 ]
record $? "teardown ran per cell (3 lines)"
[ -z "$(ls -A "$wsroot")" ]
record $? "workspace dirs removed"
wsmeta="$wsruns/bench-hermetic-ws-ws-only-r1/meta.json"
wantsha="$(python3 -c 'import hashlib;print(hashlib.sha256(open("'"$work"'/fix.patch","rb").read()).hexdigest())')"
python3 - "$wsmeta" "$wantsha" <<'EOF'
import json, sys
meta = json.load(open(sys.argv[1]))
ws = meta["env"]["workspace"]
assert ws["rev"] == "seed-r1", ws
assert ws["patch_sha256"] == sys.argv[2], ws
EOF
record $? "meta.json stamps workspace rev + patch sha256"
cmp -s "$wsruns/bench-hermetic-ws-ws-only-r1/artifacts/applied.txt" "$work/fix.patch"
record $? "patch handover applied by user cmd (artifact == patch bytes)"
run_expect 0 "ws bench --keep-workspaces" -- \
  catacomb bench "$work/ws-basket.yaml" --runs-dir "$work/wsruns2" \
  --manifest "$work/ws2.manifest.jsonl" --workspaces-dir "$wsroot" --keep-workspaces
[ -n "$(ls -A "$wsroot")" ] && [ "$(grep -c torn "$work/wslog/teardown.log")" -eq 6 ]
record $? "--keep-workspaces keeps dirs, teardown still ran"
```

Also assert the negative stamp on the existing SQL basket's meta (one line in step 10's python block: `assert "workspace" not in meta["env"]`), and add a seeded-failure sub-assert: a copy of the ws basket with `workspace: {cmd: ["sh", "-c", "exit 3"]}` run with `--fail-fast` must exit non-zero and its manifest note must contain `workspace failed`.

- [ ] **Step 3: Run the whole hermetic E2E against a fresh build**

Run: `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh`
Expected: exit 0, all steps PASS including the new step 20.

- [ ] **Step 4: Update the header comment** step map in `run.sh` (add step 20 to the assertion-order paragraph).

- [ ] **Step 5: Commit**

```bash
git add e2e/hermetic/
git commit -m "test(e2e): hermetic SP-W step — isolation proof, stamps, patch handover, teardown, keep"
```

---

### Task 7: Live E2E — workspace on the SQL basket

**Files:**

- Modify: `e2e/basket-sql.yaml`, `e2e/run.sh`

**Interfaces:** consumes final schema from Task 1; validated by the weekly live gate (not per-PR).

- [ ] **Step 1: Convert the live SQL task to a workspace.** In `e2e/basket-sql.yaml`: remove `dir: .`, add (keeping the existing comments' spirit — YAML comments are fine outside Go):

```yaml
    workspace:
      cmd: ["sh", "-c", "cp \"$E2E_DIR\"/sql-live.sh \"$E2E_DIR\"/verify_sql.py ."]
```

`./sql-live.sh` and `./verify_sql.py` now resolve inside the fresh per-cell dir; `out/result.csv` no longer leaks across cells (today it survives from rep to rep in `e2e/`).

- [ ] **Step 2: Export the anchor in `e2e/run.sh`.** Where the driver stages the SQL fixtures (near the `sqlite3 "$sqldb"` seeding), add `export E2E_DIR="$e2e_dir"` so `workspace.cmd` (which inherits the driver's environment through catacomb) can copy the scripts.

- [ ] **Step 3: Static validation** (live cells cost money; the weekly gate is the acceptance):

Run: `bash -n e2e/run.sh && bin/catacomb bench e2e/basket-sql.yaml --dry-run`
Expected: `bash -n` clean; dry-run prints the 15-cell expansion (schema accepted).

- [ ] **Step 4: Commit**

```bash
git add e2e/basket-sql.yaml e2e/run.sh
git commit -m "test(e2e): live SQL basket runs in per-cell workspaces"
```

Post-merge follow-up (not a task): trigger `e2e-live.yml` via workflow_dispatch or wait for the Monday cron; A-vs-A must stay clean and degraded must still gate.

---

## Final verification (after Task 7, before the PR)

- [ ] `make fmt && make lint && make cover` — all green, coverage 100%.
- [ ] `go test ./... -race` — green.
- [ ] `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh` — exit 0.
- [ ] `npx --yes markdownlint-cli2 "**/*.md" "#node_modules"` — clean.
- [ ] Squash PR `feat: SP-W per-cell workspace isolation` referencing ADR-0028; all required checks SUCCESS before merge.
