package daemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

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
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, ln, "tok") }()

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
	require.Error(t, d.Serve(context.Background(), ln, "tok"))
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
