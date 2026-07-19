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
)

const markerExecA = "execA"

const markerExecB = "execB"

func markedObservations(exec string, commands []string) []model.Observation {
	base := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	obs := []model.Observation{
		{
			ObsID: exec + "-p", RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
			Kind: "user_prompt", Correlation: model.Correlation{UUID: "u1", SessionID: exec},
			EventTime: at(0), ObservedAt: at(0), Seq: 1,
		},
		{
			ObsID: exec + "-mark", RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
			Kind:        "marker",
			Correlation: model.Correlation{UUID: "m1", SessionID: exec},
			Attrs:       map[string]any{"name": "plan", "boundary": "start"},
			EventTime:   at(1), ObservedAt: at(1), Seq: 2,
		},
		{
			ObsID: exec + "-p2", RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
			Kind: "user_prompt", Correlation: model.Correlation{UUID: "u2", SessionID: exec},
			EventTime: at(2), ObservedAt: at(2), Seq: 3,
		},
	}
	for i, cmd := range commands {
		msg := "msg_" + cmd
		use := i*2 + 4
		input, err := json.Marshal(map[string]string{"command": cmd})
		if err != nil {
			panic(err)
		}
		obs = append(obs,
			model.Observation{
				ObsID: exec + "-t" + cmd, RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
				Kind:        "assistant_turn",
				Correlation: model.Correlation{MessageID: msg, SessionID: exec},
				EventTime:   at(use), ObservedAt: at(use), Seq: uint64(use),
			},
			model.Observation{
				ObsID: exec + "-u" + cmd, RunID: exec, ExecutionID: exec, Source: model.SourceJSONL,
				Kind:        "assistant_tool_use",
				Correlation: model.Correlation{ToolUseID: "toolu_" + cmd, MessageID: msg, SessionID: exec},
				Attrs:       map[string]any{"name": "Bash", "status": string(model.StatusOK)},
				Payload:     &model.Payload{Input: input},
				EventTime:   at(use + 1), ObservedAt: at(use + 1), Seq: uint64(use + 1),
			},
		)
	}
	return obs
}

func markedSnapshot(t *testing.T, exec string, commands []string) ([]*model.Node, []*model.Edge) {
	t.Helper()
	g := reduce.NewGraph()
	g.ApplyAll(markedObservations(exec, commands))
	nodes, edges := g.Snapshot()
	var markers int
	for _, n := range nodes {
		if n.Type == model.NodeMarker {
			markers++
		}
	}
	require.Positive(t, markers, "fixture must contain synthesized marker nodes")
	return nodes, edges
}

func canonicalKeys(t *testing.T, nodes []*model.Node) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range nodes {
		if n.StepKey != "" {
			out[n.ID] = n.StepKey
		}
	}
	require.NotEmpty(t, out)
	return out
}

func TestBuildItemsKeysMatchCanonicalStepKeyOnMarkedGraph(t *testing.T) {
	nodes, edges := markedSnapshot(t, markerExecA, []string{"ls", "pwd"})
	want := canonicalKeys(t, nodes)

	items := buildItems(nodes, edges)
	require.Len(t, items, len(want))
	for _, it := range items {
		assert.Equal(t, want[it.node.ID], it.step, "diff step key must equal canonical Node.StepKey for %s", it.node.ID)
	}
}

func TestBuildItemsIgnoresMarkersWhenComputingKeys(t *testing.T) {
	nodes, edges := markedSnapshot(t, markerExecA, []string{"ls", "pwd"})
	markerFree, markerFreeEdges := withoutMarkers(nodes, edges)
	for _, n := range markerFree {
		assert.NotEqual(t, model.NodeMarker, n.Type)
	}
	assert.Less(t, len(markerFree), len(nodes))
	assert.Less(t, len(markerFreeEdges), len(edges))

	keys := stepkey.Compute(markerFree, markerFreeEdges)
	for _, it := range buildItems(nodes, edges) {
		assert.Equal(t, keys[it.node.ID].Key, it.step)
		assert.Equal(t, keys[it.node.ID].Content, it.content)
		assert.Equal(t, keys[it.node.ID].PathKey, it.pathKey)
	}
}

func TestDiffGraphsReportsCanonicalStepKeysForDifferentMarkedTranscripts(t *testing.T) {
	an, ae := markedSnapshot(t, markerExecA, []string{"ls", "pwd"})
	bn, be := markedSnapshot(t, markerExecB, []string{"ls", "whoami"})

	wantA := canonicalKeys(t, an)
	wantB := canonicalKeys(t, bn)
	reportedA := map[string]bool{}
	reportedB := map[string]bool{}

	res := DiffGraphs(an, ae, bn, be)
	require.NotEmpty(t, res.Unchanged)
	for _, m := range res.Unchanged {
		reportedA[m.AStepKey] = true
		reportedB[m.BStepKey] = true
	}
	for _, c := range res.Changed {
		reportedA[c.AStepKey] = true
		reportedB[c.BStepKey] = true
	}
	for _, s := range res.Removed {
		reportedA[s.StepKey] = true
	}
	for _, s := range res.Added {
		reportedB[s.StepKey] = true
	}

	assert.Subset(t, canonicalValues(wantA), keysOf(reportedA))
	assert.Subset(t, canonicalValues(wantB), keysOf(reportedB))
}

func canonicalValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
