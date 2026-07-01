//go:build windows

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessStartTimeWindowsSelf(t *testing.T) {
	tok, err := processStartTime(os.Getpid())
	require.NoError(t, err)
	assert.Positive(t, tok)
}

func TestProcessStartTimeWindowsError(t *testing.T) {
	_, err := processStartTime(1 << 30)
	require.Error(t, err)
}
