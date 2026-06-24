package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/export/otlp"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

type fakeSpanExporter struct {
	mu     sync.Mutex
	spans  []sdktrace.ReadOnlySpan
	closed bool
}

func (f *fakeSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spans = append(f.spans, spans...)
	return nil
}

func (f *fakeSpanExporter) Shutdown(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeSpanExporter) spanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.spans)
}

type fakeExporter struct {
	mu        sync.Mutex
	snapshots [][]*model.Node
	deltas    []cdc.GraphDelta
	flushes   []string
	shutdowns int
}

func (f *fakeExporter) Name() string { return "fake" }

func (f *fakeExporter) SnapshotState(_ context.Context, nodes []*model.Node, _ []*model.Edge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = append(f.snapshots, nodes)
	return nil
}

func (f *fakeExporter) ApplyDelta(_ context.Context, d cdc.GraphDelta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deltas = append(f.deltas, d)
	return nil
}

func (f *fakeExporter) FlushRun(_ context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes = append(f.flushes, runID)
	return nil
}

func (f *fakeExporter) Shutdown(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdowns++
	return nil
}

func (f *fakeExporter) snapshotCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.snapshots)
}

func (f *fakeExporter) deltaCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deltas)
}

func loopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestServeStartsExporterConsumer(t *testing.T) {
	fake := &fakeSpanExporter{}
	orig := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = orig })

	d := New(tempStore(t))
	fixedExecID(d)
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.exporterConsumers) > 0
	}, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))
	require.Eventually(t, func() bool { return fake.spanCount() > 0 }, 3*time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-errc)
}

func TestServeExporterSnapshotsExistingGraphs(t *testing.T) {
	fake := &fakeSpanExporter{}
	orig := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = orig })

	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.exporterConsumers) > 0
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-errc)
}

func TestServeSelfLoopEndpointSkipsExporter(t *testing.T) {
	var called atomic.Bool
	orig := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		called.Store(true)
		return nil, errors.New("otlp exporter: endpoint targets the daemon's own receiver — self-loop refused")
	}
	t.Cleanup(func() { newExporterFn = orig })

	d := New(tempStore(t))
	fixedExecID(d)
	httpLn, grpcLn := loopback(t), loopback(t)
	d.SetOTLPEndpoint("grpc://" + grpcLn.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok2") }()
	require.Eventually(t, called.Load, 2*time.Second, 5*time.Millisecond)
	d.mu.Lock()
	consumerNil := len(d.exporterConsumers) == 0
	d.mu.Unlock()
	assert.True(t, consumerNil)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"post-skip"}`)))
	cancel()
	require.NoError(t, <-errc)
}

func TestExporterConsumerLoopExitsOnChannelClose(t *testing.T) {
	exited := make(chan struct{})
	origHook := consumerLoopExitHook
	consumerLoopExitHook = func() { close(exited) }
	t.Cleanup(func() { consumerLoopExitHook = origHook })

	fake := &fakeSpanExporter{}
	orig := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = orig })

	d := New(tempStore(t))
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())
	d.mu.Lock()
	consumer := d.exporterConsumers[0]
	d.mu.Unlock()
	require.NotNil(t, consumer)
	d.bus.Unsubscribe(consumer)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer loop did not exit after channel close")
	}
}

func TestStartExporterFlushesTerminalRunsOnAttach(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/g.db"

	s1, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	d1 := New(s1)
	fixedExecID(d1)
	require.NoError(t, d1.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d1.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d1.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))
	require.NoError(t, s1.Close())

	s2, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())

	fake := &fakeSpanExporter{}
	orig := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = orig })

	d2.SetOTLPEndpoint("grpc://collector.example:4317")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d2.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	assert.Positive(t, fake.spanCount(), "terminal run spans must be exported on attach")
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errReadCloser) Close() error             { return nil }

func authedReq(target, token string, body io.Reader) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, body)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestHandlerHookSuccess(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, authedReq("/hook/SessionStart", "tok", strings.NewReader(`{"session_id":"s1"}`)))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	require.NotNil(t, d.graphs["exec1"].Nodes[model.SessionNodeID("exec1")])
}

func TestHandlerHookUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, authedReq("/hook/SessionStart", "wrong", strings.NewReader(`{}`)))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, d.graphs)
}

func TestHandlerHookMissingToken(t *testing.T) {
	d := New(tempStore(t))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, authedReq("/hook/SessionStart", "", strings.NewReader(`{}`)))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, d.graphs)
}

func TestHandlerHealthzOpen(t *testing.T) {
	d := New(tempStore(t))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandlerBodyReadError(t *testing.T) {
	d := New(tempStore(t))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, authedReq("/hook/SessionStart", "tok", errReadCloser{}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlerIngestError(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, authedReq("/hook/PreToolUse", "tok", strings.NewReader("{not json}")))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestServeGraceful(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	ln, err := ListenLoopback()
	require.NoError(t, err)
	addr := ln.Addr().String()
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, ln, grpcLn, "tok") }()

	require.Eventually(t, func() bool {
		r, e := http.Get("http://" + addr + "/healthz")
		if e != nil {
			return false
		}
		_ = r.Body.Close()
		return r.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/hook/SessionStart", strings.NewReader(`{"session_id":"s1"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	cancel()
	require.NoError(t, <-errc)
}

