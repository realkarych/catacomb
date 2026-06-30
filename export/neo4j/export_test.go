package neo4j

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	neo4japi "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/model"
)

var _ exportiface.Exporter = (*Exporter)(nil)

type cypherCall struct {
	cypher string
	params map[string]any
}

type recordRunner struct {
	calls  []cypherCall
	closed bool
}

func (r *recordRunner) Run(_ context.Context, cypher string, params map[string]any) error {
	r.calls = append(r.calls, cypherCall{cypher: cypher, params: params})
	return nil
}

func (r *recordRunner) Close(_ context.Context) error {
	r.closed = true
	return nil
}

type errorRunner struct {
	runErr   error
	closeErr error
	callN    int
	failOn   int
}

func (e *errorRunner) Run(_ context.Context, _ string, _ map[string]any) error {
	e.callN++
	if e.failOn == 0 || e.callN == e.failOn {
		return e.runErr
	}
	return nil
}

func (e *errorRunner) Close(_ context.Context) error { return e.closeErr }

type stubSession struct {
	runErr   error
	closeErr error
}

func (s *stubSession) Run(_ context.Context, _ string, _ map[string]any, _ ...func(*neo4japi.TransactionConfig)) (neo4japi.ResultWithContext, error) {
	return nil, s.runErr
}

func (s *stubSession) Close(_ context.Context) error { return s.closeErr }

type stubDriver struct {
	sess     *stubSession
	closeErr error
}

func (d *stubDriver) NewSession(_ context.Context, _ neo4japi.SessionConfig) neo4jSession {
	return d.sess
}

func (d *stubDriver) Close(_ context.Context) error { return d.closeErr }

func TestNewWithValidURIConstructsExporter(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, "bolt://localhost:7687", "neo4j", "password")
	require.NoError(t, err)
	assert.Equal(t, "neo4j", e.Name())
	_ = e.Shutdown(ctx)
}

func TestNewWithMalformedURIReturnsError(t *testing.T) {
	_, err := New(context.Background(), "not-a-uri://\x00invalid", "", "")
	require.Error(t, err)
}

func TestNameIsNeo4j(t *testing.T) {
	e := ExporterWithRunner(&recordRunner{})
	assert.Equal(t, "neo4j", e.Name())
}

func TestShutdownClosesRunner(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.Shutdown(context.Background()))
	assert.True(t, r.closed)
}

func TestFlushRunIsNoop(t *testing.T) {
	e := ExporterWithRunner(&recordRunner{})
	require.NoError(t, e.FlushRun(context.Background(), "any-run"))
}

func TestNodeLabelMapping(t *testing.T) {
	cases := []struct {
		t    model.NodeType
		want string
	}{
		{model.NodeSession, "Session"},
		{model.NodeUserPrompt, "UserPrompt"},
		{model.NodeAssistantTurn, "AssistantTurn"},
		{model.NodeToolCall, "ToolCall"},
		{model.NodeSubagent, "Subagent"},
		{model.NodeMCPCall, "McpCall"},
		{model.NodeSkill, "Skill"},
		{model.NodeHookEvent, "HookEvent"},
		{model.NodeMarker, "Marker"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, nodeLabel(c.t), "nodeLabel(%s)", c.t)
	}
}

func TestNodeLabelDefaultBranch(t *testing.T) {
	assert.Equal(t, "Marker", nodeLabel(model.NodeType("unknown_type")))
}

func TestEdgeRelTypeMapping(t *testing.T) {
	cases := []struct {
		t    model.EdgeType
		want string
	}{
		{model.EdgeParentChild, "PARENT_OF"},
		{model.EdgeSequence, "NEXT"},
		{model.EdgeMarkerSpan, "IN_PHASE"},
		{model.EdgeDataDep, "DATA_DEP"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, edgeRelType(c.t), "edgeRelType(%s)", c.t)
	}
}

func TestEdgeRelTypeDefaultBranch(t *testing.T) {
	assert.Equal(t, "DATA_DEP", edgeRelType(model.EdgeType("unknown_type")))
}

func TestApplyDeltaNodeUpsertEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusRunning, Rev: 3}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 3, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].cypher, "MERGE")
	assert.Contains(t, r.calls[0].cypher, "ToolCall")
	assert.Contains(t, r.calls[0].cypher, "coalesce(n.rev,-1) < $rev")
	assert.Equal(t, "n1", r.calls[0].params["id"])
}

func TestApplyDeltaNodeStatusEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, Rev: 5}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeStatus, Rev: 5, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].cypher, "MERGE")
	assert.Contains(t, r.calls[0].cypher, "AssistantTurn")
}

func TestApplyDeltaEdgeUpsertEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "c", Rev: 2}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeUpsert, Rev: 2, RunID: "r1", Edge: edge,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].cypher, "PARENT_OF")
	assert.Contains(t, r.calls[0].cypher, "MERGE")
}

