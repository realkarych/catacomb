package reduce

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/phasekey"
)

func markerToolUse(toolUseID, name, boundary, stateRef string, occ *int, ts time.Time, seq uint64) model.Observation {
	input := map[string]any{"name": name, "boundary": boundary}
	if stateRef != "" {
		input["state_ref"] = stateRef
	}
	if occ != nil {
		input["occurrence"] = *occ
	}
	raw, _ := json.Marshal(input)
	return model.Observation{
		ObsID:       toolUseID,
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUseID},
		Attrs:       map[string]any{"name": "mcp__catacomb__mark"},
		Payload:     &model.Payload{Input: raw},
		EventTime:   ts,
		ObservedAt:  ts,
		Seq:         seq,
	}
}

func markerToolResult(toolUseID string, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID:       toolUseID + "_r",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "tool_result",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUseID},
		Attrs:       map[string]any{"name": "mcp__catacomb__mark", "status": string(model.StatusOK)},
		EventTime:   ts,
		ObservedAt:  ts,
		Seq:         seq,
	}
}

func sessionStart(ts time.Time) model.Observation {
	return model.Observation{
		ObsID:       "sess_start",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "session_start",
		Correlation: model.Correlation{SessionID: runID},
		EventTime:   ts,
		ObservedAt:  ts,
		Seq:         1,
	}
}

func sessionEnd(ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID:       "sess_end",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "session_end",
		Correlation: model.Correlation{SessionID: runID},
		EventTime:   ts,
		ObservedAt:  ts,
		Seq:         seq,
	}
}

func TestIsMarkerTool(t *testing.T) {
	assert.True(t, isMarkerTool("mcp__catacomb__mark"))
	assert.True(t, isMarkerTool("catacomb__mark"))
	assert.False(t, isMarkerTool("Bash"))
	assert.False(t, isMarkerTool("mcp__other__tool"))
	assert.False(t, isMarkerTool(""))
}

func TestMarkerToolCallSuppressedFromGraph(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t0.Add(time.Second), 2))

	toolID := model.ToolCallID(execID, "tu1")
	assert.Nil(t, g.Nodes[toolID])
}

func TestMarkerToolResultSuppressed(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t0.Add(time.Second), 2))
	g.Apply(markerToolResult("tu1", t0.Add(2*time.Second), 3))

	toolID := model.ToolCallID(execID, "tu1")
	assert.Nil(t, g.Nodes[toolID])
}

func TestSnapshotPhaseMarkerSynthesized(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 2))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 3))

	nodes, edges := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)

	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found, "phase marker node not in snapshot")
	assert.Equal(t, model.NodeMarker, found.Type)
	assert.Equal(t, t1, *found.TStart)
	assert.Equal(t, t2, *found.TEnd)
	assert.NotEmpty(t, found.PhaseKey)
	assert.Equal(t, phasekey.Compute("", "phase1", 0), found.PhaseKey)

	sessEdge := model.EdgeID(execID, model.EdgeParentChild, model.SessionNodeID(execID), markerID)
	var foundEdge bool
	for _, e := range edges {
		if e.ID == sessEdge {
			foundEdge = true
			break
		}
	}
	assert.True(t, foundEdge, "session→marker parent_child edge missing")
}

func TestSnapshotMarkerClearAndRebuild(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 2))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 3))

	markerID := model.PhaseMarkerID(execID, "phase1", 0)

	nodes1, _ := g.Snapshot()
	nodes2, _ := g.Snapshot()

	count1 := countNodes(nodes1, markerID)
	count2 := countNodes(nodes2, markerID)
	assert.Equal(t, 1, count1, "first snapshot should have exactly one phase marker")
	assert.Equal(t, 1, count2, "second snapshot should not duplicate the marker")
}

func countNodes(nodes []*model.Node, id string) int {
	n := 0
	for _, nd := range nodes {
		if nd.ID == id {
			n++
		}
	}
	return n
}

