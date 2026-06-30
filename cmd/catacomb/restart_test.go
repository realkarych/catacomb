package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func fakeRestartDeps(discPath string) restartDeps {
	return restartDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		stopFn:        func(_ int, _ bool) (bool, error) { return true, nil },
		removeDisc:    func(_ string) error { return nil },
		startDaemon:   func(_ string) error { return nil },
		pollHealthz:   func(_ context.Context, _ string) error { return nil },
		after:         func(_ time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- time.Now(); return ch },
		waitSeconds:   1,
	}
}

func TestRunRestartNoDaemonStartsNew(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	started := false
	deps.startDaemon = func(_ string) error { started = true; return nil }
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.True(t, started)
}

func TestRunRestartStopsAndRestarts(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))

	stopped, started := false, false
	deps := fakeRestartDeps(disc)
	deps.stopFn = func(_ int, _ bool) (bool, error) { stopped = true; return true, nil }
	deps.startDaemon = func(_ string) error { started = true; return nil }

	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.True(t, stopped)
	assert.True(t, started)
}

func TestRunRestartForce(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))

	var gotForce bool
	deps := fakeRestartDeps(disc)
	deps.force = true
	deps.stopFn = func(_ int, force bool) (bool, error) { gotForce = force; return true, nil }
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.True(t, gotForce)
}

func TestRunRestartStopError(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))
	deps := fakeRestartDeps(disc)
	deps.stopFn = func(_ int, _ bool) (bool, error) { return false, ErrDaemonStop }
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestRunRestartStartError(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	deps.startDaemon = func(_ string) error { return errors.New("exec failed") }
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRunRestartJSONOutput(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	deps := fakeRestartDeps(disc)
	deps.asJSON = true
	deps.readDiscovery = func(path string) (daemon.Discovery, error) {
		d, err := daemon.ReadDiscovery(path)
		if err != nil {
			return daemon.Discovery{Addr: "127.0.0.1:9999", Token: "new"}, nil
		}
		return d, nil
	}
	_ = daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:9999", Token: "new", Pid: 1})
	var out bytes.Buffer
	require.NoError(t, runRestart(context.Background(), &out, deps))
	var rep restartReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.True(t, rep.Started)
}

func TestRunRestartTextOutput(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	var out bytes.Buffer
	require.NoError(t, runRestart(context.Background(), &out, deps))
	assert.Contains(t, strings.ToLower(out.String()), "restart")
}

func TestRunRestartDiscoveryReadError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
	deps := fakeRestartDeps(filepath.Join(dir, "afile"))
	deps.readDiscovery = func(_ string) (daemon.Discovery, error) {
		return daemon.Discovery{}, errors.New("bad json")
	}
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRestartCmdRegistered(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Use == "restart" {
			found = true
		}
	}
	assert.True(t, found, "restart subcommand must be registered")
}

func TestRestartCmdForceFlag(t *testing.T) {
	cmd := newRestartCmd()
	f := cmd.Flags().Lookup("force")
	require.NotNil(t, f)
}

func TestRestartCmdJSONFlag(t *testing.T) {
	cmd := newRestartCmd()
	f := cmd.Flags().Lookup("json")
	require.NotNil(t, f)
}

func TestRunRestartStaleDiscProceeds(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))

	removeDiscCalled := false
	startCalled := false
	deps := fakeRestartDeps(disc)
	deps.stopFn = func(_ int, _ bool) (bool, error) { return false, nil }
	deps.removeDisc = func(_ string) error { removeDiscCalled = true; return nil }
	deps.startDaemon = func(_ string) error { startCalled = true; return nil }

	err := runRestart(context.Background(), io.Discard, deps)
	require.NoError(t, err)
	assert.True(t, removeDiscCalled)
	assert.True(t, startCalled)
}

func TestRunRestartRemoveDiscError(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))
	deps := fakeRestartDeps(disc)
	deps.removeDisc = func(_ string) error { return errors.New("permission denied") }
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restart: remove discovery")
}

func TestRunRestartPreservesTranscriptDir(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:          "127.0.0.1:1",
		Token:         "t",
		Pid:           99,
		TranscriptDir: "/old/transcripts",
	}))

	var gotTranscriptDir string
	deps := fakeRestartDeps(disc)
	deps.startDaemon = func(td string) error { gotTranscriptDir = td; return nil }

	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.Equal(t, "/old/transcripts", gotTranscriptDir)
}

func TestRunRestartPollRetries(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	deps.waitSeconds = 2
	calls := 0
	deps.pollHealthz = func(_ context.Context, _ string) error {
		calls++
		if calls < 2 {
			return errors.New("not ready")
		}
		return nil
	}
	deps.readDiscovery = func(_ string) (daemon.Discovery, error) {
		return daemon.Discovery{Addr: "127.0.0.1:9999"}, nil
	}
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.Equal(t, 2, calls)
}

func TestRunRestartNoDaemonTranscriptDirEmpty(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	var gotTranscriptDir string
	deps := fakeRestartDeps(disc)
	deps.startDaemon = func(td string) error { gotTranscriptDir = td; return nil }

	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.Equal(t, "", gotTranscriptDir)
}

func TestNewRestartCmdRunE(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(t.TempDir(), "none.json"))
	origExe := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("no exe") }
	t.Cleanup(func() { osExecutable = origExe })
	cmd := newRestartCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRestartUsesDownSignalForStop(t *testing.T) {
	swapSleepNoop(t)
	n := 0
	swapSignal(t, func(_ int, _ syscall.Signal) error {
		n++
		if n >= 3 {
			return errors.New("gone")
		}
		return nil
	})
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))
	deps := fakeRestartDeps(disc)
	deps.stopFn = stopDaemon
	deps.startDaemon = func(_ string) error { return nil }
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.GreaterOrEqual(t, n, 3)
}
