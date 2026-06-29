# `catacomb down` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `catacomb down` command that gracefully stops the daemon and, with escalation flags, removes catacomb's local artifacts (hooks, database, `~/.catacomb` state) — safely and non-interactively for LLM-agent callers.

**Architecture:** A new `cmd/catacomb/down.go` cobra command following the repo's `newXxxCmd()` + `runXxx(out, opts, …)` pattern. OS primitives (signal, file removal, stat, tty, confirm, discovery read) are package-level `var` seams (the same idiom as `osExecutable`/`execCommand`/`startCmd`) so every branch is unit-testable without real processes or filesystem side effects. Local purge is plain file deletion after the daemon has stopped — the `Store` interface is untouched. Hook removal reuses the existing `installHooks(path, …, true)` path, guarded by an existence check.

**Tech Stack:** Go 1.26, spf13/cobra, modernc.org/sqlite (WAL), mattn/go-isatty (already an indirect dep), testify.

## Global Constraints

- **No comments in Go code** — none, not even doc comments. Only `//go:build`, `//go:embed`, `//go:generate` directives are allowed. Enforced by `internal/codepolicy`.
- **100% test coverage**, TDD-first. Write the failing test before the implementation. The threshold never drops.
- **Module path:** `github.com/realkarych/catacomb`.
- **All new command code lives in `package main` under `cmd/catacomb/`** — it can call existing unexported helpers (`installHooks`, `settingsPath`, `osExecutable`, `osUserHomeDir`) directly.
- **Phase 1 only** (this plan): bare stop, `--uninstall`, `--purge`, `--all`, `--db`, `--force`, `--dry-run`, `--yes`, `--json`. `--external` (Postgres/Neo4j drop) is Phase 2 and is **not** in this plan.
- **Error idiom:** sentinel errors in `cmd/catacomb/errors.go`, wrapped with `fmt.Errorf("%w: %w", ErrXxx, underlying)`. Root command has `SilenceErrors: true`; a nonzero exit comes from returning a non-nil error from `RunE`.

---

## File Structure

- **Create `cmd/catacomb/down.go`** — the command: `newDownCmd`, `runDown`, the `downOpts`/`downReport` types, the process-control helpers (`signalProcess`, `stopDaemon`, `waitGone`), the scope helpers (`uninstallHooks`, `purgeLocal`, target computation), the IO seams, and report rendering.
- **Create `cmd/catacomb/down_test.go`** — all tests for the above.
- **Modify `cmd/catacomb/root.go`** — register `observe(newDownCmd())` next to `up`.
- **Modify `cmd/catacomb/errors.go`** — add `ErrDaemonStop`, `ErrConfirmationRequired`.
- **Modify `cmd/catacomb/daemon.go`** + its test — remove the discovery file on graceful daemon shutdown (Task 7, isolated).

---

## Task 1: Process-control primitives

**Files:**
- Create: `cmd/catacomb/down.go`
- Test: `cmd/catacomb/down_test.go`

**Interfaces:**
- Produces: `signalProcess(pid int, sig syscall.Signal) error`; package vars `downSignal`, `downSleep`; `stopDaemon(pid int, force bool) (bool, error)`; `waitGone(pid int) bool`; consts `downStopInterval`, `downStopAttempts`.
- Consumes: `ErrDaemonStop` (added here to `errors.go`).

- [ ] **Step 1: Add the sentinel error**

In `cmd/catacomb/errors.go`, inside the existing `var (...)` block, add:

```go
	ErrDaemonStop           = errors.New("failed to stop the catacomb daemon")
	ErrConfirmationRequired = errors.New("refusing a destructive teardown without --yes in a non-interactive shell")
```

- [ ] **Step 2: Write the failing tests**

Create `cmd/catacomb/down_test.go`:

