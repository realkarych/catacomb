package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func instantAfter(_ time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func fakeDepsWithDisc(t *testing.T, disc daemon.Discovery) upDeps {
	t.Helper()
	discPath := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))
	ch := make(chan time.Time)
	t.Cleanup(func() { close(ch) })
	deps := upDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		startDaemon:   func() error { return nil },
		installHooks:  func() error { return nil },
		pollHealthz:   func(_ context.Context, _ string) error { return nil },
		sessionCount:  func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:   func(_ string) error { return nil },
		replayDemo:    func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:         func(_ time.Duration) <-chan time.Time { return ch },
		waitSeconds:   0,
		noOpen:        false,
		noDemo:        false,
	}
	return deps
}

func TestRunUpDaemonAlreadyRunning(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)

	startCalled := false
	deps.startDaemon = func() error {
		startCalled = true
		return nil
	}
	deps.noOpen = true

	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	assert.False(t, startCalled, "startDaemon must not be called when daemon is already running")
	assert.Contains(t, out.String(), "127.0.0.1:12345")
}

func TestRunUpDaemonNotRunningStartsIt(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	realDisc := daemon.Discovery{Addr: "127.0.0.1:22222", Token: "tok2"}

	startCalled := false
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			if !startCalled {
				return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
			}
			return realDisc, nil
		},
		discoveryPath: discPath,
		startDaemon: func() error {
			startCalled = true
			return nil
		},
		installHooks: func() error { return nil },
		pollHealthz:  func(_ context.Context, _ string) error { return nil },
		sessionCount: func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:  func(_ string) error { return nil },
		replayDemo:   func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:        instantAfter,
		waitSeconds:  1,
		noOpen:       true,
	}

	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	assert.True(t, startCalled)
	assert.Contains(t, out.String(), "127.0.0.1:22222")
}

func TestRunUpStartDaemonError(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	startErr := errors.New("exec failed")
	ch := make(chan time.Time)
	t.Cleanup(func() { close(ch) })
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
		},
		discoveryPath: discPath,
		startDaemon:   func() error { return startErr },
		installHooks:  func() error { return nil },
		pollHealthz:   func(_ context.Context, _ string) error { return nil },
		sessionCount:  func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:   func(_ string) error { return nil },
		replayDemo:    func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:         func(_ time.Duration) <-chan time.Time { return ch },
		waitSeconds:   0,
	}
	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, startErr))
}

func TestRunUpHealthzFails(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.pollHealthz = func(_ context.Context, _ string) error {
		return ErrDaemonUnreachable
	}

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonUnreachable))
}

func TestRunUpHooksFail(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	hookErr := errors.New("hooks broken")
	deps.installHooks = func() error { return hookErr }

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, hookErr))
}

func TestRunUpNoOpen(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	openCalled := false
	deps.openBrowser = func(_ string) error {
		openCalled = true
		return nil
	}

	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	assert.False(t, openCalled)
	assert.Contains(t, out.String(), "127.0.0.1:12345")
}

func TestRunUpOpenBrowserError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	openErr := errors.New("browser failed")
	deps.openBrowser = func(_ string) error { return openErr }
	deps.noDemo = true

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, openErr))
}

func TestRunUpDemoFallbackFires(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) { return 0, nil }
	deps.after = instantAfter

	replayCalled := false
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error {
		replayCalled = true
		return nil
	}

	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	assert.True(t, replayCalled, "replayDemo should be called when no sessions after timeout")
	assert.Contains(t, out.String(), "demo")
}

func TestRunUpDemoFallbackSkippedWhenSessions(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) { return 2, nil }
	deps.after = instantAfter

	replayCalled := false
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error {
		replayCalled = true
		return nil
	}

	require.NoError(t, runUp(context.Background(), io.Discard, deps))
	assert.False(t, replayCalled)
}

func TestRunUpNoDemoFlag(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.noDemo = true
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) { return 0, nil }
	deps.after = instantAfter

	replayCalled := false
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error {
		replayCalled = true
		return nil
	}

	require.NoError(t, runUp(context.Background(), io.Discard, deps))
	assert.False(t, replayCalled)
}

func TestRunUpReplayDemoError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) { return 0, nil }
	deps.after = instantAfter
	replayErr := errors.New("replay broken")
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error { return replayErr }

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, replayErr))
}