func TestMarkerSpanEdges(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(3 * time.Second)
	t3 := t0.Add(5 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))

	up := ob("user_prompt", "", t1)
	up.Seq = 2
	up.Correlation.UUID = "u1"
	g.Apply(up)

	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 3))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t3, 4))

	turnObs := ob("assistant_turn", "", t2)
	turnObs.Seq = 5
	turnObs.Correlation.MessageID = "msg1"
	g.Apply(turnObs)

	_, edges := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	promptID := model.UserPromptID(execID, "u1")
	turnID := model.AssistantTurnID(execID, "msg1")

	spanToPrompt := model.EdgeID(execID, model.EdgeMarkerSpan, markerID, promptID)
	spanToTurn := model.EdgeID(execID, model.EdgeMarkerSpan, markerID, turnID)

	edgeIDs := make(map[string]bool)
	for _, e := range edges {
		edgeIDs[e.ID] = true
	}

	assert.True(t, edgeIDs[spanToPrompt], "expected marker_span to user_prompt within range")
	assert.True(t, edgeIDs[spanToTurn], "expected marker_span to assistant_turn within range")
}

func TestMarkerSpanExcludesOutOfRange(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)
	t3 := t0.Add(4 * time.Second)
	t4 := t0.Add(5 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))

	earlyPrompt := ob("user_prompt", "", t0)
	earlyPrompt.Seq = 2
	earlyPrompt.Correlation.UUID = "early"
	g.Apply(earlyPrompt)

	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 3))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 4))

	latePrompt := ob("user_prompt", "", t3)
	latePrompt.Seq = 5
	latePrompt.Correlation.UUID = "late"
	g.Apply(latePrompt)

	g.Apply(markerToolUse("tu3", "phase1", "start", "", nil, t3, 6))
	g.Apply(markerToolUse("tu4", "phase1", "end", "", nil, t4, 7))

	_, edges := g.Snapshot()

	markerID0 := model.PhaseMarkerID(execID, "phase1", 0)
	earlyID := model.UserPromptID(execID, "early")
	lateID := model.UserPromptID(execID, "late")

	edgeIDs := make(map[string]bool)
	for _, e := range edges {
		edgeIDs[e.ID] = true
	}

	assert.False(t, edgeIDs[model.EdgeID(execID, model.EdgeMarkerSpan, markerID0, earlyID)],
		"early node before phase should not get marker_span")
	assert.False(t, edgeIDs[model.EdgeID(execID, model.EdgeMarkerSpan, markerID0, lateID)],
		"late node after phase end should not get marker_span")
}

func TestPhaseKeyOnMarker(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "myphase", "start", "", nil, t1, 2))
	g.Apply(markerToolUse("tu2", "myphase", "end", "", nil, t2, 3))

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "myphase", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found)
	expected := phasekey.Compute("", "myphase", 0)
	assert.Equal(t, expected, found.PhaseKey)
	assert.Len(t, found.PhaseKey, 32)
}

func TestMarkerDeterminismShuffledArrival(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	start := markerToolUse("tu1", "phase1", "start", "", nil, t1, 2)
	end := markerToolUse("tu2", "phase1", "end", "", nil, t2, 3)

	sess := sessionStart(t0)

	g1 := NewGraph()
	g1.ApplyAll([]model.Observation{sess, start, end})
	nodes1, edges1 := g1.Snapshot()

	g2 := NewGraph()
	g2.ApplyAll([]model.Observation{sess, end, start})
	nodes2, edges2 := g2.Snapshot()

	assert.Equal(t, fingerprint(nodes1, edges1), fingerprint(nodes2, edges2))
}

func TestMarkerIdempotentResnapshot(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStart(t0),
		markerToolUse("tu1", "phase1", "start", "", nil, t1, 2),
		markerToolUse("tu2", "phase1", "end", "", nil, t2, 3),
	})

	nodes1, edges1 := g.Snapshot()
	nodes2, edges2 := g.Snapshot()
	nodes3, edges3 := g.Snapshot()

	fp1 := fingerprint(nodes1, edges1)
	assert.Equal(t, fp1, fingerprint(nodes2, edges2))
	assert.Equal(t, fp1, fingerprint(nodes3, edges3))
}

