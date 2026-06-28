package main

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

func runMarkDaemon(t *testing.T) string {
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
	}, 30*time.Second, 10*time.Millisecond)
	return discovery
}

func TestMarkCommandDelivers(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", "")
	discovery := runMarkDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	root := newRootCmd()
	root.SetArgs([]string{"mark", "--session", "s1", "--name", "phase1", "--boundary", "start"})
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	require.NoError(t, root.Execute())
	assert.Empty(t, errOut.String())
}

func TestMarkCommandWithOccurrenceAndStateRef(t *testing.T) {
	discovery := runMarkDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	root := newRootCmd()
	root.SetArgs([]string{
		"mark", "--session", "s1", "--name", "phase1", "--boundary", "start",
		"--occurrence", "3", "--state-ref", "ckpt_abc",
	})
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	require.NoError(t, root.Execute())
	assert.Empty(t, errOut.String())
}

func TestMarkCommandMissingDiscovery(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"mark", "--session", "s1", "--name", "phase1", "--boundary", "start"})
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	err := runMark(markArgs{
		discoveryPath: filepath.Join(t.TempDir(), "nope.json"),
		sessionID:     "s1",
		name:          "phase1",
		boundary:      "start",
	})
	assert.Error(t, err)
}

func TestMarkCommandBadToken(t *testing.T) {
	discovery := runMarkDaemon(t)
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: d.Addr, Token: "wrong"}))
	err = runMark(markArgs{
		discoveryPath: discovery,
		sessionID:     "s1",
		name:          "phase1",
		boundary:      "start",
	})
	assert.Error(t, err)
}

func TestMarkCommandBadAddr(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: "\x7f", Token: "t"}))
	err := runMark(markArgs{
		discoveryPath: discovery,
		sessionID:     "s1",
		name:          "phase1",
		boundary:      "start",
	})
	assert.Error(t, err)
}

func TestMarkCommandDaemonDown(t *testing.T) {
	ln, err := daemon.ListenLoopback()
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: addr, Token: "t"}))
	err = runMark(markArgs{
		discoveryPath: discovery,
		sessionID:     "s1",
		name:          "phase1",
		boundary:      "start",
	})
	assert.Error(t, err)
}
