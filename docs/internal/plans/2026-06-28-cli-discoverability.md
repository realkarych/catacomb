# CLI Discoverability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make "observe every session" and "load past sessions" first-class on `catacomb up`, expose what the daemon observes via discovery + `status`, and make `--help`/README answer both questions without reading source.

**Architecture:** Add two flags to `up` (`--global`, `--history`). `--global` routes hook install to `~/.claude/settings.json`; `--history` starts the daemon tailing `~/.claude/projects`. The daemon records its scope (transcript dir, db path, payload flag) in the discovery file so `status` can show it and `up` can print an exact restart command when a daemon is already running. Help text and README document the workflows.

**Tech Stack:** Go (stdlib + cobra), `modernc.org/sqlite`, testify. Pure Go, no cgo.

## Global Constraints

- No comments in Go code — only `//go:build`, `//go:embed`, `//go:generate` directives (enforced by `internal/codepolicy`).
- 100% test coverage; the threshold never goes down. TDD: failing test first.
- `go test -race`; table-driven; `testify/require` for fatal, `testify/assert` otherwise; no `time.Sleep`.
- Mock through the caller's interface (override package `var` function values); never mock third-party SDKs.
- Errors wrapped `fmt.Errorf("pkg.Op: %w", err)`; sentinels via `errors.Is`.
- `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`).
- All work happens in the worktree on branch `feat/cli-discoverability`. No `--no-verify`. Squash PR.
- Run from the worktree root: `/Users/karych/src/catacomb/.claude/worktrees/cli-discoverability`.

---

## File Structure

- `daemon/discovery.go` — `Discovery` struct gains `TranscriptDir`, `DBPath`, `AllowPayloadAccess`.
- `cmd/catacomb/daemon.go` — `runDaemonWith` writes the new discovery fields.
- `cmd/catacomb/status.go` — prints an `observing` line.
- `cmd/catacomb/up.go` — `--global`, `--history`; `buildInstallHooks`/`buildStartDaemon` honor them; `runUp` prints restart guidance when a daemon is already running.
- `cmd/catacomb/root.go` and per-command files — `Long` + `Example` help text.
- `README.md` — Quickstart rewrite + two named workflows.

Existing test files are extended in place: `daemon/discovery_test.go`, `cmd/catacomb/daemon_test.go`, `cmd/catacomb/status_test.go`, `cmd/catacomb/up_test.go`.

---

## Task 1: Discovery carries daemon scope

**Files:**

- Modify: `daemon/discovery.go` (the `Discovery` struct)
- Test: `daemon/discovery_test.go`
- Modify: `cmd/catacomb/daemon.go:106-110` (the `disc := daemon.Discovery{...}` literal in `runDaemonWith`)
- Test: `cmd/catacomb/daemon_test.go`

**Interfaces:**

- Produces: `daemon.Discovery` gains exported fields `TranscriptDir string`, `DBPath string`, `AllowPayloadAccess bool`. Consumed by Tasks 2 and 5.

- [ ] **Step 1: Write the failing discovery round-trip test**

In `daemon/discovery_test.go`:

```go
func TestWriteReadDiscoveryScopeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	in := Discovery{
		Addr:               "127.0.0.1:5001",
		Token:              "tok",
		TranscriptDir:      "/home/u/.claude/projects",
		DBPath:             "/home/u/.catacomb/catacomb.db",
		AllowPayloadAccess: true,
	}
	require.NoError(t, WriteDiscovery(path, in))
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, "/home/u/.claude/projects", got.TranscriptDir)
	assert.Equal(t, "/home/u/.catacomb/catacomb.db", got.DBPath)
	assert.True(t, got.AllowPayloadAccess)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./daemon/ -run TestWriteReadDiscoveryScopeFields`
Expected: compile error — `unknown field 'TranscriptDir'`.

- [ ] **Step 3: Add the fields to the struct**