func TestMarkerRebuildFromLog(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	obs := []model.Observation{
		sessionStart(t0),
		markerToolUse("tu1", "phase1", "start", "", nil, t1, 2),
		markerToolUse("tu2", "phase1", "end", "", nil, t2, 3),
	}

	g1 := NewGraph()
	g1.ApplyAll(obs)
	nodes1, edges1 := g1.Snapshot()

	g2 := NewGraph()
	g2.ApplyAll(obs)
	nodes2, edges2 := g2.Snapshot()

	assert.Equal(t, fingerprint(nodes1, edges1), fingerprint(nodes2, edges2))
}

func fingerprint(nodes []*model.Node, edges []*model.Edge) string {
	ids := make([]string, 0, len(nodes)+len(edges))
	for _, n := range nodes {
		ids = append(ids, "n:"+n.ID+":"+n.PhaseKey)
	}
	for _, e := range edges {
		ids = append(ids, "e:"+e.ID)
	}
	sort.Strings(ids)
	b, _ := json.Marshal(ids)
	return string(b)
}

func TestStepKeyUnaffectedByMarkers(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)
	t3 := t0.Add(3 * time.Second)

	use := ob("assistant_tool_use", "toolu_mk1", t1)
	use.Seq = 2
	use.Correlation.MessageID = "msg_mk1"
	use.Attrs = map[string]any{"name": "Bash"}
	use.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}

	res := ob("tool_result", "toolu_mk1", t2)
	res.Seq = 3
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	gWithout := NewGraph()
	gWithout.ApplyAll([]model.Observation{sessionStart(t0), use, res})
	nodesWithout, _ := gWithout.Snapshot()

	gWith := NewGraph()
	gWith.ApplyAll([]model.Observation{
		sessionStart(t0),
		use,
		res,
		markerToolUse("tu1", "phase1", "start", "", nil, t1, 4),
		markerToolUse("tu2", "phase1", "end", "", nil, t3, 5),
	})
	nodesWith, _ := gWith.Snapshot()

	toolID := model.ToolCallID(execID, "toolu_mk1")
	var skWithout, skWith string
	for _, n := range nodesWithout {
		if n.ID == toolID {
			skWithout = n.StepKey
		}
	}
	for _, n := range nodesWith {
		if n.ID == toolID {
			skWith = n.StepKey
		}
	}
	require.NotEmpty(t, skWithout)
	assert.Equal(t, skWithout, skWith, "step_key must not be affected by presence of markers")
}

func TestOpenPhaseNoEnd(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 2))

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, true, found.Attrs["open"])
	assert.Nil(t, found.TEnd)
	assert.Nil(t, found.DurationMS)
}

func TestOpenPhaseClosesAtSessionTEnd(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	tEnd := t0.Add(10 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 2))
	g.Apply(sessionEnd(tEnd, 3))

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, true, found.Attrs["open"])
	require.NotNil(t, found.TEnd)
	assert.Equal(t, tEnd, *found.TEnd)
}

func TestOrphanEndNoNode(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "end", "", nil, t1, 2))

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	for _, n := range nodes {
		assert.NotEqual(t, markerID, n.ID, "orphan end should not create a marker node")
	}
}

func TestExplicitOccurrenceHonored(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)
	occ := 5

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", &occ, t1, 2))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 3))

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 5)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found, "explicit occurrence=5 should produce PhaseMarkerID with occ=5")
}

func TestStateRefStoredOpaque(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "checkpoint_abc", nil, t1, 2))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 3))

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "checkpoint_abc", found.Attrs["state_ref"])
}

func TestExistingPointMarkerUnchanged(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "m1", RunID: "s1", ExecutionID: "e1", Source: model.SourceHook, Kind: "marker",
		Correlation: model.Correlation{SessionID: "s1"},
		Attrs:       map[string]any{"hook_event": "PreCompact", "trigger": "auto"},
		EventTime:   time.Unix(1, 0).UTC(), Seq: 1,
	}
	g.Apply(o)
	n := g.Nodes[model.MarkerID("e1", "m1")]
	require.NotNil(t, n)
	assert.Equal(t, model.NodeMarker, n.Type)
	assert.Equal(t, "auto", n.Attrs["trigger"])
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), n.ID))
}

