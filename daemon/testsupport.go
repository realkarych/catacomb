package daemon

import (
	"maps"

	"github.com/realkarych/catacomb/reduce"
)

func (d *Daemon) GraphsForTest() map[string]*reduce.Graph {
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.graphs)
}

func (d *Daemon) QuarantinedForTest() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.quarantined
}

func (d *Daemon) dropShardForTest(runID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.graphs, d.execBySession[runID])
	delete(d.lastSeen, runID)
}

func (d *Daemon) execForTest(runID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.execBySession[runID]
}

func (d *Daemon) EvictedForTest() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.evicted
}

func (d *Daemon) LossyForTest() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lossyRuns
}

func (d *Daemon) busConsumerCountForTest() int {
	return d.bus.ConsumerCount()
}

func (d *Daemon) busUnsubscribeFirstForTest() {
	d.bus.UnsubscribeFirst()
}
