package cdc

import (
	"sync"

	"github.com/realkarych/catacomb/model"
)

type GraphDeltaKind string

const (
	DeltaNodeUpsert   GraphDeltaKind = "node_upsert"
	DeltaEdgeUpsert   GraphDeltaKind = "edge_upsert"
	DeltaNodeStatus   GraphDeltaKind = "node_status"
	DeltaNodeMerge    GraphDeltaKind = "node_merge"
	DeltaEdgeDelete   GraphDeltaKind = "edge_delete"
	DeltaRunStarted   GraphDeltaKind = "run_started"
	DeltaSessionEnded GraphDeltaKind = "session_ended"
	DeltaRunEnded     GraphDeltaKind = "run_ended"
)

type GraphDelta struct {
	Kind        GraphDeltaKind
	Rev         uint64
	Node        *model.Node
	Edge        *model.Edge
	OldID       string
	NewID       string
	RunID       string
	ExecutionID string
}

type Consumer struct {
	C       <-chan GraphDelta
	Dropped func() int64
	ch      chan GraphDelta
	dropped int64
	dirty   map[string]GraphDelta
}

type Bus struct {
	mu        sync.Mutex
	consumers []*Consumer
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Subscribe(bufSize int) *Consumer {
	ch := make(chan GraphDelta, bufSize)
	c := &Consumer{C: ch, ch: ch, dirty: map[string]GraphDelta{}}
	c.Dropped = func() int64 {
		b.mu.Lock()
		defer b.mu.Unlock()
		return c.dropped
	}
	b.mu.Lock()
	b.consumers = append(b.consumers, c)
	b.mu.Unlock()
	return c
}

func (b *Bus) Unsubscribe(c *Consumer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, x := range b.consumers {
		if x == c {
			b.consumers = append(b.consumers[:i], b.consumers[i+1:]...)
			close(c.ch)
			return
		}
	}
}

func coalesceKey(d GraphDelta) string {
	switch d.Kind {
	case DeltaNodeUpsert, DeltaNodeStatus:
		if d.Node != nil {
			return d.Node.ID
		}
		return string(d.Kind)
	case DeltaEdgeUpsert, DeltaEdgeDelete:
		if d.Edge != nil {
			return "edge:" + d.Edge.ID
		}
		return string(d.Kind)
	default:
		return string(d.Kind) + ":" + d.RunID
	}
}

func (b *Bus) Publish(d GraphDelta) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.consumers {
		c.deliver(d)
	}
}

func (c *Consumer) deliver(d GraphDelta) {
	for k, pending := range c.dirty {
		select {
		case c.ch <- pending:
			delete(c.dirty, k)
		default:
		}
	}
	select {
	case c.ch <- d:
	default:
		c.dirty[coalesceKey(d)] = d
		c.dropped++
	}
}

func (b *Bus) TotalDropped() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var total int64
	for _, c := range b.consumers {
		total += c.dropped
	}
	return total
}
