package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func runTestDaemon(t *testing.T) (*daemon.Daemon, string) {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "g.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	d := daemon.New(s)
	ln, err := daemon.ListenLoopback()
	require.NoError(t, err)
	grpcLn, err := daemon.ListenLoopback()
	require.NoError(t, err)
	t.Cleanup(func() { _ = grpcLn.Close() })
	discovery := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: ln.Addr().String(), Token: "tok"}))
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, ln, grpcLn, "tok") }()
	t.Cleanup(func() {
		cancel()
		<-errc
	})
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)
	return d, discovery
}

func TestForwardDelivers(t *testing.T) {
	d, discovery := runTestDaemon(t)
	var warn bytes.Buffer
	forward(&warn, discovery, "SessionStart", bytes.NewReader([]byte(`{"session_id":"s1"}`)))
	assert.Empty(t, warn.String())
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestForwardNon2xx(t *testing.T) {
	_, discovery := runTestDaemon(t)
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: d.Addr, Token: "wrong"}))
	var warn bytes.Buffer
	forward(&warn, discovery, "SessionStart", bytes.NewReader([]byte(`{"session_id":"s1"}`)))
	assert.Contains(t, warn.String(), "status 401")
}

func TestForwardStdinReadError(t *testing.T) {
	var warn bytes.Buffer
	forward(&warn, filepath.Join(t.TempDir(), "d.json"), "X", errReader{})
	assert.Contains(t, warn.String(), "read stdin")
}

func TestForwardMissingDiscovery(t *testing.T) {
	var warn bytes.Buffer
	forward(&warn, filepath.Join(t.TempDir(), "nope.json"), "X", bytes.NewReader([]byte(`{}`)))
	assert.Contains(t, warn.String(), "discovery")
}

func TestForwardBadAddr(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: "\x7f", Token: "t"}))
	var warn bytes.Buffer
	forward(&warn, discovery, "X", bytes.NewReader([]byte(`{}`)))
	assert.NotEmpty(t, warn.String())
}

func TestForwardDaemonDown(t *testing.T) {
	ln, err := daemon.ListenLoopback()
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: addr, Token: "t"}))
	var warn bytes.Buffer
	forward(&warn, discovery, "X", bytes.NewReader([]byte(`{}`)))
	assert.Contains(t, warn.String(), "forward")
}

func TestHookCommandWiring(t *testing.T) {
	_, discovery := runTestDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	root := newRootCmd()
	root.SetArgs([]string{"hook", "SessionStart"})
	root.SetIn(bytes.NewReader([]byte(`{"session_id":"s9"}`)))
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	require.NoError(t, root.Execute())
}
