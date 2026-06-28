package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/diff"
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