func TestRunUpSessionCountError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) {
		return 0, errors.New("count failed")
	}
	deps.after = instantAfter

	replayCalled := false
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error {
		replayCalled = true
		return nil
	}

	require.NoError(t, runUp(context.Background(), io.Discard, deps))
	assert.True(t, replayCalled, "on count error treat as 0 sessions and replay demo")
}

func TestRunUpDiscoveryNonNotExistError(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	readErr := errors.New("read error")
	ch := make(chan time.Time)
	t.Cleanup(func() { close(ch) })
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) { return daemon.Discovery{}, readErr },
		discoveryPath: discPath,
		startDaemon:   func() error { return nil },
		installHooks:  func() error { return nil },
		pollHealthz:   func(_ context.Context, _ string) error { return nil },
		sessionCount:  func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:   func(_ string) error { return nil },
		replayDemo:    func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:         func(_ time.Duration) <-chan time.Time { return ch },
	}
	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, readErr))
}

func TestRunUpHealthzFailsAfterStart(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	startCalled := false
	ch := make(chan time.Time)
	t.Cleanup(func() { close(ch) })
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			if !startCalled {
				return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
			}
			return daemon.Discovery{Addr: "127.0.0.1:44444", Token: "tok"}, nil
		},
		discoveryPath: discPath,
		startDaemon: func() error {
			startCalled = true
			return nil
		},
		installHooks: func() error { return nil },
		pollHealthz: func(_ context.Context, _ string) error {
			return ErrDaemonUnreachable
		},
		sessionCount: func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:  func(_ string) error { return nil },
		replayDemo:   func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:        func(_ time.Duration) <-chan time.Time { return ch },
		waitSeconds:  0,
		noOpen:       true,
	}

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonUnreachable))
}

func TestRunUpPollHealthzCalledAfterStart(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	realDisc := daemon.Discovery{Addr: "127.0.0.1:33333", Token: "tok3"}

	startCalled := false
	pollCalled := false
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			if !startCalled {
				return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
			}
			return realDisc, nil
		},
		discoveryPath: discPath,
		startDaemon: func() error {
			startCalled = true
			return nil
		},
		installHooks: func() error { return nil },
		pollHealthz: func(_ context.Context, _ string) error {
			pollCalled = true
			return nil
		},
		sessionCount: func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:  func(_ string) error { return nil },
		replayDemo:   func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:        instantAfter,
		waitSeconds:  1,
		noOpen:       true,
	}

	require.NoError(t, runUp(context.Background(), io.Discard, deps))
	assert.True(t, pollCalled)
}

func TestRunUpReadDiscoveryAfterStartFails(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	startCalled := false
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			if !startCalled {
				return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
			}
			return daemon.Discovery{}, errors.New("post-start read failed")
		},
		discoveryPath: discPath,
		startDaemon: func() error {
			startCalled = true
			return nil
		},
		installHooks: func() error { return nil },
		pollHealthz:  func(_ context.Context, _ string) error { return nil },
		sessionCount: func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:  func(_ string) error { return nil },
		replayDemo:   func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:        instantAfter,
		waitSeconds:  2,
		noOpen:       true,
	}

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonUnreachable))
}

func TestRunUpURLWriteError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.noDemo = true

	err := runUp(context.Background(), failWriter{}, deps)
	require.Error(t, err)
}

func TestRunUpDemoOutputWriteError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) { return 0, nil }
	deps.after = instantAfter
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error { return nil }
	deps.noDemo = false

	var writes int
	deps.openBrowser = func(_ string) error { return nil }

	err := runUp(context.Background(), &countingWriter{maxWrites: 1, writes: &writes}, deps)
	require.Error(t, err)
}

