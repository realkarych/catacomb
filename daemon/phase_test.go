package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func getPhase(t *testing.T, srv *httptest.Server, hash, phaseSel string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/sessions/" + hash + "/phase/" + phaseSel + "?token=testtoken")
	require.NoError(t, err)
	return resp
}

func TestHandlePhaseFocus_OK(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
}

func TestHandlePhaseFocus_PhaseNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "ghost")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePhaseFocus_SessionNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "nope", "phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePhaseFocus_InvalidSelector(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1,x")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandlePhaseFocus_EmitsNodesAndEdges(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	var tick int
	nowFn = func() time.Time {
		tick++
		if tick%2 == 1 {
			return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		return time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { nowFn = time.Now })
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "end"}))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	var hasNode, hasEdge bool
	for _, ev := range evs {
		if ev.Node != nil {
			hasNode = true
		}
		if ev.Edge != nil {
			hasEdge = true
		}
	}
	assert.True(t, hasNode, "expected at least one node event")
	assert.True(t, hasEdge, "expected at least one edge event")
}

func TestHandlePhaseFocus_WideWindowIncludesTool(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	var tick int
	nowFn = func() time.Time {
		tick++
		if tick%2 == 1 {
			return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		return time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { nowFn = time.Now })
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "end"}))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	wantID := model.ToolCallID("exec1", "t1")
	var found bool
	for _, ev := range evs {
		if ev.Node != nil && ev.Node.ID == wantID {
			found = true
		}
	}
	assert.True(t, found, "expected tool node %s in wide-window phase events", wantID)
}

func TestHandlePhaseFocus_NarrowWindowExcludesTool(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	var tick int
	nowFn = func() time.Time {
		tick++
		if tick%2 == 1 {
			return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		return time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { nowFn = time.Now })
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "end"}))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	wantID := model.ToolCallID("exec1", "t1")
	for _, ev := range evs {
		if ev.Node != nil {
			assert.NotEqual(t, wantID, ev.Node.ID, "tool node must not appear in narrow-window phase events")
		}
	}
}
