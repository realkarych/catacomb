package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/daemon"
)

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestRunStatusHealthy(t *testing.T) {
	startedAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	now := startedAt.Add(12*time.Minute + 4*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions":
			_, _ = w.Write([]byte(`[{"session":"s1","node_count":30},{"session":"s2","node_count":17}]`))
		case "/metrics":
			_, _ = w.Write([]byte(`{}`))
		}
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

func discWithSummary(t *testing.T, addr string) daemon.Discovery {
	t.Helper()
	return daemon.Discovery{
		Addr:           addr,
		Token:          "tok",
		Pid:            42,
		StartedAt:      time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
		StoreBackend:   config.BackendMemory,
		SinkTypes:      []string{config.SinkOTLP},
		SourcesEnabled: []string{"hooks", "otel", "stream_json"},
		ReaperWindow:   "30m0s",
		MaxShards:      4096,
	}
}

func TestRunStatusJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"session":"s1","node_count":10}]`))
	}))
	t.Cleanup(srv.Close)

	disc := discWithSummary(t, strings.TrimPrefix(srv.URL, "http://"))
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        true,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))

	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, 42, rep.Pid)
	assert.Equal(t, config.BackendMemory, rep.StoreBackend)
	assert.Equal(t, []string{config.SinkOTLP}, rep.SinkTypes)
	assert.Contains(t, rep.SourcesEnabled, "hooks")
	assert.Equal(t, "30m0s", rep.ReaperWindow)
	assert.Equal(t, 4096, rep.MaxShards)
	assert.Equal(t, 1, rep.Sessions)
	assert.Equal(t, 10, rep.Nodes)
	assert.True(t, rep.Healthy)
}

func TestRunStatusJSONUnhealthy(t *testing.T) {
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, daemon.Discovery{
		Addr:  "127.0.0.1:1",
		Token: "tok",
		Pid:   99,
	}))
	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    http.DefaultClient,
		now:           time.Now,
		asJSON:        true,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, 99, rep.Pid)
	assert.False(t, rep.Healthy)
}

func TestRunStatusJSONNoDaemon(t *testing.T) {
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: filepath.Join(t.TempDir(), "missing.json"),
		httpClient:    http.DefaultClient,
		now:           time.Now,
		asJSON:        true,
	}
	err := runStatus(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDaemon)
}

func TestRunStatusTextEnriched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := discWithSummary(t, strings.TrimPrefix(srv.URL, "http://"))
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        false,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	text := out.String()
	assert.Contains(t, text, "memory")
	assert.Contains(t, text, "otlp")
	assert.Contains(t, text, "hooks")
	assert.Contains(t, text, "30m0s")
	assert.Contains(t, text, "4096")
}

func TestStatusCmdJSONFlag(t *testing.T) {
	cmd := newStatusCmd()
	f := cmd.Flags().Lookup("json")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

func TestRunStatusTextShowsConfigPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "catacomb.toml")
	disc := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:       strings.TrimPrefix(srv.URL, "http://"),
		Token:      "tok",
		ConfigPath: cfgPath,
	}))
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "config")
	assert.Contains(t, out.String(), cfgPath)
}

func TestRunStatusTextOmitsConfigPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			assert.NotEqual(t, "config", fields[0])
		}
	}
}

func TestRunStatusJSONIncludesConfigPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "catacomb.toml")
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, daemon.Discovery{
		Addr:       strings.TrimPrefix(srv.URL, "http://"),
		Token:      "tok",
		ConfigPath: cfgPath,
	}))
	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        true,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, cfgPath, rep.ConfigPath)
}

func TestRunStatusJSONOmitsConfigPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        true,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "config_path")
}

func TestRunStatusJSONDaemonRestarted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "oldtok",
		Pid:   22222,
	}))
	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        true,
	}
	err := runStatus(context.Background(), &out, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDaemonRestarted))
	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, 22222, rep.Pid)
	assert.False(t, rep.Healthy)
}

type statusErrWriter struct{ err error }

func (e statusErrWriter) Write(_ []byte) (int, error) { return 0, e.err }

func TestRunStatusJSONEncodeWriteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
		Pid:   1,
	}))
	writeErr := errors.New("write failure")
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        true,
	}
	err := runStatus(context.Background(), statusErrWriter{err: writeErr}, deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, writeErr)
}

func blockingServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv
}

func TestStatusHTTPClientHasTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Second, statusHTTPClient.Timeout)
}

func writeStatusDiscovery(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	disc := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:      strings.TrimPrefix(srv.URL, "http://"),
		Token:     "tok",
		Pid:       1,
		StartedAt: time.Now().Format(time.RFC3339),
	}))
	return disc
}

func driftStatusServer(t *testing.T, metricsBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions":
			_, _ = w.Write([]byte(`[{"session":"s1","node_count":3}]`))
		case "/metrics":
			_, _ = w.Write([]byte(metricsBody))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunStatusShowsDriftWhenNonzero(t *testing.T) {
	srv := driftStatusServer(t, `{"uptime_seconds":1,"drift":{"stream_json/unknown_record_type":4,"hook/unknown_hook_event":1}}`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	output := out.String()
	assert.Contains(t, output, "hook/unknown_hook_event=1")
	assert.Contains(t, output, "stream_json/unknown_record_type=4")
	assert.Less(t, strings.Index(output, "hook/"), strings.Index(output, "stream_json/"))
}

func TestRunStatusNoDriftSectionWhenHealthy(t *testing.T) {
	srv := driftStatusServer(t, `{"uptime_seconds":1}`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "drift")
}

func TestRunStatusJSONIncludesDrift(t *testing.T) {
	srv := driftStatusServer(t, `{"drift":{"otel/unknown_span_name":2}}`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now, asJSON: true}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, uint64(2), rep.Drift["otel/unknown_span_name"])
}

func TestRunStatusDriftFetchFailureIsSilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "drift")
}

func TestRunStatusDriftDecodeErrorIsSilent(t *testing.T) {
	srv := driftStatusServer(t, `{"drift":`)
	disc := writeStatusDiscovery(t, srv)
	var out bytes.Buffer
	deps := statusDeps{readDiscovery: daemon.ReadDiscovery, discoveryPath: disc, httpClient: srv.Client(), now: time.Now}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.NotContains(t, out.String(), "drift")
}

func TestRunStatusReturnsWhenDaemonBlocks(t *testing.T) {
	srv := blockingServer(t)
	disc := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		httpClient:    &http.Client{Timeout: 50 * time.Millisecond},
		now:           time.Now,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "unavailable")
}