In `daemon/discovery.go`, replace the `Discovery` struct:

```go
type Discovery struct {
	Addr               string `json:"addr"`
	Token              string `json:"token"`
	GRPCAddr           string `json:"grpc_addr,omitempty"`
	Pid                int    `json:"pid,omitempty"`
	StartedAt          string `json:"started_at,omitempty"`
	TranscriptDir      string `json:"transcript_dir,omitempty"`
	DBPath             string `json:"db_path,omitempty"`
	AllowPayloadAccess bool   `json:"allow_payload_access,omitempty"`
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./daemon/ -run TestWriteReadDiscoveryScopeFields`
Expected: PASS.

- [ ] **Step 5: Write the failing daemon-writes-scope test**

In `cmd/catacomb/daemon_test.go` (mirror `TestRunDaemonDiscoveryHasPidAndStartedAt`):

```go
func TestRunDaemonDiscoveryHasScope(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(t.TempDir(), "g.db")
	transcripts := t.TempDir()
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken, db, discovery, 30*time.Minute, 4096, "", "", "", "", "", transcripts, nil, true)
	}()
	var d daemon.Discovery
	require.Eventually(t, func() bool {
		disc, err := daemon.ReadDiscovery(discovery)
		if err != nil || disc.TranscriptDir == "" {
			return false
		}
		d = disc
		return true
	}, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, transcripts, d.TranscriptDir)
	assert.Equal(t, db, d.DBPath)
	assert.True(t, d.AllowPayloadAccess)
	cancel()
	require.NoError(t, <-errc)
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./cmd/catacomb/ -run TestRunDaemonDiscoveryHasScope`
Expected: FAIL — `d.TranscriptDir` stays empty (the daemon never sets it), so `require.Eventually` times out.

- [ ] **Step 7: Set the fields in `runDaemonWith`**

In `cmd/catacomb/daemon.go`, replace the discovery literal (around line 106):

```go
	disc := daemon.Discovery{
		Addr:               ln.Addr().String(),
		Token:              token,
		GRPCAddr:           grpcLn.Addr().String(),
		TranscriptDir:      transcriptDir,
		DBPath:             dbPath,
		AllowPayloadAccess: allowPayloadAccess,
	}
```

(`disc.Pid` and `disc.StartedAt` keep their existing assignments on the following lines.)

- [ ] **Step 8: Run both tests to verify they pass**

Run: `go test ./daemon/ ./cmd/catacomb/ -run 'Discovery'`
Expected: PASS.

- [ ] **Step 9: Full package tests + commit**

Run: `go test -race ./daemon/ ./cmd/catacomb/`
Expected: ok.

```bash
git add daemon/discovery.go daemon/discovery_test.go cmd/catacomb/daemon.go cmd/catacomb/daemon_test.go
git commit -m "feat(daemon): record transcript dir, db path, payload flag in discovery"
```

---

## Task 2: `status` shows what the daemon observes

**Files:**

- Modify: `cmd/catacomb/status.go` (add line after `token age`, add `observingLabel`)
- Test: `cmd/catacomb/status_test.go`

**Interfaces:**

- Consumes: `daemon.Discovery.TranscriptDir` (Task 1).
- Produces: `observingLabel(dir string) string`.

- [ ] **Step 1: Write the failing tests**

In `cmd/catacomb/status_test.go`:

```go
func TestRunStatusShowsObserving(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:          strings.TrimPrefix(srv.URL, "http://"),
		Token:         "tok",
		TranscriptDir: "/home/u/.claude/projects",
	}))
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "observing")
	assert.Contains(t, out.String(), "/home/u/.claude/projects")
}

func TestRunStatusObservingHistoryOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "history off")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRunStatusShowsObserving|TestRunStatusObservingHistoryOff'`
Expected: FAIL — `observingLabel` undefined / output lacks `observing`.

- [ ] **Step 3: Implement**

