package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/realkarych/catacomb/diff"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/subgraph"
)

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

func (d *Daemon) scopedGraph(hash string, spec subgraph.Spec) ([]*model.Node, []*model.Edge, error) {
	if spec.Empty() {
		return d.sessionGraphNodes(hash)
	}
	parsed, err := subgraph.ParseSpec(spec)
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
		sn, se, ok := subgraph.ScopeExecutionParsed(n, e, execID, parsed)
		if !ok {
			continue
		}
		nodes = append(nodes, sn...)
		edges = append(edges, se...)
		found = true
	}
	if !found {
		return nil, nil, subgraph.ErrPhaseNotFound
	}
	return nodes, edges, nil
}

func writeScopeErr(w http.ResponseWriter, hash string, err error) {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, subgraph.ErrPhaseNotFound):
		http.Error(w, fmt.Sprintf("phase not found in session %q", hash), http.StatusBadRequest)
	default:
		http.Error(w, fmt.Sprintf("invalid phase selector for session %q", hash), http.StatusBadRequest)
	}
}

func sideSpec(q url.Values, side string) subgraph.Spec {
	phase := q.Get("phase")
	sidePhase := q.Get(side + "Phase")
	if sidePhase == "" {
		sidePhase = phase
	}
	return subgraph.Spec{
		Phase: sidePhase,
		From:  q.Get(side + "From"),
		To:    q.Get(side + "To"),
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
	d.mu.Lock()
	aN, aE, err := d.scopedGraph(a, sideSpec(q, "a"))
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, a, err)
		return
	}
	bN, bE, err := d.scopedGraph(b, sideSpec(q, "b"))
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, b, err)
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