```go
package main

import (
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func swapSignal(t *testing.T, fn func(int, syscall.Signal) error) {
	t.Helper()
	orig := downSignal
	downSignal = fn
	t.Cleanup(func() { downSignal = orig })
}

func swapSleepNoop(t *testing.T) {
	t.Helper()
	orig := downSleep
	downSleep = func(_ time.Duration) {}
	t.Cleanup(func() { downSleep = orig })
}

func TestSignalProcessAliveAndDead(t *testing.T) {
	require.NoError(t, signalProcess(os.Getpid(), syscall.Signal(0)))
	assert.Error(t, signalProcess(1<<30, syscall.Signal(0)))
}

func TestStopDaemonNoPid(t *testing.T) {
	stopped, err := stopDaemon(0, false)
	require.NoError(t, err)
	assert.False(t, stopped)
}

func TestStopDaemonNotAlive(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	stopped, err := stopDaemon(42, false)
	require.NoError(t, err)
	assert.False(t, stopped)
}

func TestStopDaemonGraceful(t *testing.T) {
	swapSleepNoop(t)
	n := 0
	swapSignal(t, func(_ int, _ syscall.Signal) error {
		n++
		if n >= 3 {
			return errors.New("gone")
		}
		return nil
	})
	stopped, err := stopDaemon(42, false)
	require.NoError(t, err)
	assert.True(t, stopped)
}

func TestStopDaemonStuckNoForce(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, _ syscall.Signal) error { return nil })
	stopped, err := stopDaemon(42, false)
	assert.False(t, stopped)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestStopDaemonForceKill(t *testing.T) {
	swapSleepNoop(t)
	killed := false
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			killed = true
			return nil
		}
		if killed {
			return errors.New("gone")
		}
		return nil
	})
	stopped, err := stopDaemon(42, true)
	require.NoError(t, err)
	assert.True(t, stopped)
}

func TestStopDaemonSigtermError(t *testing.T) {
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			return errors.New("eperm")
		}
		return nil
	})
	_, err := stopDaemon(42, false)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestStopDaemonForceKillError(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			return errors.New("eperm")
		}
		return nil
	})
	_, err := stopDaemon(42, true)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestStopDaemonForceStillAlive(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, _ syscall.Signal) error { return nil })
	stopped, err := stopDaemon(42, true)
	assert.False(t, stopped)
	assert.ErrorIs(t, err, ErrDaemonStop)
}
```

Add the imports `"os"` and `"time"` to the test file (used above).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestSignalProcess|TestStopDaemon' -v`
Expected: FAIL (compile error — `signalProcess`/`stopDaemon`/`downSignal`/`downSleep` undefined).

- [ ] **Step 4: Write the implementation**

Create `cmd/catacomb/down.go`:

```go
package main

import (
	"os"
	"syscall"
	"time"
)

const (
	downStopInterval = 100 * time.Millisecond
	downStopAttempts = 50
)

var (
	downSignal = signalProcess
	downSleep  = time.Sleep
)

func signalProcess(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(sig)
}

func waitGone(pid int) bool {
	for i := 0; i < downStopAttempts; i++ {
		if err := downSignal(pid, syscall.Signal(0)); err != nil {
			return true
		}
		downSleep(downStopInterval)
	}
	return false
}

func stopDaemon(pid int, force bool) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	if err := downSignal(pid, syscall.Signal(0)); err != nil {
		return false, nil
	}
	if err := downSignal(pid, syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDaemonStop, err)
	}
	if waitGone(pid) {
		return true, nil
	}
	if !force {
		return false, ErrDaemonStop
	}
	if err := downSignal(pid, syscall.SIGKILL); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDaemonStop, err)
	}
	if waitGone(pid) {
		return true, nil
	}
	return false, ErrDaemonStop
}
```

Add `"fmt"` to the import block.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestSignalProcess|TestStopDaemon' -v`
Expected: PASS (all 9).

- [ ] **Step 6: Commit**

```bash
git add cmd/catacomb/down.go cmd/catacomb/down_test.go cmd/catacomb/errors.go
git commit -m "feat(down): process-control primitives for daemon stop"
```

---

## Task 2: Bare `catacomb down` (stop + remove discovery) + registration

**Files:**
- Modify: `cmd/catacomb/down.go`
- Modify: `cmd/catacomb/root.go`
- Test: `cmd/catacomb/down_test.go`

**Interfaces:**
- Consumes: `stopDaemon` (Task 1); `daemon.ReadDiscovery`, `daemon.DiscoveryPath`, `daemon.Discovery`, `daemon.WriteDiscovery`.
- Produces: `downOpts` struct, `downReport` struct, `runDown(out io.Writer, opts downOpts, discoveryPath string) error`, `newDownCmd() *cobra.Command`, seams `downReadDiscovery`/`downRemove`, `writeDownReport`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/catacomb/down_test.go`:

```go
func writeDisc(t *testing.T, pid int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{
		Addr: "127.0.0.1:1", Token: "tok", Pid: pid, DBPath: filepath.Join(dir, "catacomb.db"),
	}))
	return path, dir
}

func TestRunDownNoDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{}, path))
	assert.Contains(t, out.String(), "no daemon")
}

func TestRunDownStopsAndRemovesDiscovery(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			return nil
		}
		return errors.New("gone")
	})
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{}, path))
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
	assert.Contains(t, out.String(), "stopped")
}