func TestApplyDeltaNodeMergeDeletesThenUpserts(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	n := &model.Node{ID: "n-new", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 7}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 7, RunID: "r1", OldID: "n-old", NewID: "n-new", Node: n,
	}))
	require.Len(t, r.calls, 2)
	assert.Contains(t, strings.ToLower(r.calls[0].cypher), "detach delete")
	assert.Equal(t, "n-old", r.calls[0].params["id"])
	assert.Contains(t, r.calls[1].cypher, "MERGE")
}

func TestApplyDeltaEdgeDeleteEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1", Edge: edge,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, strings.ToLower(r.calls[0].cypher), "delete")
	assert.Equal(t, "e1", r.calls[0].params["id"])
}

func TestApplyDeltaLifecycleKindsAreNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	for _, k := range []cdc.GraphDeltaKind{cdc.DeltaRunStarted, cdc.DeltaSessionEnded, cdc.DeltaRunEnded} {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: k, Rev: 1, RunID: "r1"}))
	}
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNilNodeIsNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1"}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNilEdgeIsNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1"}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNodeMergeNilNode(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 1, RunID: "r1",
	}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaEdgeDeleteNilEdge(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1",
	}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNodeUpsertAttrsJSONEncoded(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	n := &model.Node{
		ID: "n3", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1,
		Attrs: map[string]any{"key": "val"},
	}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	props, ok := r.calls[0].params["props"].(map[string]any)
	require.True(t, ok)
	attrsStr, ok := props["attrs"].(string)
	require.True(t, ok)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(attrsStr), &m))
	assert.Equal(t, "val", m["key"])
}

func assertNoSentinelInParams(t *testing.T, params map[string]any, sentinel string) {
	t.Helper()
	for _, v := range params {
		switch val := v.(type) {
		case string:
			assert.NotContains(t, val, sentinel)
		case map[string]any:
			assertNoSentinelInParams(t, val, sentinel)
		default:
			assert.NotContains(t, fmt.Sprintf("%v", val), sentinel)
		}
	}
}

func TestApplyDeltaNodeUpsertPayloadNeverInCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	n := &model.Node{
		ID: "n4", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1,
		Payload: &model.Payload{Hash: "abc123", Input: []byte(`"UNIQUESENTINELXYZ"`)},
	}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	for _, call := range r.calls {
		assert.NotContains(t, call.cypher, "UNIQUESENTINELXYZ", "raw payload content must never appear in Cypher")
		assertNoSentinelInParams(t, call.params, "UNIQUESENTINELXYZ")
	}
}

func TestApplyDeltaNodeUpsertRunError(t *testing.T) {
	runErr := errors.New("run error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	})
	require.Error(t, err)
}

func TestApplyDeltaEdgeUpsertRunError(t *testing.T) {
	runErr := errors.New("edge run error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1", Edge: edge,
	})
	require.Error(t, err)
}

func TestApplyDeltaNodeMergeDeleteError(t *testing.T) {
	runErr := errors.New("delete error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr, failOn: 1})
	n := &model.Node{ID: "n-new", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 1, RunID: "r1", OldID: "n-old", Node: n,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node_merge delete")
}

func TestApplyDeltaEdgeDeleteRunError(t *testing.T) {
	runErr := errors.New("edge delete error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1", Edge: edge,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edge_delete")
}

func TestShutdownRunnerCloseError(t *testing.T) {
	closeErr := errors.New("close error")
	e := ExporterWithRunner(&errorRunner{closeErr: closeErr})
	err := e.Shutdown(context.Background())
	require.Error(t, err)
}

func TestSnapshotStateUpsertsBatched(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	nodes := []*model.Node{
		{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1},
		{ID: "b", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 2},
	}
	edges := []*model.Edge{
		{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1},
	}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edges))
	assert.Len(t, r.calls, 3, "two node upserts + one edge upsert")
}

func TestSnapshotStateEmptyIsNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.SnapshotState(context.Background(), nil, nil))
	assert.Empty(t, r.calls)
}

