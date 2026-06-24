package daemon

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

func TestMatchNodeEmptyFilterMatchesAll(t *testing.T) {
	f := SubFilter{}
	n := &model.Node{ID: "n1", Type: model.NodeToolCall, Tier: "core"}
	assert.True(t, matchNode(f, n))
}

func TestMatchNodeFilterByType(t *testing.T) {
	f := SubFilter{NodeTypes: []string{"tool_call"}}
	assert.True(t, matchNode(f, &model.Node{Type: model.NodeToolCall}))
	assert.False(t, matchNode(f, &model.Node{Type: model.NodeSession}))
}

func TestMatchNodeFilterByTier(t *testing.T) {
	f := SubFilter{Tiers: []string{"core"}}
	assert.True(t, matchNode(f, &model.Node{Tier: "core"}))
	assert.False(t, matchNode(f, &model.Node{Tier: "other"}))
}

func TestMatchNodeFilterByTypeAndTier(t *testing.T) {
	f := SubFilter{NodeTypes: []string{"session"}, Tiers: []string{"core"}}
	assert.True(t, matchNode(f, &model.Node{Type: model.NodeSession, Tier: "core"}))
	assert.False(t, matchNode(f, &model.Node{Type: model.NodeToolCall, Tier: "core"}))
	assert.False(t, matchNode(f, &model.Node{Type: model.NodeSession, Tier: "other"}))
}

func TestMatchEdgeEmptyRunIDMatchesAll(t *testing.T) {
	f := SubFilter{}
	assert.True(t, matchEdge(f, &model.Edge{RunID: "r1"}))
}

func TestMatchEdgeFilterByRunID(t *testing.T) {
	f := SubFilter{RunID: "r1"}
	assert.True(t, matchEdge(f, &model.Edge{RunID: "r1"}))
	assert.False(t, matchEdge(f, &model.Edge{RunID: "r2"}))
}

func TestMatchEdgeAllRunIDKeyword(t *testing.T) {
	f := SubFilter{RunID: "all"}
	assert.True(t, matchEdge(f, &model.Edge{RunID: "r-any"}))
}

func TestMatchDeltaNodeUpsertFiltersType(t *testing.T) {
	f := SubFilter{NodeTypes: []string{"session"}}
	good := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: &model.Node{Type: model.NodeSession}, RunID: "r1"}
	bad := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: &model.Node{Type: model.NodeToolCall}, RunID: "r1"}
	assert.True(t, matchDelta(f, good))
	assert.False(t, matchDelta(f, bad))
}

func TestMatchDeltaLifecyclePassesTypeFilter(t *testing.T) {
	f := SubFilter{NodeTypes: []string{"tool_call"}}
	delta := cdc.GraphDelta{Kind: cdc.DeltaRunStarted, RunID: "r1"}
	assert.True(t, matchDelta(f, delta))
}

func TestMatchDeltaRunIDFiltersLifecycle(t *testing.T) {
	f := SubFilter{RunID: "r1"}
	match := cdc.GraphDelta{Kind: cdc.DeltaRunEnded, RunID: "r1"}
	skip := cdc.GraphDelta{Kind: cdc.DeltaRunEnded, RunID: "r2"}
	assert.True(t, matchDelta(f, match))
	assert.False(t, matchDelta(f, skip))
}

func TestMatchDeltaEdgeUpsertByRunID(t *testing.T) {
	f := SubFilter{RunID: "r1"}
	match := cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Edge: &model.Edge{RunID: "r1"}, RunID: "r1"}
	skip := cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Edge: &model.Edge{RunID: "r2"}, RunID: "r2"}
	assert.True(t, matchDelta(f, match))
	assert.False(t, matchDelta(f, skip))
}

func TestSubscribeSnapshotReflectsFilter(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	f := SubFilter{NodeTypes: []string{"session"}}
	sub := d.SubscribeFiltered(f, 64)
	defer d.Unsubscribe(sub)

	var kinds []string
	for _, delta := range sub.Snapshot {
		if delta.Node != nil {
			kinds = append(kinds, string(delta.Node.Type))
		}
	}
	for _, k := range kinds {
		assert.Equal(t, "session", k)
	}
}

