package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func ingestSessionWithPayload(t *testing.T, d *Daemon, nodeSecret string) string {
	t.Helper()
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p1","tool_input":{}}`)))
	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "p1")
	input := json.RawMessage(`{"command":"ls","password":"` + nodeSecret + `"}`)
	output := json.RawMessage(`{"output":"ok"}`)
	p := &model.Payload{Input: input, Output: output}
	p.Hash = model.HashPayload(p)
	g.Nodes[nodeID].Payload = p
	g.Nodes[nodeID].PayloadHash = p.Hash
	d.mu.Unlock()
	return nodeID
}

func TestNodePayloadViewDefaultOff(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	nodeID := ingestSessionWithPayload(t, d, "supersecret")
	d.mu.Lock()
	_, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrPayloadAccessDisabled))
}

func TestNodePayloadViewEnabledByID(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	secret := "supersecret"
	nodeID := ingestSessionWithPayload(t, d, secret)
	d.mu.Lock()
	view, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	require.NoError(t, err)
	assert.Equal(t, nodeID, view.NodeID)
	assert.NotEmpty(t, view.PayloadHash)
	assert.NotNil(t, view.Redactions)
	assert.NotContains(t, string(view.Input), secret)
	assert.True(t, view.Redacted)
	require.NotEmpty(t, view.Redactions)
}

func TestNodePayloadViewEnabledByPayloadHash(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	nodeID := ingestSessionWithPayload(t, d, "topsecret")
	d.mu.Lock()
	g := d.graphs["exec1"]
	payloadHash := g.Nodes[nodeID].PayloadHash
	view, err := d.nodePayloadView("s1", payloadHash)
	d.mu.Unlock()
	require.NoError(t, err)
	assert.Equal(t, nodeID, view.NodeID)
	assert.Equal(t, payloadHash, view.PayloadHash)
}

func TestNodePayloadViewUnknownSession(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowPayloadAccess(true)
	d.mu.Lock()
	_, err := d.nodePayloadView("ghost", "any")
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestNodePayloadViewUnknownNode(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	d.mu.Lock()
	_, err := d.nodePayloadView("s1", "no-such-node")
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrPayloadNotFound))
}

func TestNodePayloadViewNilPayload(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.SessionNodeID("exec1")
	g.Nodes[nodeID].Payload = nil
	_, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrPayloadNotFound))
}

func TestNodePayloadViewRedactionsNeverNull(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p2","tool_input":{}}`)))
	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "p2")
	input := json.RawMessage(`{"command":"ls"}`)
	output := json.RawMessage(`{"result":"ok"}`)
	p := &model.Payload{Input: input, Output: output}
	p.Hash = model.HashPayload(p)
	g.Nodes[nodeID].Payload = p
	g.Nodes[nodeID].PayloadHash = p.Hash
	view, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	require.NoError(t, err)
	assert.NotNil(t, view.Redactions)
	assert.False(t, view.Redacted)
	b, jerr := json.Marshal(view)
	require.NoError(t, jerr)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.NotNil(t, m["redactions"])
}

func TestNodePayloadViewPlantedSecretRedacted(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	secret := "AKIAIOSFODNN7EXAMPLE"
	nodeID := ingestSessionWithPayload(t, d, secret)
	d.mu.Lock()
	g := d.graphs["exec1"]
	g.Nodes[nodeID].Payload.Input = json.RawMessage(`{"api_key":"` + secret + `"}`)
	view, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	require.NoError(t, err)
	assert.True(t, view.Redacted)
	assert.NotContains(t, string(view.Input), secret)
	require.NotEmpty(t, view.Redactions)
}

func TestHandleNodePayloadHTTP401NoToken(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowPayloadAccess(true)
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/s1/nodes/n1/payload")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHandleNodePayloadHTTP403Disabled(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/s1/nodes/some-node/payload?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHandleNodePayloadHTTP404UnknownSession(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowPayloadAccess(true)
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/ghost/nodes/n1/payload?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleNodePayloadHTTP404UnknownNode(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/s1/nodes/no-such-node/payload?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleNodePayloadHTTP200Enabled(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	secret := "supersecret_password_value"
	nodeID := ingestSessionWithPayload(t, d, secret)
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/s1/nodes/" + nodeID + "/payload?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	var view PayloadView
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&view))
	assert.Equal(t, nodeID, view.NodeID)
	assert.NotNil(t, view.Redactions)
	assert.NotContains(t, string(view.Input), secret)
	assert.True(t, view.Redacted)
}

func TestHandleNodePayloadHTTPHeaderAuth(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	nodeID := ingestSessionWithPayload(t, d, "secret")
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/sessions/s1/nodes/"+nodeID+"/payload", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleNodePayloadHTTP404NilPayload(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.SessionNodeID("exec1")
	g.Nodes[nodeID].Payload = nil
	d.mu.Unlock()
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/s1/nodes/" + nodeID + "/payload?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleNodePayloadHTTP403DisabledWrongToken(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/sessions/s1/nodes/n1/payload?token=wrong")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestNodePayloadViewEmptyInputOutput(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p3","tool_input":{}}`)))
	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "p3")
	p := &model.Payload{}
	p.Hash = model.HashPayload(p)
	g.Nodes[nodeID].Payload = p
	g.Nodes[nodeID].PayloadHash = p.Hash
	_, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrPayloadNotFound))
}

func TestHandleNodePayloadHTTPOutputOnlyRedacted(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p4","tool_input":{}}`)))
	secret := "ghp_" + strings.Repeat("x", 36)
	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "p4")
	output := json.RawMessage(`{"token":"` + secret + `"}`)
	p := &model.Payload{Output: output}
	p.Hash = model.HashPayload(p)
	g.Nodes[nodeID].Payload = p
	g.Nodes[nodeID].PayloadHash = p.Hash
	view, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	require.NoError(t, err)
	assert.Nil(t, view.Input)
	assert.NotContains(t, string(view.Output), secret)
	assert.True(t, view.Redacted)
}

func TestSetAllowPayloadAccess(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	before := d.allowPayloadAccess
	d.mu.Unlock()
	assert.False(t, before)
	d.SetAllowPayloadAccess(true)
	d.mu.Lock()
	after := d.allowPayloadAccess
	d.mu.Unlock()
	assert.True(t, after)
	d.SetAllowPayloadAccess(false)
	d.mu.Lock()
	final := d.allowPayloadAccess
	d.mu.Unlock()
	assert.False(t, final)
}
