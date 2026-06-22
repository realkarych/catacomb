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

func TestCopyNodeIsolatesAttrs(t *testing.T) {
	src := &model.Node{
		ID:          "n1",
		RunID:       "r1",
		Attrs:       map[string]any{"k": "v"},
		Annotations: map[string]any{"a": "b"},
		Sources:     []model.SourceRef{{ObsID: "o1"}},
	}
	cp := copyNode(src)
	cp.Attrs["k"] = "changed"
	cp.Annotations["a"] = "changed"
	cp.Sources[0].ObsID = "o2"
	assert.Equal(t, "v", src.Attrs["k"])
	assert.Equal(t, "b", src.Annotations["a"])
	assert.Equal(t, "o1", src.Sources[0].ObsID)
}

func TestCopyNodeNilMaps(t *testing.T) {
	src := &model.Node{ID: "n2"}
	cp := copyNode(src)
	require.NotNil(t, cp)
	assert.Equal(t, "n2", cp.ID)
	assert.Nil(t, cp.Attrs)
	assert.Nil(t, cp.Annotations)
	assert.Nil(t, cp.Sources)
}

func TestCopyEdgeIsolatesAttrs(t *testing.T) {
	src := &model.Edge{
		ID:    "e1",
		RunID: "r1",
		Attrs: map[string]any{"x": "y"},
	}
	cp := copyEdge(src)
	cp.Attrs["x"] = "changed"
	assert.Equal(t, "y", src.Attrs["x"])
}

func TestCopyEdgeNilAttrs(t *testing.T) {
	src := &model.Edge{ID: "e2"}
	cp := copyEdge(src)
	require.NotNil(t, cp)
	assert.Equal(t, "e2", cp.ID)
	assert.Nil(t, cp.Attrs)
}

func TestCopyRaceAttrsAndSources(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	consumer := d.bus.Subscribe(256)
	t.Cleanup(func() { d.bus.Unsubscribe(consumer) })

	var wg sync.WaitGroup
	wg.Add(2)

	done := make(chan struct{})
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case delta, ok := <-consumer.C:
				if !ok {
					return
				}
				if delta.Node != nil {
					_, _ = json.Marshal(delta.Node.Attrs)
					_, _ = json.Marshal(delta.Node.Sources)
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer close(done)
		for i := range 50 {
			_ = d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t`+string(rune('A'+i%26))+`","tool_input":{}}`))
		}
	}()

	wg.Wait()
}

func TestPublishDeltaUsesDeepCopy(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	consumer := d.bus.Subscribe(256)
	t.Cleanup(func() { d.bus.Unsubscribe(consumer) })

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	var delta cdc.GraphDelta
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-consumer.C:
			if ev.Node != nil {
				delta = ev
				goto got
			}
		case <-timeout:
			t.Fatal("no node delta received")
		}
	}
got:
	if delta.Node.Attrs == nil {
		delta.Node.Attrs = map[string]any{}
	}
	delta.Node.Attrs["injected"] = true

	d.mu.Lock()
	g := d.graphs["exec1"]
	d.mu.Unlock()
	for _, n := range g.Nodes {
		_, ok := n.Attrs["injected"]
		assert.False(t, ok, "mutating bus copy must not affect live graph node")
	}
}
