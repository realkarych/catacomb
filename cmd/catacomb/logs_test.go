package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type errReadCloser struct{ err error }

func (e errReadCloser) Read(_ []byte) (int, error) { return 0, e.err }
func (e errReadCloser) Close() error               { return nil }

type countedReadCloser struct{ reads int }

func (c *countedReadCloser) Read(p []byte) (int, error) {
	c.reads++
	if c.reads == 1 {
		return 0, io.EOF
	}
	return 0, errors.New("follow disk error")
}
func (c *countedReadCloser) Close() error { return nil }

func openFileRC(path string) (io.ReadCloser, error) { return os.Open(path) }

func TestRunLogsNoFile(t *testing.T) {
	var out bytes.Buffer
	deps := logsDeps{
		logPath: filepath.Join(t.TempDir(), "missing.log"),
		openLog: openFileRC,
		follow:  false,
	}
	require.NoError(t, runLogs(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "no log file yet")
}

func TestRunLogsReadsExistingContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("hello from daemon\n"), 0o600))
	var out bytes.Buffer
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: false}
	require.NoError(t, runLogs(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "hello from daemon")
}

func TestRunLogsOpenError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
	badPath := filepath.Join(dir, "afile")
	deps := logsDeps{
		logPath: badPath,
		openLog: func(_ string) (io.ReadCloser, error) { return nil, os.ErrPermission },
		follow:  false,
	}
	err := runLogs(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRunLogsInitialReadError(t *testing.T) {
	deps := logsDeps{
		logPath: "ignored",
		openLog: func(_ string) (io.ReadCloser, error) {
			return errReadCloser{err: errors.New("disk error")}, nil
		},
		follow: false,
	}
	err := runLogs(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.ErrorContains(t, err, "logs: read")
}

func TestRunLogsFollowReadError(t *testing.T) {
	tick := make(chan time.Time, 1)
	tick <- time.Now()
	deps := logsDeps{
		logPath: "ignored",
		openLog: func(_ string) (io.ReadCloser, error) {
			return &countedReadCloser{}, nil
		},
		follow: true,
		tick:   tick,
	}
	err := runLogs(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.ErrorContains(t, err, "logs: follow read")
}

func TestRunLogsFollowPicksUpNewContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("line1\n"), 0o600))

	tick := make(chan time.Time, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var mu syncBuffer
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: true, tick: tick}

	errc := make(chan error, 1)
	go func() { errc <- runLogs(ctx, &mu, deps) }()

	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("line2\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	tick <- time.Now()

	require.Eventually(t, func() bool {
		return strings.Contains(mu.String(), "line2")
	}, 2*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
	assert.Contains(t, mu.String(), "line1")
}

func TestRunLogsFollowExitsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("x\n"), 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	tick := make(chan time.Time)
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: true, tick: tick}
	require.NoError(t, runLogs(ctx, io.Discard, deps))
}

func TestRunLogsFollowExitsOnClosedTick(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("x\n"), 0o600))
	tick := make(chan time.Time)
	close(tick)
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: true, tick: tick}
	require.NoError(t, runLogs(context.Background(), io.Discard, deps))
}

func TestLogsCmdRegistered(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Use == "logs" {
			found = true
		}
	}
	assert.True(t, found, "logs subcommand must be registered")
}

func TestLogsCmdFollowFlag(t *testing.T) {
	cmd := newLogsCmd()
	f := cmd.Flags().Lookup("follow")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

func TestNewLogsCmdRunsNoFollow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(dir, "daemon.json"))
	logPath := filepath.Join(dir, "daemon.json.log")
	require.NoError(t, os.WriteFile(logPath, []byte("hello daemon\n"), 0o600))
	cmd := newLogsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.ExecuteContext(context.Background()))
	assert.Contains(t, buf.String(), "hello daemon")
}

func TestNewLogsCmdRunsFollow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(dir, "daemon.json"))
	logPath := filepath.Join(dir, "daemon.json.log")
	require.NoError(t, os.WriteFile(logPath, []byte("x\n"), 0o600))
	cmd := newLogsCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"-f"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.NoError(t, cmd.ExecuteContext(ctx))
}
