package diff

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/stepkey"
)

func ts(sec int64) *time.Time {
	t := time.Unix(sec, 0).UTC()
	return &t
}

func costPtr(f float64) *float64 { return &f }
func durPtr(i int64) *int64      { return &i }
func tokPtr(i int64) *int64      { return &i }

func pipelineGraph(prefix string, baseSec int64, commands []string) ([]*model.Node, []*model.Edge) {
	sess := &model.Node{
		ID:     prefix + "sess",
		Type:   model.NodeSession,
		Status: model.StatusOK,
		TStart: ts(baseSec),
	}
	prompt := &model.Node{
		ID:     prefix + "prompt",
		Type:   model.NodeUserPrompt,
		Status: model.StatusOK,
		TStart: ts(baseSec + 1),
	}
	turn := &model.Node{
		ID:     prefix + "turn",
		Type:   model.NodeAssistantTurn,
		Status: model.StatusOK,
		TStart: ts(baseSec + 2),
	}
	nodes := []*model.Node{sess, prompt, turn}
	edges := []*model.Edge{
		{Type: model.EdgeParentChild, Src: sess.ID, Dst: prompt.ID},
		{Type: model.EdgeParentChild, Src: prompt.ID, Dst: turn.ID},
	}
	for i, cmd := range commands {
		tool := &model.Node{
			ID:     prefix + "tool" + string(rune('0'+i)),
			Type:   model.NodeToolCall,
			Name:   "Bash",
			Status: model.StatusOK,
			TStart: ts(baseSec + 3 + int64(i)),
		}
		tool.Payload = &model.Payload{Input: []byte(`{"command":"` + cmd + `"}`)}
		nodes = append(nodes, tool)
		edges = append(edges, &model.Edge{
			Type: model.EdgeParentChild,
			Src:  turn.ID,
			Dst:  tool.ID,
		})
	}
	return nodes, edges
}

func setCost(nodes []*model.Node, nodeID string, cost float64) {
	for _, n := range nodes {
		if n.ID == nodeID {
			n.CostUSD = costPtr(cost)
			return
		}
	}
}

func setDur(nodes []*model.Node, nodeID string, dur int64) {
	for _, n := range nodes {
		if n.ID == nodeID {
			n.DurationMS = durPtr(dur)
			return
		}
	}
}

func TestDiffIdenticalRunsAllUnchanged(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls", "pwd"})
	bn, be := pipelineGraph("b", 2000, []string{"ls", "pwd"})

	result := DiffGraphs(an, ae, bn, be)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
	assert.Len(t, result.Unchanged, 2)
	for _, u := range result.Unchanged {
		assert.Equal(t, "step_key", u.Tier)
	}
}

func TestDiffCostAndDurationRegression(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls"})
	bn, be := pipelineGraph("b", 2000, []string{"ls"})

	setCost(bn, "btool0", 0.05)
	setDur(bn, "btool0", 1234)

	result := DiffGraphs(an, ae, bn, be)

	require.Len(t, result.Changed, 1)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	c := result.Changed[0]
	require.NotNil(t, c.Deltas.CostUSD)
	assert.InDelta(t, 0.05, c.Deltas.CostUSD.Delta, 1e-9)
	require.NotNil(t, c.Deltas.DurationMS)
	assert.Equal(t, int64(1234), c.Deltas.DurationMS.Delta)
	assert.Nil(t, c.Deltas.Args)
}

func TestDiffAddedToolAtEnd(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls"})
	bn, be := pipelineGraph("b", 2000, []string{"ls", "whoami"})

	result := DiffGraphs(an, ae, bn, be)

	assert.Empty(t, result.Removed)
	assert.Len(t, result.Unchanged, 1)
	require.Len(t, result.Added, 1)
	assert.Equal(t, "Bash", result.Added[0].Tool)
}

func TestDiffRemovedTool(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls", "whoami"})
	bn, be := pipelineGraph("b", 2000, []string{"ls"})

	result := DiffGraphs(an, ae, bn, be)

	assert.Empty(t, result.Added)
	assert.Len(t, result.Unchanged, 1)
	require.Len(t, result.Removed, 1)
	assert.Equal(t, "Bash", result.Removed[0].Tool)
}

func TestDiffArgsDelta(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls"})
	bn, be := pipelineGraph("b", 2000, []string{"ls"})

	for _, n := range an {
		if n.ID == "atool0" {
			n.Payload.Input = []byte(`{"command":"ls","meta":"v1"}`)
		}
	}
	for _, n := range bn {
		if n.ID == "btool0" {
			n.Payload.Input = []byte(`{"command":"ls","meta":"v2"}`)
		}
	}

	result := DiffGraphs(an, ae, bn, be)

	require.Len(t, result.Changed, 1)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	c := result.Changed[0]
	require.NotNil(t, c.Deltas.Args)
	assert.NotEqual(t, c.Deltas.Args.Before, c.Deltas.Args.After)
}

func TestDiffTokensOutDelta(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls"})
	bn, be := pipelineGraph("b", 2000, []string{"ls"})

	for _, n := range bn {
		if n.ID == "btool0" {
			n.TokensOut = tokPtr(50)
		}
	}

	result := DiffGraphs(an, ae, bn, be)

	require.Len(t, result.Changed, 1)
	require.NotNil(t, result.Changed[0].Deltas.TokensOut)
	assert.Equal(t, int64(50), result.Changed[0].Deltas.TokensOut.Delta)
}

func TestNormArgsNilPayload(t *testing.T) {
	n := &model.Node{ID: "x", Status: model.StatusOK}
	assert.Equal(t, "", normArgs(n))
}

