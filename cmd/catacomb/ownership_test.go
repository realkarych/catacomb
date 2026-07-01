package main

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func swapOwnershipProbes(t *testing.T, alive func(int) bool, start func(int) (int64, error)) {
	t.Helper()
	oa, os2 := ownershipAlive, ownershipStartTime
	ownershipAlive, ownershipStartTime = alive, start
	t.Cleanup(func() { ownershipAlive, ownershipStartTime = oa, os2 })
}

func swapOwnershipBootID(t *testing.T, fn func() string) {
	t.Helper()
	orig := ownershipBootID
	ownershipBootID = fn
	t.Cleanup(func() { ownershipBootID = orig })
}

func TestIsOurLiveDaemonNonPositivePid(t *testing.T) {
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: 0, StartToken: 5}))
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: -1, StartToken: 5}))
}

func TestIsOurLiveDaemonDead(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return false }, func(int) (int64, error) { return 0, nil })
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 5}))
}

func TestIsOurLiveDaemonNoTokenLivenessOnly(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return true }, func(int) (int64, error) {
		return 0, errors.New("should not be called")
	})
	assert.True(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 0}))
}

func TestIsOurLiveDaemonTokenMatch(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return true }, func(int) (int64, error) { return 777, nil })
	swapOwnershipBootID(t, func() string { return "boot-A" })
	assert.True(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 777, BootID: "boot-A"}))
}

func TestIsOurLiveDaemonBootIDMismatch(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return true }, func(int) (int64, error) { return 777, nil })
	swapOwnershipBootID(t, func() string { return "boot-B" })
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 777, BootID: "boot-A"}))
}

func TestIsOurLiveDaemonEmptyBootIDSufficient(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return true }, func(int) (int64, error) { return 777, nil })
	swapOwnershipBootID(t, func() string { return "" })
	assert.True(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 777, BootID: ""}))
}

func TestIsOurLiveDaemonTokenMismatch(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return true }, func(int) (int64, error) { return 111, nil })
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 777}))
}

func TestIsOurLiveDaemonStartTimeError(t *testing.T) {
	swapOwnershipProbes(t, func(int) bool { return true }, func(int) (int64, error) {
		return 0, errors.New("boom")
	})
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: 42, StartToken: 777}))
}

func TestOwnershipDefaultProbesLiveSelf(t *testing.T) {
	assert.True(t, isOurLiveDaemon(daemon.Discovery{Pid: os.Getpid(), StartToken: 0}))
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: 1 << 30, StartToken: 0}))
}

func TestOwnershipDefaultStartTimeSelf(t *testing.T) {
	assert.False(t, isOurLiveDaemon(daemon.Discovery{Pid: os.Getpid(), StartToken: -999}))
}

func TestOwnershipDefaultProbesFullSelf(t *testing.T) {
	tok, err := processStartTime(os.Getpid())
	require.NoError(t, err)
	assert.True(t, isOurLiveDaemon(daemon.Discovery{
		Pid: os.Getpid(), StartToken: tok, BootID: ownershipBootID(),
	}))
}

func TestOwnershipAliveUsesSignal(t *testing.T) {
	assert.True(t, ownershipAlive(os.Getpid()))
	orig := downSignal
	downSignal = func(int, syscall.Signal) error { return errors.New("dead") }
	t.Cleanup(func() { downSignal = orig })
	assert.False(t, ownershipAlive(os.Getpid()))
}
