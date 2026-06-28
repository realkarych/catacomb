package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

type MarkInput struct {
	SessionID  string `json:"session_id"`
	Name       string `json:"name"`
	Boundary   string `json:"boundary"`
	Occurrence *int   `json:"occurrence,omitempty"`
	StateRef   string `json:"state_ref,omitempty"`
}

func (d *Daemon) IngestMark(m MarkInput) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			d.quarantine("mark", nil, fmt.Sprintf("panic: %v", r))
			err = nil
		}
	}()
	execID, known := d.execBySession[m.SessionID]
	if !known {
		execID = d.newExecID()
		d.execBySession[m.SessionID] = execID
	}
	g, inMem := d.graphs[execID]
	if !inMem {
		g = reduce.NewGraphWithPricer(d.pricer)
		if known {
			prior, loadErr := d.store.ObservationsForExecution(execID)
			if loadErr != nil {
				d.quarantine("mark", nil, loadErr.Error())
				return nil
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
		}
		d.graphs[execID] = g
	}
	now := nowFn().UTC()
	attrs := map[string]any{
		"name":     m.Name,
		"boundary": m.Boundary,
	}
	if m.StateRef != "" {
		attrs["state_ref"] = m.StateRef
	}
	if m.Occurrence != nil {
		attrs["occurrence"] = float64(*m.Occurrence)
	}
	o := model.Observation{
		ObsID:       ulid.Make().String(),
		RunID:       m.SessionID,
		ExecutionID: execID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: m.SessionID},
		Attrs:       attrs,
		EventTime:   now,
		ObservedAt:  now,
		Seq:         d.next(),
	}
	if err := d.applyAndPersist(g, o); err != nil {
		d.quarantine("mark", nil, err.Error())
		return nil
	}
	d.lastSeen[o.RunID] = o.ObservedAt
	return nil
}

func (d *Daemon) handleMark(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var m MarkInput
	if err := json.Unmarshal(body, &m); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = d.IngestMark(m)
	w.WriteHeader(http.StatusNoContent)
}
