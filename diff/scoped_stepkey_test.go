package diff

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/stepkey"
	"github.com/realkarych/catacomb/subgraph"
)

func promptPerCommandObservations(t *testing.T, exec string, commands []string) []model.Observation {
	t.Helper()
	base := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	obs := []model.Observation{{
		ObsID: exec + "-mark", RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
		Kind: "marker", Correlation: model.Correlation{UUID: "m1", SessionID: exec},
		Attrs:     map[string]any{"name": "plan", "boundary": "start"},
		EventTime: at(0), ObservedAt: at(0), Seq: 1,
	}}
	for i, command := range commands {
		tick := i*3 + 1
		msg := "msg_" + command
		input, err := json.Marshal(map[string]string{"command": command})
		require.NoError(t, err)
		obs = append(obs,
			model.Observation{
				ObsID: exec + "-p" + command, RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
				Kind: "user_prompt", Correlation: model.Correlation{UUID: "u" + command, SessionID: exec},
				EventTime: at(tick), ObservedAt: at(tick), Seq: uint64(tick + 1),
			},
			model.Observation{
				ObsID: exec + "-t" + command, RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
				Kind: "assistant_turn", Correlation: model.Correlation{MessageID: msg, SessionID: exec},
				EventTime: at(tick + 1), ObservedAt: at(tick + 1), Seq: uint64(tick + 2),
			},
			model.Observation{
				ObsID: exec + "-u" + command, RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
				Kind:        "assistant_tool_use",
				Correlation: model.Correlation{ToolUseID: "toolu_" + command, MessageID: msg, SessionID: exec},
				Attrs:       map[string]any{"name": "Bash", "status": string(model.StatusOK)},
				Payload:     &model.Payload{Input: input},
				EventTime:   at(tick + 2), ObservedAt: at(tick + 2), Seq: uint64(tick + 3),
			},
		)
	}
	return obs
}

func promptScopedSnapshot(t *testing.T, exec string, commands []string) ([]*model.Node, []*model.Edge, []*model.Node, []*model.Edge) {
	t.Helper()
	g := reduce.NewGraph()
	g.ApplyAll(promptPerCommandObservations(t, exec, commands))
	nodes, edges := g.Snapshot()
	parsed, err := subgraph.ParseSpec(subgraph.Spec{Phase: "plan"})
	require.NoError(t, err)
	sn, se, ok := subgraph.ScopeExecutionParsedAnchored(nodes, edges, exec, parsed)
	require.True(t, ok)
	return nodes, edges, sn, se
}

func pathKeysByNode(nodes []*model.Node, edges []*model.Edge) map[string]string {
	out := map[string]string{}
	for id, k := range stepkey.Compute(nodes, edges) {
		out[id] = k.PathKey
	}
	return out
}

func TestPhaseScopedGraphKeepsPromptLevelInPathKeys(t *testing.T) {
	full, fullEdges, scoped, scopedEdges := promptScopedSnapshot(t, markerExecA, []string{"ls", "pwd"})

	fullKeys := pathKeysByNode(withoutMarkers(full, fullEdges))
	scopedKeys := pathKeysByNode(scoped, scopedEdges)
	require.Len(t, scopedKeys, 2)

	lsID := markerExecA + ":tool:toolu_ls"
	pwdID := markerExecA + ":tool:toolu_pwd"
	require.NotEqual(t, fullKeys[lsID], fullKeys[pwdID],
		"fixture must put the two tool calls under different prompts")
	assert.NotEqual(t, scopedKeys[lsID], scopedKeys[pwdID],
		"scoping must not collapse tool calls from different prompts onto one path key")
}

func TestDiffOfPhaseScopedGraphsDistinguishesSwappedPrompts(t *testing.T) {
	_, _, an, ae := promptScopedSnapshot(t, markerExecA, []string{"ls", "pwd"})
	_, _, bn, be := promptScopedSnapshot(t, markerExecB, []string{"pwd", "ls"})

	res := DiffGraphs(an, ae, bn, be)

	assert.Empty(t, res.Added)
	assert.Empty(t, res.Removed)
	matches := allMatches(res)
	require.Len(t, matches, 2)
	for _, m := range matches {
		assert.Equal(t, "content", m.Tier,
			"a tool call that moved to another prompt must not keep the same step key")
		assert.NotEqual(t, m.AStepKey, m.BStepKey)
		assert.Equal(t, m.AContentKey, m.BContentKey)
	}
}

func allMatches(res DiffResult) []Match {
	out := make([]Match, 0, len(res.Unchanged)+len(res.Changed))
	out = append(out, res.Unchanged...)
	for _, c := range res.Changed {
		out = append(out, c.Match)
	}
	return out
}
