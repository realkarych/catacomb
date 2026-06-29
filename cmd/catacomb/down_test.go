package main

import (
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

func TestRunDownRemoveError(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	orig := downRemove
	downRemove = func(string) error { return errors.New("permission denied") }
	t.Cleanup(func() { downRemove = orig })
	err := runDown(io.Discard, downOpts{}, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "down: remove discovery")
}

func TestRunDownAllFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{all: true}, path))
	assert.Contains(t, out.String(), "no daemon")
}

func TestRunDownDryRun(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{dryRun: true}, path))
	assert.Contains(t, out.String(), "would")
}

func TestWriteDownReportLoops(t *testing.T) {
	rep := downReport{
		HooksRemoved:     []string{"hook1"},
		DatabasesRemoved: []string{"db1"},
		StateRemoved:     []string{"state1"},
		Warnings:         []string{"warn1"},
	}
	var out strings.Builder
	require.NoError(t, writeDownReport(&out, rep, false))
	s := out.String()
	assert.Contains(t, s, "hooks: hook1")
	assert.Contains(t, s, "database: db1")
	assert.Contains(t, s, "state: state1")
	assert.Contains(t, s, "warning: warn1")
}
