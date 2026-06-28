package daemon

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/realkarych/catacomb/diff"
	"github.com/realkarych/catacomb/model"
)

func (d *Daemon) sessionGraphNodes(hash string) ([]*model.Node, []*model.Edge, error) {
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, nil, ErrSessionNotFound
	}
	var nodes []*model.Node
	var edges []*model.Edge
	for _, execID := range execs {
		n, e := d.graphs[execID].Snapshot()
		nodes = append(nodes, n...)
		edges = append(edges, e...)
	}
	return nodes, edges, nil
}

func (d *Daemon) handleDiff(w http.ResponseWriter, r *http.Request) {
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	if a == "" || b == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	aN, aE, err := d.sessionGraphNodes(a)
	if errors.Is(err, ErrSessionNotFound) {
		d.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	bN, bE, err := d.sessionGraphNodes(b)
	if errors.Is(err, ErrSessionNotFound) {
		d.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	result := diff.DiffGraphs(aN, aE, bN, bE)
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
