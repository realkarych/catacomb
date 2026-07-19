package reduce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/stepkey"
)

func nodeByID(nodes []*model.Node, id string) *model.Node {
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func toolCallObs(toolUseID, msgID, name, input string, t0 time.Time) []model.Observation {
	use := ob("assistant_tool_use", toolUseID, t0)
	use.Correlation.MessageID = msgID
	use.Attrs = map[string]any{"name": name}
	use.Payload = &model.Payload{Input: []byte(input)}
	res := ob("tool_result", toolUseID, t0.Add(time.Second))
	res.Attrs = map[string]any{"status": string(model.StatusOK)}
	return []model.Observation{use, res}
}

func TestSnapshotStampsExactlyTheKeysStepkeyComputesForTheSnapshotItself(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.ApplyAll(toolCallObs("toolu_sk1", "msg_sk1", "Bash", `{"command":"ls"}`, t0))
	g.ApplyAll(toolCallObs("toolu_sk2", "msg_sk1", "mcp__fs__read", `{"path":"a.go"}`, t0.Add(2*time.Second)))

	nodes, edges := g.Snapshot()
	want := stepkey.Compute(nodes, edges)
	require.Len(t, want, 2)

	stamped := map[string]string{}
	for _, n := range nodes {
		if n.StepKey == "" {
			assert.Empty(t, n.StepKeyMethod, "node %s carries a method without a key", n.ID)
			continue
		}
		assert.Equal(t, stepkey.Method, n.StepKeyMethod)
		stamped[n.ID] = n.StepKey
	}

	wantStamped := map[string]string{}
	for id, k := range want {
		wantStamped[id] = k.Key
	}
	assert.Equal(t, wantStamped, stamped,
		"Snapshot must stamp every eligible node with its own computed key and no other node")
}

func TestSnapshotSessionNodeHasNoStepKey(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	up := ob("user_prompt", "", t0)
	up.Correlation.UUID = "u_sk1"

	g := NewGraph()
	g.Apply(up)

	nodes, _ := g.Snapshot()

	sess := nodeByID(nodes, model.SessionNodeID(execID))
	require.NotNil(t, sess)
	assert.Empty(t, sess.StepKey)
	assert.Empty(t, sess.StepKeyMethod)
}

func TestSnapshotStepKeyIgnoresTimestampsButTracksToolIdentity(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	keyFor := func(obs []model.Observation) string {
		g := NewGraph()
		g.ApplyAll(obs)
		nodes, _ := g.Snapshot()
		n := nodeByID(nodes, model.ToolCallID(execID, "toolu_stable"))
		require.NotNil(t, n)
		require.NotEmpty(t, n.StepKey)
		return n.StepKey
	}

	base := keyFor(toolCallObs("toolu_stable", "msg_stable", "Read", `{"file_path":"a.go"}`, t0))

	assert.Equal(t, base, keyFor(toolCallObs("toolu_stable", "msg_stable", "Read", `{"file_path":"a.go"}`, t0.Add(time.Hour))),
		"the step key identifies a step across runs, so wall-clock drift must not move it")
	assert.NotEqual(t, base, keyFor(toolCallObs("toolu_stable", "msg_stable", "Write", `{"file_path":"a.go"}`, t0)),
		"a different tool is a different step")
	assert.NotEqual(t, base, keyFor(toolCallObs("toolu_stable", "msg_stable", "Read", `{"file_path":"b.go"}`, t0)),
		"a different target is a different step")
}

func TestSnapshotRestampsStepKeysWhenALateObservationShiftsOccurrenceIndex(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.ApplyAll(toolCallObs("toolu_a", "msg_r", "Read", `{"file_path":"a.go"}`, t0))

	first, _ := g.Snapshot()
	before := nodeByID(first, model.ToolCallID(execID, "toolu_a")).StepKey
	require.NotEmpty(t, before)

	g.ApplyAll(toolCallObs("toolu_earlier", "msg_r", "Read", `{"file_path":"a.go"}`, t0.Add(-time.Hour)))
	second, edges := g.Snapshot()

	want := stepkey.Compute(second, edges)
	for _, n := range second {
		if k, ok := want[n.ID]; ok {
			assert.Equal(t, k.Key, n.StepKey, "node %s kept a stale key from an earlier snapshot", n.ID)
		}
	}
	after := nodeByID(second, model.ToolCallID(execID, "toolu_a")).StepKey
	assert.NotEqual(t, before, after,
		"an out-of-order earlier sibling pushes this call to occurrence 1, so its key must be recomputed")
}