func TestRunDownStaleDiscoveryRemoved(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{}, path))
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunDownReadDiscoveryError(t *testing.T) {
	orig := downReadDiscovery
	downReadDiscovery = func(string) (daemon.Discovery, error) { return daemon.Discovery{}, errors.New("boom") }
	t.Cleanup(func() { downReadDiscovery = orig })
	err := runDown(io.Discard, downOpts{}, "/whatever")
	assert.Error(t, err)
}

func TestRunDownStopError(t *testing.T) {
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			return errors.New("eperm")
		}
		return nil
	})
	path, _ := writeDisc(t, 4242)
	err := runDown(io.Discard, downOpts{}, path)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestRunDownJSON(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{asJSON: true}, path))
	var rep downReport
	require.NoError(t, json.Unmarshal([]byte(out.String()), &rep))
	assert.True(t, rep.DiscoveryRemoved)
}

func TestNewDownCmdRegistered(t *testing.T) {
	cmd := newDownCmd()
	assert.Equal(t, "down", cmd.Name())
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(t.TempDir(), "none.json"))
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())
}
```

Add imports to the test file: `"encoding/json"`, `"io"`, `"path/filepath"`, `"strings"`, and `"github.com/realkarych/catacomb/daemon"`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRunDown|TestNewDownCmd' -v`
Expected: FAIL (undefined `runDown`, `downOpts`, `downReport`, `newDownCmd`, `downReadDiscovery`).

- [ ] **Step 3: Write the implementation**

Add to `cmd/catacomb/down.go` (and extend the import block with `"encoding/json"`, `"errors"`, `"io"`, `"github.com/spf13/cobra"`, `"github.com/realkarych/catacomb/daemon"`):

```go
type downOpts struct {
	uninstall bool
	purge     bool
	all       bool
	dbPaths   []string
	force     bool
	dryRun    bool
	yes       bool
	asJSON    bool
}

type downReport struct {
	DaemonStopped    bool     `json:"daemon_stopped"`
	DiscoveryRemoved bool     `json:"discovery_removed"`
	HooksRemoved     []string `json:"hooks_removed"`
	DatabasesRemoved []string `json:"databases_removed"`
	StateRemoved     []string `json:"state_removed"`
	Warnings         []string `json:"warnings,omitempty"`
	DryRun           bool     `json:"dry_run"`
}

var (
	downReadDiscovery = daemon.ReadDiscovery
	downRemove        = os.Remove
)

func newDownCmd() *cobra.Command {
	var opts downOpts
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the daemon and optionally remove catacomb's artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDown(cmd.OutOrStdout(), opts, daemon.DiscoveryPath())
		},
	}
	cmd.Flags().BoolVar(&opts.uninstall, "uninstall", false, "also remove catacomb hook entries from .claude/settings.json")
	cmd.Flags().BoolVar(&opts.purge, "purge", false, "also delete the local database and ~/.catacomb state")
	cmd.Flags().BoolVar(&opts.all, "all", false, "shorthand for --uninstall --purge")
	cmd.Flags().StringArrayVar(&opts.dbPaths, "db", nil, "additional database file to delete under --purge (repeatable)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "escalate a stuck daemon stop to SIGKILL")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print what would be done without changing anything")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "skip the confirmation prompt (required in non-interactive shells)")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "output a machine-readable report")
	return cmd
}

func runDown(out io.Writer, opts downOpts, discoveryPath string) error {
	if opts.all {
		opts.uninstall = true
		opts.purge = true
	}
	rep := downReport{DryRun: opts.dryRun}

	disc, derr := downReadDiscovery(discoveryPath)
	haveDisc := derr == nil
	if derr != nil && !errors.Is(derr, os.ErrNotExist) {
		return derr
	}

	if !haveDisc {
		_, _ = fmt.Fprintln(out, "no daemon running")
	} else {
		stopped, serr := stopDaemon(disc.Pid, opts.force)
		if serr != nil {
			return serr
		}
		rep.DaemonStopped = stopped
		if err := downRemove(discoveryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("down: remove discovery: %w", err)
		}
		rep.DiscoveryRemoved = true
	}

	return writeDownReport(out, rep, opts.asJSON)
}

func writeDownReport(out io.Writer, rep downReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	if rep.DaemonStopped {
		_, _ = fmt.Fprintln(out, "daemon stopped")
	}
	for _, h := range rep.HooksRemoved {
		_, _ = fmt.Fprintf(out, "removed hooks: %s\n", h)
	}
	for _, d := range rep.DatabasesRemoved {
		_, _ = fmt.Fprintf(out, "removed database: %s\n", d)
	}
	for _, s := range rep.StateRemoved {
		_, _ = fmt.Fprintf(out, "removed state: %s\n", s)
	}
	for _, w := range rep.Warnings {
		_, _ = fmt.Fprintf(out, "warning: %s\n", w)
	}
	return nil
}
```

