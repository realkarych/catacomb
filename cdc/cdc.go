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
	Run         *model.Run
}

type Consumer struct {
	C        <-chan GraphDelta
	Dropped  func() int64
	ch       chan GraphDelta
	wake     chan struct{}
	done     chan struct{}
	drained  sync.WaitGroup
	mu       sync.Mutex
	dropped  int64
	dirty    map[string]GraphDelta
	draining bool
}

type Bus struct {
	mu        sync.Mutex
	consumers []*Consumer
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Subscribe(bufSize int) *Consumer {
	c := &Consumer{
		ch:    make(chan GraphDelta, bufSize),
		wake:  make(chan struct{}, 1),
		done:  make(chan struct{}),
		dirty: map[string]GraphDelta{},
	}
	c.C = c.ch
	c.Dropped = c.droppedCount
	c.drained.Add(1)
	go c.drain()
	b.mu.Lock()
	b.consumers = append(b.consumers, c)
	b.mu.Unlock()
	return c
}

func (b *Bus) Unsubscribe(c *Consumer) {
	b.mu.Lock()
	for i, x := range b.consumers {
		if x == c {
			b.consumers = append(b.consumers[:i], b.consumers[i+1:]...)
			b.mu.Unlock()
			close(c.done)
			c.drained.Wait()
			close(c.ch)
			return
		}
	}
	b.mu.Unlock()
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
	c.mu.Lock()
	if !c.draining {
		select {
		case c.ch <- d:
			c.mu.Unlock()
			return
		default:
		}
	}
	c.dirty[coalesceKey(d)] = d
	c.dropped++
	c.draining = true
	c.mu.Unlock()
	c.signalWake()
}

func (c *Consumer) drain() {
	defer c.drained.Done()
	for {
		c.mu.Lock()
		key, d, ok := c.lowestLocked()
		if !ok {
			c.draining = false
			c.mu.Unlock()
			select {
			case <-c.wake:
				continue
			case <-c.done:
				return
			}
		}
		delete(c.dirty, key)
		c.mu.Unlock()
		select {
		case c.ch <- d:
		case <-c.wake:
			c.restore(key, d)
		case <-c.done:
			return
		}
	}
}

func (c *Consumer) lowestLocked() (string, GraphDelta, bool) {
	var (
		bestKey   string
		bestDelta GraphDelta
		found     bool
	)
	for k, d := range c.dirty {
		if !found || d.Rev < bestDelta.Rev || (d.Rev == bestDelta.Rev && k < bestKey) {
			bestKey, bestDelta, found = k, d, true
		}
	}
	return bestKey, bestDelta, found
}

func (c *Consumer) restore(key string, d GraphDelta) {
	c.mu.Lock()
	if _, ok := c.dirty[key]; !ok {
		c.dirty[key] = d
	}
	c.mu.Unlock()
}

func (c *Consumer) signalWake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Consumer) droppedCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped
}

func (b *Bus) TotalDropped() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var total int64
	for _, c := range b.consumers {
		total += c.droppedCount()
	}
	return total
}

func (b *Bus) ConsumerCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.consumers)
}