func TestSecondaryChannelMarkerObs(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))

	startObs := model.Observation{
		ObsID:       "obs_start",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: runID},
		Attrs:       map[string]any{"name": "sec_phase", "boundary": "start"},
		EventTime:   t1,
		ObservedAt:  t1,
		Seq:         2,
	}
	endObs := model.Observation{
		ObsID:       "obs_end",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: runID},
		Attrs:       map[string]any{"name": "sec_phase", "boundary": "end"},
		EventTime:   t2,
		ObservedAt:  t2,
		Seq:         3,
	}
	g.ApplyAll([]model.Observation{startObs, endObs})

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "sec_phase", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found, "secondary channel phase marker should be synthesized")
	assert.Equal(t, t1, *found.TStart)
	assert.Equal(t, t2, *found.TEnd)
}

func TestMultipleOccurrencesSameName(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)
	t3 := t0.Add(3 * time.Second)
	t4 := t0.Add(4 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 2))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 3))
	g.Apply(markerToolUse("tu3", "phase1", "start", "", nil, t3, 4))
	g.Apply(markerToolUse("tu4", "phase1", "end", "", nil, t4, 5))

	nodes, _ := g.Snapshot()

	id0 := model.PhaseMarkerID(execID, "phase1", 0)
	id1 := model.PhaseMarkerID(execID, "phase1", 1)

	nodeMap := map[string]*model.Node{}
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	require.NotNil(t, nodeMap[id0])
	require.NotNil(t, nodeMap[id1])
	assert.Equal(t, t1, *nodeMap[id0].TStart)
	assert.Equal(t, t2, *nodeMap[id0].TEnd)
	assert.Equal(t, t3, *nodeMap[id1].TStart)
	assert.Equal(t, t4, *nodeMap[id1].TEnd)
}

func TestMarkerSpanSelfExcluded(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 2))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 3))

	_, edges := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	selfEdge := model.EdgeID(execID, model.EdgeMarkerSpan, markerID, markerID)
	for _, e := range edges {
		assert.NotEqual(t, selfEdge, e.ID, "marker_span self-edge must not exist")
	}
}

func TestExtractMarkerFromPayload(t *testing.T) {
	occ := 2
	o := markerToolUse("tu1", "mymarker", "start", "ref_abc", &occ, time.Now(), 1)
	name, boundary, stateRef, occVal, ok := extractMarkerFromPayload(o)
	assert.True(t, ok)
	assert.Equal(t, "mymarker", name)
	assert.Equal(t, "start", boundary)
	assert.Equal(t, "ref_abc", stateRef)
	assert.Equal(t, 2, occVal)
}

func TestExtractMarkerFromPayloadMissingPayload(t *testing.T) {
	o := model.Observation{}
	_, _, _, _, ok := extractMarkerFromPayload(o)
	assert.False(t, ok)
}

func TestExtractMarkerFromPayloadEmptyName(t *testing.T) {
	raw := []byte(`{"name":"","boundary":"start"}`)
	o := model.Observation{Payload: &model.Payload{Input: raw}}
	_, _, _, _, ok := extractMarkerFromPayload(o)
	assert.False(t, ok)
}

func TestExtractMarkerFromPayloadInvalidJSON(t *testing.T) {
	o := model.Observation{Payload: &model.Payload{Input: []byte(`not json`)}}
	_, _, _, _, ok := extractMarkerFromPayload(o)
	assert.False(t, ok)
}

func TestExtractMarkerFromAttrs(t *testing.T) {
	o := model.Observation{
		Attrs: map[string]any{
			"name":       "myphase",
			"boundary":   "end",
			"state_ref":  "stateX",
			"occurrence": float64(3),
		},
	}
	name, boundary, stateRef, occ, ok := extractMarkerFromAttrs(o)
	assert.True(t, ok)
	assert.Equal(t, "myphase", name)
	assert.Equal(t, "end", boundary)
	assert.Equal(t, "stateX", stateRef)
	assert.Equal(t, 3, occ)
}

