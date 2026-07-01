package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/repro"
	"github.com/realkarych/catacomb/store"
)

type failSinceStore struct{}

func (f *failSinceStore) Persist([]model.Observation, []*model.Node, []*model.Edge) error {
	return nil
}

func (f *failSinceStore) AppendDeltas(model.Observation, []cdc.GraphDelta) error {
	return nil
}
func (f *failSinceStore) MaxSeq() (uint64, error) { return 0, nil }
func (f *failSinceStore) ObservationsSince(uint64) ([]model.Observation, error) {
	return nil, errors.New("since")
}
func (f *failSinceStore) UpsertRun(model.Run) error               { return nil }
func (f *failSinceStore) ListOpenRuns() ([]model.Run, error)      { return nil, nil }
func (f *failSinceStore) Runs() ([]model.Run, error)              { return nil, nil }
func (f *failSinceStore) Quarantine(model.QuarantineRecord) error { return nil }
func (f *failSinceStore) QuarantineCount() (int64, error)         { return 0, nil }
func (f *failSinceStore) ObservationsForExecution(string) ([]model.Observation, error) {
	return nil, nil
}
func (f *failSinceStore) UpsertTailCursor(model.TailCursor) error                    { return nil }
func (f *failSinceStore) LoadTailCursors() ([]model.TailCursor, error)               { return nil, nil }
func (f *failSinceStore) Close() error                                               { return nil }
func (f *failSinceStore) UpsertAnnotation(model.Annotation) error                    { return nil }
func (f *failSinceStore) AnnotationsForExecution(string) ([]model.Annotation, error) { return nil, nil }
func (f *failSinceStore) MoveAnnotations(string, string, string) error               { return nil }

func openFailSince(config.StoreConfig) (store.Store, error) {
	return &failSinceStore{}, nil
}

func testDaemonDeps() daemonDeps {
	return daemonDeps{
		openStore:  store.Open,
		listen:     daemon.ListenLoopback,
		listenGRPC: daemon.ListenLoopback,
		newToken:   daemon.NewToken,
	}
}

func testDaemonParams(t *testing.T) daemonParams {
	t.Helper()
	return daemonParams{
		store:         config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "g.db")}},
		discoveryPath: filepath.Join(t.TempDir(), "d.json"),
		reaperWindow:  30 * time.Minute,
		maxShards:     4096,
	}
}

func TestRunDaemonOpenError(t *testing.T) {
	deps := testDaemonDeps()
	deps.openStore = func(config.StoreConfig) (store.Store, error) { return nil, errors.New("open") }
	err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
	require.Error(t, err)
}

func TestRunDaemonListenError(t *testing.T) {
	deps := testDaemonDeps()
	deps.listen = func() (net.Listener, error) { return nil, errors.New("listen") }
	err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
	require.Error(t, err)
}

func TestRunDaemonDiscoveryError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
	p := testDaemonParams(t)
	p.discoveryPath = filepath.Join(dir, "afile", "d.json")
	err := runDaemonWith(context.Background(), testDaemonDeps(), p)
	require.Error(t, err)
}

func TestRunDaemonRefusesWhenAlreadyRunning(t *testing.T) {
	p := testDaemonParams(t)
	require.NoError(t, daemon.WriteDiscovery(p.discoveryPath, daemon.Discovery{
		Addr: "127.0.0.1:1", Token: "t", Pid: 4242, StartToken: 99,
	}))
	orig := daemonOwned
	daemonOwned = func(daemon.Discovery) bool { return true }
	t.Cleanup(func() { daemonOwned = orig })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runDaemonWith(ctx, testDaemonDeps(), p)
	require.ErrorIs(t, err, ErrDaemonAlreadyRunning)
}