In `cmd/catacomb/root.go`, register next to `up`:

```go
	root.AddCommand(observe(newDownCmd()))
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestRunDown|TestNewDownCmd' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/down.go cmd/catacomb/down_test.go cmd/catacomb/root.go
git commit -m "feat(down): bare 'catacomb down' stops daemon and clears discovery"
```

---

## Task 3: `--uninstall` scope (reuse installHooks, existence-guarded)

**Files:**
- Modify: `cmd/catacomb/down.go`
- Test: `cmd/catacomb/down_test.go`

**Interfaces:**
- Consumes: existing `installHooks(path, discovery, exe string, uninstall bool) error`, `settingsPath(project, global bool) (string, error)`, `osExecutable`, `daemon.DiscoveryPath`.
- Produces: `uninstallHooks() ([]string, error)`, seams `downStat`, `downHookTargets`.

- [ ] **Step 1: Write the failing tests**

Append to `down_test.go`:

```go
func TestUninstallHooksPrunesExistingOnly(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj.json")
	glob := filepath.Join(t.TempDir(), "absent-global.json")
	require.NoError(t, installHooks(proj, "/run/d.json", "/usr/bin/catacomb", false))

	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{proj, glob}, nil }
	t.Cleanup(func() { downHookTargets = orig })

	removed, err := uninstallHooks()
	require.NoError(t, err)
	assert.Equal(t, []string{proj}, removed)

	_, statErr := os.Stat(glob)
	assert.True(t, os.IsNotExist(statErr), "absent settings file must not be created")
}

func TestRunDownUninstall(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj.json")
	require.NoError(t, installHooks(proj, "/run/d.json", "/usr/bin/catacomb", false))
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{proj}, nil }
	t.Cleanup(func() { downHookTargets = orig })

	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{uninstall: true}, path))
	assert.Contains(t, out.String(), "removed hooks")
}

func TestUninstallHooksTargetError(t *testing.T) {
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return nil, errors.New("no home") }
	t.Cleanup(func() { downHookTargets = orig })
	_, err := uninstallHooks()
	assert.Error(t, err)
}

func TestDefaultHookTargets(t *testing.T) {
	got, err := downHookTargets()
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestUninstallHooks|TestRunDownUninstall|TestDefaultHookTargets' -v`
Expected: FAIL (undefined `uninstallHooks`, `downHookTargets`).

- [ ] **Step 3: Write the implementation**

Add to `down.go`:

```go
var (
	downStat        = os.Stat
	downHookTargets = defaultHookTargets
)

func defaultHookTargets() ([]string, error) {
	project, err := settingsPath(false, false)
	if err != nil {
		return nil, err
	}
	global, err := settingsPath(false, true)
	if err != nil {
		return nil, err
	}
	return []string{project, global}, nil
}

func uninstallHooks() ([]string, error) {
	targets, err := downHookTargets()
	if err != nil {
		return nil, err
	}
	exe, err := osExecutable()
	if err != nil {
		return nil, fmt.Errorf("down: executable: %w", err)
	}
	var removed []string
	for _, path := range targets {
		if _, statErr := downStat(path); statErr != nil {
			continue
		}
		if err := installHooks(path, daemon.DiscoveryPath(), exe, true); err != nil {
			return nil, err
		}
		removed = append(removed, path)
	}
	return removed, nil
}
```

In `runDown`, after the discovery/stop block and before `writeDownReport`, add:

```go
	if opts.uninstall {
		removed, err := uninstallHooks()
		if err != nil {
			return err
		}
		rep.HooksRemoved = removed
	}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestUninstallHooks|TestRunDownUninstall|TestDefaultHookTargets' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/down.go cmd/catacomb/down_test.go
git commit -m "feat(down): --uninstall prunes hook entries from existing settings only"
```

---

## Task 4: Confirmation & non-TTY gate

**Files:**
- Modify: `cmd/catacomb/down.go`
- Test: `cmd/catacomb/down_test.go`

**Interfaces:**
- Produces: `confirmDestructive(out io.Writer, opts downOpts) (bool, error)`; seams `downIsTerminal`, `downConfirm`; helpers `defaultIsTerminal`, `readConfirm`.
- Consumes: `ErrConfirmationRequired` (Task 1).

- [ ] **Step 1: Write the failing tests**

Append to `down_test.go`:

