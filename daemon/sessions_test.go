package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
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

func TestSessionSummaryExposesRepro(t *testing.T) {
	dir := shortTempDir(t)
	d := New(tempStore(t))
	fixedExecID(d)
	p, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
	require.NoError(t, d.Ingest("SessionStart", p))

	d.mu.Lock()
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	require.NotNil(t, sum.Repro)
	assert.Equal(t, dir, sum.Repro.Cwd)
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

func TestSessionSummaryCountsByTypeAndStatus(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "t1")
	g.Nodes[nodeID].Status = model.StatusError
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	s := sums[0]

	require.NotNil(t, s.CountsByType)
	require.NotNil(t, s.CountsByStatus)

	totalByType := 0
	for _, c := range s.CountsByType {
		totalByType += c
	}
	assert.Equal(t, s.NodeCount, totalByType, "counts_by_type must sum to NodeCount")

	totalByStatus := 0
	for _, c := range s.CountsByStatus {
		totalByStatus += c
	}
	assert.Equal(t, s.NodeCount, totalByStatus, "counts_by_status must sum to NodeCount")

	assert.Greater(t, s.CountsByType[string(model.NodeToolCall)], 0)
	assert.Greater(t, s.CountsByStatus[string(model.StatusError)], 0)
}

func TestSessionSummaryErrorRate(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t2","tool_input":{}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "t1")
	g.Nodes[nodeID].Status = model.StatusError
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	s := sums[0]
	assert.Equal(t, 1, s.ErrorCount)
	expected := float64(1) / float64(s.NodeCount)
	assert.InDelta(t, expected, s.ErrorRate, 1e-9)
}

func TestSessionSummaryErrorRateZeroWhenNoNodes(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	for k := range g.Nodes {
		delete(g.Nodes, k)
	}
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, 0, sum.NodeCount)
	assert.Equal(t, float64(0), sum.ErrorRate)
}

func TestSessionSummaryMapsNotNullWhenEmpty(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	for k := range g.Nodes {
		delete(g.Nodes, k)
	}
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	b, err := json.Marshal(sum)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	cbt, ok := m["counts_by_type"]
	assert.True(t, ok, "counts_by_type must be present")
	assert.NotNil(t, cbt, "counts_by_type must not be null")

	cbs, ok := m["counts_by_status"]
	assert.True(t, ok, "counts_by_status must be present")
	assert.NotNil(t, cbs, "counts_by_status must not be null")
}

func TestSessionSummaryCountsDeterministic(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	d.mu.Lock()
	sum1 := d.summarizeSession("s1")
	sum2 := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, sum1.CountsByType, sum2.CountsByType)
	assert.Equal(t, sum1.CountsByStatus, sum2.CountsByStatus)
	assert.Equal(t, sum1.ErrorRate, sum2.ErrorRate)
}

func TestSessionSummaryModelIDFromIngestedObservation(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)

	sessionInit := []byte(`{"type":"system","subtype":"init","session_id":"s1","model":"claude-sonnet-4-6"}`)
	require.NoError(t, d.IngestStreamJSON(sessionInit, "s1"))

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	assert.Equal(t, "claude-sonnet-4-6", sums[0].ModelID)
}

func TestSessionSummaryLastActivityUsesMaxNodeTEnd(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	early := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	earlyEnd := time.Date(2026, 6, 1, 10, 0, 5, 0, time.UTC)
	latest := time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC)
	for _, n := range g.Nodes {
		n.TStart = &early
		n.TEnd = &earlyEnd
	}
	g.Nodes["n-latest"] = &model.Node{ID: "n-latest", RunID: "s1", Type: model.NodeToolCall, TStart: &early, TEnd: &latest}
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, latest.UTC().Format(time.RFC3339), sum.LastActivity)
}

func TestSessionSummaryLastActivityFallsBackToTStart(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	for k := range g.Nodes {
		delete(g.Nodes, k)
	}
	started := time.Date(2026, 6, 2, 9, 15, 0, 0, time.UTC)
	g.Nodes["n-open"] = &model.Node{ID: "n-open", RunID: "s1", Type: model.NodeToolCall, TStart: &started}
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, started.UTC().Format(time.RFC3339), sum.LastActivity)
}

