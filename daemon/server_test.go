package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/realkarych/catacomb/export/otlp"
	"github.com/realkarych/catacomb/model"
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
		return d.exporterConsumer != nil
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
		return d.exporterConsumer != nil
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-errc)
}

func TestServeSelfLoopEndpointSkipsExporter(t *testing.T) {
	d := New(tempStore(t))
	httpLn, grpcLn := loopback(t), loopback(t)
	d.SetOTLPEndpoint("grpc://" + grpcLn.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok2") }()
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.exporterConsumer == nil
	}, time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-errc)
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
