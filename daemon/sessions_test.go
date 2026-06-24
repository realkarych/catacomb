package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func TestExecutionsForSessionSurvivesRecover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	d.mu.Lock()
	before := d.executionsForSession("s1")
	d.mu.Unlock()
	require.Equal(t, []string{"exec1"}, before)
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())

	d2.mu.Lock()
	after := d2.executionsForSession("s1")
	d2.mu.Unlock()
	assert.Equal(t, before, after)
}

func TestExecutionsForSessionUnknown(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	defer d.mu.Unlock()
	assert.Empty(t, d.executionsForSession("nope"))
}

func TestExecutionsForSessionEmpty(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	defer d.mu.Unlock()
	assert.Nil(t, d.executionsForSession(""))
}

func TestSessionSummariesBasic(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	s := sums[0]
	assert.Equal(t, "s1", s.Session)
	assert.GreaterOrEqual(t, s.NodeCount, 2)
	assert.Equal(t, 1, s.ToolCount)
	assert.NotNil(t, s.RunIDs)
	assert.Contains(t, s.RunIDs, "s1")
}

func TestSessionSummariesEmpty(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()
	assert.Empty(t, sums)
}

func TestSessionSummariesMultiple(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 2)
	assert.Equal(t, "s1", sums[0].Session)
	assert.Equal(t, "s2", sums[1].Session)
}

func TestSessionSummaryRunningStatus(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.Equal(t, "running", sums[0].Status)
	assert.Nil(t, sums[0].DurationMS)
	assert.Empty(t, sums[0].EndedAt)
}

func TestSessionSummaryEndedHasDuration(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.NotEmpty(t, sums[0].StartedAt)
	assert.NotEmpty(t, sums[0].EndedAt)
	assert.NotNil(t, sums[0].DurationMS)
}

func TestSessionSummaryRunIDsNeverNull(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	b, err := json.Marshal(sum)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	runIDs, ok := m["run_ids"]
	assert.True(t, ok)
	assert.NotNil(t, runIDs)
}

func TestSessionGraphDeltasScoped(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))

	d.mu.Lock()
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)
	require.NotEmpty(t, evs)
	for _, ev := range evs {
		assert.Equal(t, "exec1", ev.ExecutionID)
		assert.Contains(t, []string{"node_upsert", "edge_upsert"}, ev.Kind)
		if ev.Node != nil {
			assert.Nil(t, ev.Node.Payload)
		}
	}
}

func TestSessionGraphDeltasUnknown404(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	_, err := d.sessionGraphDeltas("ghost")
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestSessionsEndpointBearerGated(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp2, err := http.Get(srv.URL + "/v1/sessions?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Contains(t, resp2.Header.Get("Content-Type"), "application/json")
}

func TestSessionGraphEndpoint(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions/s1/graph?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	assert.NotEmpty(t, evs)

	resp404, err := http.Get(srv.URL + "/v1/sessions/ghost/graph?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp404.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp404.StatusCode)

	respUnauth, err := http.Get(srv.URL + "/v1/sessions/s1/graph")
	require.NoError(t, err)
	defer func() { _ = respUnauth.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, respUnauth.StatusCode)
}

func TestSessionsListEndpointJSON(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var sums []SessionSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sums))
	require.Len(t, sums, 1)
	assert.Equal(t, "s1", sums[0].Session)
	assert.NotNil(t, sums[0].RunIDs)
}

func TestSessionGraphGraphEndpointHeaderAuth(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/sessions/s1/graph", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionGraphDeltasIncludesEdges(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	g.Edges["e1"] = &model.Edge{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "a", Dst: "b"}
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()

	require.NoError(t, err)
	var hasEdge bool
	for _, ev := range evs {
		if ev.Kind == "edge_upsert" {
			hasEdge = true
		}
	}
	assert.True(t, hasEdge)
}

func TestStatusRankError(t *testing.T) {
	assert.Equal(t, 3, statusRank(model.StatusError))
}

func TestFoldStatusKeepsCurrent(t *testing.T) {
	result := foldStatus("error", model.StatusOK)
	assert.Equal(t, "error", result)
}

func TestSessionSummaryErrorStatus(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "t1")
	g.Nodes[nodeID].Status = model.StatusError
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.Equal(t, 1, sums[0].ErrorCount)
}

func TestSessionSummaryWithCost(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "t1")
	cost := 0.001
	g.Nodes[nodeID].CostUSD = &cost
	g.Nodes[nodeID].Attrs = map[string]any{"cost_source": "reported"}
	tok := int64(10)
	g.Nodes[nodeID].TokensIn = &tok
	g.Nodes[nodeID].TokensOut = &tok
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.NotNil(t, sums[0].CostUSD)
	assert.Equal(t, "reported", sums[0].CostSource)
	assert.Equal(t, int64(10), sums[0].TokensIn)
	assert.Equal(t, int64(10), sums[0].TokensOut)
}

func TestSessionSummaryWithEstimatedCost(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "t1")
	cost := 0.001
	g.Nodes[nodeID].CostUSD = &cost
	g.Nodes[nodeID].Attrs = map[string]any{"cost_source": "estimated"}
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.NotNil(t, sums[0].CostUSD)
	assert.Equal(t, "estimated", sums[0].CostSource)
}

func TestSessionSummaryModelID(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	r := g.Runs["s1"]
	r.ModelID = "claude-opus-4"
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.Equal(t, "claude-opus-4", sums[0].ModelID)
}

func TestSessionSummaryNodeWithUnknownRun(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	g.Nodes["orphan"] = &model.Node{ID: "orphan", RunID: "unknown-run", Type: model.NodeSession}
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.Equal(t, "s1", sums[0].Session)
}

func TestSessionSummaryRunInGraphNotInSession(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	now := time.Now().UTC()
	g.Runs["other-run"] = &model.Run{
		ID:         "other-run",
		SessionIDs: []string{"other-session"},
		Status:     model.StatusOK,
		EndedAt:    &now,
	}
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 2)
	var s1Sum SessionSummary
	for _, s := range sums {
		if s.Session == "s1" {
			s1Sum = s
		}
	}
	assert.Equal(t, "s1", s1Sum.Session)
	assert.NotContains(t, s1Sum.RunIDs, "other-run")
}

func TestSessionSummaryWithStartedAndEndedAt(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	r := g.Runs["s1"]
	now := time.Now().UTC()
	earlier := now.Add(-time.Minute)
	r.StartedAt = &earlier
	r.EndedAt = &now
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.NotEmpty(t, sums[0].StartedAt)
	assert.NotEmpty(t, sums[0].EndedAt)
	assert.NotNil(t, sums[0].DurationMS)
}

func TestExecutionsForSessionSubscribeSnapshotAfterRecover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())

	sub := d2.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d2.Unsubscribe(sub)

	require.NotEmpty(t, sub.Snapshot, "snapshot must be non-empty after recover")

	d2.mu.Lock()
	scopedExecs := d2.executionsForSession("s1")
	d2.mu.Unlock()

	scopedSet := map[string]bool{}
	for _, e := range scopedExecs {
		scopedSet[e] = true
	}
	for _, delta := range sub.Snapshot {
		assert.True(t, scopedSet[delta.ExecutionID], "snapshot delta must be scoped to s1 executions")
	}
}
