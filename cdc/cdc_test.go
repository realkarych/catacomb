package cdc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestDeltaKindConstants(t *testing.T) {
	assert.Equal(t, GraphDeltaKind("node_upsert"), DeltaNodeUpsert)
	assert.Equal(t, GraphDeltaKind("edge_upsert"), DeltaEdgeUpsert)
	assert.Equal(t, GraphDeltaKind("node_status"), DeltaNodeStatus)
	assert.Equal(t, GraphDeltaKind("node_merge"), DeltaNodeMerge)
	assert.Equal(t, GraphDeltaKind("edge_delete"), DeltaEdgeDelete)
	assert.Equal(t, GraphDeltaKind("run_started"), DeltaRunStarted)
	assert.Equal(t, GraphDeltaKind("session_ended"), DeltaSessionEnded)
	assert.Equal(t, GraphDeltaKind("run_ended"), DeltaRunEnded)
}

func TestPublishFanOutToAllConsumers(t *testing.T) {
	b := NewBus()
	c1 := b.Subscribe(4)
	c2 := b.Subscribe(8)
	d := GraphDelta{Kind: DeltaNodeUpsert, Rev: 7, Node: &model.Node{ID: "n1"}, RunID: "r1"}
	b.Publish(d)
	got1 := <-c1.C
	got2 := <-c2.C
	assert.Equal(t, "n1", got1.Node.ID)
	assert.Equal(t, uint64(7), got2.Rev)
}

func TestUnsubscribeStopsDeliveryAndClosesChannel(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(2)
	b.Unsubscribe(c)
	b.Publish(GraphDelta{Kind: DeltaRunStarted, RunID: "r1"})
	_, open := <-c.C
	assert.False(t, open)
}

func TestUnsubscribeUnknownConsumerIsNoOp(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)
	other := &Consumer{}
	b.Unsubscribe(other)
	b.Publish(GraphDelta{Kind: DeltaRunStarted, RunID: "r2"})
	got := <-c.C
	assert.Equal(t, "r2", got.RunID)
}

func TestDroppedStartsAtZero(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)
	require.NotNil(t, c.Dropped)
	assert.Equal(t, int64(0), c.Dropped())
}
