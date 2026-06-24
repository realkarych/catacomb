package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func TestRunWatchPrintsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"kind\":\"node_upsert\",\"rev\":1}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))

	var buf strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runWatch(ctx, disc, "", nil, nil, srv.Client(), &buf)
	require.NoError(t, err)
}

func TestRunWatchDiscoveryError(t *testing.T) {
	err := runWatch(context.Background(), "/no/such/path.json", "", nil, nil, http.DefaultClient, io.Discard)
	require.Error(t, err)
}

func TestRunWatchDiscoveryNotFoundReturnsErrNoDaemon(t *testing.T) {
	err := runWatch(context.Background(), t.TempDir()+"/missing.json", "", nil, nil, http.DefaultClient, io.Discard)
	assert.True(t, errors.Is(err, ErrNoDaemon))
}

func TestRunWatchDiscoveryParseError(t *testing.T) {
	disc := t.TempDir() + "/bad.json"
	require.NoError(t, os.WriteFile(disc, []byte("{not json}"), 0o600))
	err := runWatch(context.Background(), disc, "", nil, nil, http.DefaultClient, io.Discard)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNoDaemon))
}

func TestRunWatchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "wrongtoken",
	}))

	err := runWatch(context.Background(), disc, "", nil, nil, srv.Client(), io.Discard)
	require.Error(t, err)
}

func TestRunWatchBuildURL(t *testing.T) {
	called := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- r.Clone(context.Background())
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = runWatch(ctx, disc, "run-A", []string{"tool_call"}, []string{"core"}, srv.Client(), io.Discard)
	}()

	req := <-called
	cancel()
	assert.Equal(t, "run-A", req.URL.Query().Get("run"))
	assert.Equal(t, "Bearer tok", req.Header.Get("Authorization"))
	assert.Contains(t, req.URL.Query()["type"], "tool_call")
	assert.Contains(t, req.URL.Query()["tier"], "core")
}

func TestWatchCmdFlagsRegistered(t *testing.T) {
	cmd := newWatchCmd()
	require.NotNil(t, cmd.Flags().Lookup("run"))
	require.NotNil(t, cmd.Flags().Lookup("type"))
	require.NotNil(t, cmd.Flags().Lookup("tier"))
}

func TestRunWatchPrintsLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"kind\":\"node_upsert\",\"rev\":1}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"kind\":\"run_ended\",\"rev\":2}\n\n")
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))

	var buf strings.Builder
	err := runWatch(context.Background(), disc, "", nil, nil, srv.Client(), &buf)
	require.NoError(t, err)

	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	assert.Equal(t, 2, len(lines))
	assert.Contains(t, lines[0], `"kind":"node_upsert"`)
	assert.Contains(t, lines[1], `"kind":"run_ended"`)
}

func TestRunWatchBadAddr(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "host:badport",
		Token: "tok",
	}))
	err := runWatch(context.Background(), disc, "", nil, nil, http.DefaultClient, io.Discard)
	require.Error(t, err)
}

func TestRunWatchDoErrorNoCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  addr,
		Token: "tok",
	}))

	err := runWatch(context.Background(), disc, "", nil, nil, http.DefaultClient, io.Discard)
	require.Error(t, err)
}

func TestRunWatchCtxCancelDuringScan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"kind\":\"node_upsert\",\"rev\":1}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runWatch(ctx, disc, "", nil, nil, srv.Client(), pw)
	}()
	sc := bufio.NewScanner(pr)
	require.True(t, sc.Scan())
	cancel()
	require.NoError(t, <-errCh)
	_ = pr.Close()
}

func TestWatchCmdRunE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"kind\":\"node_upsert\",\"rev\":1}\n\n")
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))

	t.Setenv("CATACOMB_DISCOVERY", disc)
	cmd := newWatchCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}
