package main

import (
	"errors"
	"net"
	"os"
)

var (
	ErrDiffInput            = errors.New("diff: invalid input")
	ErrNoDaemon             = errors.New("no catacomb daemon running. Start one: catacomb up")
	ErrDaemonUnreachable    = errors.New("catacomb daemon is unreachable. Restart it: catacomb up")
	ErrHooksNotInstalled    = errors.New("catacomb hooks are not installed. Install them: catacomb install-hooks")
	ErrDaemonRestarted      = errors.New("catacomb daemon restarted (token mismatch). Re-open the UI: catacomb ui")
	ErrStoreNotFound        = errors.New("no catacomb store found. Create one: catacomb up")
	ErrRunNotFound          = errors.New("run not found")
	ErrUnknownSink          = errors.New("unknown export sink")
	ErrSinkNotConfigured    = errors.New("export sink not configured (missing --otlp-export-endpoint / --postgres-export-dsn / --neo4j-export-uri)")
	ErrModeUnsupported      = errors.New("export mode not supported for this sink (use --mode materialized)")
	ErrDaemonStop           = errors.New("failed to stop the catacomb daemon")
	ErrDaemonAlreadyRunning = errors.New("a catacomb daemon is already running for this discovery path. Stop it first: catacomb down")
	ErrConfirmationRequired = errors.New("refusing a destructive teardown without --yes in a non-interactive shell")
	ErrBaselineNotFound     = errors.New("baseline not found")
	ErrEmptyGroup           = errors.New("selector matched no runs")
	errRegressionDetected   = errors.New("regression detected")
)

type operationalError struct{ err error }

func (e *operationalError) Error() string { return e.err.Error() }

func (e *operationalError) Unwrap() error { return e.err }

func operational(err error) error { return &operationalError{err: err} }

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
	if errors.Is(err, ErrStoreNotFound) {
		return ErrStoreNotFound.Error()
	}
	if errors.Is(err, ErrRunNotFound) {
		return ErrRunNotFound.Error()
	}
	var opErr *operationalError
	if errors.As(err, &opErr) {
		return opErr.Error()
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
