package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderErrGeneric(t *testing.T) {
	err := errors.New("something went wrong")
	assert.Equal(t, "something went wrong", renderErr(err))
}

func TestRenderErrKeepsEveryLayerOfAWrappedChain(t *testing.T) {
	wrapped := fmt.Errorf("diff: /path/x.jsonl: open: %w (%w)", os.ErrNotExist, ErrDiffInput)
	assert.Equal(t, "diff: /path/x.jsonl: open: file does not exist (diff: invalid input)", renderErr(wrapped))
	assert.ErrorIs(t, wrapped, ErrDiffInput)
	assert.ErrorIs(t, wrapped, os.ErrNotExist)
}

func TestOperationalNilPassesThrough(t *testing.T) {
	assert.Nil(t, operational(nil))
}

func TestOperationalWrapsWithoutHidingTheCauseOrItsMessage(t *testing.T) {
	cause := fmt.Errorf("regress name:g: run %q dir %q: %w", "ghost-1", "/runs/ghost-1", os.ErrNotExist)
	err := operational(cause)

	assert.Equal(t, `regress name:g: run "ghost-1" dir "/runs/ghost-1": file does not exist`, renderErr(err))
	assert.ErrorIs(t, err, os.ErrNotExist)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestRenderErrStoreNotFoundNamesTheRemedy(t *testing.T) {
	rendered := renderErr(fmt.Errorf("trends: %w", ErrStoreNotFound))
	assert.Equal(t, "trends: no catacomb store found; create one with a write-path command like 'catacomb baseline set'", rendered)
}

func TestEverySentinelSurvivesWrappingAsAnErrorsIsTarget(t *testing.T) {
	for _, sentinel := range []error{
		ErrDiffInput, ErrStoreNotFound, ErrUnknownSink,
		ErrBaselineNotFound, ErrEmptyGroup, errRegressionDetected,
	} {
		wrapped := fmt.Errorf("x: %w", sentinel)
		assert.ErrorIs(t, wrapped, sentinel)
	}
}