```go
func swapTTY(t *testing.T, v bool) {
	t.Helper()
	orig := downIsTerminal
	downIsTerminal = func() bool { return v }
	t.Cleanup(func() { downIsTerminal = orig })
}

func TestConfirmYesFlagBypasses(t *testing.T) {
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true, yes: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmDryRunBypasses(t *testing.T) {
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true, dryRun: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmNonDestructiveBypasses(t *testing.T) {
	ok, err := confirmDestructive(io.Discard, downOpts{uninstall: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmNonTTYRefuses(t *testing.T) {
	swapTTY(t, false)
	_, err := confirmDestructive(io.Discard, downOpts{purge: true})
	assert.ErrorIs(t, err, ErrConfirmationRequired)
}

func TestConfirmTTYPromptYes(t *testing.T) {
	swapTTY(t, true)
	orig := downConfirm
	downConfirm = func(io.Writer, string) (bool, error) { return true, nil }
	t.Cleanup(func() { downConfirm = orig })
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmTTYPromptNo(t *testing.T) {
	swapTTY(t, true)
	orig := downConfirm
	downConfirm = func(io.Writer, string) (bool, error) { return false, nil }
	t.Cleanup(func() { downConfirm = orig })
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestReadConfirmVariants(t *testing.T) {
	for _, in := range []string{"y\n", "yes\n", "Y\n"} {
		ok, err := readConfirm(strings.NewReader(in), io.Discard, "p? ")
		require.NoError(t, err)
		assert.True(t, ok)
	}
	ok, err := readConfirm(strings.NewReader("n\n"), io.Discard, "p? ")
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = readConfirm(strings.NewReader(""), io.Discard, "p? ")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDefaultIsTerminalCallable(t *testing.T) {
	_ = defaultIsTerminal()
}

func TestDownConfirmReadsStdin(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	_, _ = w.WriteString("y\n")
	_ = w.Close()
	ok, err := downConfirm(io.Discard, "p? ")
	require.NoError(t, err)
	assert.True(t, ok)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestConfirm|TestReadConfirm|TestDefaultIsTerminal|TestDownConfirm' -v`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Write the implementation**

Add to `down.go` (extend imports with `"bufio"`, `"strings"`, and `"github.com/mattn/go-isatty"`):

```go
var (
	downIsTerminal = defaultIsTerminal
	downConfirm    = func(out io.Writer, prompt string) (bool, error) {
		return readConfirm(os.Stdin, out, prompt)
	}
)

func defaultIsTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

func readConfirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	_, _ = fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func confirmDestructive(out io.Writer, opts downOpts) (bool, error) {
	if !opts.purge || opts.yes || opts.dryRun {
		return true, nil
	}
	if !downIsTerminal() {
		return false, ErrConfirmationRequired
	}
	return downConfirm(out, "This permanently deletes catacomb data. Continue? [y/N] ")
}
```

Run `go mod tidy` so `github.com/mattn/go-isatty` becomes a direct dependency.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestConfirm|TestReadConfirm|TestDefaultIsTerminal|TestDownConfirm' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/down.go cmd/catacomb/down_test.go go.mod go.sum
git commit -m "feat(down): confirmation + non-TTY gate for destructive scopes"
```

---

## Task 5: `--purge` scope (database + state deletion)

**Files:**
- Modify: `cmd/catacomb/down.go`
- Test: `cmd/catacomb/down_test.go`

**Interfaces:**
- Consumes: `confirmDestructive` (Task 4); `osUserHomeDir`; `downRemove` (Task 2).
- Produces: `purgeLocal(opts downOpts, disc daemon.Discovery, haveDisc bool, discoveryPath string) (dbs, state, warnings []string, err error)`; helpers `dbTargets`, `stateTargets`, `removeWithSiblings`; seam `downRemoveAll`.

- [ ] **Step 1: Write the failing tests**

Append to `down_test.go`:

```go
func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func TestPurgeRemovesDBWithSiblings(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "catacomb.db")
	touch(t, db)
	touch(t, db+"-wal")
	touch(t, db+"-shm")
	disc := daemon.Discovery{DBPath: db}

	dbs, _, _, err := purgeLocal(downOpts{purge: true, yes: true}, disc, true, filepath.Join(dir, "daemon.json"))
	require.NoError(t, err)
	assert.Contains(t, dbs, db)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_, statErr := os.Stat(db + suffix)
		assert.True(t, os.IsNotExist(statErr))
	}
}

func TestPurgeExtraDBPaths(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "other.db")
	touch(t, extra)
	dbs, _, _, err := purgeLocal(downOpts{purge: true, yes: true, dbPaths: []string{extra}}, daemon.Discovery{}, false, filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	assert.Contains(t, dbs, extra)
}