func jsonStr(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return b
}

func promptNode(id string, ts *time.Time, input json.RawMessage, kind string) *model.Node {
	n := &model.Node{ID: id, RunID: "s1", Type: model.NodeUserPrompt, TStart: ts}
	if input != nil {
		n.Payload = &model.Payload{Input: input}
	}
	if kind != "" {
		n.Attrs = map[string]any{"prompt_kind": kind}
	}
	return n
}

func TestSessionSummaryLabelFromFirstPrompt(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, jsonStr(t, "Fix the\nlogin   bug"), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, "Fix the login bug", sum.Label)
}

func TestSessionSummaryLabelOnlySystemPrompts(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, jsonStr(t, "/clear caveat"), "system")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)
}

func TestSessionSummaryLabelNoPrompts(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)
}

func TestSessionSummaryLabelGateOff(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, jsonStr(t, "Fix the login bug"), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)

	b, err := json.Marshal(sum)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, ok := m["label"]
	assert.False(t, ok, "label must be omitted when empty")
}

func TestSessionSummaryLabelEarliestWins(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	early := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	g.Nodes["p-late"] = promptNode("p-late", &late, jsonStr(t, "later prompt"), "")
	g.Nodes["p-early"] = promptNode("p-early", &early, jsonStr(t, "earliest prompt"), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, "earliest prompt", sum.Label)
}

func TestSessionSummaryLabelNilTStartSortsLast(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p-nil"] = promptNode("p-nil", nil, jsonStr(t, "untimed prompt"), "")
	g.Nodes["p-timed"] = promptNode("p-timed", &ts, jsonStr(t, "timed prompt"), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Equal(t, "timed prompt", sum.Label)
}

func TestSessionSummaryLabelRedactsSecret(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	secret := "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, jsonStr(t, "deploy with "+secret), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.NotContains(t, sum.Label, secret)
	assert.Contains(t, sum.Label, "redacted")
}

func TestSessionSummaryLabelTruncates(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, jsonStr(t, strings.Repeat("ab ", 40)), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	r := []rune(sum.Label)
	require.Equal(t, sessionLabelMaxRunes+1, len(r))
	assert.Equal(t, '…', r[len(r)-1])
}

func TestSessionSummaryLabelEmptyTextWhitespace(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, jsonStr(t, "  \n\t  "), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)
}

func TestSessionSummaryLabelNonStringPayload(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, json.RawMessage(`{"k":"v"}`), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)
}

func TestSessionSummaryLabelInvalidJSONPayload(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, json.RawMessage("not json"), "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)
}

func TestSessionSummaryLabelPromptWithoutPayload(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetAllowPayloadAccess(true)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	ts := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	g.Nodes["p1"] = promptNode("p1", &ts, nil, "")
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.Label)
}

func TestSessionSummaryLastActivityAbsentWhenNoTimestamps(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	for _, n := range g.Nodes {
		n.TStart = nil
		n.TEnd = nil
	}
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Empty(t, sum.LastActivity)

	b, err := json.Marshal(sum)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, ok := m["last_activity"]
	assert.False(t, ok, "last_activity must be omitted when no node timestamps")
}

func TestSummarizeRunAggregatesByRunID(t *testing.T) {
	g := reduce.NewGraph()
	g.Runs["r1"] = &model.Run{ID: "r1", Status: model.StatusOK}
	g.Runs["r2"] = &model.Run{ID: "r2", Status: model.StatusOK}
	tokIn := int64(10)
	tokOut := int64(5)
	g.Nodes["n1"] = &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, TokensIn: &tokIn, TokensOut: &tokOut}
	g.Nodes["n2"] = &model.Node{ID: "n2", RunID: "r1", Type: model.NodeAssistantTurn}
	g.Nodes["n3"] = &model.Node{ID: "n3", RunID: "r2", Type: model.NodeToolCall}
	g.Nodes["n4"] = &model.Node{ID: "n4", RunID: "r1", Type: model.NodeSkill}

	sum := SummarizeRun("r1", []*reduce.Graph{g})

	assert.Equal(t, 3, sum.NodeCount)
	assert.Equal(t, 2, sum.ToolCount)
	assert.Equal(t, int64(10), sum.TokensIn)
	assert.Equal(t, int64(5), sum.TokensOut)
	assert.Equal(t, "r1", sum.Session)
	assert.Equal(t, []string{"r1"}, sum.RunIDs)
}

