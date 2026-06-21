package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

type failSinceStore struct{}

func (f *failSinceStore) Persist([]model.Observation, []*model.Node, []*model.Edge) error {
	return nil
}

func (f *failSinceStore) AppendAndApply(model.Observation, []*model.Node, []*model.Edge) error {
	return nil
}
func (f *failSinceStore) MaxSeq() (uint64, error) { return 0, nil }
func (f *failSinceStore) ObservationsSince(uint64) ([]model.Observation, error) {
	return nil, errors.New("since")
}
func (f *failSinceStore) UpsertRun(model.Run) error          { return nil }
func (f *failSinceStore) ListOpenRuns() ([]model.Run, error) { return nil, nil }
func (f *failSinceStore) Runs() ([]model.Run, error)         { return nil, nil }
func (f *failSinceStore) Close() error                       { return nil }

func openFailSince(string) (store.Store, error) {
	return &failSinceStore{}, nil
}

func TestRunDaemonOpenError(t *testing.T) {
	open := func(string) (store.Store, error) { return nil, errors.New("open") }
	err := runDaemonWith(context.Background(), open, daemon.ListenLoopback, daemon.NewToken, "x", filepath.Join(t.TempDir(), "d.json"))
	require.Error(t, err)
}

func TestRunDaemonListenError(t *testing.T) {
	listen := func() (net.Listener, error) { return nil, errors.New("listen") }
	err := runDaemonWith(context.Background(), store.OpenSQLite, listen, daemon.NewToken, filepath.Join(t.TempDir(), "g.db"), filepath.Join(t.TempDir(), "d.json"))
	require.Error(t, err)
}

func TestRunDaemonDiscoveryError(t *testing.T) {
	dir := t.TempDir()
	badDiscovery := filepath.Join(dir, "afile", "d.json")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
	err := runDaemonWith(context.Background(), store.OpenSQLite, daemon.ListenLoopback, daemon.NewToken, filepath.Join(dir, "g.db"), badDiscovery)
	require.Error(t, err)
}

func TestRunDaemonRecoverError(t *testing.T) {
	err := runDaemonWith(context.Background(), openFailSince, daemon.ListenLoopback, daemon.NewToken, "x", filepath.Join(t.TempDir(), "d.json"))
	require.Error(t, err)
}

func TestRunDaemonNewTokenError(t *testing.T) {
	failToken := func() (string, error) { return "", errors.New("token") }
	err := runDaemonWith(context.Background(), store.OpenSQLite, daemon.ListenLoopback, failToken, filepath.Join(t.TempDir(), "g.db"), filepath.Join(t.TempDir(), "d.json"))
	require.Error(t, err)
}

func awaitHealthz(t *testing.T, addr string) {
	t.Helper()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)
}

func readAddr(t *testing.T, discovery string) string {
	t.Helper()
	var addr string
	require.Eventually(t, func() bool {
		d, err := daemon.ReadDiscovery(discovery)
		if err != nil {
			return false
		}
		addr = d.Addr
		return addr != ""
	}, 2*time.Second, 10*time.Millisecond)
	return addr
}

func TestDaemonEndToEnd(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "g.db")
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.NewToken, dbPath, discovery)
	}()
	awaitHealthz(t, readAddr(t, discovery))

	for _, f := range []struct{ typ, file string }{
		{"SessionStart", "sessionstart.json"},
		{"UserPromptSubmit", "userpromptsubmit.json"},
		{"PreToolUse", "pretooluse.json"},
		{"PostToolUse", "posttooluse.json"},
	} {
		payload, err := os.ReadFile(filepath.Join("..", "..", "ingest", "hook", "testdata", f.file))
		require.NoError(t, err)
		warn := &bytes.Buffer{}
		forward(warn, discovery, f.typ, bytes.NewReader(payload))
		require.Empty(t, warn.String())
	}

	cancel()
	require.NoError(t, <-errc)

	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(obs), 4)
}

func TestDaemonCommandWiring(t *testing.T) {
	dir := t.TempDir()
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	root := newRootCmd()
	root.SetArgs([]string{"daemon", "--db", filepath.Join(dir, "g.db"), "--discovery", discovery})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-done)
}

func TestDaemonCommandDefaultDiscovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(dir, "d.json"))
	ctx, cancel := context.WithCancel(context.Background())
	root := newRootCmd()
	root.SetArgs([]string{"daemon", "--db", filepath.Join(dir, "g.db")})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	awaitHealthz(t, readAddr(t, filepath.Join(dir, "d.json")))
	cancel()
	require.NoError(t, <-done)
}
