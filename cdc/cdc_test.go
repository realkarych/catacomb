package cdc

import (
	"testing"
	"time"

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

func TestPublishDropsAndCoalescesWhenFull(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)
	n := &model.Node{ID: "n1", Status: model.StatusRunning, Rev: 1}
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 1, Node: n})
	dropMe := &model.Node{ID: "n1", Status: model.StatusOK, Rev: 2}
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 2, Node: dropMe})
	assert.Equal(t, int64(1), c.Dropped())
	first := <-c.C
	assert.Equal(t, uint64(1), first.Rev)
}

func TestDroppedDeltaReEmittedWithLatestStateOnNextPublish(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)
	n1 := &model.Node{ID: "n1", Rev: 1}
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 1, Node: n1})
	stale := &model.Node{ID: "n1", Rev: 2}
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 2, Node: stale})
	latest := &model.Node{ID: "n1", Rev: 9}
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 9, Node: latest})
	assert.Equal(t, int64(2), c.Dropped())
	first := <-c.C
	assert.Equal(t, uint64(1), first.Rev)
	b.Publish(GraphDelta{Kind: DeltaRunStarted, RunID: "flush"})
	got := drainLatestForID(t, c, "n1")
	assert.Equal(t, uint64(9), got.Rev)
}

func drainLatestForID(t *testing.T, c *Consumer, id string) GraphDelta {
	t.Helper()
	var last GraphDelta
	found := false
	for {
		select {
		case d := <-c.C:
			if d.Node != nil && d.Node.ID == id {
				last = d
				found = true
			}
		default:
			require.True(t, found, "no delta for id %q drained", id)
			return last
		}
	}
}

func TestCoalesceKeyDistinguishesNodeEdgeAndLifecycle(t *testing.T) {
	assert.Equal(t, "n1", coalesceKey(GraphDelta{Kind: DeltaNodeUpsert, Node: &model.Node{ID: "n1"}}))
	assert.Equal(t, "n2", coalesceKey(GraphDelta{Kind: DeltaNodeStatus, Node: &model.Node{ID: "n2"}}))
	assert.Equal(t, string(DeltaNodeUpsert), coalesceKey(GraphDelta{Kind: DeltaNodeUpsert, Node: nil}))
	assert.Equal(t, "edge:e1", coalesceKey(GraphDelta{Kind: DeltaEdgeUpsert, Edge: &model.Edge{ID: "e1"}}))
	assert.Equal(t, "edge:e2", coalesceKey(GraphDelta{Kind: DeltaEdgeDelete, Edge: &model.Edge{ID: "e2"}}))
	assert.Equal(t, string(DeltaEdgeUpsert), coalesceKey(GraphDelta{Kind: DeltaEdgeUpsert, Edge: nil}))
	assert.Equal(t, "run_ended:r1", coalesceKey(GraphDelta{Kind: DeltaRunEnded, RunID: "r1"}))
	assert.Equal(t, "session_ended:r1", coalesceKey(GraphDelta{Kind: DeltaSessionEnded, RunID: "r1"}))
	assert.Equal(t, "run_started:r1", coalesceKey(GraphDelta{Kind: DeltaRunStarted, RunID: "r1"}))
}

func TestLifecycleDeltaNotCoalescedAwayByNodeChurn(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 1, Node: &model.Node{ID: "n1"}})
	b.Publish(GraphDelta{Kind: DeltaSessionEnded, Rev: 2, RunID: "r1"})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 3, Node: &model.Node{ID: "n1"}})
	<-c.C
	var sawEnded bool
	for i := range 8 {
		select {
		case d := <-c.C:
			if d.Kind == DeltaSessionEnded {
				sawEnded = true
			}
		default:
		}
		b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: uint64(10 + i), Node: &model.Node{ID: "filler"}})
	}
	assert.True(t, sawEnded)
}

func TestConsumerCountTracksSubscriptions(t *testing.T) {
	b := NewBus()
	assert.Equal(t, 0, b.ConsumerCount())
	c1 := b.Subscribe(1)
	assert.Equal(t, 1, b.ConsumerCount())
	c2 := b.Subscribe(1)
	assert.Equal(t, 2, b.ConsumerCount())
	b.Unsubscribe(c1)
	assert.Equal(t, 1, b.ConsumerCount())
	b.Unsubscribe(c2)
	assert.Equal(t, 0, b.ConsumerCount())
}

func TestTotalDroppedAggregatesAcrossConsumers(t *testing.T) {
	b := NewBus()
	c1 := b.Subscribe(0)
	c2 := b.Subscribe(0)
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 1, Node: &model.Node{ID: "a"}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 2, Node: &model.Node{ID: "b"}})
	assert.Equal(t, int64(2), c1.Dropped())
	assert.Equal(t, int64(2), c2.Dropped())
	assert.Equal(t, int64(4), b.TotalDropped())
}

func TestDirtyFlushIsRevOrdered(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(5)

	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 100, Node: &model.Node{ID: "occupy", Rev: 100}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 200, Node: &model.Node{ID: "occupy2", Rev: 200}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 300, Node: &model.Node{ID: "occupy3", Rev: 300}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 400, Node: &model.Node{ID: "occupy4", Rev: 400}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 500, Node: &model.Node{ID: "occupy5", Rev: 500}})

	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 30, Node: &model.Node{ID: "c", Rev: 30}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 10, Node: &model.Node{ID: "a", Rev: 10}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 20, Node: &model.Node{ID: "b", Rev: 20}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 10, Node: &model.Node{ID: "z", Rev: 10}})

	<-c.C
	<-c.C
	<-c.C
	<-c.C
	<-c.C

	b.Publish(GraphDelta{Kind: DeltaRunStarted, Rev: 999, RunID: "flush"})

	var revs []uint64
	for {
		select {
		case d := <-c.C:
			revs = append(revs, d.Rev)
		default:
			assert.Equal(t, []uint64{10, 10, 20, 30, 999}, revs)
			return
		}
	}
}

func TestDirtyDrainStopsWhenChannelRefillsDuringFlush(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)

	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 1, Node: &model.Node{ID: "fill", Rev: 1}})

	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 10, Node: &model.Node{ID: "a", Rev: 10}})
	b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 20, Node: &model.Node{ID: "b", Rev: 20}})

	<-c.C

	b.Publish(GraphDelta{Kind: DeltaRunStarted, Rev: 50, RunID: "trigger"})

	require.Eventually(t, func() bool {
		select {
		case d := <-c.C:
			return d.Rev == 10 || d.Rev == 20
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestPublishConcurrentWithReaderEventuallyDeliversFinal(t *testing.T) {
	b := NewBus()
	c := b.Subscribe(1)
	done := make(chan struct{})
	go func() {
		for i := 1; i <= 200; i++ {
			b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: uint64(i), Node: &model.Node{ID: "n1", Rev: uint64(i)}})
		}
		close(done)
	}()
	var max uint64
	for {
		select {
		case d := <-c.C:
			if d.Rev > max {
				max = d.Rev
			}
		case <-done:
			require.Eventually(t, func() bool {
				b.Publish(GraphDelta{Kind: DeltaNodeUpsert, Rev: 999, Node: &model.Node{ID: "n1", Rev: 999}})
				select {
				case d := <-c.C:
					if d.Rev > max {
						max = d.Rev
					}
				default:
				}
				return max == 999
			}, time.Second, time.Millisecond)
			assert.Equal(t, uint64(999), max)
			return
		}
	}
}