func TestSummarizeRunCarriesLabels(t *testing.T) {
	g := reduce.NewGraph()
	g.Runs["r1"] = &model.Run{ID: "r1", Status: model.StatusOK, Labels: map[string]string{"basket": "b1"}}
	g.Nodes["n1"] = &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall}

	sum := SummarizeRun("r1", []*reduce.Graph{g})

	assert.Equal(t, map[string]string{"basket": "b1"}, sum.Labels)
}

func TestSummarizeRunOmitsLabelsWhenAbsent(t *testing.T) {
	g := reduce.NewGraph()
	g.Runs["r1"] = &model.Run{ID: "r1", Status: model.StatusOK}
	g.Nodes["n1"] = &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall}

	sum := SummarizeRun("r1", []*reduce.Graph{g})

	assert.Nil(t, sum.Labels)

	b, err := json.Marshal(sum)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	_, ok := m["labels"]
	assert.False(t, ok, "labels must be omitted when empty")
}

func TestSummarizeSessionLiveCarriesLabels(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	run := d.graphs["exec1"].Runs["s1"]
	run.Labels = map[string]string{"basket": "b1", "rep": "1"}
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	require.Equal(t, map[string]string{"basket": "b1", "rep": "1"}, sum.Labels)

	sum.Labels["basket"] = "mutated"
	assert.Equal(t, "b1", run.Labels["basket"])
}

func TestSummarizeSessionLiveOmitsLabelsWhenAbsent(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	sum := d.summarizeSession("s1")
	d.mu.Unlock()

	assert.Nil(t, sum.Labels)
}

func TestSummarizeSessionLiveDeterministicLabelPick(t *testing.T) {
	for i := 0; i < 64; i++ {
		d := New(tempStore(t))
		fixedExecID(d)
		require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

		d.mu.Lock()
		g := d.graphs["exec1"]
		g.Runs["run-a"] = &model.Run{ID: "run-a", Status: model.StatusOK, SessionIDs: []string{"s1"}, Labels: map[string]string{"pick": "a"}}
		g.Runs["run-b"] = &model.Run{ID: "run-b", Status: model.StatusOK, SessionIDs: []string{"s1"}, Labels: map[string]string{"pick": "b"}}
		sum := d.summarizeSession("s1")
		d.mu.Unlock()

		require.Equal(t, map[string]string{"pick": "a"}, sum.Labels)
	}
}

func TestSummarizeGraphsDeterministicLabelPick(t *testing.T) {
	build := func(order []string) *reduce.Graph {
		g := reduce.NewGraph()
		runs := map[string]*model.Run{
			"run-a": {ID: "run-a", Status: model.StatusOK, SessionIDs: []string{"s1"}, Labels: map[string]string{"pick": "a"}},
			"run-b": {ID: "run-b", Status: model.StatusOK, SessionIDs: []string{"s1"}, Labels: map[string]string{"pick": "b"}},
		}
		for _, id := range order {
			g.Runs[id] = runs[id]
		}
		g.Nodes["n1"] = &model.Node{ID: "n1", RunID: "run-a", Type: model.NodeToolCall}
		return g
	}
	for _, order := range [][]string{{"run-a", "run-b"}, {"run-b", "run-a"}} {
		for i := 0; i < 32; i++ {
			sum := SummarizeSession("s1", []*reduce.Graph{build(order)})
			require.Equal(t, map[string]string{"pick": "a"}, sum.Labels)
		}
	}
}

