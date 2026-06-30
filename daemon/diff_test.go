package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/diff"
	"github.com/realkarych/catacomb/model"
)

func TestHandleDiff_Identical(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s2","tool_name":"Bash","tool_use_id":"t2","tool_input":{}}`)))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=s1&b=s2&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Unchanged)
	assert.Empty(t, result.Changed)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
}

func TestHandleDiff_Added(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s2","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=s1&b=s2&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Added)
	assert.Empty(t, result.Removed)
}

func TestHandleDiff_Removed(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=s1&b=s2&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Removed)
	assert.Empty(t, result.Added)
}

func TestHandleDiff_MissingParam(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?b=s2&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp2, err := http.Get(srv.URL + "/v1/diff?a=s1&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
}

func TestHandleDiff_NotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=ghost&b=s1&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp2, err := http.Get(srv.URL + "/v1/diff?a=s1&b=ghost&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

func TestHandleDiff_Unauthorized(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=s1&b=s2")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHandleDiff_ChangedArgsPayloadGate_AccessOff(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"ls"}}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s2","tool_name":"Bash","tool_use_id":"t2","tool_input":{"command":"pwd"}}`)))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=s1&b=s2&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "pwd")
	assert.NotContains(t, string(body), "ls")
}

func TestHandleDiff_ChangedArgsPayloadGate_AccessOn(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"ls"}}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s2","tool_name":"Bash","tool_use_id":"t2","tool_input":{"command":"pwd"}}`)))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/diff?a=s1&b=s2&token=testtoken")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	var found bool
	for _, cs := range result.Changed {
		if cs.Deltas.Args != nil {
			found = true
			assert.Contains(t, cs.Deltas.Args.Before+cs.Deltas.Args.After, "pwd")
		}
	}
	assert.True(t, found, "expected a changed step with arg deltas")
}

func advancingClock(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var tick int64
	nowFn = func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	}
	t.Cleanup(func() { nowFn = time.Now })
}

func phaseSession(t *testing.T, d *Daemon) {
	t.Helper()
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "end"}))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase2", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t2","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase2", Boundary: "end"}))
}

func getDiff(t *testing.T, srv *httptest.Server, query string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/diff?token=testtoken&" + query)
	require.NoError(t, err)
	return resp
}

func TestHandleDiff_PhaseScopeOK(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=phase1&bPhase=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
}

func TestHandleDiff_WithinRunPhases(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=phase1&bPhase=phase2")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
	assert.Empty(t, result.Unchanged)
}

func TestHandleDiff_PhaseNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=ghost")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleDiff_InvalidSelector(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&bPhase=phase1,x")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleDiff_SessionNotFoundWithPhase(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=nope&b=s1&aPhase=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleDiff_CombinedPhaseParam(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&phase=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
}

func TestHandleDiff_RangeFromTo(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aFrom=phase1&aTo=phase2&bFrom=phase1&bTo=phase2")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
}

func TestHandleDiff_RangeRequiresBoth(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aFrom=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleDiff_PhaseScopeNarrowsSideA(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=phase1")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Added)
	assert.Empty(t, result.Removed)
}

func TestHandleDiff_RangeScopeNarrowsSideA(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aFrom=phase1&aTo=phase2")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Added)
	assert.Empty(t, result.Removed)
}

func TestHandleDiff_PhaseUnionAcrossExecutions(t *testing.T) {
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

	d.mu.Lock()
	delete(d.execBySession, "s1")
	d.mu.Unlock()

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "end"}))

	d.mu.Lock()
	execs := d.executionsForSession("s1")
	d.mu.Unlock()
	require.Len(t, execs, 2)

	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=phase1&bPhase=phase1")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
	assert.Len(t, result.Unchanged, 2)
}

func TestHandleDiff_WithinRunPhasesContent(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	clocks := []time.Time{
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1970, 1, 1, 0, 0, 1, 0, time.UTC),
		time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	var idx int
	nowFn = func() time.Time {
		c := clocks[idx%len(clocks)]
		idx++
		return c
	}
	t.Cleanup(func() { nowFn = time.Now })
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "wide", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "wide", Boundary: "end"}))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "narrow", Boundary: "start"}))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "narrow", Boundary: "end"}))
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=wide&bPhase=narrow")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	toolNodeID := model.ToolCallID("exec1", "t1")
	assert.NotEmpty(t, result.Removed, "tool node %s is in wide phase but absent in narrow phase; must appear in Removed", toolNodeID)
	assert.Empty(t, result.Added)
}
