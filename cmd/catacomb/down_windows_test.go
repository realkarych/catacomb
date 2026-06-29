//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalProcessTerminatesWindows(t *testing.T) {
	cmd := exec.Command("ping", "-n", "30", "127.0.0.1")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	require.NoError(t, signalProcess(cmd.Process.Pid, syscall.SIGTERM))
	_ = cmd.Wait()
	assert.Error(t, signalProcess(1<<30, syscall.SIGTERM))
}