func TestSummarizeSessionFreeFnMatchesMethod(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"sess-x"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"sess-x","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"sess-x","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	d.mu.Lock()
	d.graphs["exec1"].Nodes["skill-x"] = &model.Node{ID: "skill-x", RunID: "sess-x", Type: model.NodeSkill}
	dSum := d.summarizeSession("sess-x")
	graphs := graphSlice(d.graphs)
	d.mu.Unlock()

	freeSum := SummarizeSession("sess-x", graphs)

	assert.Equal(t, 2, dSum.ToolCount)
	assert.Equal(t, dSum.NodeCount, freeSum.NodeCount)
	assert.Equal(t, dSum.ToolCount, freeSum.ToolCount)
	assert.Equal(t, dSum.RunIDs, freeSum.RunIDs)
}

func TestSummarizeGraphsCoverage(t *testing.T) {
	g := reduce.NewGraph()
	now := time.Now().UTC()
	earlier := now.Add(-time.Minute)

	g.Runs["run1"] = &model.Run{
		ID:         "run1",
		Status:     model.StatusError,
		ModelID:    "claude-opus-4",
		StartedAt:  &earlier,
		EndedAt:    &now,
		SessionIDs: []string{"sess-1"},
	}

	cost1 := 0.001
	cost2 := 0.0005
	tok := int64(100)

	g.Nodes["n1"] = &model.Node{
		ID:        "n1",
		RunID:     "run1",
		Type:      model.NodeToolCall,
		Status:    model.StatusError,
		CostUSD:   &cost1,
		Attrs:     map[string]any{"cost_source": "reported"},
		TEnd:      &now,
		TokensIn:  &tok,
		TokensOut: &tok,
	}
	g.Nodes["n2"] = &model.Node{
		ID:      "n2",
		RunID:   "run1",
		Type:    model.NodeAssistantTurn,
		CostUSD: &cost2,
		Attrs:   map[string]any{"cost_source": "estimated"},
	}

	sum := SummarizeSession("sess-1", []*reduce.Graph{g})

	assert.Equal(t, "sess-1", sum.Session)
	assert.Equal(t, "claude-opus-4", sum.ModelID)
	assert.Equal(t, 1, sum.ErrorCount)
	assert.NotNil(t, sum.CostUSD)
	assert.Equal(t, "reported", sum.CostSource)
	assert.NotEmpty(t, sum.StartedAt)
	assert.NotEmpty(t, sum.EndedAt)
	assert.NotNil(t, sum.DurationMS)
	assert.NotEmpty(t, sum.LastActivity)
	assert.Equal(t, 2, sum.NodeCount)
	assert.Equal(t, []string{"run1"}, sum.RunIDs)
	assert.InDelta(t, 0.5, sum.ErrorRate, 0.001)
}

func TestSummarizeGraphsReproDeepCopied(t *testing.T) {
	g := reduce.NewGraph()
	src := &model.ReproMeta{Cwd: "/orig", CatacombVersion: "v1"}
	g.Runs["r1"] = &model.Run{ID: "r1", SessionIDs: []string{"s1"}, Repro: src}

	sum := SummarizeSession("s1", []*reduce.Graph{g})
	require.NotNil(t, sum.Repro)
	require.Equal(t, "/orig", sum.Repro.Cwd)

	src.Cwd = "/mutated"
	src.CatacombVersion = "v2"
	assert.Equal(t, "/orig", sum.Repro.Cwd)
	assert.Equal(t, "v1", sum.Repro.CatacombVersion)
}

func TestSummarizeSessionReproDeepCopied(t *testing.T) {
	dir := shortTempDir(t)
	d := New(tempStore(t))
	fixedExecID(d)
	p, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
	require.NoError(t, d.Ingest("SessionStart", p))

	d.mu.Lock()
	sum := d.summarizeSession("s1")
	r := d.graphs["exec1"].Runs["s1"]
	d.mu.Unlock()

	require.NotNil(t, sum.Repro)
	require.NotNil(t, r.Repro)
	require.Equal(t, dir, sum.Repro.Cwd)

	r.Repro.Cwd = "/mutated"
	assert.Equal(t, dir, sum.Repro.Cwd)
}

