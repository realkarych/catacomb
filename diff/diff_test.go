package diff

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
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

func TestTierOfContentAndPosition(t *testing.T) {
	sameContent := item{
		node:    &model.Node{ID: "x", Status: model.StatusOK},
		step:    "sk1",
		content: "same",
		pathKey: "pk1",
	}
	diffContent := item{
		node:    &model.Node{ID: "y", Status: model.StatusOK},
		step:    "sk2",
		content: "other",
		pathKey: "pk2",
	}
	sameContentB := item{
		node:    &model.Node{ID: "z", Status: model.StatusOK},
		step:    "sk2",
		content: "same",
		pathKey: "pk2",
	}

	assert.Equal(t, "step_key", tierOf(sameContent, sameContent))
	assert.Equal(t, "content", tierOf(sameContent, sameContentB))
	assert.Equal(t, "position", tierOf(sameContent, diffContent))
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