func TestSnapshotStateNodeRunError(t *testing.T) {
	runErr := errors.New("node run error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	nodes := []*model.Node{{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1}}
	err := e.SnapshotState(context.Background(), nodes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot upsert node")
}

func TestSnapshotStateEdgeRunError(t *testing.T) {
	runErr := errors.New("edge run error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1}}
	err := e.SnapshotState(context.Background(), nil, edges)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot upsert edge")
}

func TestSessionRunnerRunOK(t *testing.T) {
	sess := &stubSession{}
	drv := &stubDriver{sess: sess}
	sr := &sessionRunner{d: drv}
	require.NoError(t, sr.Run(context.Background(), "RETURN 1", nil))
}

func TestSessionRunnerRunError(t *testing.T) {
	sess := &stubSession{runErr: errors.New("bolt error")}
	drv := &stubDriver{sess: sess}
	sr := &sessionRunner{d: drv}
	err := sr.Run(context.Background(), "RETURN 1", nil)
	require.Error(t, err)
}

func TestSessionRunnerClose(t *testing.T) {
	sess := &stubSession{}
	drv := &stubDriver{sess: sess}
	sr := &sessionRunner{d: drv}
	require.NoError(t, sr.Close(context.Background()))
}

func TestSessionRunnerCloseError(t *testing.T) {
	drv := &stubDriver{sess: &stubSession{}, closeErr: errors.New("close error")}
	sr := &sessionRunner{d: drv}
	err := sr.Close(context.Background())
	require.Error(t, err)
}

func TestDriverAdapterNewSession(t *testing.T) {
	drv, err := neo4japi.NewDriverWithContext("bolt://localhost:7687", neo4japi.BasicAuth("u", "p", ""))
	require.NoError(t, err)
	defer func() { _ = drv.Close(context.Background()) }()
	da := &driverAdapter{d: drv}
	sess := da.NewSession(context.Background(), neo4japi.SessionConfig{})
	require.NotNil(t, sess)
	_ = sess.Close(context.Background())
}

func TestDriverAdapterClose(t *testing.T) {
	drv, err := neo4japi.NewDriverWithContext("bolt://localhost:7687", neo4japi.BasicAuth("u", "p", ""))
	require.NoError(t, err)
	da := &driverAdapter{d: drv}
	require.NoError(t, da.Close(context.Background()))
}

func TestNewFullFactoryError(t *testing.T) {
	factErr := errors.New("factory error")
	_, err := newFull(context.Background(), "bolt://localhost:7687", "neo4j", "pw",
		func(_ context.Context, _, _, _ string) (runner, error) { return nil, factErr },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neo4j exporter")
}

func TestJsonMarshalNil(t *testing.T) {
	assert.Equal(t, "null", jsonMarshal(nil))
}

func TestJsonMarshalChannel(t *testing.T) {
	ch := make(chan struct{})
	result := jsonMarshal(ch)
	assert.Equal(t, "null", result)
}

func TestApplyDeltaRunStartedNilRunIsNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaRunStarted, Rev: 1, RunID: "r1",
	}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaRunEndedNilRunIsNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaRunEnded, Rev: 1, RunID: "r1",
	}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaRunStartedEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	run := &model.Run{ID: "r1", Status: model.StatusRunning, LastSeq: 5}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaRunStarted, Rev: 5, RunID: "r1", Run: run,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].cypher, "MERGE")
	assert.Contains(t, r.calls[0].cypher, ":Run")
	assert.Contains(t, r.calls[0].cypher, "coalesce(n.last_seq,-1) <= $last_seq")
	assert.Equal(t, "r1", r.calls[0].params["id"])
}

func TestApplyDeltaRunEndedEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	run := &model.Run{ID: "r1", Status: model.StatusAbandoned, LastSeq: 10}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaRunEnded, Rev: 10, RunID: "r1", Run: run,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].cypher, "MERGE")
	assert.Contains(t, r.calls[0].cypher, ":Run")
}

func TestApplyDeltaRunStartedRunError(t *testing.T) {
	runErr := errors.New("run error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	run := &model.Run{ID: "r1", LastSeq: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaRunStarted, Rev: 1, RunID: "r1", Run: run,
	})
	require.Error(t, err)
}

func TestSnapshotRunsEmptyIsNoop(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	require.NoError(t, e.SnapshotRuns(context.Background(), nil))
	assert.Empty(t, r.calls)
}

func TestSnapshotRunsEmitsCypher(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	runs := []model.Run{
		{ID: "r1", Status: model.StatusRunning, LastSeq: 1},
		{ID: "r2", Status: model.StatusOK, LastSeq: 3},
	}
	require.NoError(t, e.SnapshotRuns(context.Background(), runs))
	assert.Len(t, r.calls, 2, "one cypher per run")
	for _, call := range r.calls {
		assert.Contains(t, call.cypher, "MERGE")
		assert.Contains(t, call.cypher, ":Run")
	}
}

func TestSnapshotRunsRunError(t *testing.T) {
	runErr := errors.New("run error")
	e := ExporterWithRunner(&errorRunner{runErr: runErr})
	runs := []model.Run{{ID: "r1", LastSeq: 1}}
	err := e.SnapshotRuns(context.Background(), runs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot upsert run")
}

func TestSnapshotRunsReproJSONEncoded(t *testing.T) {
	r := &recordRunner{}
	e := ExporterWithRunner(r)
	repro := &model.ReproMeta{Cwd: "/test/cwd", PromptsHash: "abc123"}
	runs := []model.Run{{ID: "r1", Repro: repro, LastSeq: 1}}
	require.NoError(t, e.SnapshotRuns(context.Background(), runs))
	require.Len(t, r.calls, 1)
	props, ok := r.calls[0].params["props"].(map[string]any)
	require.True(t, ok)
	reproStr, ok := props["repro"].(string)
	require.True(t, ok)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(reproStr), &m))
	assert.Equal(t, "/test/cwd", m["cwd"])
}

func TestSafeRFC3339Nil(t *testing.T) {
	assert.Equal(t, "", safeRFC3339(nil))
}

func TestSafeRFC3339NonNil(t *testing.T) {
	now := time.Now()
	result := safeRFC3339(&now)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "T")
}
