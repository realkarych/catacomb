package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

const subBufSize = 256

var sseTickerFn = func() *time.Ticker {
	return time.NewTicker(15 * time.Second)
}

var sseJSONMarshal = json.Marshal

type sseEvent struct {
	Kind        string      `json:"kind"`
	Rev         uint64      `json:"rev"`
	RunID       string      `json:"run_id,omitempty"`
	ExecutionID string      `json:"execution_id,omitempty"`
	Node        *model.Node `json:"node,omitempty"`
	Edge        *model.Edge `json:"edge,omitempty"`
	OldID       string      `json:"old_id,omitempty"`
	NewID       string      `json:"new_id,omitempty"`
}

func deltaToSSE(d cdc.GraphDelta) sseEvent {
	return sseEvent{
		Kind:        string(d.Kind),
		Rev:         d.Rev,
		RunID:       d.RunID,
		ExecutionID: d.ExecutionID,
		Node:        d.Node,
		Edge:        d.Edge,
		OldID:       d.OldID,
		NewID:       d.NewID,
	}
}

func parseSubFilter(r *http.Request) SubFilter {
	q := r.URL.Query()
	f := SubFilter{
		RunID: q.Get("run"),
	}
	for _, raw := range q["type"] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				f.NodeTypes = append(f.NodeTypes, part)
			}
		}
	}
	for _, raw := range q["tier"] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				f.Tiers = append(f.Tiers, part)
			}
		}
	}
	return f
}

func (d *Daemon) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	f := parseSubFilter(r)
	sub := d.SubscribeFiltered(f, subBufSize)
	defer d.Unsubscribe(sub)

	writeEvent := func(delta cdc.GraphDelta) bool {
		ev := deltaToSSE(delta)
		b, err := sseJSONMarshal(ev)
		if err != nil {
			return true
		}
		if delta.Rev > 0 {
			_, err = fmt.Fprintf(w, "id: %d\n", delta.Rev)
			if err != nil {
				return false
			}
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", b)
		if err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for _, snap := range sub.Snapshot {
		if !writeEvent(snap) {
			return
		}
	}

	ticker := sseTickerFn()
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case delta, ok := <-sub.Consumer.C:
			if !ok {
				return
			}
			if !matchDelta(f, delta) {
				continue
			}
			if !writeEvent(delta) {
				return
			}
		case <-ticker.C:
			_, err := fmt.Fprint(w, ": ping\n\n")
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