func TestPurgeWarnsWhenNoKnownDB(t *testing.T) {
	dir := t.TempDir()
	_, _, warns, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{}, false, filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	require.NotEmpty(t, warns)
	assert.Contains(t, warns[0], "--db")
}

func TestPurgeRemovesStateAndLog(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })

	catacombDir := filepath.Join(home, ".catacomb")
	require.NoError(t, os.MkdirAll(catacombDir, 0o700))
	discPath := filepath.Join(t.TempDir(), "daemon.json")
	touch(t, discPath+".log")

	_, state, _, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{DBPath: "/nope"}, true, discPath)
	require.NoError(t, err)
	assert.Contains(t, state, catacombDir)
	_, statErr := os.Stat(catacombDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestPurgeHomeError(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = origHome })
	_, _, _, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{}, false, "/x/d.json")
	assert.Error(t, err)
}

func TestRunDownPurgeAbortsOnNo(t *testing.T) {
	swapTTY(t, true)
	orig := downConfirm
	downConfirm = func(io.Writer, string) (bool, error) { return false, nil }
	t.Cleanup(func() { downConfirm = orig })
	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{purge: true}, path))
	assert.Contains(t, out.String(), "aborted")
}

func TestRunDownPurgeNonTTY(t *testing.T) {
	swapTTY(t, false)
	path := filepath.Join(t.TempDir(), "absent.json")
	err := runDown(io.Discard, downOpts{purge: true}, path)
	assert.ErrorIs(t, err, ErrConfirmationRequired)
}

