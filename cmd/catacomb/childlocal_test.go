package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamPeek(t *testing.T) {
	p := &streamPeek{}
	p.onLine([]byte("not json"))
	p.onLine([]byte(`{"type":"system","session_id":"s-1"}`))
	p.onLine([]byte(`{"type":"system","session_id":"s-2"}`))
	p.onLine([]byte(`{"type":"result","total_cost_usd":0.5}`))
	require.Equal(t, "s-1", p.sessionID)
	require.NotNil(t, p.costUSD)
	require.InDelta(t, 0.5, *p.costUSD, 1e-9)
}

func TestRunChildLocal(t *testing.T) {
	t.Setenv("GO_HELPER_OFFLINE", "1")
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestHelperOfflineChild")
	}
	t.Cleanup(func() { execCommand = orig })
	var out bytes.Buffer
	peek := &streamPeek{}
	err := runChildLocal(&out, io.Discard, []string{"claude"}, "", []string{"X=1"}, peek.onLine)
	require.NoError(t, err)
	require.Equal(t, "sess-h", peek.sessionID)
	require.Contains(t, out.String(), "sess-h")
}

func TestRunChildLocalExitCode(t *testing.T) {
	t.Setenv("GO_HELPER_OFFLINE", "1")
	t.Setenv("GO_HELPER_OFFLINE_EXIT3", "1")
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestHelperOfflineChild")
	}
	t.Cleanup(func() { execCommand = orig })
	err := runChildLocal(io.Discard, io.Discard, []string{"claude"}, "", nil, func([]byte) {})
	code, ok := exitInfo(err)
	require.False(t, ok)
	require.Equal(t, 3, code)
}

func TestRunChildLocalStartError(t *testing.T) {
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "does-not-exist-binary"))
	}
	t.Cleanup(func() { execCommand = orig })
	err := runChildLocal(io.Discard, io.Discard, []string{"nope"}, "", nil, func([]byte) {})
	require.Error(t, err)
}

func TestHelperOfflineChild(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_HELPER_OFFLINE") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_OFFLINE_EXIT3") == "1" {
		os.Exit(3)
	}
	fmt.Println(`{"type":"system","session_id":"sess-h"}`)
	fmt.Println(`{"type":"result","total_cost_usd":0.25}`)
	os.Exit(0)
}

func TestLineObserverFlushesUnterminatedLine(t *testing.T) {
	var got []string
	w := &lineObserver{observe: func(line []byte) { got = append(got, string(line)) }}
	n, err := w.Write([]byte("no newline here"))
	require.NoError(t, err)
	assert.Equal(t, len("no newline here"), n)
	assert.Empty(t, got)

	w.flush()
	require.Len(t, got, 1)
	assert.Equal(t, "no newline here", got[0])

	w.flush()
	assert.Len(t, got, 1)
}

func TestLineObserverOverflowStopsObserving(t *testing.T) {
	var calls int
	w := &lineObserver{observe: func([]byte) { calls++ }}
	big := bytes.Repeat([]byte("a"), maxObserverBuffer+1)

	n, err := w.Write(big)
	require.NoError(t, err)
	assert.Equal(t, len(big), n)
	assert.Zero(t, calls)

	n, err = w.Write([]byte(`{"session_id":"s1"}` + "\n"))
	require.NoError(t, err)
	assert.Equal(t, len(`{"session_id":"s1"}`)+1, n)
	assert.Zero(t, calls)

	w.flush()
	assert.Zero(t, calls)
}