func TestSubscribeSnapshotNoFilter(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	assert.NotEmpty(t, sub.Snapshot)
}

func TestSubscribeSnapshotDeepCopyIsolation(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	for i, delta := range sub.Snapshot {
		if delta.Node != nil {
			if sub.Snapshot[i].Node.Attrs == nil {
				sub.Snapshot[i].Node.Attrs = map[string]any{}
			}
			sub.Snapshot[i].Node.Attrs["injected"] = true
		}
	}

	d.mu.Lock()
	g := d.graphs["exec1"]
	d.mu.Unlock()
	for _, n := range g.Nodes {
		_, ok := n.Attrs["injected"]
		assert.False(t, ok)
	}
}

func TestUnsubscribeDetachesConsumer(t *testing.T) {
	d := New(tempStore(t))
	before := d.busConsumerCountForTest()

	sub := d.SubscribeFiltered(SubFilter{}, 4)
	assert.Equal(t, before+1, d.busConsumerCountForTest())

	d.Unsubscribe(sub)
	assert.Equal(t, before, d.busConsumerCountForTest())
}

func TestSubscribeSnapshotEdgeFilteredByRunID(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	g.Edges["e1"] = &model.Edge{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "a", Dst: "b"}
	d.mu.Unlock()

	sub := d.SubscribeFiltered(SubFilter{RunID: "other-run"}, 64)
	defer d.Unsubscribe(sub)

	for _, delta := range sub.Snapshot {
		if delta.Edge != nil {
			assert.NotEqual(t, "s1", delta.Edge.RunID)
		}
	}
}

func TestSubscribeRaceFreeHandshake(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	sub := d.SubscribeFiltered(SubFilter{}, 512)
	defer d.Unsubscribe(sub)

	snapshotRevs := map[uint64]bool{}
	for _, delta := range sub.Snapshot {
		snapshotRevs[delta.Rev] = true
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
		require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))
	}()

	collected := map[uint64]bool{}
	deadline := time.After(2 * time.Second)
outer:
	for {
		select {
		case delta, ok := <-sub.Consumer.C:
			if !ok {
				break outer
			}
			collected[delta.Rev] = true
		case <-deadline:
			break outer
		}
	}
	wg.Wait()

	for rev := range collected {
		assert.False(t, snapshotRevs[rev], "rev %d appeared in both snapshot and stream", rev)
	}
}

func TestSubscribeRaceJSON(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)

	sub := d.SubscribeFiltered(SubFilter{}, 512)
	defer d.Unsubscribe(sub)

	var wg sync.WaitGroup
	wg.Add(2)

	done := make(chan struct{})
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case delta, ok := <-sub.Consumer.C:
				if !ok {
					return
				}
				if delta.Node != nil {
					_, _ = json.Marshal(delta.Node.Attrs)
					_, _ = json.Marshal(delta.Node.Annotations)
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer close(done)
		for i := range 40 {
			_ = d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t`+string(rune('A'+i%26))+`","tool_input":{}}`))
		}
	}()

	wg.Wait()
}

func TestSubscribeFilteredBySession(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	for _, delta := range sub.Snapshot {
		assert.Equal(t, "exec1", delta.ExecutionID)
	}
	assert.NotEmpty(t, sub.Snapshot)
}

func TestSubscribeSessionUnknownEmptySnapshot(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	sub := d.SubscribeFiltered(SubFilter{SessionID: "ghost"}, 64)
	defer d.Unsubscribe(sub)
	assert.Empty(t, sub.Snapshot)
}

func TestSubscriptionMatchExecSet(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	inSession := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec1", RunID: "s1"}
	outSession := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec2", RunID: "s2"}

	assert.True(t, sub.match(inSession))
	assert.False(t, sub.match(outSession))
}

func TestSubscriptionMatchNoExecSet(t *testing.T) {
	d := New(tempStore(t))
	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	delta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "any", RunID: "r1"}
	assert.True(t, sub.match(delta))
}
