package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunSuccessReturnsZero(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"version"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Empty(t, errBuf.String())
}

func TestRunStatusNoDaemonRendersErrorAndReturnsOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATACOMB_DISCOVERY", dir+"/missing.json")

	var out, errBuf bytes.Buffer
	code := run([]string{"status"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "no catacomb daemon running")
	assert.Contains(t, errBuf.String(), "catacomb up")
}

func TestRunUnknownSubcommandReturnsOne(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"does-not-exist"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.NotEmpty(t, errBuf.String())
}