func TestServeListenerError(t *testing.T) {
	d := New(tempStore(t))
	ln, err := ListenLoopback()
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	grpcLn := loopbackListener(t)
	require.Error(t, d.Serve(context.Background(), ln, grpcLn, "tok"))
}

func TestReapLoopStopsOnContextCancel(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(time.Millisecond)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.reapLoop(ctx); close(done) }()
	require.Eventually(t, func() bool {
		open, err := s.ListOpenRuns()
		return err == nil && len(open) == 0
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reapLoop did not stop")
	}
}

func TestReapLoopLogsReapError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	d.SetReaperWindow(time.Millisecond)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.reapLoop(ctx); close(done) }()
	require.Eventually(t, func() bool { return s.appendCount() >= 2 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reapLoop did not stop")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var m Metrics
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
	assert.Equal(t, 1, m.OpenRuns)
}

func TestOTLPHTTPEndpoint(t *testing.T) {
	d := New(tempStore(t))
	req := makeOTLPToolReq("s1", "t1", "Bash")
	body, err := proto.Marshal(req)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer tok")
	r.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-protobuf", rec.Header().Get("Content-Type"))
	var resp collectorv1.ExportTraceServiceResponse
	require.NoError(t, proto.Unmarshal(rec.Body.Bytes(), &resp))
}

func TestOTLPHTTPUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(""))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOTLPHTTPBadBody(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader("not-proto-garbage"))
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOTLPHTTPBodyReadError(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/traces", errReadCloser{})
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStreamJSONHTTPEndpoint(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader(`{"type":"system","subtype":"init","session_id":"s1","model":"m"}
{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}
`)
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, time.Second, 10*time.Millisecond)
}

func TestStreamJSONHTTPUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", strings.NewReader(""))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStreamJSONHTTPBlankLinesSkipped(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader("\n\n{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"s1\"}\n\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, d.GraphsForTest(), 1)
}

