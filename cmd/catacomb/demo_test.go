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

func TestDemoHTTPClientHasTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Second, demoHTTPClient.Timeout)
}

func TestRunDemoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/transcript", r.URL.Path)
		assert.Equal(t, "Bearer testtok", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		assert.Greater(t, len(body), 0)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "testtok",
	}))

	var out bytes.Buffer
	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		transcript:    demoTranscript,
		httpClient:    srv.Client(),
	}
	require.NoError(t, runDemo(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "demo-0001")
	assert.NotContains(t, out.String(), "View it:")
	assert.NotContains(t, out.String(), "http://")
}

func TestRunDemoNoDaemon(t *testing.T) {
	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: t.TempDir() + "/missing.json",
		transcript:    demoTranscript,
		httpClient:    http.DefaultClient,
	}
	err := runDemo(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoDaemon))
}

func TestRunDemoHTTPNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))

	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		transcript:    demoTranscript,
		httpClient:    srv.Client(),
	}
	err := runDemo(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestRunDemoDiscoveryParseError(t *testing.T) {
	disc := t.TempDir() + "/bad.json"
	require.NoError(t, os.WriteFile(disc, []byte("{bad json}"), 0o600))

	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		transcript:    demoTranscript,
		httpClient:    http.DefaultClient,
	}
	err := runDemo(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNoDaemon))
}

func TestDemoCmdRegistered(t *testing.T) {
	root := newRootCmd()
	var found bool
	for _, sub := range root.Commands() {
		if sub.Use == "demo" {
			found = true
		}
	}
	assert.True(t, found, "demo subcommand must be registered")
}

func TestDemoCmdRunE(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	t.Setenv("CATACOMB_DISCOVERY", disc)

	origClient := demoHTTPClient
	demoHTTPClient = srv.Client()
	t.Cleanup(func() { demoHTTPClient = origClient })

	cmd := newDemoCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
	assert.True(t, called)
}

func TestRunDemoOutputOmitsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "mytoken",
	}))

	var out bytes.Buffer
	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		transcript:    demoTranscript,
		httpClient:    srv.Client(),
	}
	require.NoError(t, runDemo(context.Background(), &out, deps))
	output := out.String()
	assert.Contains(t, output, "demo-0001 ingested")
	assert.NotContains(t, output, "mytoken")
}

func TestRunDemoRequestError(t *testing.T) {
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

	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		transcript:    demoTranscript,
		httpClient:    http.DefaultClient,
	}
	err := runDemo(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRunDemoNewRequestError(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "host with spaces:99",
		Token: "tok",
	}))

	deps := demoDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: disc,
		transcript:    demoTranscript,
		httpClient:    http.DefaultClient,
	}
	err := runDemo(context.Background(), io.Discard, deps)
	require.Error(t, err)
}
