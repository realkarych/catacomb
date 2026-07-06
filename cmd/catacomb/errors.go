package main

import "errors"

var (
	ErrDiffInput          = errors.New("diff: invalid input")
	ErrStoreNotFound      = errors.New("no catacomb store found; create one with a write-path command like 'catacomb baseline set'")
	ErrUnknownSink        = errors.New("unknown export format (only jsonl is supported)")
	ErrBaselineNotFound   = errors.New("baseline not found")
	ErrEmptyGroup         = errors.New("selector matched no runs")
	errRegressionDetected = errors.New("regression detected")
)

type operationalError struct{ err error }

func (e *operationalError) Error() string { return e.err.Error() }

func (e *operationalError) Unwrap() error { return e.err }

func operational(err error) error { return &operationalError{err: err} }

func renderErr(err error) string {
	return err.Error()
}
