package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestRunStatusHealthy(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	now := startedAt.Add(12*time.Minute + 4*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/sessions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"session":"s1","node_count":30},{"session":"s2","node_count":17}]`))
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      strings.TrimPrefix(srv.URL, "http://"),
		Token:     "tok",
		Pid:       12345,
		StartedAt: startedAt.Format(time.RFC3339),
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    srv.Client(),
		now:           fixedNow(now),
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	output := out.String()
	assert.Contains(t, output, strings.TrimPrefix(srv.URL, "http://"))
	assert.Contains(t, output, "12345")
	assert.Contains(t, output, "12m4s")
	assert.Contains(t, output, "2")
	assert.Contains(t, output, "47")
}

func TestRunStatusNoDaemon(t *testing.T) {
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: t.TempDir() + "/missing.json",
		httpClient:    http.DefaultClient,
		now:           time.Now,
	}
	err := runStatus(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoDaemon))
}

func TestRunStatusDeadDaemon(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	now := startedAt.Add(5 * time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      addr,
		Token:     "tok",
		Pid:       99999,
		StartedAt: startedAt.Format(time.RFC3339),
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    http.DefaultClient,
		now:           fixedNow(now),
	}
	err := runStatus(context.Background(), &out, deps)
	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, addr)
	assert.Contains(t, output, "99999")
	assert.Contains(t, output, "unavailable")
}

func TestRunStatusDiscoveryParseError(t *testing.T) {
	disc := t.TempDir() + "/bad.json"
	require.NoError(t, os.WriteFile(disc, []byte("{bad json}"), 0o600))

	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    http.DefaultClient,
		now:           time.Now,
	}
	err := runStatus(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNoDaemon))
}

func TestRunStatusDaemonRestarted(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	now := startedAt.Add(3 * time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      strings.TrimPrefix(srv.URL, "http://"),
		Token:     "oldtok",
		Pid:       11111,
		StartedAt: startedAt.Format(time.RFC3339),
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    srv.Client(),
		now:           fixedNow(now),
	}
	err := runStatus(context.Background(), &out, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonRestarted))
}

func TestStatusCmdRegistered(t *testing.T) {
	root := newRootCmd()
	var found bool
	for _, sub := range root.Commands() {
		if sub.Use == "status" {
			found = true
		}
	}
	assert.True(t, found, "status subcommand must be registered")
}

func TestStatusCmdRunE(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      strings.TrimPrefix(srv.URL, "http://"),
		Token:     "tok",
		Pid:       1,
		StartedAt: startedAt.Format(time.RFC3339),
	}))
	t.Setenv("CATACOMB_DISCOVERY", disc)

	origClient := statusHTTPClient
	statusHTTPClient = srv.Client()
	t.Cleanup(func() { statusHTTPClient = origClient })

	origNow := statusNowFn
	statusNowFn = fixedNow(startedAt.Add(time.Minute))
	t.Cleanup(func() { statusNowFn = origNow })

	cmd := newStatusCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), strings.TrimPrefix(srv.URL, "http://"))
}

func TestRunStatusNoStartedAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
		Pid:   42,
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    srv.Client(),
		now:           time.Now,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	output := out.String()
	assert.Contains(t, output, "unknown")
}

func TestRunStatusFetchSessionsNewRequestError(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      "host with spaces:99",
		Token:     "tok",
		Pid:       1,
		StartedAt: startedAt.Format(time.RFC3339),
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    http.DefaultClient,
		now:           fixedNow(startedAt.Add(time.Minute)),
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "unavailable")
}

func TestRunStatusFetchSessionsHTTPError(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      strings.TrimPrefix(srv.URL, "http://"),
		Token:     "tok",
		Pid:       1,
		StartedAt: startedAt.Format(time.RFC3339),
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    srv.Client(),
		now:           fixedNow(startedAt.Add(time.Minute)),
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "unavailable")
}

func TestRunStatusFetchSessionsDecodeError(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not valid json`))
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      strings.TrimPrefix(srv.URL, "http://"),
		Token:     "tok",
		Pid:       1,
		StartedAt: startedAt.Format(time.RFC3339),
	}))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    srv.Client(),
		now:           fixedNow(startedAt.Add(time.Minute)),
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "unavailable")
}

func TestRunStatusShowsObserving(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:          strings.TrimPrefix(srv.URL, "http://"),
		Token:         "tok",
		TranscriptDir: "/home/u/.claude/projects",
	}))
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "observing")
	assert.Contains(t, out.String(), "/home/u/.claude/projects")
}

func TestRunStatusObservingHistoryOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "history off")
}