In `cmd/catacomb/status.go`, add after the `token age` write (after line 74):

```go
	_, _ = fmt.Fprintf(w, "observing\t%s\n", observingLabel(disc.TranscriptDir))
```

And add the helper:

```go
func observingLabel(dir string) string {
	if dir == "" {
		return "history off (enable: catacomb up --history)"
	}
	return dir
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./cmd/catacomb/ -run 'TestRunStatus'`
Expected: PASS (existing status tests still green — the new line does not collide with their `Contains` assertions).

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/status.go cmd/catacomb/status_test.go
git commit -m "feat(status): show the transcript scope the daemon is observing"
```

---

## Task 3: `up --global` installs hooks for all projects

**Files:**

- Modify: `cmd/catacomb/up.go` (`buildInstallHooks`, `newUpCmd`)
- Test: `cmd/catacomb/up_test.go`

**Interfaces:**

- Consumes: `settingsPath(project, global bool) (string, error)` from `installhooks.go`.
- Produces: `buildInstallHooks(discPath string, global bool) func() error`.

- [ ] **Step 1: Write the failing tests**

In `cmd/catacomb/up_test.go`:

```go
func TestBuildInstallHooksGlobalPath(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	origExe := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origExe })

	require.NoError(t, buildInstallHooks(t.TempDir()+"/d.json", true)())
	_, err := os.Stat(filepath.Join(home, ".claude", "settings.json"))
	require.NoError(t, err)
}

func TestBuildInstallHooksProjectPath(t *testing.T) {
	t.Chdir(t.TempDir())
	origExe := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origExe })

	require.NoError(t, buildInstallHooks(t.TempDir()+"/d.json", false)())
	_, err := os.Stat(filepath.Join(".claude", "settings.json"))
	require.NoError(t, err)
}

func TestBuildInstallHooksGlobalHomeError(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = origHome })
	origExe := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origExe })

	err := buildInstallHooks(t.TempDir()+"/d.json", true)()
	require.Error(t, err)
}

