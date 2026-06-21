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
