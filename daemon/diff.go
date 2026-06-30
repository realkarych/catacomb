package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/realkarych/catacomb/diff"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/subgraph"
)

var ErrPhaseNotFound = errors.New("daemon: phase not found")

func nodesWithoutPayload(nodes []*model.Node) []*model.Node {
	out := make([]*model.Node, len(nodes))
	for i, n := range nodes {
		cp := copyNode(n)
		cp.Payload = nil
		out[i] = cp
	}
	return out
}

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

func (d *Daemon) scopedGraph(hash, phaseSel string) ([]*model.Node, []*model.Edge, error) {
	if phaseSel == "" {
		return d.sessionGraphNodes(hash)
	}
	name, occ, err := subgraph.ParseSelector(phaseSel)
	if err != nil {
		return nil, nil, err
	}
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, nil, ErrSessionNotFound
	}
	var nodes []*model.Node
	var edges []*model.Edge
	found := false
	for _, execID := range execs {
		n, e := d.graphs[execID].Snapshot()
		sn, se, ok := subgraph.ScopeExecution(n, e, execID, name, occ)
		if !ok {
			continue
		}
		nodes = append(nodes, sn...)
		edges = append(edges, se...)
		found = true
	}
	if !found {
		return nil, nil, ErrPhaseNotFound
	}
	return nodes, edges, nil
}

func writeScopeErr(w http.ResponseWriter, hash, phase string, err error) {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, ErrPhaseNotFound):
		http.Error(w, fmt.Sprintf("phase %q not found in session %q", phase, hash), http.StatusBadRequest)
	default:
		http.Error(w, fmt.Sprintf("invalid phase selector %q for session %q", phase, hash), http.StatusBadRequest)
	}
}

func (d *Daemon) handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a := q.Get("a")
	b := q.Get("b")
	if a == "" || b == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	aPhase := q.Get("aPhase")
	bPhase := q.Get("bPhase")
	d.mu.Lock()
	aN, aE, err := d.scopedGraph(a, aPhase)
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, a, aPhase, err)
		return
	}
	bN, bE, err := d.scopedGraph(b, bPhase)
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, b, bPhase, err)
		return
	}
	if !d.allowPayloadAccess {
		aN = nodesWithoutPayload(aN)
		bN = nodesWithoutPayload(bN)
	}
	result := diff.DiffGraphs(aN, aE, bN, bE)
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
