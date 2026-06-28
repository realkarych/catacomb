package main

import (
	"errors"
	"net"
	"os"
)

var (
	ErrDiffInput         = errors.New("diff: invalid input")
	ErrNoDaemon          = errors.New("no catacomb daemon running. Start one: catacomb up")
	ErrDaemonUnreachable = errors.New("catacomb daemon is unreachable. Restart it: catacomb up")
	ErrHooksNotInstalled = errors.New("catacomb hooks are not installed. Install them: catacomb install-hooks")
	ErrDaemonRestarted   = errors.New("catacomb daemon restarted (token mismatch). Re-open the UI: catacomb ui")
)

func renderErr(err error) string {
	if errors.Is(err, ErrDiffInput) {
		return err.Error()
	}
	if errors.Is(err, ErrNoDaemon) {
		return ErrNoDaemon.Error()
	}
	if errors.Is(err, ErrDaemonUnreachable) {
		return ErrDaemonUnreachable.Error()
	}
	if errors.Is(err, ErrHooksNotInstalled) {
		return ErrHooksNotInstalled.Error()
	}
	if errors.Is(err, ErrDaemonRestarted) {
		return ErrDaemonRestarted.Error()
	}
	if errors.Is(err, os.ErrNotExist) {
		return ErrNoDaemon.Error()
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return ErrDaemonUnreachable.Error()
	}
	return err.Error()
}
