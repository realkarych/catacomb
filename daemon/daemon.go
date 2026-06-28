package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/ingest/hook"
	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	otelingest "github.com/realkarych/catacomb/ingest/otel"
	streamjsoningest "github.com/realkarych/catacomb/ingest/streamjson"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/pricing"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

const (
	defaultReaperWindow = 30 * time.Minute
	defaultMaxShards    = 4096
)

var (
	nowFn         = time.Now
	getwdFn       = os.Getwd
	applyFn       = func(g *reduce.Graph, o model.Observation) { g.Apply(o) }
	drainFn       = func(g *reduce.Graph) []cdc.GraphDelta { return g.DrainDeltas() }
	parseFn       = otelingest.Parse
	streamParseFn = streamjsoningest.Parse
	tailParseFn   = ijsonl.Parse
)

func cwdTranscriptExclude() string {
	wd, err := getwdFn()
	if err != nil || wd == "" {
		return ""
	}
	enc := strings.ReplaceAll(wd, "/", "-")
	enc = strings.ReplaceAll(enc, "\\", "-")
	enc = strings.ReplaceAll(enc, ".", "-")
	return enc + string(os.PathSeparator)
}

type Daemon struct {
	store              store.Store
	newExecID          func() string
	mu                 sync.Mutex
	seq                uint64
	graphs             map[string]*reduce.Graph
	execBySession      map[string]string
	bus                *cdc.Bus
	quarantined        int64
	evicted            int64
	reaperWindow       time.Duration
	maxShards          int
	lastSeen           map[string]time.Time
	startedAt          time.Time
	storeWriteErrors   int64
	otlpEndpoint       string
	otlpProject        string
	exporterConsumers  []*cdc.Consumer
	postgresDSN        string
	neo4jURI           string
	neo4jUser          string
	neo4jPassword      string
	dbPath             string
	transcriptDir      string
	transcriptExclude  []string
	lossyRuns          int64
	pricer             reduce.Pricer
	allowPayloadAccess bool
	allowAnnotations   bool
}

func New(s store.Store) *Daemon {
	eng := pricing.New()
	return &Daemon{
		store:         s,
		newExecID:     func() string { return ulid.Make().String() },
		graphs:        map[string]*reduce.Graph{},
		execBySession: map[string]string{},
		bus:           cdc.NewBus(),
		lastSeen:      map[string]time.Time{},
		reaperWindow:  defaultReaperWindow,
		maxShards:     defaultMaxShards,
		startedAt:     nowFn(),
		pricer: reduce.PricerFunc(func(in reduce.PriceInputs) (reduce.PriceResult, bool) {
			r, ok := eng.Cost(pricing.Inputs{
				ModelID:     in.ModelID,
				TokensIn:    in.TokensIn,
				TokensOut:   in.TokensOut,
				CacheReadIn: in.CacheReadIn,
				CacheWrite:  in.CacheWrite,
				ReportedUSD: in.ReportedUSD,
			})
			return reduce.PriceResult{USD: r.USD, Source: r.Source}, ok
		}),
	}
}

