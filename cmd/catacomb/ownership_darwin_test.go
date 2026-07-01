//go:build darwin

package main

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

func TestProcessStartTimeDarwinSelf(t *testing.T) {
	tok, err := processStartTime(os.Getpid())
	require.NoError(t, err)
	assert.Positive(t, tok)
}

func TestProcessStartTimeDarwinError(t *testing.T) {
	orig := sysctlKinfoProc
	sysctlKinfoProc = func(string, ...int) (*unix.KinfoProc, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { sysctlKinfoProc = orig })
	_, err := processStartTime(os.Getpid())
	require.Error(t, err)
}
