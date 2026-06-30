package daemon

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/subgraph"
)

func (d *Daemon) phaseFocusDeltas(hash, phaseSel string) ([]sseEvent, error) {
	parsed, err := subgraph.ParseSpec(subgraph.Spec{Phase: phaseSel})
	if err != nil {
		return nil, err
	}
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, ErrSessionNotFound
	}
	out := []sseEvent{}
	found := false
	for _, execID := range execs {
		n, e := d.graphs[execID].Snapshot()
		sn, se, ok := subgraph.ScopeExecutionParsed(n, e, execID, parsed)
		if !ok {
			continue
		}
		found = true
		for _, node := range sn {
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         node.Rev,
				Node:        copyNode(node),
				RunID:       node.RunID,
				ExecutionID: execID,
			}))
		}
		for _, edge := range se {
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaEdgeUpsert,
				Rev:         edge.Rev,
				Edge:        copyEdge(edge),
				RunID:       edge.RunID,
				ExecutionID: execID,
			}))
		}
	}
	if !found {
		return nil, subgraph.ErrPhaseNotFound
	}
	return out, nil
}

func (d *Daemon) handlePhaseFocus(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	phaseSel := r.PathValue("phaseSel")
	d.mu.Lock()
	evs, err := d.phaseFocusDeltas(hash, phaseSel)
	d.mu.Unlock()
	switch {
	case errors.Is(err, ErrSessionNotFound), errors.Is(err, subgraph.ErrPhaseNotFound):
		w.WriteHeader(http.StatusNotFound)
		return
	case errors.Is(err, subgraph.ErrInvalidSelector):
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(evs)
}
