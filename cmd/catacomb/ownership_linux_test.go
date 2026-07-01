//go:build linux

package main

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessStartTimeLinuxSelf(t *testing.T) {
	tok, err := processStartTime(os.Getpid())
	require.NoError(t, err)
	assert.Positive(t, tok)
}

func TestProcessStartTimeLinuxReadError(t *testing.T) {
	_, err := processStartTime(1 << 30)
	require.Error(t, err)
}

func TestProcessStartTimeLinuxNoParen(t *testing.T) {
	orig := readProcStat
	readProcStat = func(string) ([]byte, error) { return []byte("1234 no-paren-here"), nil }
	t.Cleanup(func() { readProcStat = orig })
	_, err := processStartTime(1)
	assert.ErrorIs(t, err, errProcStatForm)
}

func TestProcessStartTimeLinuxTooFewFields(t *testing.T) {
	orig := readProcStat
	readProcStat = func(string) ([]byte, error) { return []byte("1234 (cat) R 1 2 3"), nil }
	t.Cleanup(func() { readProcStat = orig })
	_, err := processStartTime(1)
	assert.ErrorIs(t, err, errProcStatForm)
}

func TestProcessStartTimeLinuxNonNumericStarttime(t *testing.T) {
	orig := readProcStat
	fields := "R p1 p2 p3 p4 p5 p6 p7 p8 p9 p10 p11 p12 p13 p14 p15 p16 p17 p18 xx t1 t2"
	readProcStat = func(string) ([]byte, error) { return []byte("1234 (cat) " + fields), nil }
	t.Cleanup(func() { readProcStat = orig })
	_, err := processStartTime(1)
	assert.ErrorIs(t, err, errProcStatForm)
}

func TestProcessStartTimeLinuxParsesField22(t *testing.T) {
	orig := readProcStat
	fields := "R p1 p2 p3 p4 p5 p6 p7 p8 p9 p10 p11 p12 p13 p14 p15 p16 p17 p18 987654 t1 t2"
	readProcStat = func(string) ([]byte, error) { return []byte("1234 (weird ) name) " + fields), nil }
	t.Cleanup(func() { readProcStat = orig })
	tok, err := processStartTime(1)
	require.NoError(t, err)
	assert.Equal(t, int64(987654), tok)
}

func TestProcessStartTimeLinuxReadErrorInjected(t *testing.T) {
	orig := readProcStat
	readProcStat = func(string) ([]byte, error) { return nil, errors.New("perm") }
	t.Cleanup(func() { readProcStat = orig })
	_, err := processStartTime(1)
	require.Error(t, err)
}