func TestRemoveWithSiblingsRemoveError(t *testing.T) {
	orig := downRemove
	downRemove = func(string) error { return errors.New("eperm") }
	t.Cleanup(func() { downRemove = orig })
	_, err := removeWithSiblings("/whatever.db")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestPurge|TestRunDownPurge|TestRemoveWithSiblings' -v`
Expected: FAIL (undefined `purgeLocal`, `removeWithSiblings`, `downRemoveAll`).

- [ ] **Step 3: Write the implementation**

Add to `down.go` (extend imports with `"path/filepath"`):

```go
var downRemoveAll = os.RemoveAll

func dbTargets(opts downOpts, disc daemon.Discovery, haveDisc bool) []string {
	var targets []string
	if haveDisc && disc.DBPath != "" {
		targets = append(targets, disc.DBPath)
	}
	targets = append(targets, opts.dbPaths...)
	return targets
}

func removeWithSiblings(db string) (bool, error) {
	removedAny := false
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := downRemove(db + suffix)
		if err == nil {
			removedAny = true
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("down: remove %s: %w", db+suffix, err)
		}
	}
	return removedAny, nil
}

func stateTargets(discoveryPath string) ([]string, error) {
	home, err := osUserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("down: home: %w", err)
	}
	return []string{discoveryPath + ".log", filepath.Join(home, ".catacomb")}, nil
}

func purgeLocal(opts downOpts, disc daemon.Discovery, haveDisc bool, discoveryPath string) ([]string, []string, []string, error) {
	var dbs, state, warnings []string

	targets := dbTargets(opts, disc, haveDisc)
	if len(targets) == 0 {
		warnings = append(warnings, "no database path known; other databases may remain where you ran catacomb (pass --db)")
	}
	for _, db := range targets {
		removed, err := removeWithSiblings(db)
		if err != nil {
			return nil, nil, nil, err
		}
		if removed {
			dbs = append(dbs, db)
		}
	}

	st, err := stateTargets(discoveryPath)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, path := range st {
		err := downRemoveAll(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, nil, fmt.Errorf("down: remove %s: %w", path, err)
		}
		if _, statErr := downStat(path); os.IsNotExist(statErr) {
			state = append(state, path)
		}
	}
	return dbs, state, warnings, nil
}
```

Insert the confirmation gate and purge call into `runDown`. Place the gate immediately after the `if opts.all { … }` normalization and before reading discovery:

```go
	proceed, cerr := confirmDestructive(out, opts)
	if cerr != nil {
		return cerr
	}
	if !proceed {
		_, _ = fmt.Fprintln(out, "aborted")
		return nil
	}
```

And add the purge block in `runDown` after the `--uninstall` block:

```go
	if opts.purge {
		dbs, state, warns, err := purgeLocal(opts, disc, haveDisc, discoveryPath)
		if err != nil {
			return err
		}
		rep.DatabasesRemoved = dbs
		rep.StateRemoved = state
		rep.Warnings = warns
	}
```

Note on `stateTargets`: `downRemoveAll` of `discoveryPath+".log"` removes the log; the `~/.catacomb` entry removes the default run dir + discovery + log together when discovery lives there. The `downStat` re-check guards the report against listing paths that never existed.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestPurge|TestRunDownPurge|TestRemoveWithSiblings' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/down.go cmd/catacomb/down_test.go
git commit -m "feat(down): --purge deletes local database, WAL siblings, and ~/.catacomb state"
```

---

## Task 6: `--all`, `--dry-run`, and full-report integration

**Files:**
- Modify: `cmd/catacomb/down.go`
- Test: `cmd/catacomb/down_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `--dry-run` planning path through `runDown` (no side effects); integration coverage of `--all`.

- [ ] **Step 1: Write the failing tests**

Append to `down_test.go`:

```go
func TestRunDownAllNoDaemon(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj.json")
	require.NoError(t, installHooks(proj, "/run/d.json", "/usr/bin/catacomb", false))
	origTargets := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{proj}, nil }
	t.Cleanup(func() { downHookTargets = origTargets })

	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })

	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{all: true, yes: true}, path))
	assert.Contains(t, out.String(), "removed hooks")
}

func TestRunDownDryRunChangesNothing(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "catacomb.db")
	touch(t, db)
	path, _ := writeDisc(t, 4242)

	called := false
	swapSignal(t, func(int, syscall.Signal) error { called = true; return nil })

	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{all: true, dryRun: true, dbPaths: []string{db}}, path))

	assert.False(t, called, "dry-run must not signal the daemon")
	_, statErr := os.Stat(db)
	assert.False(t, os.IsNotExist(statErr), "dry-run must not delete the database")
	_, discErr := os.Stat(path)
	assert.False(t, os.IsNotExist(discErr), "dry-run must not remove discovery")
	assert.Contains(t, out.String(), "would")
}

func TestRunDownDryRunJSON(t *testing.T) {
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{dryRun: true, asJSON: true}, path))
	var rep downReport
	require.NoError(t, json.Unmarshal([]byte(out.String()), &rep))
	assert.True(t, rep.DryRun)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRunDownAll|TestRunDownDryRun' -v`
Expected: FAIL — `TestRunDownDryRun*` fail because the current `runDown` executes side effects regardless of `dryRun`.

- [ ] **Step 3: Write the implementation**

Add a dry-run planning branch to `runDown`, placed right after the confirmation gate and the `disc, derr := …` read (so the plan can probe liveness read-only), and before the live stop block. Wrap the existing side-effecting blocks so they are skipped under `dryRun`:

```go
	if opts.dryRun {
		return planDown(out, opts, disc, haveDisc, discoveryPath)
	}
```

Then add `planDown`:

```go
func planDown(out io.Writer, opts downOpts, disc daemon.Discovery, haveDisc bool, discoveryPath string) error {
	rep := downReport{DryRun: true}
	if haveDisc {
		if err := downSignal(disc.Pid, syscall.Signal(0)); err == nil {
			rep.DaemonStopped = true
		}
		rep.DiscoveryRemoved = true
	}
	if opts.uninstall {
		if targets, err := downHookTargets(); err == nil {
			for _, path := range targets {
				if _, statErr := downStat(path); statErr == nil {
					rep.HooksRemoved = append(rep.HooksRemoved, path)
				}
			}
		}
	}
	if opts.purge {
		rep.DatabasesRemoved = dbTargets(opts, disc, haveDisc)
		if st, err := stateTargets(discoveryPath); err == nil {
			rep.StateRemoved = st
		}
		if len(dbTargets(opts, disc, haveDisc)) == 0 {
			rep.Warnings = append(rep.Warnings, "no database path known; pass --db")
		}
	}
	return writePlan(out, rep, opts.asJSON)
}

func writePlan(out io.Writer, rep downReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	if rep.DaemonStopped {
		_, _ = fmt.Fprintln(out, "would stop daemon")
	}
	if rep.DiscoveryRemoved {
		_, _ = fmt.Fprintln(out, "would remove discovery file")
	}
	for _, h := range rep.HooksRemoved {
		_, _ = fmt.Fprintf(out, "would remove hooks: %s\n", h)
	}
	for _, d := range rep.DatabasesRemoved {
		_, _ = fmt.Fprintf(out, "would remove database: %s\n", d)
	}
	for _, s := range rep.StateRemoved {
		_, _ = fmt.Fprintf(out, "would remove state: %s\n", s)
	}
	for _, w := range rep.Warnings {
		_, _ = fmt.Fprintf(out, "warning: %s\n", w)
	}
	return nil
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestRunDownAll|TestRunDownDryRun' -v`
Expected: PASS.

- [ ] **Step 5: Run the whole package with coverage**

Run: `go test ./cmd/catacomb/ -cover`
Expected: PASS. Then confirm 100% for the new file:

Run: `go test ./cmd/catacomb/ -coverprofile=/tmp/cov.out && go tool cover -func=/tmp/cov.out | grep down.go`
Expected: every `down.go` symbol at `100.0%`. If any line is uncovered, add a targeted test before committing.

- [ ] **Step 6: Commit**

```bash
git add cmd/catacomb/down.go cmd/catacomb/down_test.go
git commit -m "feat(down): --all shorthand and --dry-run planning with no side effects"
```

---

## Task 7: Daemon removes its discovery file on graceful shutdown

This is isolated from the command and closes the papercut where a manual `kill` or Ctrl-C on `catacomb daemon` leaves a stale discovery pointer. `down` already cleans stale discovery defensively; this makes any graceful exit self-clean.

**Files:**
- Modify: `cmd/catacomb/daemon.go`
- Test: `cmd/catacomb/daemon_test.go` (or the existing test file covering `runDaemonWith`)

**Interfaces:**
- Consumes: existing `runDaemonWith(...)`, `daemon.WriteDiscovery`, `daemon.ReadDiscovery`.
- Produces: discovery-file removal after `d.Serve` returns.

- [ ] **Step 1: Write the failing test**

Find the existing test that drives `runDaemonWith` with fake listeners/store and a cancellable context (mirror its setup). Add:

```go
func TestRunDaemonRemovesDiscoveryOnShutdown(t *testing.T) {
	discPath := filepath.Join(t.TempDir(), "daemon.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runDaemonWith(
		ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken,
		filepath.Join(t.TempDir(), "g.db"), discPath,
		0, 4096, "", "catacomb", "", "", "", "", "", nil, false, false,
	)
	require.NoError(t, err)
	_, statErr := os.Stat(discPath)
	assert.True(t, os.IsNotExist(statErr))
}
```

Adjust the argument list to match the exact current `runDaemonWith` signature if it has drifted.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/catacomb/ -run TestRunDaemonRemovesDiscoveryOnShutdown -v`
Expected: FAIL (discovery file still present after shutdown).

- [ ] **Step 3: Write the implementation**

In `cmd/catacomb/daemon.go`, change the final line of `runDaemonWith` from:

```go
	return d.Serve(ctx, ln, grpcLn, token)
```

to:

```go
	err = d.Serve(ctx, ln, grpcLn, token)
	_ = os.Remove(discoveryPath)
	return err
```

(`os` is already imported in `daemon.go`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./cmd/catacomb/ -run TestRunDaemonRemovesDiscoveryOnShutdown -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/daemon.go cmd/catacomb/daemon_test.go
git commit -m "fix(daemon): remove discovery file on graceful shutdown"
```

---

## Final verification

- [ ] **Full suite + coverage gate**

Run: `go test ./... -cover`
Expected: all packages PASS, no coverage regression.

- [ ] **Code policy**

Run: `go test ./internal/codepolicy/...` (and/or the repo's lint target, e.g. `make lint`)
Expected: PASS — confirms no comments slipped into the new Go files.

- [ ] **Manual smoke (optional, real daemon)**

```bash
make build
./bin/catacomb up --no-open --no-demo
./bin/catacomb down --dry-run
./bin/catacomb down --all --yes
./bin/catacomb status   # expect: no daemon running
```

Expected: `down --dry-run` lists actions without changing anything; `down --all --yes` stops the daemon, removes hooks, deletes the DB and `~/.catacomb`; `status` reports no daemon.

---

## Self-Review (completed)

**Spec coverage:** bare stop (T2), `--uninstall` (T3), `--purge` DB+WAL+state (T5), `--all` (T6), `--db` targeting (T5), honest no-DB warning (T5), `--dry-run` no-side-effects (T6), `--yes`/non-TTY refusal (T4), `--json` report (T2/T6), `--force` SIGKILL (T1), order-of-operations capture of `db_path` before stop (T2 reads discovery before stopping; T5 receives `disc`), daemon discovery self-cleanup (T7). `--external` is explicitly Phase 2, not covered here, per the spec's phasing.

**Placeholder scan:** none — every code step shows complete, comment-free Go.

**Type consistency:** `downOpts`/`downReport` fields, `runDown`/`purgeLocal`/`confirmDestructive`/`stopDaemon` signatures, and the seam vars (`downSignal`, `downSleep`, `downRemove`, `downRemoveAll`, `downStat`, `downReadDiscovery`, `downIsTerminal`, `downConfirm`, `downHookTargets`) are referenced consistently across tasks.