type countingWriter struct {
	maxWrites int
	writes    *int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	*w.writes++
	if *w.writes > w.maxWrites {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func TestRunUpDemoFallbackAfterFirstSessionCountHasSessions(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.noOpen = true

	callCount := 0
	deps.sessionCount = func(_ context.Context, _ daemon.Discovery) (int, error) {
		callCount++
		if callCount == 1 {
			return 0, nil
		}
		return 3, nil
	}
	deps.after = instantAfter

	replayCalled := false
	deps.replayDemo = func(_ context.Context, _ daemon.Discovery) error {
		replayCalled = true
		return nil
	}

	require.NoError(t, runUp(context.Background(), io.Discard, deps))
	assert.False(t, replayCalled, "if sessions appear after timer, no demo needed")
}

func TestRunUpReadinessLoopConvergesAfterK(t *testing.T) {
	discPath := t.TempDir() + "/d.json"
	realDisc := daemon.Discovery{Addr: "127.0.0.1:55555", Token: "tok5"}

	startCalled := false
	readCallCount := 0
	pollCallCount := 0

	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			if !startCalled {
				return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
			}
			readCallCount++
			if readCallCount < 3 {
				return daemon.Discovery{}, errors.New("not ready yet")
			}
			return realDisc, nil
		},
		discoveryPath: discPath,
		startDaemon: func() error {
			startCalled = true
			return nil
		},
		installHooks: func() error { return nil },
		pollHealthz: func(_ context.Context, _ string) error {
			pollCallCount++
			return nil
		},
		sessionCount: func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:  func(_ string) error { return nil },
		replayDemo:   func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:        instantAfter,
		waitSeconds:  5,
		noOpen:       true,
	}

	var out strings.Builder
	require.NoError(t, runUp(context.Background(), &out, deps))
	assert.True(t, startCalled)
	assert.Equal(t, 3, readCallCount)
	assert.Equal(t, 1, pollCallCount)
	assert.Contains(t, out.String(), "127.0.0.1:55555")
}

func TestRunUpReadinessLoopNeverReady(t *testing.T) {
	discPath := t.TempDir() + "/d.json"

	startCalled := false
	deps := upDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) {
			if !startCalled {
				return daemon.Discovery{}, fmt.Errorf("x: %w", os.ErrNotExist)
			}
			return daemon.Discovery{}, errors.New("never ready")
		},
		discoveryPath: discPath,
		startDaemon: func() error {
			startCalled = true
			return nil
		},
		installHooks: func() error { return nil },
		pollHealthz:  func(_ context.Context, _ string) error { return nil },
		sessionCount: func(_ context.Context, _ daemon.Discovery) (int, error) { return 1, nil },
		openBrowser:  func(_ string) error { return nil },
		replayDemo:   func(_ context.Context, _ daemon.Discovery) error { return nil },
		after:        instantAfter,
		waitSeconds:  3,
	}

	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonUnreachable))
}

func TestBuildStartDaemonOsExecutableError(t *testing.T) {
	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("no exe") }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	fn := buildStartDaemon(t.TempDir()+"/d.json", "")
	err := fn()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve executable")
}

func TestBuildStartDaemonCreateRunDirError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	fn := buildStartDaemon(filepath.Join(blocker, "run", "d.json"), "")
	err := fn()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create run dir")
}

func TestBuildStartDaemonLogOpenError(t *testing.T) {
	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	dir := t.TempDir()
	discPath := dir + "/d.json"
	require.NoError(t, os.MkdirAll(discPath+".log", 0o700))

	fn := buildStartDaemon(discPath, "")
	err := fn()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open daemon log")
}

func TestBuildStartDaemonCreatesRunDir(t *testing.T) {
	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	origStartCmd := startCmd
	startCmd = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startCmd = origStartCmd })

	base := t.TempDir()
	discPath := base + "/run/d.json"
	runDir := base + "/run"

	require.NoDirExists(t, runDir)
	fn := buildStartDaemon(discPath, "")
	require.NoError(t, fn())
	assert.DirExists(t, runDir)
}

func TestBuildStartDaemonStartError(t *testing.T) {
	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	origStartCmd := startCmd
	startCmd = func(_ *exec.Cmd) error { return errors.New("start failed") }
	t.Cleanup(func() { startCmd = origStartCmd })

	fn := buildStartDaemon(t.TempDir()+"/d.json", "")
	err := fn()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start daemon")
}

func TestBuildStartDaemonSuccess(t *testing.T) {
	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	origStartCmd := startCmd
	startCmd = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startCmd = origStartCmd })

	fn := buildStartDaemon(t.TempDir()+"/d.json", "")
	err := fn()
	require.NoError(t, err)
}

func TestBuildInstallHooksOsExecutableError(t *testing.T) {
	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("no exe") }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	fn := buildInstallHooks(t.TempDir()+"/d.json", false)
	err := fn()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve executable for hooks")
}

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

func TestProdSessionCountCallsFetchSessionCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"session":"s1","node_count":5}]`))
	}))
	t.Cleanup(srv.Close)

	origClient := statusHTTPClient
	statusHTTPClient = srv.Client()
	t.Cleanup(func() { statusHTTPClient = origClient })

	disc := daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}
	n, err := prodSessionCount(context.Background(), disc)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestProdReplaydemoCallsRunDemo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	origClient := demoHTTPClient
	demoHTTPClient = srv.Client()
	t.Cleanup(func() { demoHTTPClient = origClient })

	disc := daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}
	err := prodReplayDemo(context.Background(), disc)
	require.NoError(t, err)
}

func TestProdPollHealthzOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	origClient := statusHTTPClient
	statusHTTPClient = srv.Client()
	t.Cleanup(func() { statusHTTPClient = origClient })

	addr := strings.TrimPrefix(srv.URL, "http://")
	err := prodPollHealthz(context.Background(), addr)
	require.NoError(t, err)
}

func TestUpPollHealthzBadURL(t *testing.T) {
	err := upPollHealthz(context.Background(), "host with invalid\x00addr:99")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonUnreachable))
}

func TestUpPollHealthzConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	err := upPollHealthz(context.Background(), addr)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonUnreachable))
}

func TestUpCmdHasFlags(t *testing.T) {
	cmd := newUpCmd()
	assert.NotNil(t, cmd.Flags().Lookup("no-open"))
	assert.NotNil(t, cmd.Flags().Lookup("no-demo"))
}

func TestUpCmdRegisteredInRoot(t *testing.T) {
	root := newRootCmd()
	var found bool
	for _, sub := range root.Commands() {
		if sub.Use == "up" {
			found = true
		}
	}
	assert.True(t, found, "up subcommand must be registered")
}

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

func TestUpCmdRunEWithHistory(t *testing.T) {
	t.Chdir(t.TempDir())
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	discPath := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))
	t.Setenv("CATACOMB_DISCOVERY", discPath)

	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() { osUserHomeDir = origHome })

	origOpenBrowser := openBrowser
	openBrowser = func(_ string) error { return nil }
	t.Cleanup(func() { openBrowser = origOpenBrowser })

	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "/usr/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	origPollHealthz := upPollHealthz
	upPollHealthz = func(_ context.Context, _ string) error { return nil }
	t.Cleanup(func() { upPollHealthz = origPollHealthz })

	cmd := newUpCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.ParseFlags([]string{"--no-open", "--no-demo", "--history"}))
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "127.0.0.1:12345")
}

func TestUpCmdRunEHistoryHomeError(t *testing.T) {
	t.Chdir(t.TempDir())
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = origHome })

	cmd := newUpCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.ParseFlags([]string{"--history"}))
	err := cmd.Execute()
	require.Error(t, err)
}

func TestRestartCommandMinimal(t *testing.T) {
	got := restartCommand(daemon.Discovery{}, "/p")
	assert.Equal(t, "catacomb daemon --transcript-dir \"/p\"", got)
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
	assert.Contains(t, s, "--transcript-dir \"/home/u/.claude/projects\"")
	assert.Contains(t, s, "--db \"/home/u/.catacomb/catacomb.db\"")
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

func TestUpCmdRunE(t *testing.T) {
	t.Chdir(t.TempDir())
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	discPath := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))
	t.Setenv("CATACOMB_DISCOVERY", discPath)

	origOpenBrowser := openBrowser
	openBrowser = func(_ string) error { return nil }
	t.Cleanup(func() { openBrowser = origOpenBrowser })

	origStartCmd := startCmd
	startCmd = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startCmd = origStartCmd })

	origOsExecutable := osExecutable
	osExecutable = func() (string, error) { return "/usr/bin/catacomb", nil }
	t.Cleanup(func() { osExecutable = origOsExecutable })

	origPollHealthz := upPollHealthz
	upPollHealthz = func(_ context.Context, _ string) error { return nil }
	t.Cleanup(func() { upPollHealthz = origPollHealthz })

	cmd := newUpCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.ParseFlags([]string{"--no-open", "--no-demo"}))
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "127.0.0.1:12345")
}