func (d *Daemon) Subscribe(bufSize int) *cdc.Consumer {
	return d.bus.Subscribe(bufSize)
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

func (d *Daemon) SetOTLPEndpoint(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.otlpEndpoint = s
}

func (d *Daemon) SetOTLPProject(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.otlpProject = s
}

func (d *Daemon) SetPostgresDSN(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.postgresDSN = s
}

func (d *Daemon) SetNeo4j(uri, user, password string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.neo4jURI = uri
	d.neo4jUser = user
	d.neo4jPassword = password
}

func (d *Daemon) Recover() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	obs, err := d.store.ObservationsSince(0)
	if err != nil {
		return err
	}
	var maxSeq uint64
	for _, o := range obs {
		g, ok := d.graphs[o.ExecutionID]
		if !ok {
			g = reduce.NewGraphWithPricer(d.pricer)
			d.graphs[o.ExecutionID] = g
		}
		g.Apply(o)
		if o.Correlation.SessionID != "" {
			d.execBySession[o.Correlation.SessionID] = o.ExecutionID
		}
		d.lastSeen[o.RunID] = o.ObservedAt
		if o.Seq > maxSeq {
			maxSeq = o.Seq
		}
	}
	d.seq = maxSeq
	for _, g := range d.graphs {
		_ = g.DrainDeltas()
	}
	for _, g := range d.graphs {
		for _, r := range g.RunsSnapshot() {
			if err := d.store.UpsertRun(r); err != nil {
				return err
			}
		}
	}
	if err := reattachAnnotations(d); err != nil {
		return err
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
		g = reduce.NewGraphWithPricer(d.pricer)
		if known {
			prior, err := d.store.ObservationsForExecution(execID)
			if err != nil {
				return err
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
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

func (d *Daemon) IngestOTLP(req *collectorv1.ExportTraceServiceRequest) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			var raw []byte
			if req != nil {
				raw, _ = proto.Marshal(req)
			}
			d.quarantine("otel", raw, fmt.Sprintf("panic: %v", r))
			err = nil
		}
	}()
	if req == nil {
		d.quarantine("otel", nil, "nil request")
		return nil
	}
	sessionID := otelingest.SessionID(req)
	execID, known := d.execBySession[sessionID]
	if !known {
		execID = d.newExecID()
		d.execBySession[sessionID] = execID
	}
	obs, err := parseFn(req, execID, d.next)
	if err != nil {
		var raw []byte
		raw, _ = proto.Marshal(req)
		d.quarantine("otel", raw, err.Error())
		return nil
	}
	g, inMem := d.graphs[execID]
	if !inMem {
		g = reduce.NewGraphWithPricer(d.pricer)
		if known {
			prior, loadErr := d.store.ObservationsForExecution(execID)
			if loadErr != nil {
				d.quarantine("otel", nil, loadErr.Error())
				return nil
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
		}
		d.graphs[execID] = g
	}
	for _, o := range obs {
		if err := d.applyAndPersist(g, o); err != nil {
			var raw []byte
			raw, _ = proto.Marshal(req)
			d.quarantine("otel", raw, err.Error())
			return nil
		}
		d.lastSeen[o.RunID] = o.ObservedAt
	}
	return nil
}

func (d *Daemon) IngestStreamJSON(line []byte, sessionID string) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			d.quarantine("stream-json", line, fmt.Sprintf("panic: %v", r))
			err = nil
		}
	}()
	execID, known := d.execBySession[sessionID]
	if !known {
		execID = d.newExecID()
		d.execBySession[sessionID] = execID
	}
	obs, err := streamParseFn(line, execID, d.next)
	if err != nil {
		d.quarantine("stream-json", line, err.Error())
		return nil
	}
	g, inMem := d.graphs[execID]
	if !inMem {
		g = reduce.NewGraphWithPricer(d.pricer)
		if known {
			prior, loadErr := d.store.ObservationsForExecution(execID)
			if loadErr != nil {
				d.quarantine("stream-json", line, loadErr.Error())
				return nil
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
		}
		d.graphs[execID] = g
	}
	for _, o := range obs {
		if err := d.applyAndPersist(g, o); err != nil {
			d.quarantine("stream-json", line, err.Error())
			return nil
		}
		d.lastSeen[o.RunID] = o.ObservedAt
	}
	return nil
}

func (d *Daemon) IngestTranscript(line []byte, sessionID string) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			d.quarantine("jsonl", line, fmt.Sprintf("panic: %v", r))
			err = nil
		}
	}()
	execID, known := d.execBySession[sessionID]
	if !known {
		execID = d.newExecID()
		d.execBySession[sessionID] = execID
	}
	obs, err := tailParseFn(bytes.NewReader(line), execID, d.next, func(time.Time) time.Time { return nowFn().UTC() })
	if err != nil {
		d.quarantine("jsonl", line, err.Error())
		return nil
	}
	g, inMem := d.graphs[execID]
	if !inMem {
		g = reduce.NewGraphWithPricer(d.pricer)
		if known {
			prior, loadErr := d.store.ObservationsForExecution(execID)
			if loadErr != nil {
				d.quarantine("jsonl", line, loadErr.Error())
				return nil
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
		}
		d.graphs[execID] = g
	}
	for _, o := range obs {
		if err := d.applyAndPersist(g, o); err != nil {
			d.quarantine("jsonl", line, err.Error())
			return nil
		}
		d.lastSeen[o.RunID] = o.ObservedAt
	}
	return nil
}

func (d *Daemon) IngestSubagentMeta(m model.SubagentMeta) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			d.quarantine("subagent_meta", nil, fmt.Sprintf("panic: %v", r))
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
				d.quarantine("subagent_meta", nil, loadErr.Error())
				return nil
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
		}
		d.graphs[execID] = g
	}
	now := nowFn().UTC()
	o := model.Observation{
		ObsID:       ulid.Make().String(),
		RunID:       m.SessionID,
		ExecutionID: execID,
		Source:      model.SourceJSONL,
		Kind:        "subagent_stop",
		Correlation: model.Correlation{
			AgentID:         m.AgentID,
			ParentToolUseID: m.ToolUseID,
			SessionID:       m.SessionID,
		},
		Attrs:      map[string]any{"subagent_type": m.AgentType, "description": m.Description},
		EventTime:  now,
		ObservedAt: now,
		Seq:        d.next(),
	}
	if err := d.applyAndPersist(g, o); err != nil {
		d.quarantine("subagent_meta", nil, err.Error())
		return nil
	}
	d.lastSeen[o.RunID] = o.ObservedAt
	return nil
}

