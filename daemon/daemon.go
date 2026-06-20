package daemon

import (
	"encoding/json"
	"sync"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/ingest/hook"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

type Daemon struct {
	store         store.Store
	newExecID     func() string
	mu            sync.Mutex
	seq           uint64
	graphs        map[string]*reduce.Graph
	execBySession map[string]string
}

func New(s store.Store) *Daemon {
	return &Daemon{
		store:         s,
		newExecID:     func() string { return ulid.Make().String() },
		graphs:        map[string]*reduce.Graph{},
		execBySession: map[string]string{},
	}
}

func (d *Daemon) Recover() error {
	obs, err := d.store.ObservationsSince(0)
	if err != nil {
		return err
	}
	var max uint64
	for _, o := range obs {
		g, ok := d.graphs[o.ExecutionID]
		if !ok {
			g = reduce.NewGraph()
			d.graphs[o.ExecutionID] = g
		}
		g.Apply(o)
		d.execBySession[o.RunID] = o.ExecutionID
		if o.Seq > max {
			max = o.Seq
		}
	}
	d.seq = max
	return nil
}

func (d *Daemon) Ingest(hookType string, payload []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	sessionID := sessionIDOf(payload)
	execID, ok := d.execBySession[sessionID]
	if !ok {
		execID = d.newExecID()
		d.execBySession[sessionID] = execID
	}
	obs, err := hook.Parse(hookType, payload, execID, d.next)
	if err != nil {
		return err
	}
	g, ok := d.graphs[execID]
	if !ok {
		g = reduce.NewGraph()
		d.graphs[execID] = g
	}
	for _, o := range obs {
		g.Apply(o)
		nodes, edges := g.Snapshot()
		if err := d.store.AppendAndApply(o, nodes, edges); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) next() uint64 {
	d.seq++
	return d.seq
}

func sessionIDOf(payload []byte) string {
	var e struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(payload, &e); err != nil {
		return ""
	}
	return e.SessionID
}