func TestNormArgsInvalidJSON(t *testing.T) {
	n := &model.Node{
		ID:      "x",
		Status:  model.StatusOK,
		Payload: &model.Payload{Input: []byte("not json")},
	}
	assert.Equal(t, "not json", normArgs(n))
}

func TestDiffStatusTokensArgsAndOrderStable(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls"})
	bn, be := pipelineGraph("b", 2000, []string{"ls"})

	for _, n := range an {
		if n.ID == "atool0" {
			n.Status = model.StatusError
			n.TokensIn = tokPtr(10)
		}
	}
	for _, n := range bn {
		if n.ID == "btool0" {
			n.Status = model.StatusOK
			n.TokensIn = tokPtr(20)
		}
	}

	result1 := DiffGraphs(an, ae, bn, be)

	anShuffled := make([]*model.Node, len(an))
	copy(anShuffled, an)
	anShuffled[0], anShuffled[len(anShuffled)-1] = anShuffled[len(anShuffled)-1], anShuffled[0]
	result2 := DiffGraphs(anShuffled, ae, bn, be)

	require.Len(t, result1.Changed, 1)
	c := result1.Changed[0]
	require.NotNil(t, c.Deltas.Status)
	assert.Equal(t, "error", c.Deltas.Status.Before)
	assert.Equal(t, "ok", c.Deltas.Status.After)
	require.NotNil(t, c.Deltas.TokensIn)
	assert.Equal(t, int64(10), c.Deltas.TokensIn.Delta)

	assert.Equal(t, result1.Added, result2.Added)
	assert.Equal(t, result1.Removed, result2.Removed)
	assert.Equal(t, result1.Changed, result2.Changed)
	assert.Equal(t, result1.Unchanged, result2.Unchanged)
}

func TestDiffOrderStableUnderFullReversal(t *testing.T) {
	an, ae := pipelineGraph("a", 1000, []string{"ls", "pwd"})
	bn, be := pipelineGraph("b", 2000, []string{"ls", "pwd"})

	result1 := DiffGraphs(an, ae, bn, be)

	anRev := make([]*model.Node, len(an))
	copy(anRev, an)
	for i, j := 0, len(anRev)-1; i < j; i, j = i+1, j-1 {
		anRev[i], anRev[j] = anRev[j], anRev[i]
	}
	aeRev := make([]*model.Edge, len(ae))
	copy(aeRev, ae)
	for i, j := 0, len(aeRev)-1; i < j; i, j = i+1, j-1 {
		aeRev[i], aeRev[j] = aeRev[j], aeRev[i]
	}
	bnRev := make([]*model.Node, len(bn))
	copy(bnRev, bn)
	for i, j := 0, len(bnRev)-1; i < j; i, j = i+1, j-1 {
		bnRev[i], bnRev[j] = bnRev[j], bnRev[i]
	}
	beRev := make([]*model.Edge, len(be))
	copy(beRev, be)
	for i, j := 0, len(beRev)-1; i < j; i, j = i+1, j-1 {
		beRev[i], beRev[j] = beRev[j], beRev[i]
	}

	result2 := DiffGraphs(anRev, aeRev, bnRev, beRev)

	require.Equal(t, result1, result2)
}

func bashNode(id, cmd string, sec int64) *model.Node {
	n := &model.Node{ID: id, Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(sec)}
	n.Payload = &model.Payload{Input: []byte(`{"command":"` + cmd + `"}`)}
	return n
}

func TestStepsAreReportedInWallClockOrderNotStepKeyOrder(t *testing.T) {
	turn := &model.Node{ID: "turn", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	early := bashNode("early", "ls", 1)
	late := bashNode("late", "cat x", 2)
	nodes := []*model.Node{turn, early, late}
	edges := []*model.Edge{
		{Type: model.EdgeParentChild, Src: "turn", Dst: "early"},
		{Type: model.EdgeParentChild, Src: "turn", Dst: "late"},
	}

	keys := stepkey.Compute(nodes, edges)
	require.Greater(t, keys["early"].Key, keys["late"].Key,
		"fixture must invert the step-key tiebreak, otherwise wall-clock ordering is unobservable")

	result := DiffGraphs(nodes, edges, nil, nil)

	require.Len(t, result.Removed, 2)
	assert.Equal(t,
		[]string{keys["early"].Content, keys["late"].Content},
		[]string{result.Removed[0].ContentKey, result.Removed[1].ContentKey},
		"steps must be emitted oldest-first")
}

func TestRepeatedContentIsRealignedByLongestCommonSubsequenceNotByPosition(t *testing.T) {
	build := func(prefix string, cmds []string) ([]*model.Node, []*model.Edge) {
		turn := &model.Node{ID: prefix + "turn", Type: model.NodeAssistantTurn, Status: model.StatusOK}
		nodes := []*model.Node{turn}
		edges := []*model.Edge{}
		for i, c := range cmds {
			id := fmt.Sprintf("%s%d", prefix, i)
			nodes = append(nodes, bashNode(id, c, int64(i+1)))
			edges = append(edges, &model.Edge{Type: model.EdgeParentChild, Src: turn.ID, Dst: id})
		}
		return nodes, edges
	}

	an, ae := build("a", []string{"ls", "ls", "cat x"})
	bn, be := build("b", []string{"echo one", "echo two", "ls", "ls", "cat x"})

	result := DiffGraphs(an, ae, bn, be)

	assert.Empty(t, result.Removed, "nothing was dropped from the A run")
	assert.Empty(t, result.Changed,
		"the two repeated ls calls must realign onto the shifted ls calls, not onto the inserted echo calls")
	assert.Len(t, result.Unchanged, 3)
	assert.Len(t, result.Added, 2)
}