func sessionTotalGraph(withResult bool, resultCost *float64) *reduce.Graph {
	g := reduce.NewGraph()
	g.Runs["s1"] = &model.Run{ID: "s1", Status: model.StatusOK, SessionIDs: []string{"s1"}}
	est1, est2 := 0.30, 0.40
	ti1, to1 := int64(100), int64(50)
	ti2, to2 := int64(200), int64(70)
	g.Nodes["e:turn:m1"] = &model.Node{ID: "e:turn:m1", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est1, TokensIn: &ti1, TokensOut: &to1, Attrs: map[string]any{"cost_source": "estimated"}}
	g.Nodes["e:turn:m2"] = &model.Node{ID: "e:turn:m2", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est2, TokensIn: &ti2, TokensOut: &to2, Attrs: map[string]any{"cost_source": "estimated"}}
	if withResult {
		tit, tot := int64(999), int64(999)
		n := &model.Node{ID: "e:turn:", RunID: "s1", Type: model.NodeAssistantTurn, TokensIn: &tit, TokensOut: &tot, Attrs: map[string]any{"session_total": true}}
		if resultCost != nil {
			n.CostUSD = resultCost
			n.Attrs["cost_source"] = "reported"
		}
		g.Nodes["e:turn:"] = n
	}
	return g
}

func TestSummarizeRunReportedSessionTotalReplacesEstimates(t *testing.T) {
	rep := 0.50
	sum := SummarizeRun("s1", []*reduce.Graph{sessionTotalGraph(true, &rep)})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.50, *sum.CostUSD, 1e-12)
	assert.Equal(t, "reported", sum.CostSource)
	assert.Equal(t, int64(300), sum.TokensIn)
	assert.Equal(t, int64(120), sum.TokensOut)
}

func TestSummarizeRunFallsBackToEstimatesWithoutResult(t *testing.T) {
	sum := SummarizeRun("s1", []*reduce.Graph{sessionTotalGraph(false, nil)})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.70, *sum.CostUSD, 1e-12)
	assert.Equal(t, "estimated", sum.CostSource)
	assert.Equal(t, int64(300), sum.TokensIn)
	assert.Equal(t, int64(120), sum.TokensOut)
}

func TestSummarizeRunSessionTotalWithoutCostStillExcludedFromTokens(t *testing.T) {
	sum := SummarizeRun("s1", []*reduce.Graph{sessionTotalGraph(true, nil)})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.70, *sum.CostUSD, 1e-12)
	assert.Equal(t, "estimated", sum.CostSource)
	assert.Equal(t, int64(300), sum.TokensIn)
	assert.Equal(t, int64(120), sum.TokensOut)
}

func TestSummarizeRunMixedGraphsSumPerGraphBest(t *testing.T) {
	rep := 0.50
	g1 := sessionTotalGraph(true, &rep)
	g2 := reduce.NewGraph()
	g2.Runs["s1"] = &model.Run{ID: "s1", Status: model.StatusOK, SessionIDs: []string{"s1"}}
	est := 0.20
	g2.Nodes["e2:turn:m9"] = &model.Node{ID: "e2:turn:m9", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est, Attrs: map[string]any{"cost_source": "estimated"}}

	sum := SummarizeRun("s1", []*reduce.Graph{g1, g2})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.70, *sum.CostUSD, 1e-12)
	assert.Equal(t, "reported", sum.CostSource)
}

func TestSessionSummariesReportedSessionTotal(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	est, rep := 0.30, 0.50
	ti, to := int64(100), int64(40)
	tit, tot := int64(999), int64(999)
	g.Nodes["exec1:turn:m1"] = &model.Node{ID: "exec1:turn:m1", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est, TokensIn: &ti, TokensOut: &to, Attrs: map[string]any{"cost_source": "estimated"}}
	g.Nodes["exec1:turn:"] = &model.Node{ID: "exec1:turn:", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &rep, TokensIn: &tit, TokensOut: &tot, Attrs: map[string]any{"cost_source": "reported", "session_total": true}}
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	require.NotNil(t, sums[0].CostUSD)
	assert.InDelta(t, 0.50, *sums[0].CostUSD, 1e-12)
	assert.Equal(t, "reported", sums[0].CostSource)
	assert.Equal(t, int64(100), sums[0].TokensIn)
	assert.Equal(t, int64(40), sums[0].TokensOut)
}
