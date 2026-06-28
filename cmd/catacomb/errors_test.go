package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrNoDaemonEndsInCommand(t *testing.T) {
	assert.True(t, strings.HasSuffix(ErrNoDaemon.Error(), "catacomb up"))
}

func TestErrDaemonUnreachableEndsInCommand(t *testing.T) {
	assert.True(t, strings.HasSuffix(ErrDaemonUnreachable.Error(), "catacomb up"))
}

func TestErrHooksNotInstalledEndsInCommand(t *testing.T) {
	assert.True(t, strings.HasSuffix(ErrHooksNotInstalled.Error(), "catacomb install-hooks"))
}

func TestErrDaemonRestartedEndsInCommand(t *testing.T) {
	assert.True(t, strings.HasSuffix(ErrDaemonRestarted.Error(), "catacomb ui"))
}

func TestErrNoDaemonIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrNoDaemon)
	assert.True(t, errors.Is(wrapped, ErrNoDaemon))
}

func TestRenderErrNoDaemon(t *testing.T) {
	assert.Equal(t, ErrNoDaemon.Error(), renderErr(ErrNoDaemon))
}

func TestRenderErrDaemonUnreachable(t *testing.T) {
	assert.Equal(t, ErrDaemonUnreachable.Error(), renderErr(ErrDaemonUnreachable))
}

func TestRenderErrHooksNotInstalled(t *testing.T) {
	assert.Equal(t, ErrHooksNotInstalled.Error(), renderErr(ErrHooksNotInstalled))
}

func TestRenderErrDaemonRestarted(t *testing.T) {
	assert.Equal(t, ErrDaemonRestarted.Error(), renderErr(ErrDaemonRestarted))
}

func TestRenderErrOsNotExist(t *testing.T) {
	err := &os.PathError{Op: "open", Path: "/no/such", Err: os.ErrNotExist}
	assert.Equal(t, ErrNoDaemon.Error(), renderErr(err))
}

func TestRenderErrNetOpError(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: &net.AddrError{Err: "refused"}}
	assert.Equal(t, ErrDaemonUnreachable.Error(), renderErr(err))
}

func TestRenderErrGeneric(t *testing.T) {
	err := errors.New("something went wrong")
	assert.Equal(t, "something went wrong", renderErr(err))
}

func TestRenderErrWrappedNoDaemon(t *testing.T) {
	wrapped := fmt.Errorf("context: %w", ErrNoDaemon)
	assert.Equal(t, ErrNoDaemon.Error(), renderErr(wrapped))
}

func TestRenderErrWrappedUnreachable(t *testing.T) {
	wrapped := fmt.Errorf("context: %w", ErrDaemonUnreachable)
	assert.Equal(t, ErrDaemonUnreachable.Error(), renderErr(wrapped))
}

func TestRenderErrDiffInput(t *testing.T) {
	wrapped := fmt.Errorf("diff: /path/x.jsonl: open: %w (%w)", os.ErrNotExist, ErrDiffInput)
	assert.Equal(t, wrapped.Error(), renderErr(wrapped))
}

func TestErrStoreNotFoundIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrStoreNotFound)
	assert.True(t, errors.Is(wrapped, ErrStoreNotFound))
}

func TestErrRunNotFoundIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrRunNotFound)
	assert.True(t, errors.Is(wrapped, ErrRunNotFound))
}

func TestErrUnknownSinkIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrUnknownSink)
	assert.True(t, errors.Is(wrapped, ErrUnknownSink))
}

func TestErrSinkNotConfiguredIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrSinkNotConfigured)
	assert.True(t, errors.Is(wrapped, ErrSinkNotConfigured))
}

func TestErrModeUnsupportedIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrModeUnsupported)
	assert.True(t, errors.Is(wrapped, ErrModeUnsupported))
}

func TestRenderErrStoreNotFound(t *testing.T) {
	assert.Equal(t, ErrStoreNotFound.Error(), renderErr(ErrStoreNotFound))
}

func TestRenderErrRunNotFound(t *testing.T) {
	assert.Equal(t, ErrRunNotFound.Error(), renderErr(ErrRunNotFound))
}