func TestRunDaemonProceedsWhenDiscoveryStale(t *testing.T) {
	p := testDaemonParams(t)
	require.NoError(t, daemon.WriteDiscovery(p.discoveryPath, daemon.Discovery{
		Addr: "127.0.0.1:1", Token: "t", Pid: 4242, StartToken: 99,
	}))
	orig := daemonOwned
	daemonOwned = func(daemon.Discovery) bool { return false }
	t.Cleanup(func() { daemonOwned = orig })
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	readAddr(t, p.discoveryPath)
	cancel()
	<-errc
}

func TestRunDaemonWritesStartToken(t *testing.T) {
	p := testDaemonParams(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	var tok int64
	require.Eventually(t, func() bool {
		d, err := daemon.ReadDiscovery(p.discoveryPath)
		if err != nil {
			return false
		}
		tok = d.StartToken
		return true
	}, 30*time.Second, 10*time.Millisecond)
	cancel()
	<-errc
	assert.NotZero(t, tok)
}

func TestRunDaemonRecoverError(t *testing.T) {
	deps := testDaemonDeps()
	deps.openStore = openFailSince
	err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
	require.Error(t, err)
}

func TestRunDaemonNewTokenError(t *testing.T) {
	deps := testDaemonDeps()
	deps.newToken = func() (string, error) { return "", errors.New("token") }
	err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
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
	}, 30*time.Second, 10*time.Millisecond)
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
	}, 30*time.Second, 10*time.Millisecond)
	return addr
}

func boolPtr(b bool) *bool { return &b }

func TestSinkTypeStrings(t *testing.T) {
	assert.Nil(t, sinkTypeStrings(nil))
	assert.Nil(t, sinkTypeStrings([]config.Sink{}))
	got := sinkTypeStrings([]config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"},
		{Type: config.SinkPostgres, DSN: "postgres://secret"},
	})
	assert.Equal(t, []string{config.SinkOTLP, config.SinkPostgres}, got)
}

func TestEnabledSourceNames(t *testing.T) {
	tr, fa := boolPtr(true), boolPtr(false)
	got := enabledSourceNames(config.SourcesConfig{
		Hooks:      config.SourceToggle{Enabled: tr},
		Otel:       config.SourceToggle{Enabled: fa},
		StreamJSON: config.SourceToggle{Enabled: nil},
		JSONL:      config.JSONLSource{Enabled: tr},
	})
	assert.Equal(t, []string{"hooks", "stream_json", "jsonl"}, got)
}

func TestEnabledSourceNamesAllDefault(t *testing.T) {
	got := enabledSourceNames(config.SourcesConfig{})
	assert.Equal(t, []string{"hooks", "otel", "stream_json", "jsonl"}, got)
}

func TestRunDaemonWithSummaryFields(t *testing.T) {
	discFile := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discFile
	p.reaperWindow = 15 * time.Minute
	p.maxShards = 512
	p.sinks = []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"},
	}
	enabled := true
	p.sources = config.SourcesConfig{
		Hooks: config.SourceToggle{Enabled: &enabled},
		Otel:  config.SourceToggle{Enabled: boolPtr(false)},
	}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discFile))
	disc, err := daemon.ReadDiscovery(discFile)
	require.NoError(t, err)
	assert.Equal(t, config.BackendSQLite, disc.StoreBackend)
	assert.Equal(t, []string{config.SinkOTLP}, disc.SinkTypes)
	assert.Contains(t, disc.SourcesEnabled, "hooks")
	assert.NotContains(t, disc.SourcesEnabled, "otel")
	assert.Equal(t, "15m0s", disc.ReaperWindow)
	assert.Equal(t, 512, disc.MaxShards)
	cancel()
	require.NoError(t, <-errc)
}

func readToken(t *testing.T, discovery string) string {
	t.Helper()
	var tok string
	require.Eventually(t, func() bool {
		d, err := daemon.ReadDiscovery(discovery)
		if err != nil {
			return false
		}
		tok = d.Token
		return tok != ""
	}, 30*time.Second, 10*time.Millisecond)
	return tok
}