func TestStreamJSONHTTPBodyReadError(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", errReadCloser{})
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStreamJSONHTTPNonJSONLineSessionID(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader("not-json\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStreamJSONHTTPThreadsSessionAcrossLines(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	body := strings.NewReader(`{"type":"system","subtype":"init","session_id":"s1","model":"m"}
{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"result","total_cost_usd":0.01}
`)
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, time.Second, 10*time.Millisecond)
	execID := d.execForTest("s1")
	assert.Equal(t, "exec1", execID)
	assert.Empty(t, d.execForTest(""))
	g := d.GraphsForTest()[execID]
	require.NotNil(t, g)
	require.NotNil(t, g.Nodes[model.SessionNodeID(execID)])
	require.NotNil(t, g.Nodes[model.ToolCallID(execID, "t1")])
}

type panicWriter struct{ http.ResponseWriter }

func (panicWriter) WriteHeader(int) { panic("write-header-panic") }

func TestStreamJSONHTTPHandlerPanicRecovered(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader("")
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	pw := panicWriter{httptest.NewRecorder()}
	d.handleStreamJSON(pw, r)
}

type tailLoadErrStore struct{ store.Store }

func (s *tailLoadErrStore) LoadTailCursors() ([]model.TailCursor, error) {
	return nil, errors.New("load cursors failed")
}

func (s *tailLoadErrStore) UpsertTailCursor(model.TailCursor) error { return nil }

func TestServeStartsTailerIngestsTranscript(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s9.jsonl")
	require.NoError(t, os.WriteFile(p, []byte(`{"type":"assistant","sessionId":"s9","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_9","name":"Bash","input":{"command":"ls"}}]}}`+"\n"), 0o600))
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetTranscriptDir(dir)
	origTick := tailTick
	tailTick = 10 * time.Millisecond
	t.Cleanup(func() { tailTick = origTick })
	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()
	require.Eventually(t, func() bool {
		return d.execForTest("s9") != ""
	}, 3*time.Second, 10*time.Millisecond)
	exec := d.execForTest("s9")
	require.Eventually(t, func() bool {
		g := d.GraphsForTest()[exec]
		return g != nil && g.Nodes[model.ToolCallID(exec, "toolu_9")] != nil
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	<-errc
}

func TestTailLoopDisabledWhenNoDir(t *testing.T) {
	d := New(tempStore(t))
	d.tailLoop(context.Background())
}

func TestTailLoopLoadError(t *testing.T) {
	dir := t.TempDir()
	d := New(&tailLoadErrStore{Store: tempStore(t)})
	d.SetTranscriptDir(dir)
	d.tailLoop(context.Background())
}

func TestStartExporterAttachesTwoConsumersWhenBothConfigured(t *testing.T) {
	fake := &fakeSpanExporter{}
	origOTLP := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = origOTLP })

	fakeExp := &fakeExporter{}
	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d := New(tempStore(t))
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	d.mu.Lock()
	n := len(d.exporterConsumers)
	d.mu.Unlock()
	assert.Equal(t, 2, n)
	assert.Len(t, d.ExporterConsumersForTest(), 2)
}

func TestStartExporterPostgresErrorDisablesOnlyPostgres(t *testing.T) {
	fake := &fakeSpanExporter{}
	origOTLP := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = origOTLP })

	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return nil, errors.New("connection refused")
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d := New(tempStore(t))
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	d.mu.Lock()
	n := len(d.exporterConsumers)
	d.mu.Unlock()
	assert.Equal(t, 1, n)
}

func TestWiringPostgresDSNAttachesExporterAndReceivesDelta(t *testing.T) {
	fakeExp := &fakeExporter{}
	orig := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = orig })

	d := New(tempStore(t))
	fixedExecID(d)
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	require.Eventually(t, func() bool {
		return len(d.ExporterConsumersForTest()) > 0
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.Eventually(t, func() bool {
		return fakeExp.deltaCount() > 0
	}, 3*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
}

func TestWiringEmptyPostgresDSNAttachesNothing(t *testing.T) {
	called := false
	orig := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		called = true
		return &fakeExporter{}, nil
	}
	t.Cleanup(func() { newPostgresFn = orig })

	d := New(tempStore(t))
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	assert.False(t, called)
	assert.Empty(t, d.ExporterConsumersForTest())
}

func TestWiringOTLPAndPostgresRunTogether(t *testing.T) {
	fake := &fakeSpanExporter{}
	origOTLP := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = origOTLP })

	fakeExp := &fakeExporter{}
	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d := New(tempStore(t))
	fixedExecID(d)
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	require.Eventually(t, func() bool {
		return len(d.ExporterConsumersForTest()) == 2
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))

	require.Eventually(t, func() bool { return fake.spanCount() > 0 }, 3*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return fakeExp.deltaCount() > 0 }, 3*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
}

func TestSnapshotReceivedByPostgresExporterOnAttach(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	fakeExp := &fakeExporter{}
	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	assert.Positive(t, fakeExp.snapshotCount(), "snapshot must be called at attach")
}

func TestWiringNeo4jURIAttachesExporterAndReceivesDelta(t *testing.T) {
	fakeExp := &fakeExporter{}
	orig := newNeo4jFn
	newNeo4jFn = func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newNeo4jFn = orig })

	d := New(tempStore(t))
	fixedExecID(d)
	d.SetNeo4j("bolt://localhost:7687", "neo4j", "pw")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	require.Eventually(t, func() bool {
		return len(d.ExporterConsumersForTest()) > 0
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.Eventually(t, func() bool {
		return fakeExp.deltaCount() > 0
	}, 3*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
}

func TestWiringEmptyNeo4jURIAttachesNothing(t *testing.T) {
	called := false
	orig := newNeo4jFn
	newNeo4jFn = func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
		called = true
		return &fakeExporter{}, nil
	}
	t.Cleanup(func() { newNeo4jFn = orig })

	d := New(tempStore(t))
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	assert.False(t, called)
	assert.Empty(t, d.ExporterConsumersForTest())
}

func TestNewNeo4jFnWrapperLine(t *testing.T) {
	orig := newNeo4jFn
	t.Cleanup(func() { newNeo4jFn = orig })
	exp, err := newNeo4jFn(context.Background(), "bolt://localhost:7687", "neo4j", "pw")
	require.NoError(t, err)
	require.NotNil(t, exp)
}

func TestStartExporterNeo4jErrorDisablesOnlyNeo4j(t *testing.T) {
	fake := &fakeSpanExporter{}
	origOTLP := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = origOTLP })

	origNeo4j := newNeo4jFn
	newNeo4jFn = func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
		return nil, errors.New("connection refused")
	}
	t.Cleanup(func() { newNeo4jFn = origNeo4j })

	d := New(tempStore(t))
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	d.SetNeo4j("bolt://localhost:7687", "neo4j", "pw")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	d.mu.Lock()
	n := len(d.exporterConsumers)
	d.mu.Unlock()
	assert.Equal(t, 1, n)
}

func TestAuthedAllowQueryHeaderValid(t *testing.T) {
	d := New(tempStore(t))
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := d.authedAllowQuery("mytoken", next)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r.Header.Set("Authorization", "Bearer mytoken")
	handler(rec, r)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthedAllowQueryParamValid(t *testing.T) {
	d := New(tempStore(t))
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := d.authedAllowQuery("mytoken", next)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe?token=mytoken", nil)
	handler(rec, r)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthedAllowQueryBothAbsent401(t *testing.T) {
	d := New(tempStore(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := d.authedAllowQuery("mytoken", next)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	handler(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthedAllowQueryWrongHeader401(t *testing.T) {
	d := New(tempStore(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := d.authedAllowQuery("mytoken", next)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r.Header.Set("Authorization", "Bearer wrongtoken")
	handler(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthedAllowQueryWrongParam401(t *testing.T) {
	d := New(tempStore(t))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := d.authedAllowQuery("mytoken", next)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe?token=wrongtoken", nil)
	handler(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTranscriptRouteBearer(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", strings.NewReader(""))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTranscriptRouteRegistered(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", strings.NewReader(""))
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStaticHandlerServesRoot(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
}

func TestStaticHandlerDoesNotShadowHealthz(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStaticHandlerDoesNotShadowMetrics(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStaticHandlerDoesNotShadowSubscribe(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/subscribe")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestStaticHandlerMissingAsset404(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/no-such-file.xyz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSSEQueryTokenE2E(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	addr := httpLn.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/v1/subscribe?token=tok", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	events := make(chan map[string]any, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			events <- ev
		}
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev["kind"].(string) == "node_upsert" {
				cancel()
				<-errc
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for node_upsert via ?token= auth")
		}
	}
}

func TestStaticHandlerFullSmokeIndex(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `id="app"`)
	assert.Contains(t, string(body), "<title>Catacomb</title>")
	assert.Contains(t, string(body), `type="module"`)
}

func TestStaticHandlerSmokeHashedAssetResolves(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/assets/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.NotEqual(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestStaticHandlerUnknownAsset404(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/does-not-exist.png")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