func TestExtractMarkerFromAttrsNoOccurrence(t *testing.T) {
	o := model.Observation{
		Attrs: map[string]any{"name": "p", "boundary": "start"},
	}
	_, _, _, occ, ok := extractMarkerFromAttrs(o)
	assert.True(t, ok)
	assert.Equal(t, -1, occ)
}

func TestExtractMarkerFromAttrsMissingBoundary(t *testing.T) {
	o := model.Observation{Attrs: map[string]any{"name": "p"}}
	_, _, _, _, ok := extractMarkerFromAttrs(o)
	assert.False(t, ok)
}

func TestNoMarkerNodesWhenNoBounds(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.Apply(sessionStart(t0))
	nodes, _ := g.Snapshot()
	for _, n := range nodes {
		assert.NotEqual(t, model.NodeMarker, n.Type,
			"no marker nodes expected when no boundary observations")
	}
}

func TestSynthesizeMarkersNoSessionNode(t *testing.T) {
	g := NewGraph()
	s := g.execState("orphan_exec")
	s.markerBounds = []markerBound{
		{name: "p", boundary: "start", occ: -1, ts: time.Now(), seq: 1},
	}
	g.synthesizeMarkers()
	assert.Empty(t, g.synthMarkerNodes)
}

func TestMarkerToolResultWithoutNameSuppressed(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.Apply(sessionStart(t0))
	g.Apply(markerToolUse("tu_noname", "phase1", "start", "", nil, t0.Add(time.Second), 2))

	res := model.Observation{
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "tool_result",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: "tu_noname"},
		Attrs:       map[string]any{"status": string(model.StatusOK)},
		EventTime:   t0.Add(2 * time.Second),
		Seq:         3,
	}
	g.Apply(res)

	toolID := model.ToolCallID(execID, "tu_noname")
	assert.Nil(t, g.Nodes[toolID], "tool_result for marker tool should be suppressed even without name attr")
}

func TestAddMarkerSpansSkipsNilTStart(t *testing.T) {
	t1 := time.Date(2026, 6, 1, 0, 0, 1, 0, time.UTC)
	t2 := t1.Add(time.Second)

	g := NewGraph()
	g.Apply(markerToolUse("tu1", "phase1", "start", "", nil, t1, 1))
	g.Apply(markerToolUse("tu2", "phase1", "end", "", nil, t2, 2))

	_, edges := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	sessID := model.SessionNodeID(execID)
	selfSpan := model.EdgeID(execID, model.EdgeMarkerSpan, markerID, sessID)
	for _, e := range edges {
		assert.NotEqual(t, selfSpan, e.ID,
			"session node with nil TStart should not get a marker_span edge")
	}
}

func TestSubagentEnclosingStepKey(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	g := NewGraph()
	g.Apply(sessionStart(t0))

	subagentStop := model.Observation{
		ObsID:       "sa_stop",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "subagent_stop",
		Correlation: model.Correlation{SessionID: runID, AgentID: "agent1", ParentToolUseID: "tu_parent"},
		Attrs:       map[string]any{"subagent_type": "claude"},
		EventTime:   t0,
		ObservedAt:  t0,
		Seq:         2,
	}
	g.Apply(subagentStop)

	startObs := model.Observation{
		ObsID:       "obs_start",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: runID, AgentID: "agent1"},
		Attrs:       map[string]any{"name": "subphase", "boundary": "start"},
		EventTime:   t1,
		ObservedAt:  t1,
		Seq:         3,
	}
	endObs := model.Observation{
		ObsID:       "obs_end",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: runID, AgentID: "agent1"},
		Attrs:       map[string]any{"name": "subphase", "boundary": "end"},
		EventTime:   t2,
		ObservedAt:  t2,
		Seq:         4,
	}
	g.ApplyAll([]model.Observation{startObs, endObs})

	nodes, _ := g.Snapshot()

	markerID := model.PhaseMarkerID(execID, "subphase", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found, "subagent-originated phase marker should be synthesized")
	assert.NotEmpty(t, found.PhaseKey, "phase_key should be computed with subagent's step_key")
}
