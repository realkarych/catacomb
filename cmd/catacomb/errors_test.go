package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderErrGeneric(t *testing.T) {
	err := errors.New("something went wrong")
	assert.Equal(t, "something went wrong", renderErr(err))
}

func TestRenderErrDiffInput(t *testing.T) {
	wrapped := fmt.Errorf("diff: /path/x.jsonl: open: %w (%w)", os.ErrNotExist, ErrDiffInput)
	assert.Equal(t, wrapped.Error(), renderErr(wrapped))
}

func TestOperationalNilPassesThrough(t *testing.T) {
	assert.Nil(t, operational(nil))
}

func TestRenderErrOperationalKeepsMessage(t *testing.T) {
	err := operational(fmt.Errorf("regress name:g: run %q dir %q: %w", "ghost-1", "/runs/ghost-1", os.ErrNotExist))
	assert.Equal(t, err.Error(), renderErr(err))
}

func TestRenderErrStoreNotFound(t *testing.T) {
	assert.Equal(t, ErrStoreNotFound.Error(), renderErr(ErrStoreNotFound))
}

func TestErrStoreNotFoundIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrStoreNotFound)
	assert.True(t, errors.Is(wrapped, ErrStoreNotFound))
}

func TestErrUnknownSinkIsCheckable(t *testing.T) {
	wrapped := fmt.Errorf("x: %w", ErrUnknownSink)
	assert.True(t, errors.Is(wrapped, ErrUnknownSink))
}