func TestUpCmdHasGlobalFlag(t *testing.T) {
	assert.NotNil(t, newUpCmd().Flags().Lookup("global"))
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestBuildInstallHooks|TestUpCmdHasGlobalFlag'`
Expected: FAIL — `buildInstallHooks` takes one arg / `global` flag missing.

- [ ] **Step 3: Update `buildInstallHooks` and the existing caller signature**

Replace `buildInstallHooks` in `cmd/catacomb/up.go`:

```go
func buildInstallHooks(discPath string, global bool) func() error {
	return func() error {
		exe, err := osExecutable()
		if err != nil {
			return fmt.Errorf("up: resolve executable for hooks: %w", err)
		}
		path, err := settingsPath(false, global)
		if err != nil {
			return fmt.Errorf("up: resolve settings path: %w", err)
		}
		return installHooks(path, discPath, exe, false)
	}
}
```

- [ ] **Step 4: Update the existing one-arg test caller**

In `cmd/catacomb/up_test.go`, change `TestBuildInstallHooksOsExecutableError`'s call from `buildInstallHooks(t.TempDir() + "/d.json")` to `buildInstallHooks(t.TempDir()+"/d.json", false)`.

- [ ] **Step 5: Wire the `--global` flag in `newUpCmd`**

In `newUpCmd`, change the var line to `var noOpen, noDemo, global bool`, change the `installHooks` field to `buildInstallHooks(discPath, global)`, and register the flag after the existing flags:

```go
	cmd.Flags().BoolVar(&global, "global", false, "install hooks for all projects (~/.claude/settings.json) instead of the current directory")
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./cmd/catacomb/ -run 'TestBuildInstallHooks|TestUpCmd'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/catacomb/up.go cmd/catacomb/up_test.go
git commit -m "feat(up): --global installs hooks for every project"
```

---

## Task 4: `up --history` starts the daemon tailing past sessions

**Files:**

- Modify: `cmd/catacomb/up.go` (`buildStartDaemon`, `claudeProjectsDir`, `newUpCmd`)
- Test: `cmd/catacomb/up_test.go`

**Interfaces:**

- Produces: `claudeProjectsDir() (string, error)`; `buildStartDaemon(discPath, transcriptDir string) func() error`. The resolved transcript dir is consumed by Task 5 via `upDeps.projectsDir`.

- [ ] **Step 1: Write the failing tests**

In `cmd/catacomb/up_test.go`:

```go
func TestClaudeProjectsDir(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "/home/u", nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	dir, err := claudeProjectsDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/u", ".claude", "projects"), dir)
}

func TestClaudeProjectsDirHomeError(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = origHome })
	_, err := claudeProjectsDir()
	require.Error(t, err)
}

func TestBuildStartDaemonHistoryAppendsTranscriptDir(t *testing.T) {
	origExe := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origExe })
	origStart := startCmd
	startCmd = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startCmd = origStart })
	origExec := execCommand
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return origExec(name, args...)
	}
	t.Cleanup(func() { execCommand = origExec })

	require.NoError(t, buildStartDaemon(t.TempDir()+"/d.json", "/home/u/.claude/projects")())
	assert.Contains(t, gotArgs, "--transcript-dir")
	assert.Contains(t, gotArgs, "/home/u/.claude/projects")
}

func TestBuildStartDaemonNoHistoryOmitsTranscriptDir(t *testing.T) {
	origExe := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origExe })
	origStart := startCmd
	startCmd = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startCmd = origStart })
	origExec := execCommand
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return origExec(name, args...)
	}
	t.Cleanup(func() { execCommand = origExec })

	require.NoError(t, buildStartDaemon(t.TempDir()+"/d.json", "")())
	assert.NotContains(t, gotArgs, "--transcript-dir")
}

func TestUpCmdHasHistoryFlag(t *testing.T) {
	assert.NotNil(t, newUpCmd().Flags().Lookup("history"))
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'ClaudeProjectsDir|BuildStartDaemonHistory|BuildStartDaemonNoHistory|TestUpCmdHasHistoryFlag'`
Expected: FAIL — `claudeProjectsDir` undefined / `buildStartDaemon` takes one arg / `history` flag missing.

- [ ] **Step 3: Add `claudeProjectsDir` and update `buildStartDaemon`**

In `cmd/catacomb/up.go`, add:

```go
func claudeProjectsDir() (string, error) {
	home, err := osUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("up: resolve home: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}
```

Replace `buildStartDaemon` so it takes a transcript dir and appends the flag when non-empty:

```go
func buildStartDaemon(discPath, transcriptDir string) func() error {
	return func() error {
		exe, err := osExecutable()
		if err != nil {
			return fmt.Errorf("up: resolve executable: %w", err)
		}
		logPath := discPath + ".log"
		if mkErr := os.MkdirAll(filepath.Dir(logPath), 0o700); mkErr != nil {
			return fmt.Errorf("up: create run dir: %w", mkErr)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("up: open daemon log: %w", err)
		}
		args := []string{"daemon"}
		if transcriptDir != "" {
			args = append(args, "--transcript-dir", transcriptDir)
		}
		c := execCommand(exe, args...)
		c.Stdout = f
		c.Stderr = f
		if err := startCmd(c); err != nil {
			_ = f.Close()
			return fmt.Errorf("up: start daemon: %w", err)
		}
		_ = f.Close()
		return nil
	}
}
```

- [ ] **Step 4: Update existing `buildStartDaemon` one-arg callers in tests**

In `cmd/catacomb/up_test.go`, the six `TestBuildStartDaemon*` tests call `buildStartDaemon(<path>)`. Add `, ""` to each call: `buildStartDaemon(t.TempDir()+"/d.json", "")` (and the `filepath.Join(blocker, "run", "d.json")` / `discPath` / `base+"/run/d.json"` variants likewise).

- [ ] **Step 5: Wire the `--history` flag in `newUpCmd`**

Change the var line to `var noOpen, noDemo, global, history bool`. Replace the `RunE` body's deps construction so it resolves the transcript dir up front and passes it to `buildStartDaemon`:

```go
		RunE: func(cmd *cobra.Command, _ []string) error {
			discPath := daemon.DiscoveryPath()
			var transcriptDir string
			if history {
				projects, err := claudeProjectsDir()
				if err != nil {
					return err
				}
				transcriptDir = projects
			}
			deps := upDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: discPath,
				startDaemon:   buildStartDaemon(discPath, transcriptDir),
				installHooks:  buildInstallHooks(discPath, global),
				pollHealthz:   prodPollHealthz,
				sessionCount:  prodSessionCount,
				openBrowser:   openBrowser,
				replayDemo:    prodReplayDemo,
				after:         time.After,
				waitSeconds:   5,
				noOpen:        noOpen,
				noDemo:        noDemo,
			}
			return runUp(cmd.Context(), cmd.OutOrStdout(), deps)
		},
```

Register the flag:

```go
	cmd.Flags().BoolVar(&history, "history", false, "tail ~/.claude/projects so past sessions appear in the UI")
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./cmd/catacomb/ -run 'ClaudeProjectsDir|BuildStartDaemon|TestUpCmd'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/catacomb/up.go cmd/catacomb/up_test.go
git commit -m "feat(up): --history tails ~/.claude/projects when starting the daemon"
```

---

## Task 5: `up --history` informs (never restarts) a running daemon

**Files:**

- Modify: `cmd/catacomb/up.go` (`upDeps`, `runUp` else-branch, `newUpCmd`, new helpers, `strings` import)
- Test: `cmd/catacomb/up_test.go`

**Interfaces:**

- Consumes: `daemon.Discovery.{Pid,TranscriptDir,DBPath,AllowPayloadAccess}` (Task 1); `upDeps.projectsDir` set from the resolved transcript dir (Task 4).
- Produces: `reportHistoryScope(out io.Writer, disc daemon.Discovery, projectsDir string) error`; `restartCommand(disc daemon.Discovery, projectsDir string) string`.

- [ ] **Step 1: Write the failing tests**

In `cmd/catacomb/up_test.go`:

```go
func TestRestartCommandMinimal(t *testing.T) {
	got := restartCommand(daemon.Discovery{}, "/p")
	assert.Equal(t, "catacomb daemon --transcript-dir /p", got)
}

func TestRunUpHistoryAlreadyAllScope(t *testing.T) {
	projects := "/home/u/.claude/projects"
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok", TranscriptDir: projects}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.history = true
	deps.projectsDir = projects
	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "already observing all history")
}

func TestRunUpHistoryRestartHint(t *testing.T) {
	disc := daemon.Discovery{
		Addr: "127.0.0.1:12345", Token: "tok", Pid: 4242,
		TranscriptDir:      "/home/u/.catacomb/tail-scope",
		DBPath:             "/home/u/.catacomb/catacomb.db",
		AllowPayloadAccess: true,
	}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.history = true
	deps.projectsDir = "/home/u/.claude/projects"
	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	s := out.String()
	assert.Contains(t, s, "kill 4242")
	assert.Contains(t, s, "--transcript-dir /home/u/.claude/projects")
	assert.Contains(t, s, "--db /home/u/.catacomb/catacomb.db")
	assert.Contains(t, s, "--allow-payload-access")
}

func TestRunUpHistoryAlreadyAllScopeWriteError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok", TranscriptDir: "/p"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.history = true
	deps.projectsDir = "/p"
	require.Error(t, runUp(context.Background(), failWriter{}, deps))
}

func TestRunUpHistoryRestartHintWriteError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok", TranscriptDir: "/other"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.history = true
	deps.projectsDir = "/p"
	require.Error(t, runUp(context.Background(), failWriter{}, deps))
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRestartCommand|TestRunUpHistory'`
Expected: FAIL — `restartCommand`/`reportHistoryScope` undefined, `upDeps` has no `history`/`projectsDir`.

- [ ] **Step 3: Add deps fields, the else-branch reader, and helpers**

In `cmd/catacomb/up.go`, add `"strings"` to the imports. Add two fields to `upDeps`:

```go
	history     bool
	projectsDir string
```

In `runUp`, replace the `else` branch (the daemon-already-running path) with:

```go
	} else {
		if pollErr := deps.pollHealthz(ctx, disc.Addr); pollErr != nil {
			return pollErr
		}
		if deps.history {
			if err := reportHistoryScope(out, disc, deps.projectsDir); err != nil {
				return err
			}
		}
	}
```

Add the helpers:

```go
func reportHistoryScope(out io.Writer, disc daemon.Discovery, projectsDir string) error {
	if projectsDir != "" && disc.TranscriptDir == projectsDir {
		_, err := fmt.Fprintf(out, "daemon already observing all history (%s)\n", projectsDir)
		return err
	}
	_, err := fmt.Fprintf(out, "daemon already running (pid %d); --history applies only when starting a fresh daemon.\nto tail all history, restart it:\n\n  %s\n\n", disc.Pid, restartCommand(disc, projectsDir))
	return err
}

func restartCommand(disc daemon.Discovery, projectsDir string) string {
	var b strings.Builder
	if disc.Pid != 0 {
		_, _ = fmt.Fprintf(&b, "kill %d && ", disc.Pid)
	}
	_, _ = fmt.Fprintf(&b, "catacomb daemon --transcript-dir %s", projectsDir)
	if disc.DBPath != "" {
		_, _ = fmt.Fprintf(&b, " --db %s", disc.DBPath)
	}
	if disc.AllowPayloadAccess {
		b.WriteString(" --allow-payload-access")
	}
	return b.String()
}
```

- [ ] **Step 4: Set the deps in `newUpCmd`**

In the `RunE` deps literal (from Task 4), add the two fields:

```go
				history:     history,
				projectsDir: transcriptDir,
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./cmd/catacomb/ -run 'TestRestartCommand|TestRunUpHistory'`
Expected: PASS.

- [ ] **Step 6: Run the whole package + commit**

Run: `go test -race ./cmd/catacomb/`
Expected: ok.

```bash
git add cmd/catacomb/up.go cmd/catacomb/up_test.go
git commit -m "feat(up): --history prints an exact restart command when a daemon is already running"
```

---

## Task 6: Help text answers the recipes

**Files:**

- Modify: `cmd/catacomb/root.go`, `cmd/catacomb/up.go`, `cmd/catacomb/daemon.go`, `cmd/catacomb/installhooks.go`, `cmd/catacomb/replay.go`, `cmd/catacomb/observe.go`, `cmd/catacomb/demo.go` (add `Long` / `Example`; no logic change)
- Test: `cmd/catacomb/root_test.go` (or `up_test.go` if no `root_test.go` exists)

**Interfaces:** none (cobra metadata only).

- [ ] **Step 1: Write the failing assertions**

Add to `cmd/catacomb/up_test.go`:

```go
func TestRootHelpHasRecipes(t *testing.T) {
	assert.Contains(t, newRootCmd().Long, "Common recipes")
	assert.Contains(t, newRootCmd().Long, "--global")
	assert.Contains(t, newRootCmd().Long, "--history")
}

func TestUpHelpDocumentsScope(t *testing.T) {
	cmd := newUpCmd()
	assert.Contains(t, cmd.Long, "--global")
	assert.Contains(t, cmd.Long, "--history")
	assert.NotEmpty(t, cmd.Example)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRootHelpHasRecipes|TestUpHelpDocumentsScope'`
Expected: FAIL — `Long`/`Example` empty.

- [ ] **Step 3: Set root `Long`**

In `cmd/catacomb/root.go`, add a `Long` to the root command literal:

```go
		Long: `Catacomb builds a real-time execution graph of your Claude Code sessions —
prompts, turns, tool calls, MCP calls, and subagents — and serves it in a
web UI and a terminal observer.

Common recipes:
  Observe every session (all projects):
      catacomb up --global

  Load past sessions into the UI:
      catacomb up --history

  Read conversation content in the UI (off by default):
      catacomb daemon --allow-payload-access

Run 'catacomb <command> --help' for details on any command.`,
```

- [ ] **Step 4: Set `up` `Long` + `Example`**

In `newUpCmd`, add to the command literal:

```go
		Long: `Start the daemon if it is not already running, install the Claude Code
hooks, print the bearer URL, and open the web UI.

By default up observes only sessions started in the current directory, and
only live activity. Use --global to install hooks for every project, and
--history to load sessions you have already run.

If a daemon is already running, --history does not restart it; up prints the
exact command to restart it with history enabled.`,
		Example: `  # observe the current project, live only
  catacomb up

  # observe every project (all live sessions)
  catacomb up --global

  # also load past sessions from ~/.claude/projects
  catacomb up --global --history`,
```

- [ ] **Step 5: Set `Long`/`Example` on the remaining commands**

`newDaemonCmd` (`cmd/catacomb/daemon.go`):

```go
		Long: `Run the catacomb daemon: it receives hook events, builds the live graph,
persists it to SQLite, and serves the web UI and gRPC feed.

Pass --transcript-dir ~/.claude/projects to also tail recorded transcripts,
which backfills past sessions and follows live ones. Pass --allow-payload-access
to enable the token-gated content endpoint (off by default).`,
		Example: `  # live only
  catacomb daemon

  # backfill and tail every past + live session
  catacomb daemon --transcript-dir ~/.claude/projects --allow-payload-access`,
```

`newInstallHooksCmd` (`cmd/catacomb/installhooks.go`):

```go
		Long: `Wire the catacomb hook forwarder into Claude Code settings.json.

--project (default) writes ./.claude/settings.json and observes only this
directory. --global writes ~/.claude/settings.json and observes every project.`,
		Example: `  # current project only
  catacomb install-hooks

  # every project
  catacomb install-hooks --global`,
```

`newReplayCmd` (`cmd/catacomb/replay.go`):

```go
		Long: `Build a graph from a single recorded Claude Code transcript and persist it
to a standalone SQLite database. This does not feed the running daemon or the
web UI; to load history into the UI, use catacomb up --history.`,
		Example: `  catacomb replay ~/.claude/projects/<project>/<session>.jsonl`,
```

`newObserveCmd` (`cmd/catacomb/observe.go`):

```go
		Long: `Interactive terminal observer over the live daemon feed: sessions, the node
tree, and per-node detail. Pass a session hash to open straight into it.`,
		Example: `  catacomb observe`,
```

`newDemoCmd` (`cmd/catacomb/demo.go`):

```go
		Long: `Ingest a bundled synthetic transcript into the running daemon so you can see
a populated graph without a live session.`,
		Example: `  catacomb demo`,
```

(If any of these command files build the `cobra.Command` with a returned literal, add the `Long`/`Example` keys to that literal; do not change `Use`, `Short`, `Args`, or `RunE`.)

- [ ] **Step 6: Run to verify pass + format**

Run: `go test ./cmd/catacomb/ -run 'Help|TestUpCmd|TestUpHelp'`
Expected: PASS.
Run: `gofumpt -l cmd/catacomb/ && goimports -l -local github.com/realkarych/catacomb cmd/catacomb/`
Expected: no files listed.

- [ ] **Step 7: Commit**

```bash
git add cmd/catacomb/root.go cmd/catacomb/up.go cmd/catacomb/daemon.go cmd/catacomb/installhooks.go cmd/catacomb/replay.go cmd/catacomb/observe.go cmd/catacomb/demo.go cmd/catacomb/up_test.go
git commit -m "docs(cli): add Long help + examples and root recipes"
```

---

## Task 7: README documents the workflows

**Files:**

- Modify: `README.md`

**Interfaces:** none.

- [ ] **Step 1: Rewrite the Quickstart section**

Replace the current Quickstart block (the `catacomb up` paragraph) so it stops implying `up` covers everything, and add the two named workflows. New content:

````markdown
## Quickstart

```sh
catacomb up
```

`catacomb up` starts the daemon if it is not already running, installs the
Claude Code hooks for the **current directory**, prints the bearer URL, and
opens the web UI. It observes **live** sessions started under that directory.

### Observe every session

To observe sessions in **every** project (not just the current directory),
install the hooks globally:

```sh
catacomb up --global
```

This writes `~/.claude/settings.json`, so any Claude Code session — from any
directory — is observed.

### Load past sessions

`up` and the hooks only see sessions that run *after* they are installed. To
backfill the sessions you have **already** run, start the daemon tailing the
Claude Code transcript directory:

```sh
catacomb up --history          # tails ~/.claude/projects when starting the daemon
```

On startup the daemon reads every existing transcript (sessions and their
subagents) and then follows live ones. Tail cursors are persisted, so
re-running the daemon does not duplicate history. If a daemon is already
running, `up --history` prints the exact command to restart it with history
enabled rather than restarting it for you.

Combine both for full coverage:

```sh
catacomb up --global --history
```

Other commands:

```sh
catacomb status           # daemon addr, pid, uptime, what it's observing, counts
catacomb observe [hash]   # interactive terminal observer
catacomb ui               # print the bearer URL and (re-)open the browser
catacomb demo             # ingest the bundled demo transcript into a running daemon
catacomb version          # print the version
```

To read conversation content in the UI, start the daemon with
`--allow-payload-access` (off by default — see [Privacy](#privacy)).

By default the daemon's database is `catacomb.db` in the directory you launch
it from, and its discovery file lives under `~/.catacomb/run/`.
````

- [ ] **Step 2: Lint the README**

Run: `npx --no-install markdownlint-cli README.md` (or `markdownlint README.md`).
Expected: no output (clean). If `markdownlint` is not installed locally, rely on CI.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): document observe-every-session and load-past-sessions"
```

---

## Final verification (after all tasks)

- [ ] Run the full gate from the worktree root:

Run: `make cover` (race tests + 100% coverage) and `make lint` and `go test ./internal/codepolicy/`.
Expected: coverage gate passes at 100%; lint clean; no comment-policy violations.

- [ ] Open the PR (squash) when the user asks.

---

## Self-review notes

- **Spec coverage:** §3.1 → Task 3; §3.2 start path → Task 4, inform path → Task 5; §3.3 → Task 1; §3.4 → Task 2; §4 help → Task 6; §5 README → Task 7; §6 testing folded into every task.
- **Signature changes that break existing callers (must be updated in the same task):** `buildInstallHooks` gains `global bool` (Task 3, Step 4 updates `TestBuildInstallHooksOsExecutableError`); `buildStartDaemon` gains `transcriptDir string` (Task 4, Step 4 updates the six `TestBuildStartDaemon*` callers).
- **Unused-field guard:** `upDeps.history`/`projectsDir` are added and read in the same task (Task 5), so `staticcheck` U1000 will not fire.
- **Type consistency:** `claudeProjectsDir`, `buildStartDaemon(discPath, transcriptDir)`, `buildInstallHooks(discPath, global)`, `reportHistoryScope`, `restartCommand`, `observingLabel` names are used identically across tasks.
