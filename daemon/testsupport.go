package daemon

import "github.com/realkarych/catacomb/reduce"

func (d *Daemon) GraphsForTest() map[string]*reduce.Graph {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]*reduce.Graph, len(d.graphs))
	for k, v := range d.graphs {
		out[k] = v
	}
	return out
}
