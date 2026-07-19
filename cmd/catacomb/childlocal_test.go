package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/drift"
)

func stubChildContext(t *testing.T) {
	t.Helper()
	t.Setenv("GO_HELPER_OFFLINE", "1")
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperOfflineChild")
	}
	t.Cleanup(func() { execCommandContext = orig })
}

func TestStreamPeek(t *testing.T) {
	p := &streamPeek{}
	p.onLine([]byte("not json"))
	p.onLine([]byte(`{"type":"system","session_id":"s-1"}`))
	p.onLine([]byte(`{"type":"system","session_id":"s-2"}`))
	p.onLine([]byte(`{"type":"result","total_cost_usd":0.5}`))
	require.Equal(t, "s-1", p.sessionID)
	require.Equal(t, "s-1", p.session())
	require.NotNil(t, p.costUSD)
	require.InDelta(t, 0.5, *p.costUSD, 1e-9)
	require.Same(t, p.costUSD, p.cost())
}

func TestCodexPeek(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "captures thread id from thread.started",
			lines: []string{`{"type":"thread.started","thread_id":"th-1"}`},
			want:  "th-1",
		},
		{
			name: "first thread id wins",
			lines: []string{
				`{"type":"thread.started","thread_id":"th-1"}`,
				`{"type":"thread.started","thread_id":"th-2"}`,
			},
			want: "th-1",
		},
		{
			name: "non json lines ignored",
			lines: []string{
				"not json",
				`{"type":"thread.started","thread_id":"th-1"}`,
			},
			want: "th-1",
		},
		{
			name:  "thread id outside thread.started ignored",
			lines: []string{`{"type":"turn.started","thread_id":"th-9"}`},
			want:  "",
		},
		{
			name: "empty thread id skipped",
			lines: []string{
				`{"type":"thread.started","thread_id":""}`,
				`{"type":"thread.started","thread_id":"th-late"}`,
			},
			want: "th-late",
		},
		{
			name:  "turn.completed usage records nothing",
			lines: []string{`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":20}}`},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &codexPeek{}
			for _, line := range tt.lines {
				p.onLine([]byte(line))
			}
			assert.Equal(t, tt.want, p.session())
			assert.Nil(t, p.cost())
		})
	}
}

func TestNewPeeker(t *testing.T) {
	_, codex := newPeeker(drift.RuntimeCodex).(*codexPeek)
	assert.True(t, codex)
	_, claude := newPeeker(drift.RuntimeClaudeCode).(*streamPeek)
	assert.True(t, claude)
	_, unset := newPeeker("").(*streamPeek)
	assert.True(t, unset)
}

func TestStreamPeekCostOnlyFromResult(t *testing.T) {
	p := &streamPeek{}
	p.onLine([]byte(`{"type":"system","total_cost_usd":9.99}`))
	require.Nil(t, p.costUSD)
	p.onLine([]byte(`{"type":"result","total_cost_usd":0.5}`))
	require.NotNil(t, p.costUSD)
	require.InDelta(t, 0.5, *p.costUSD, 1e-9)
}

func TestRunChildLocal(t *testing.T) {
	stubChildContext(t)
	var out bytes.Buffer
	peek := &streamPeek{}
	err := runChildLocal(t.Context(), &out, io.Discard, []string{"claude"}, "", []string{"X=1"}, peek.onLine)
	require.NoError(t, err)
	require.Equal(t, "sess-h", peek.sessionID)
	require.Contains(t, out.String(), "sess-h")
}

func TestRunChildLocalObservesFinalLineWithoutTrailingNewline(t *testing.T) {
	stubChildContext(t)
	t.Setenv("GO_HELPER_OFFLINE_NO_NEWLINE", "1")
	var out bytes.Buffer
	peek := &streamPeek{}
	require.NoError(t, runChildLocal(t.Context(), &out, io.Discard, []string{"claude"}, "", nil, peek.onLine))
	assert.Equal(t, "sess-nonl", peek.session())
	assert.Equal(t, `{"type":"system","session_id":"sess-nonl"}`, out.String())
}

func TestRunChildLocalExitCode(t *testing.T) {
	stubChildContext(t)
	t.Setenv("GO_HELPER_OFFLINE_EXIT3", "1")
	err := runChildLocal(t.Context(), io.Discard, io.Discard, []string{"claude"}, "", nil, func([]byte) {})
	code, ok := exitInfo(err)
	require.False(t, ok)
	require.Equal(t, 3, code)
}

func TestRunChildLocalStartError(t *testing.T) {
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "does-not-exist-binary"))
	}
	t.Cleanup(func() { execCommandContext = orig })
	err := runChildLocal(t.Context(), io.Discard, io.Discard, []string{"nope"}, "", nil, func([]byte) {})
	require.Error(t, err)
}

func TestRunChildLocalCancelled(t *testing.T) {
	stubChildContext(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := runChildLocal(ctx, io.Discard, io.Discard, []string{"claude"}, "", nil, func([]byte) {})
	require.ErrorIs(t, err, context.Canceled)
}

func TestRunChildLocalTimeout(t *testing.T) {
	stubChildContext(t)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
	defer cancel()
	err := runChildLocal(ctx, io.Discard, io.Discard, []string{"claude"}, "", nil, func([]byte) {})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestHelperOfflineChild(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_HELPER_OFFLINE") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_OFFLINE_EXIT3") == "1" {
		os.Exit(3)
	}
	if os.Getenv("GO_HELPER_OFFLINE_NO_NEWLINE") == "1" {
		fmt.Print(`{"type":"system","session_id":"sess-nonl"}`)
		os.Exit(0)
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