func TestRunDaemonWithSinksFromParams(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.sinks = []config.Sink{{Type: config.SinkJSONL, Path: filepath.Join(t.TempDir(), "out.jsonl")}}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonWithLegacyOTLPFlagMergedAsSink(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.otlpEndpoint = "grpc://collector.example:4317"
	p.otlpProject = "test-proj"
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonSourcesFromParams(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.sources = config.SourcesConfig{
		Hooks:      config.SourceToggle{Enabled: boolPtr(true)},
		Otel:       config.SourceToggle{Enabled: boolPtr(false)},
		StreamJSON: config.SourceToggle{Enabled: boolPtr(true)},
		JSONL:      config.JSONLSource{Enabled: boolPtr(false)},
	}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	addr := readAddr(t, discovery)
	awaitHealthz(t, addr)
	req, err := http.NewRequest("POST", "http://"+addr+"/v1/traces", strings.NewReader(""))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+readToken(t, discovery))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonTranscriptFlagOverridesSource(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.transcriptDir = t.TempDir()
	p.transcriptExclude = []string{"*.tmp"}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestDaemonCommandTranscriptFlagsSetSource(t *testing.T) {
	t.Setenv("CATACOMB_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	dir := t.TempDir()
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	root := newRootCmd()
	root.SetArgs([]string{
		"daemon", "--db", filepath.Join(dir, "g.db"), "--discovery", discovery,
		"--transcript-dir", t.TempDir(), "--transcript-exclude", "*.tmp",
	})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-done)
}

func TestDaemonEndToEnd(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "g.db")
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.store = config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: dbPath}}
	p.discoveryPath = discovery
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
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
		forward(warn, discovery, f.typ, bytes.NewReader(payload), "")
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
	t.Setenv("CATACOMB_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
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
	t.Setenv("CATACOMB_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
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

func TestDaemonCommandReaperWindowFlag(t *testing.T) {
	t.Setenv("CATACOMB_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	dir := t.TempDir()
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	root := newRootCmd()
	root.SetArgs([]string{"daemon", "--db", filepath.Join(dir, "g.db"), "--discovery", discovery, "--reaper-window", "1h"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-done)
}

func TestDaemonCommandMaxShardsFlag(t *testing.T) {
	t.Setenv("CATACOMB_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	dir := t.TempDir()
	discovery := filepath.Join(dir, "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	root := newRootCmd()
	root.SetArgs([]string{"daemon", "--db", filepath.Join(dir, "g.db"), "--discovery", discovery, "--max-shards", "128"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-done)
}

func TestRunDaemonWithGRPCListenError(t *testing.T) {
	deps := testDaemonDeps()
	deps.listenGRPC = func() (net.Listener, error) { return nil, errors.New("grpc listen") }
	err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
	require.Error(t, err)
}

func TestRunDaemonDiscoveryHasGRPCAddr(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	var grpcAddr string
	require.Eventually(t, func() bool {
		d, err := daemon.ReadDiscovery(discovery)
		if err != nil || d.GRPCAddr == "" {
			return false
		}
		grpcAddr = d.GRPCAddr
		return true
	}, 30*time.Second, 10*time.Millisecond)
	require.NotEmpty(t, grpcAddr)
	cancel()
	require.NoError(t, <-errc)
}

func TestDaemonOTLPFlagRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("otlp-export-endpoint")
	require.NotNil(t, f)
	require.Equal(t, "", f.DefValue)
}

func TestDaemonOTLPProjectFlagRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("otlp-export-project")
	require.NotNil(t, f)
	require.Equal(t, "catacomb", f.DefValue)
}

func TestRunDaemonWithOTLPEndpoint(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.otlpEndpoint = "grpc://collector.example:4317"
	p.otlpProject = "phoenix-demo"
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonWithTranscriptDir(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	disc := filepath.Join(t.TempDir(), "d.json")
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = disc
	p.reaperWindow = time.Minute
	p.maxShards = 16
	p.transcriptDir = t.TempDir()
	p.transcriptExclude = []string{"x-*.jsonl"}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(disc)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	<-errc
}

func TestTranscriptDirFlag(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("transcript-dir")
	require.NotNil(t, f)
	require.Equal(t, "", f.DefValue)
}

func TestTranscriptExcludeFlag(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("transcript-exclude")
	require.NotNil(t, f)
}

func TestNeo4jFlagsRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	for _, name := range []string{"neo4j-export-uri", "neo4j-export-user", "neo4j-export-password"} {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f, "flag --%s must exist", name)
		require.Equal(t, "", f.DefValue, "flag --%s default must be empty", name)
	}
}

func TestRunDaemonWithNeo4jURISet(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.neo4jURI = "bolt://localhost:7687"
	p.neo4jUser = "neo4j"
	p.neo4jPassword = "pw"
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonWithPostgresDSN(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.postgresDSN = "postgres://user:pass@localhost:5432/db"
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestAllowPayloadAccessFlagRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("allow-payload-access")
	require.NotNil(t, f)
	require.Equal(t, "false", f.DefValue)
}

func TestAllowAnnotationsFlagRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("allow-annotations")
	require.NotNil(t, f)
	require.Equal(t, "false", f.DefValue)
}

func TestRunDaemonWithAllowPayloadAccessTrue(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.allowPayloadAccess = true
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonDiscoveryHasScope(t *testing.T) {
	db := filepath.Join(t.TempDir(), "g.db")
	transcripts := t.TempDir()
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.store = config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: db}}
	p.discoveryPath = discovery
	p.transcriptDir = transcripts
	p.allowPayloadAccess = true
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	var d daemon.Discovery
	require.Eventually(t, func() bool {
		disc, err := daemon.ReadDiscovery(discovery)
		if err != nil || disc.TranscriptDir == "" {
			return false
		}
		d = disc
		return true
	}, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, transcripts, d.TranscriptDir)
	assert.Equal(t, db, d.DBPath)
	assert.True(t, d.AllowPayloadAccess)
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonConfigTranscriptDirInDiscovery(t *testing.T) {
	transcripts := t.TempDir()
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	enabled := true
	p.sources = config.SourcesConfig{JSONL: config.JSONLSource{Enabled: &enabled, TranscriptDir: transcripts}}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	assert.Equal(t, transcripts, d.TranscriptDir)
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonRemovesDiscoveryOnShutdown(t *testing.T) {
	discPath := filepath.Join(t.TempDir(), "daemon.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := testDaemonParams(t)
	p.discoveryPath = discPath
	err := runDaemonWith(ctx, testDaemonDeps(), p)
	require.NoError(t, err)
	_, statErr := os.Stat(discPath)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunDaemonDiscoveryHasPidAndStartedAt(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	var d daemon.Discovery
	require.Eventually(t, func() bool {
		disc, err := daemon.ReadDiscovery(discovery)
		if err != nil || disc.Pid == 0 {
			return false
		}
		d = disc
		return true
	}, 30*time.Second, 10*time.Millisecond)
	require.NotZero(t, d.Pid)
	_, err := time.Parse(time.RFC3339, d.StartedAt)
	require.NoError(t, err)
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonMemoryBackendServesAndDiscoveryDBEmpty(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := daemonParams{store: config.StoreConfig{Backend: config.BackendMemory}, discoveryPath: discovery, reaperWindow: 30 * time.Minute, maxShards: 4096}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	assert.Equal(t, "", d.DBPath)
	cancel()
	require.NoError(t, <-errc)
}

func TestStoreDBPath(t *testing.T) {
	assert.Equal(t, "/x.db", storeDBPath(config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: "/x.db"}}))
	assert.Equal(t, "", storeDBPath(config.StoreConfig{Backend: config.BackendMemory}))
}

func TestDaemonConfigFlagRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("config")
	require.NotNil(t, f)
	assert.Equal(t, "", f.DefValue)
}

func TestDaemonDBFlagDefaultEmpty(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("db")
	require.NotNil(t, f)
	assert.Equal(t, "", f.DefValue)
}

func TestResolveDiscoveryEmpty(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", "")
	got := resolveDiscovery("")
	assert.NotEmpty(t, got)
}

func TestDaemonCommandHomeError(t *testing.T) {
	orig := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = orig })
	root := newRootCmd()
	root.SetArgs([]string{"daemon"})
	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
}

func TestRunDaemonConfigPathInDiscovery(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.configPath = "/etc/catacomb/custom.yaml"
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	assert.Equal(t, "/etc/catacomb/custom.yaml", d.ConfigPath)
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonStartupLogLine(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.postgresDSN = "postgres://user:secret@host/db"
	p.sinks = []config.Sink{{Type: config.SinkPostgres, DSN: "postgres://user:secret@host/db"}}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)

	line := buf.String()
	assert.Contains(t, line, "catacomb daemon started")
	assert.Contains(t, line, "addr=")
	assert.NotContains(t, line, "secret")
	assert.NotContains(t, line, "postgres://")
}

func TestDaemonCommandConfigError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("store:\n  nope: 1\n"), 0o600))
	t.Setenv("CATACOMB_CONFIG", bad)
	root := newRootCmd()
	root.SetArgs([]string{"daemon"})
	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
}

func TestFirstOTLPSinkAbsent(t *testing.T) {
	ep, proj := firstOTLPSink(nil)
	assert.Equal(t, "", ep)
	assert.Equal(t, "", proj)
}

func TestFirstOTLPSinkPresent(t *testing.T) {
	sinks := []config.Sink{
		{Type: config.SinkPostgres, DSN: "postgres://host/db"},
		{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "myproj"},
	}
	ep, proj := firstOTLPSink(sinks)
	assert.Equal(t, "grpc://host:4317", ep)
	assert.Equal(t, "myproj", proj)
}

func TestFirstOTLPSinkMultipleReturnsFirst(t *testing.T) {
	sinks := []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://first:4317", Project: "proj-a"},
		{Type: config.SinkOTLP, Endpoint: "grpc://second:4317", Project: "proj-b"},
	}
	ep, proj := firstOTLPSink(sinks)
	assert.Equal(t, "grpc://first:4317", ep)
	assert.Equal(t, "proj-a", proj)
}