func (d *Daemon) MarkLossy(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	execID, ok := d.execBySession[sessionID]
	if !ok {
		return
	}
	g, ok := d.graphs[execID]
	if !ok {
		return
	}
	r, ok := g.Runs[sessionID]
	if !ok {
		return
	}
	if r.Meta == nil {
		r.Meta = map[string]any{}
	}
	r.Meta["lossy"] = true
	gaps, _ := r.Meta["lossy_gaps"].(int64)
	r.Meta["lossy_gaps"] = gaps + 1
	d.lossyRuns++
	if err := d.store.UpsertRun(*r); err != nil {
		log.Printf("catacomb: lossy run persist failed: %v", err)
	}
}

func (d *Daemon) SetAllowPayloadAccess(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.allowPayloadAccess = v
}

func (d *Daemon) SetTranscriptDir(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transcriptDir = s
}

func (d *Daemon) SetTranscriptExclude(globs []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transcriptExclude = globs
}

func (d *Daemon) SetDBPath(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dbPath = s
}

func (d *Daemon) applyAndPersist(g *reduce.Graph, o model.Observation) error {
	applyFn(g, o)
	deltas := drainFn(g)
	if err := d.store.AppendDeltas(o, deltas); err != nil {
		d.storeWriteErrors++
		return err
	}
	if err := d.store.UpsertRun(*g.Runs[o.RunID]); err != nil {
		d.storeWriteErrors++
		return err
	}
	for _, delta := range deltas {
		if delta.Kind == cdc.DeltaNodeMerge && delta.OldID != "" && delta.NewID != "" {
			d.carryOverMergeLocked(delta.ExecutionID, delta.OldID, delta.NewID)
		}
		d.publishDelta(delta)
	}
	return nil
}

func (d *Daemon) publishDelta(delta cdc.GraphDelta) {
	if delta.Node != nil {
		delta.Node = copyNode(delta.Node)
	}
	if delta.Edge != nil {
		delta.Edge = copyEdge(delta.Edge)
	}
	d.bus.Publish(delta)
}

type Metrics struct {
	UptimeSeconds       int64  `json:"uptime_seconds"`
	OpenRuns            int    `json:"open_runs"`
	Shards              int    `json:"shards"`
	MaxSeq              uint64 `json:"max_seq"`
	Quarantined         int64  `json:"quarantined"`
	Evicted             int64  `json:"evicted"`
	StoreWriteErrors    int64  `json:"store_write_errors"`
	DeltasDropped       int64  `json:"deltas_dropped"`
	ExporterLag         int64  `json:"exporter_lag"`
	ReaperWindowSeconds int64  `json:"reaper_window_seconds"`
	LossyRuns           int64  `json:"lossy_runs"`
}

func (d *Daemon) metricsSnapshot() Metrics {
	d.mu.Lock()
	defer d.mu.Unlock()
	open := 0
	for _, g := range d.graphs {
		for _, r := range g.Runs {
			if r.Status == model.StatusRunning {
				open++
			}
		}
	}
	var lag int64
	for _, c := range d.exporterConsumers {
		lag += c.Dropped()
	}
	return Metrics{
		UptimeSeconds:       int64(nowFn().Sub(d.startedAt).Seconds()),
		OpenRuns:            open,
		Shards:              len(d.graphs),
		MaxSeq:              d.seq,
		Quarantined:         d.quarantined,
		Evicted:             d.evicted,
		StoreWriteErrors:    d.storeWriteErrors,
		DeltasDropped:       d.bus.TotalDropped(),
		ExporterLag:         lag,
		ReaperWindowSeconds: int64(d.reaperWindow.Seconds()),
		LossyRuns:           d.lossyRuns,
	}
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

func (d *Daemon) executionsForSession(hash string) []string {
	if hash == "" {
		return nil
	}
	var out []string
	for execID, g := range d.graphs {
		for _, r := range g.Runs {
			if slices.Contains(r.SessionIDs, hash) {
				out = append(out, execID)
				break
			}
		}
	}
	sort.Strings(out)
	return out
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
