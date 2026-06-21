package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/ingest/hook"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

const (
	defaultReaperWindow = 30 * time.Minute
	defaultMaxShards    = 4096
)

var (
	nowFn   = time.Now
	applyFn = func(g *reduce.Graph, o model.Observation) { g.Apply(o) }
)

type Daemon struct {
	store         store.Store
	newExecID     func() string
	mu            sync.Mutex
	seq           uint64
	graphs        map[string]*reduce.Graph
	execBySession map[string]string
	quarantined   int64
	evicted       int64
	reaperWindow  time.Duration
	maxShards     int
	lastSeen      map[string]time.Time
}

func New(s store.Store) *Daemon {
	return &Daemon{
		store:         s,
		newExecID:     func() string { return ulid.Make().String() },
		graphs:        map[string]*reduce.Graph{},
		execBySession: map[string]string{},
		lastSeen:      map[string]time.Time{},
		reaperWindow:  defaultReaperWindow,
		maxShards:     defaultMaxShards,
	}
}

func (d *Daemon) SetMaxShards(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maxShards = n
}

func (d *Daemon) SetReaperWindow(w time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if w <= 0 {
		w = defaultReaperWindow
	}
	d.reaperWindow = w
}

func (d *Daemon) Recover() error {
	d.mu.Lock()
	defer d.mu.Unlock()
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
		d.lastSeen[o.RunID] = o.ObservedAt
		if o.Seq > max {
			max = o.Seq
		}
	}
	d.seq = max
	for _, g := range d.graphs {
		for _, r := range g.RunsSnapshot() {
			if err := d.store.UpsertRun(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Daemon) Ingest(hookType string, payload []byte) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			d.quarantine(hookType, payload, fmt.Sprintf("panic: %v", r))
			err = nil
		}
	}()
	if e := d.ingestLocked(hookType, payload); e != nil {
		d.quarantine(hookType, payload, e.Error())
	}
	return nil
}

func (d *Daemon) ingestLocked(hookType string, payload []byte) error {
	sessionID := sessionIDOf(payload)
	execID, known := d.execBySession[sessionID]
	if !known {
		execID = d.newExecID()
		d.execBySession[sessionID] = execID
	}
	obs, err := hook.Parse(hookType, payload, execID, d.next)
	if err != nil {
		return err
	}
	g, inMem := d.graphs[execID]
	if !inMem {
		g = reduce.NewGraph()
		if known {
			prior, err := d.store.ObservationsForExecution(execID)
			if err != nil {
				return err
			}
			g.ApplyAll(prior)
		}
		d.graphs[execID] = g
	}
	for _, o := range obs {
		if err := d.applyAndPersist(g, o); err != nil {
			return err
		}
		d.lastSeen[o.RunID] = o.ObservedAt
	}
	return nil
}

func (d *Daemon) applyAndPersist(g *reduce.Graph, o model.Observation) error {
	applyFn(g, o)
	nodes, edges := g.Snapshot()
	if err := d.store.AppendAndApply(o, nodes, edges); err != nil {
		return err
	}
	return d.store.UpsertRun(*g.Runs[o.RunID])
}

func (d *Daemon) quarantine(hookType string, payload []byte, msg string) {
	d.quarantined++
	rec := model.QuarantineRecord{Raw: payload, HookType: hookType, Err: msg, At: nowFn().UTC()}
	if err := d.store.Quarantine(rec); err != nil {
		log.Printf("catacomb: quarantine write failed: %v", err)
	}
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

type shardRef struct {
	execID, runID string
	ended         time.Time
}

func (d *Daemon) terminalShards() []shardRef {
	var out []shardRef
	for execID, g := range d.graphs {
		for runID, r := range g.Runs {
			if r.Status == model.StatusOK || r.Status == model.StatusAbandoned {
				out = append(out, shardRef{execID, runID, *r.EndedAt})
			}
		}
	}
	return out
}

func (d *Daemon) evictShard(execID, runID string) {
	delete(d.graphs, execID)
	delete(d.lastSeen, runID)
	d.evicted++
}

func (d *Daemon) evictTerminal(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, t := range d.terminalShards() {
		if now.Sub(t.ended) > d.reaperWindow {
			d.evictShard(t.execID, t.runID)
		}
	}
	if d.maxShards > 0 && len(d.graphs) > d.maxShards {
		rest := d.terminalShards()
		sort.Slice(rest, func(i, j int) bool { return rest[i].ended.Before(rest[j].ended) })
		for _, t := range rest {
			if len(d.graphs) <= d.maxShards {
				break
			}
			d.evictShard(t.execID, t.runID)
		}
	}
}

func (d *Daemon) reapIdle(now time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for execID, g := range d.graphs {
		for runID, r := range g.Runs {
			if r.Status != model.StatusRunning {
				continue
			}
			last := d.lastSeen[runID]
			if now.Sub(last) < d.reaperWindow {
				continue
			}
			o := model.Observation{
				ObsID:       ulid.Make().String(),
				RunID:       runID,
				ExecutionID: execID,
				Source:      model.SourceHook,
				Kind:        "run_ended",
				Attrs:       map[string]any{"reason": "timeout"},
				EventTime:   now.UTC(),
				ObservedAt:  now.UTC(),
				Seq:         d.next(),
			}
			if err := d.applyAndPersist(g, o); err != nil {
				return err
			}
		}
	}
	return nil
}