func TestDedupSinksNoDuplicates(t *testing.T) {
	sinks := []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"},
		{Type: config.SinkPostgres, DSN: "postgres://host/db"},
		{Type: config.SinkJSONL, Path: "/var/log/out.jsonl"},
	}
	got := dedupSinks(sinks)
	assert.Equal(t, sinks, got)
}

func TestDedupSinksDuplicateRemoved(t *testing.T) {
	configSink := config.Sink{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "myproj"}
	legacySink := config.Sink{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "myproj"}
	sinks := []config.Sink{configSink, legacySink}
	got := dedupSinks(sinks)
	require.Len(t, got, 1)
	assert.Equal(t, configSink, got[0])
}

func TestReproConfigFromConfigOnlyOTLPSink(t *testing.T) {
	sinks := []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://collector:4317", Project: "phoenix"},
	}
	transcriptDir := "/home/user/.claude/projects"
	sources := config.SourcesConfig{
		JSONL: config.JSONLSource{Enabled: boolPtr(true), TranscriptDir: transcriptDir},
	}
	ep, proj := firstOTLPSink(dedupSinks(sinks))
	got := repro.Config{OTLPEndpoint: ep, OTLPProject: proj, TranscriptDir: sources.JSONL.TranscriptDir}
	assert.Equal(t, repro.Config{
		OTLPEndpoint:  "grpc://collector:4317",
		OTLPProject:   "phoenix",
		TranscriptDir: transcriptDir,
	}, got)
}
