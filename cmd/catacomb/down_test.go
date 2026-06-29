package main

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

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
