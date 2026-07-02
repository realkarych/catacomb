package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	_ "modernc.org/sqlite"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/config"
	otelingest "github.com/realkarych/catacomb/ingest/otel"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/repro"
	"github.com/realkarych/catacomb/store"
)

func tempStore(t *testing.T) store.Store {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "g.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fixedExecID(d *Daemon) {
	var n int
	d.newExecID = func() string {
		n++
		return "exec" + string(rune('0'+n))
	}
}

func TestIngestSessionStart(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1","source":"startup"}`)))
	n := d.graphs["exec1"].Nodes[model.SessionNodeID("exec1")]
	require.NotNil(t, n)
	assert.Equal(t, model.StatusRunning, n.Status)
}

func TestIngestToolPairSameExecution(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))
	assert.Len(t, d.graphs, 1)
	n := d.graphs["exec1"].Nodes[model.ToolCallID("exec1", "t1")]
	require.NotNil(t, n)
	assert.Equal(t, model.StatusOK, n.Status)
}

func TestIngestNewSessionMintsExecution(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s2"}`)))
	assert.Equal(t, "exec1", d.execBySession["s1"])
	assert.Equal(t, "exec2", d.execBySession["s2"])
}

func TestIngestUnknownTypeNoError(t *testing.T) {
	d := New(tempStore(t))
	require.NoError(t, d.Ingest("Mystery", []byte(`{"session_id":"s1"}`)))
}

func TestIngestMalformedPayload(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("PreToolUse", []byte("{not json}")))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestSeqMonotonic(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("UserPromptSubmit", []byte(`{"session_id":"s1","prompt":"hi"}`)))
	assert.Equal(t, uint64(2), d.seq)
}

func TestIngestStoreError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestRecoverRebuildsGraphsAndSeq(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())
	assert.Equal(t, uint64(2), d2.seq)
	assert.Equal(t, "exec1", d2.execBySession["s1"])
	require.NotNil(t, d2.graphs["exec1"].Nodes[model.ToolCallID("exec1", "t1")])
	require.NoError(t, d2.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))
	assert.Equal(t, uint64(3), d2.seq)
}

func TestRecoverError(t *testing.T) {
	d := New(&errStore{failSince: true})
	require.Error(t, d.Recover())
}

func TestSessionIDOf(t *testing.T) {
	assert.Equal(t, "s1", sessionIDOf([]byte(`{"session_id":"s1"}`)))
	assert.Equal(t, "", sessionIDOf([]byte("{bad}")))
}

func TestNewDefaultExecIDIsULID(t *testing.T) {
	d := New(tempStore(t))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	assert.Len(t, d.execBySession, 1)
	for _, execID := range d.execBySession {
		assert.NotEmpty(t, execID)
	}
}

type appendErrStore struct {
	store.Store
	mu          sync.Mutex
	appends     int64
	quarantined int64
}

func (s *appendErrStore) AppendDeltas(model.Observation, []cdc.GraphDelta) error {
	s.mu.Lock()
	s.appends++
	s.mu.Unlock()
	return errors.New("append")
}

func (s *appendErrStore) appendCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appends
}

func (s *appendErrStore) Quarantine(model.QuarantineRecord) error {
	s.mu.Lock()
	s.quarantined++
	s.mu.Unlock()
	return nil
}

func (s *appendErrStore) QuarantineCount() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quarantined, nil
}

type quarantineErrStore struct{ store.Store }

func (s *quarantineErrStore) Quarantine(model.QuarantineRecord) error { return errors.New("q") }

func TestIngestQuarantinesParseError(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("PreToolUse", []byte("{not json")))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestQuarantinesPersistError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestRecoversPanic(t *testing.T) {
	orig := applyFn
	applyFn = func(*reduce.Graph, model.Observation) { panic("boom") }
	t.Cleanup(func() { applyFn = orig })
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestQuarantineWriteErrorLogged(t *testing.T) {
	d := New(&quarantineErrStore{Store: tempStore(t)})
	require.NoError(t, d.Ingest("PreToolUse", []byte("{not json")))
}

func TestIngestPoisonDoesNotStopOtherRuns(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"good"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte("{poison")))
	require.NoError(t, d.Ingest("UserPromptSubmit", []byte(`{"session_id":"good","prompt":"hi"}`)))
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	assert.NotEmpty(t, obs)
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

type runUpsertErrStore struct{ store.Store }

func (s *runUpsertErrStore) UpsertRun(model.Run) error { return errors.New("upsert") }

func TestIngestPersistsRun(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	runs, err := s.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "s1", runs[0].ID)
	assert.Equal(t, model.StatusRunning, runs[0].Status)
}

func TestRecoverRepersistsRuns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "g.db")
	s1, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	d1 := New(s1)
	require.NoError(t, d1.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, s1.Close())

	s2, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())
	runs, err := s2.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "s1", runs[0].ID)
}

func TestRecoverRunUpsertError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "g.db")
	s1, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	d1 := New(s1)
	require.NoError(t, d1.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, s1.Close())

	s2, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(&runUpsertErrStore{Store: s2})
	assert.Error(t, d2.Recover())
}

func TestIngestRunUpsertError(t *testing.T) {
	d := New(&runUpsertErrStore{Store: tempStore(t)})
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

func TestReapIdleAbandonsQuiescentRun(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(time.Minute)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.reapIdle(time.Now().Add(time.Hour)))
	runs, err := s.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, model.StatusAbandoned, runs[0].Status)
	assert.Equal(t, "timeout", runs[0].EndReason)
}

func TestReapIdleSkipsActiveRun(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(time.Hour)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.reapIdle(time.Now()))
	open, err := s.ListOpenRuns()
	require.NoError(t, err)
	assert.Len(t, open, 1)
}

func TestReapIdleSkipsAlreadyEndedRun(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(time.Minute)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.reapIdle(time.Now().Add(time.Hour)))
	runs, err := s.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, model.StatusOK, runs[0].Status)
}

func TestReapIdlePersistError(t *testing.T) {
	d := New(&appendErrStore{Store: tempStore(t)})
	d.SetReaperWindow(time.Minute)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	assert.Error(t, d.reapIdle(time.Now().Add(time.Hour)))
}

func TestSetReaperWindowClampsNonPositive(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(0)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.reapIdle(time.Now()))
	open, err := s.ListOpenRuns()
	require.NoError(t, err)
	assert.Len(t, open, 1)
}

type reloadErrStore struct{ store.Store }

func (s *reloadErrStore) ObservationsForExecution(string) ([]model.Observation, error) {
	return nil, errors.New("reload")
}

func TestIngestReloadsEvictedShard(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1"}`)))
	d.dropShardForTest("s1")
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{"ok":true}}`)))
	g := d.GraphsForTest()[d.execForTest("s1")]
	require.NotNil(t, g)
	n := g.Nodes[model.ToolCallID(d.execForTest("s1"), "t1")]
	require.NotNil(t, n)
	assert.Equal(t, model.StatusOK, n.Status)
}

func TestIngestReloadError(t *testing.T) {
	d := New(&reloadErrStore{Store: tempStore(t)})
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	d.dropShardForTest("s1")
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1"}`)))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

type errStore struct {
	failSince bool
}

func (e *errStore) Persist([]model.Observation, []*model.Node, []*model.Edge) error { return nil }

func (e *errStore) AppendDeltas(model.Observation, []cdc.GraphDelta) error {
	return nil
}

func (e *errStore) MaxSeq() (uint64, error) { return 0, nil }

func (e *errStore) ObservationsSince(uint64) ([]model.Observation, error) {
	if e.failSince {
		return nil, errors.New("since")
	}
	return nil, nil
}

func (e *errStore) UpsertRun(model.Run) error                                    { return nil }
func (e *errStore) ListOpenRuns() ([]model.Run, error)                           { return nil, nil }
func (e *errStore) Runs() ([]model.Run, error)                                   { return nil, nil }
func (e *errStore) Quarantine(model.QuarantineRecord) error                      { return nil }
func (e *errStore) QuarantineCount() (int64, error)                              { return 0, nil }
func (e *errStore) ObservationsForExecution(string) ([]model.Observation, error) { return nil, nil }
func (e *errStore) LoadTailCursors() ([]model.TailCursor, error)                 { return nil, nil }
func (e *errStore) UpsertTailCursor(model.TailCursor) error                      { return nil }
func (e *errStore) Close() error                                                 { return nil }
func (e *errStore) UpsertAnnotation(model.Annotation) error                      { return nil }
func (e *errStore) AnnotationsForExecution(string) ([]model.Annotation, error)   { return nil, nil }
func (e *errStore) MoveAnnotations(string, string, string) error                 { return nil }
func (e *errStore) UpsertBaseline(model.Baseline) error                          { return nil }
func (e *errStore) GetBaseline(string) (model.Baseline, bool, error) {
	return model.Baseline{}, false, nil
}
func (e *errStore) ListBaselines() ([]model.Baseline, error) { return nil, nil }
func (e *errStore) DeleteBaseline(string) error              { return nil }

func TestEvictTerminalAfterCooldown(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(time.Minute)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1"}`)))
	d.evictTerminal(time.Now().Add(time.Hour))
	assert.Empty(t, d.GraphsForTest())
	assert.Equal(t, int64(1), d.EvictedForTest())
}

func TestEvictTerminalKeepsRunningAndWithinCooldown(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetReaperWindow(time.Hour)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"run1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"done"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"done"}`)))
	d.evictTerminal(time.Now())
	assert.Len(t, d.GraphsForTest(), 2)
}

func TestEvictTerminalSoftCap(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetMaxShards(1)
	for _, sid := range []string{"a", "b", "c"} {
		require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"`+sid+`"}`)))
		require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"`+sid+`"}`)))
	}
	d.evictTerminal(time.Now())
	assert.LessOrEqual(t, len(d.GraphsForTest()), 1)
}

func TestMetricsSnapshot(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"open1"}`)))
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"done1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"done1"}`)))
	m := d.metricsSnapshot()
	assert.Equal(t, 1, m.OpenRuns)
	assert.Equal(t, 2, m.Shards)
	assert.GreaterOrEqual(t, m.MaxSeq, uint64(3))
	assert.GreaterOrEqual(t, m.ReaperWindowSeconds, int64(1))
}

func TestMetricsStoreWriteErrors(t *testing.T) {
	d := New(&appendErrStore{Store: tempStore(t)})
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	assert.Equal(t, int64(1), d.metricsSnapshot().StoreWriteErrors)
}

func TestMetricsUpsertWriteError(t *testing.T) {
	d := New(&runUpsertErrStore{Store: tempStore(t)})
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	assert.Equal(t, int64(1), d.metricsSnapshot().StoreWriteErrors)
}

func makeOTLPToolReq(sessionID, toolUseID, toolName string) *collectorv1.ExportTraceServiceRequest {
	return &collectorv1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key:   "session.id",
							Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: sessionID}},
						},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								SpanId: []byte{0, 0, 0, 0, 0, 0, 0, 1},
								Name:   "claude_code.tool",
								Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
								Attributes: []*commonv1.KeyValue{
									{
										Key:   "tool_use_id",
										Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: toolUseID}},
									},
									{
										Key:   "tool_name",
										Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: toolName}},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestIngestOTLPMergesByToolUseID(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	req := makeOTLPToolReq("s1", "t1", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	execID := d.execForTest("s1")
	g := d.GraphsForTest()[execID]
	require.NotNil(t, g)
	n := g.Nodes[model.ToolCallID(execID, "t1")]
	require.NotNil(t, n)
	assert.Len(t, n.Sources, 2)
}

func TestIngestOTLPNewSession(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	req := makeOTLPToolReq("s-new", "t2", "Read")
	require.NoError(t, d.IngestOTLP(req))
	execID := d.execForTest("s-new")
	assert.NotEmpty(t, execID)
	g := d.GraphsForTest()[execID]
	require.NotNil(t, g)
	n := g.Nodes[model.ToolCallID(execID, "t2")]
	require.NotNil(t, n)
}

func TestIngestOTLPParseError(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestOTLP(nil))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestOTLPParseErrorViaSeam(t *testing.T) {
	orig := parseFn
	parseFn = func(_ *collectorv1.ExportTraceServiceRequest, _ string, _ func() uint64) ([]model.Observation, error) {
		return nil, errors.New("parse fail")
	}
	t.Cleanup(func() { parseFn = orig })
	s := tempStore(t)
	d := New(s)
	req := makeOTLPToolReq("s1", "t1", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestOTLPPanic(t *testing.T) {
	orig := applyFn
	applyFn = func(*reduce.Graph, model.Observation) { panic("otel-boom") }
	t.Cleanup(func() { applyFn = orig })
	s := tempStore(t)
	d := New(s)
	req := makeOTLPToolReq("s-panic", "t3", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestOTLPPersistError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	req := makeOTLPToolReq("s-perr", "t4", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestSessionIDOfOTLP(t *testing.T) {
	req := makeOTLPToolReq("sess-abc", "t5", "Bash")
	assert.Equal(t, "sess-abc", otelingest.SessionID(req))
}

func TestSessionIDOfOTLPNilReq(t *testing.T) {
	assert.Equal(t, "", otelingest.SessionID(nil))
}

func TestSessionIDOfOTLPNoResourceSpans(t *testing.T) {
	req := &collectorv1.ExportTraceServiceRequest{}
	assert.Equal(t, "", otelingest.SessionID(req))
}

func TestSessionIDOfOTLPNoMatchingAttr(t *testing.T) {
	req := &collectorv1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key:   "other.attr",
							Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "val"}},
						},
					},
				},
			},
		},
	}
	assert.Equal(t, "", otelingest.SessionID(req))
}

func TestIngestOTLPReloadsEvictedShard(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"otel-sess"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"otel-sess","tool_name":"Bash","tool_use_id":"t1"}`)))
	d.dropShardForTest("otel-sess")
	req := makeOTLPToolReq("otel-sess", "t1", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	execID := d.execForTest("otel-sess")
	g := d.GraphsForTest()[execID]
	require.NotNil(t, g)
	n := g.Nodes[model.ToolCallID(execID, "t1")]
	require.NotNil(t, n)
}

func TestIngestOTLPReloadError(t *testing.T) {
	d := New(&reloadErrStore{Store: tempStore(t)})
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"otel-err"}`)))
	d.dropShardForTest("otel-err")
	req := makeOTLPToolReq("otel-err", "t1", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

func TestIngestOTLPShardsByGenAIConversationID(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	req := &collectorv1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key:   "gen_ai.conversation.id",
							Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "conv-1"}},
						},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								SpanId: []byte{0, 0, 0, 0, 0, 0, 0, 2},
								Name:   "claude_code.llm_request",
								Attributes: []*commonv1.KeyValue{
									{
										Key:   "message.id",
										Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "msg_conv1"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, d.IngestOTLP(req))
	execID := d.execForTest("conv-1")
	assert.NotEmpty(t, execID)
	graphs := d.GraphsForTest()
	g := graphs[execID]
	require.NotNil(t, g)
	runs := g.RunsSnapshot()
	require.Len(t, runs, 1)
	assert.Equal(t, "conv-1", runs[0].ID)
}

func drainDeltas(t *testing.T, c *cdc.Consumer, want int) []cdc.GraphDelta {
	t.Helper()
	var got []cdc.GraphDelta
	require.Eventually(t, func() bool {
		for {
			select {
			case d := <-c.C:
				got = append(got, d)
			default:
				return len(got) >= want
			}
		}
	}, time.Second, time.Millisecond)
	return got
}

func hasKind(ds []cdc.GraphDelta, k cdc.GraphDeltaKind) bool {
	for _, d := range ds {
		if d.Kind == k {
			return true
		}
	}
	return false
}

type countingStore struct {
	store.Store
	mu    sync.Mutex
	nodes []int
	edges []int
}

func (c *countingStore) AppendDeltas(o model.Observation, deltas []cdc.GraphDelta) error {
	if err := c.Store.AppendDeltas(o, deltas); err != nil {
		return err
	}
	var n, e int
	for _, d := range deltas {
		switch d.Kind {
		case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus, cdc.DeltaNodeMerge:
			if d.Node != nil {
				n++
			}
		case cdc.DeltaEdgeUpsert:
			if d.Edge != nil {
				e++
			}
		}
	}
	c.mu.Lock()
	c.nodes = append(c.nodes, n)
	c.edges = append(c.edges, e)
	c.mu.Unlock()
	return nil
}

func TestAppendDeltasWritesOnlyChangedRowsNotWholeGraph(t *testing.T) {
	cs := &countingStore{Store: tempStore(t)}
	d := New(cs)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Read","tool_use_id":"t2","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Grep","tool_use_id":"t3","tool_input":{}}`)))

	g := d.graphs["exec1"]
	require.Greater(t, len(g.Nodes), 3)
	totalNodes := len(g.Nodes)
	totalEdges := len(g.Edges)

	cs.mu.Lock()
	cs.nodes = nil
	cs.edges = nil
	cs.mu.Unlock()

	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	cs.mu.Lock()
	defer cs.mu.Unlock()
	require.Len(t, cs.nodes, 1)
	assert.Equal(t, 1, cs.nodes[0], "one PostToolUse must persist exactly one node delta, not the whole graph")
	assert.LessOrEqual(t, cs.edges[0], 1, "one PostToolUse must touch at most one edge, not the whole graph")
	assert.Less(t, cs.nodes[0], totalNodes, "must write fewer node rows than the full graph (O(N^2) guard)")
	assert.Less(t, cs.edges[0], totalEdges, "must write fewer edge rows than the full graph (O(N^2) guard)")
}

func TestAppendDeltasPersistsEdgeDeleteToTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	d := New(s)
	fixedExecID(d)

	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	sessionEdge := model.EdgeID("exec1", model.EdgeParentChild, model.SessionNodeID("exec1"), model.ToolCallID("exec1", "t1"))
	assert.True(t, edgeRowExists(t, path, sessionEdge), "session->tool edge must be persisted before reparent")

	require.NoError(t, d.IngestTranscript([]byte(`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`), "s1"))
	turnEdge := model.EdgeID("exec1", model.EdgeParentChild, model.AssistantTurnID("exec1", "m1"), model.ToolCallID("exec1", "t1"))
	assert.True(t, edgeRowExists(t, path, turnEdge), "turn->tool edge must be persisted after reparent")
	assert.False(t, edgeRowExists(t, path, sessionEdge), "superseded session->tool edge row must be deleted from the table")
}

func TestRecoverReplaysReparentTombstoneAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestTranscript([]byte(`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`), "s1"))
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())

	g := d2.graphs["exec1"]
	require.NotNil(t, g)
	tool := model.ToolCallID("exec1", "t1")
	turnEdge := model.EdgeID("exec1", model.EdgeParentChild, model.AssistantTurnID("exec1", "m1"), tool)
	sessionEdge := model.EdgeID("exec1", model.EdgeParentChild, model.SessionNodeID("exec1"), tool)
	assert.Contains(t, g.Edges, turnEdge, "replayed graph must keep the reparented turn edge")
	assert.NotContains(t, g.Edges, sessionEdge, "replayed graph must not resurrect the tombstoned session edge")
}

func edgeRowExists(t *testing.T, path, id string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM edges WHERE id = ?", id).Scan(&n))
	return n > 0
}

func TestLiveDeltasFlowToConsumer(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	c := d.Subscribe(64)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	ds := drainDeltas(t, c, 2)
	assert.True(t, hasKind(ds, cdc.DeltaRunStarted))
	assert.True(t, hasKind(ds, cdc.DeltaNodeUpsert))
}

func TestDeltasDroppedReportedInMetrics(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	c := d.Subscribe(0)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	_ = c
	m := d.metricsSnapshot()
	assert.Positive(t, m.DeltasDropped)
}

func TestReloadDoesNotRepublishHistory(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	d.dropShardForTest("s1")
	c := d.Subscribe(64)
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))
	ds := drainDeltas(t, c, 1)
	assert.False(t, hasKind(ds, cdc.DeltaRunStarted))
	for _, x := range ds {
		assert.NotEqual(t, uint64(1), x.Rev, "historical seq=1 delta was republished")
	}
}

func TestRecoverDoesNotPublish(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	d2 := New(s)
	c := d2.Subscribe(64)
	require.NoError(t, d2.Recover())
	select {
	case got := <-c.C:
		t.Fatalf("Recover republished a delta: %+v", got)
	default:
	}
}

func TestIngestOTLPReloadDoesNotRepublish(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	d.dropShardForTest("s1")
	c := d.Subscribe(64)
	req := makeOTLPToolReq("s1", "t1", "Bash")
	require.NoError(t, d.IngestOTLP(req))
	ds := drainDeltas(t, c, 1)
	assert.False(t, hasKind(ds, cdc.DeltaRunStarted))
	for _, x := range ds {
		assert.NotEqual(t, uint64(1), x.Rev, "historical seq=1 delta was republished via OTLP reload")
	}
}

func TestPublishedDeltaIsIndependentSnapshot(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	c := d.Subscribe(64)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	ds := drainDeltas(t, c, 1)
	var toolDelta *cdc.GraphDelta
	for i := range ds {
		if ds[i].Kind == cdc.DeltaNodeUpsert && ds[i].Node != nil {
			toolDelta = &ds[i]
			break
		}
	}
	require.NotNil(t, toolDelta)
	statusAtDrain := toolDelta.Node.Status
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))
	assert.Equal(t, statusAtDrain, toolDelta.Node.Status, "published delta node was mutated by a later apply")
}

func TestExporterLagZeroWhenNoExporter(t *testing.T) {
	d := New(tempStore(t))
	m := d.metricsSnapshot()
	assert.Equal(t, int64(0), m.ExporterLag)
}

func TestExporterLagReflectsConsumerDropped(t *testing.T) {
	d := New(tempStore(t))
	c := d.Subscribe(0)
	d.mu.Lock()
	d.exporterConsumers = []*cdc.Consumer{c}
	d.mu.Unlock()
	d.bus.Publish(cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, Node: &model.Node{ID: "x"}})
	d.bus.Publish(cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 2, Node: &model.Node{ID: "y"}})
	m := d.metricsSnapshot()
	assert.Positive(t, m.ExporterLag)
}

func TestIngestStreamJSONSessionInit(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-8"}`), "s1"))
	execID := d.execForTest("s1")
	require.Equal(t, "exec1", execID)
	n := d.GraphsForTest()[execID].Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n)
	assert.Equal(t, model.StatusRunning, n.Status)
}

func TestIngestStreamJSONMergesByToolUseIDWithHook(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"toolu_x","tool_input":{}}`)))
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestStreamJSON(line, "s1"))
	execID := d.execForTest("s1")
	n := d.GraphsForTest()[execID].Nodes[model.ToolCallID(execID, "toolu_x")]
	require.NotNil(t, n)
	assert.Len(t, n.Sources, 2)
}

func TestIngestStreamJSONParseErrorViaSeam(t *testing.T) {
	orig := streamParseFn
	streamParseFn = func(_ []byte, _ string, _ func() uint64) ([]model.Observation, error) {
		return nil, errors.New("parse fail")
	}
	t.Cleanup(func() { streamParseFn = orig })
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONBadJSONQuarantines(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{not json`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONPanic(t *testing.T) {
	orig := applyFn
	applyFn = func(*reduce.Graph, model.Observation) { panic("sj-boom") }
	t.Cleanup(func() { applyFn = orig })
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONPersistError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONReloadsEvictedShard(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	execID := d.execForTest("s1")
	d.dropShardForTest("s1")
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`), "s1"))
	g := d.GraphsForTest()[execID]
	require.NotNil(t, g)
	require.NotNil(t, g.Nodes[model.SessionNodeID(execID)])
	require.NotNil(t, g.Nodes[model.ToolCallID(execID, "t1")])
}

func TestIngestStreamJSONReloadError(t *testing.T) {
	base := tempStore(t)
	d := New(base)
	fixedExecID(d)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	d.dropShardForTest("s1")
	d.store = &reloadErrStore{Store: base}
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1"}}`), "s1"))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

func TestIngestTranscriptBuildsToolNode(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	line := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestTranscript(line, "s1"))
	exec := d.execForTest("s1")
	require.Equal(t, "exec1", exec)
	n := d.GraphsForTest()[exec].Nodes[model.ToolCallID(exec, "toolu_1")]
	require.NotNil(t, n)
	assert.Equal(t, "Bash", n.Name)
	require.NotEmpty(t, n.Sources)
	assert.Equal(t, model.SourceJSONL, n.Sources[0].Source)
}

func TestIngestTranscriptQuarantinesBadLine(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestTranscript([]byte("{not json}"), "s1"))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

func TestIngestTranscriptKnownSessionReloadsEvictedShard(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	line := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestTranscript(line, "s1"))
	d.dropShardForTest("s1")
	line2 := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:01:00Z","message":{"role":"assistant","id":"m2","content":[{"type":"tool_use","id":"toolu_3","name":"Read","input":{"path":"a"}}]}}`)
	require.NoError(t, d.IngestTranscript(line2, "s1"))
	exec := d.execForTest("s1")
	g := d.GraphsForTest()[exec]
	require.NotNil(t, g.Nodes[model.ToolCallID(exec, "toolu_2")])
	require.NotNil(t, g.Nodes[model.ToolCallID(exec, "toolu_3")])
}

func TestIngestTranscriptReloadError(t *testing.T) {
	base := tempStore(t)
	d := New(base)
	fixedExecID(d)
	line := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestTranscript(line, "s1"))
	d.dropShardForTest("s1")
	d.store = &reloadErrStore{Store: base}
	require.NoError(t, d.IngestTranscript(line, "s1"))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

func TestIngestTranscriptPanicRecovers(t *testing.T) {
	orig := applyFn
	applyFn = func(*reduce.Graph, model.Observation) { panic("transcript-boom") }
	t.Cleanup(func() { applyFn = orig })
	s := tempStore(t)
	d := New(s)
	line := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_4","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestTranscript(line, "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestTranscriptParseErrorViaSeam(t *testing.T) {
	orig := tailParseFn
	tailParseFn = func(_ io.Reader, _ string, _ func() uint64, _ func(time.Time) time.Time) ([]model.Observation, error) {
		return nil, errors.New("parse fail")
	}
	t.Cleanup(func() { tailParseFn = orig })
	s := tempStore(t)
	d := New(s)
	line := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_5","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestTranscript(line, "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestTranscriptPersistError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	line := []byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_6","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestTranscript(line, "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestMarkLossySetsRunMetaAndCounter(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestTranscript([]byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}`), "s1"))
	d.MarkLossy("s1")
	exec := d.execForTest("s1")
	g := d.GraphsForTest()[exec]
	assert.Equal(t, true, g.Runs["s1"].Meta["lossy"])
	assert.Equal(t, int64(1), d.LossyForTest())
}

func TestMarkLossyUnknownSessionNoop(t *testing.T) {
	d := New(tempStore(t))
	d.MarkLossy("nope")
	assert.Equal(t, int64(0), d.LossyForTest())
}

func TestMarkLossyKnownSessionNoGraph(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.mu.Lock()
	d.execBySession["s1"] = "exec1"
	d.mu.Unlock()
	d.MarkLossy("s1")
	assert.Equal(t, int64(0), d.LossyForTest())
}

func TestMarkLossyKnownGraphNoRun(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.mu.Lock()
	d.execBySession["s1"] = "exec1"
	d.graphs["exec1"] = reduce.NewGraph()
	d.mu.Unlock()
	d.MarkLossy("s1")
	assert.Equal(t, int64(0), d.LossyForTest())
}

func TestMetricsIncludesLossyRuns(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestTranscript([]byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}`), "s1"))
	d.MarkLossy("s1")
	assert.Equal(t, int64(1), d.metricsSnapshot().LossyRuns)
}

func TestSetDBPath(t *testing.T) {
	d := New(tempStore(t))
	d.SetDBPath("/data/catacomb.db")
	d.mu.Lock()
	got := d.dbPath
	d.mu.Unlock()
	assert.Equal(t, "/data/catacomb.db", got)
}

type lossyUpsertErrStore struct{ store.Store }

func (s *lossyUpsertErrStore) UpsertRun(model.Run) error { return errors.New("upsert lossy") }

func TestMarkLossyUpsertRunError(t *testing.T) {
	base := tempStore(t)
	d := New(base)
	fixedExecID(d)
	require.NoError(t, d.IngestTranscript([]byte(`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}`), "s1"))
	d.store = &lossyUpsertErrStore{Store: base}
	d.MarkLossy("s1")
	assert.Equal(t, int64(1), d.LossyForTest())
}

func TestIngestSubagentMetaCreatesNode(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	m := model.SubagentMeta{
		SessionID:   "s1",
		AgentID:     "agent_42",
		ToolUseID:   "toolu_parent",
		AgentType:   "general-purpose",
		Description: "Review PR",
	}
	require.NoError(t, d.IngestSubagentMeta(m))
	g := d.graphs["exec1"]
	require.NotNil(t, g)
	assert.NotEmpty(t, g.Nodes)
}

func TestIngestSubagentMetaNewSession(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	m := model.SubagentMeta{
		SessionID: "fresh-session",
		AgentID:   "agent_1",
	}
	require.NoError(t, d.IngestSubagentMeta(m))
	assert.NotEmpty(t, d.execBySession["fresh-session"])
}

func TestIngestSubagentMetaQuarantinesOnStoreError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	m := model.SubagentMeta{SessionID: "s1", AgentID: "a1"}
	require.NoError(t, d.IngestSubagentMeta(m))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestSubagentMetaRecoversPanic(t *testing.T) {
	orig := applyFn
	applyFn = func(*reduce.Graph, model.Observation) { panic("boom") }
	t.Cleanup(func() { applyFn = orig })
	d := New(tempStore(t))
	m := model.SubagentMeta{SessionID: "s1", AgentID: "a1"}
	require.NoError(t, d.IngestSubagentMeta(m))
}

func TestIngestSubagentMetaKnownSessionLoadsGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())
	d2.dropShardForTest("s1")
	m := model.SubagentMeta{SessionID: "s1", AgentID: "a1", ToolUseID: "toolu_x"}
	require.NoError(t, d2.IngestSubagentMeta(m))
	require.NotNil(t, d2.graphs["exec1"])
}

func TestIngestSubagentMetaKnownSessionReloadError(t *testing.T) {
	d := New(&reloadErrStore{Store: tempStore(t)})
	d.mu.Lock()
	d.execBySession["s1"] = "exec1"
	d.mu.Unlock()
	m := model.SubagentMeta{SessionID: "s1", AgentID: "a1"}
	require.NoError(t, d.IngestSubagentMeta(m))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}

func TestCwdTranscriptExcludeEncodes(t *testing.T) {
	orig := getwdFn
	getwdFn = func() (string, error) { return "/Users/test/.cache/proj.v2", nil }
	t.Cleanup(func() { getwdFn = orig })
	assert.Equal(t, "-Users-test--cache-proj-v2"+string(os.PathSeparator), cwdTranscriptExclude())
}

func TestCwdTranscriptExcludeErrorReturnsEmpty(t *testing.T) {
	orig := getwdFn
	getwdFn = func() (string, error) { return "", errors.New("no cwd") }
	t.Cleanup(func() { getwdFn = orig })
	assert.Empty(t, cwdTranscriptExclude())
}

type failAnnotationsStore struct{ store.Store }

func (s *failAnnotationsStore) AnnotationsForExecution(string) ([]model.Annotation, error) {
	return nil, errors.New("fail")
}

func TestRecoverReattachAnnotationsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s1, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d1 := New(s1)
	fixedExecID(d1)
	require.NoError(t, d1.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, s1.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(&failAnnotationsStore{Store: s2})
	require.Error(t, d2.Recover())
}

func TestApplyAndPersistCarryOverDeltaNodeMerge(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowAnnotations(true)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	execID := d.execForTest("s1")
	t1ID := model.ToolCallID(execID, "t1")
	t2ID := model.ToolCallID(execID, "t2")
	sourceKey := model.NodeSourceKey(t1ID)
	require.NoError(t, d.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`5`)))

	origApply := applyFn
	applyFn = func(g *reduce.Graph, o model.Observation) {
		g.Nodes[t2ID] = &model.Node{ID: t2ID, RunID: o.RunID, Type: model.NodeToolCall}
	}
	t.Cleanup(func() { applyFn = origApply })

	origDrain := drainFn
	drainFn = func(g *reduce.Graph) []cdc.GraphDelta {
		return []cdc.GraphDelta{{
			Kind:        cdc.DeltaNodeMerge,
			OldID:       t1ID,
			NewID:       t2ID,
			ExecutionID: execID,
		}}
	}
	t.Cleanup(func() { drainFn = origDrain })

	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	d.mu.Lock()
	newNode := d.graphs[execID].Nodes[t2ID]
	d.mu.Unlock()

	require.NotNil(t, newNode)
	assert.Equal(t, json.RawMessage(`5`), newNode.Annotations["eval.score"])
}

type failOnNthAppendStore struct {
	store.Store
	count int
	failN int
}

func (s *failOnNthAppendStore) AppendDeltas(o model.Observation, deltas []cdc.GraphDelta) error {
	s.count++
	if s.count == s.failN {
		return errors.New("nth append fail")
	}
	return s.Store.AppendDeltas(o, deltas)
}

func TestSetReproConfig(t *testing.T) {
	d := New(nil)
	d.SetReproConfig(repro.Config{OTLPEndpoint: "grpc://x:4317"})
	d.mu.Lock()
	got := d.reproConfig
	d.mu.Unlock()
	assert.Equal(t, repro.Config{OTLPEndpoint: "grpc://x:4317"}, got)
}

func TestSetReproConfigDifferentConfigsDifferentHashes(t *testing.T) {
	dir := t.TempDir()

	makeHash := func(endpoint string) string {
		d := New(tempStore(t))
		d.SetReproConfig(repro.Config{OTLPEndpoint: endpoint})
		d.SetReproCapture(func(_ string, cfg repro.Config) repro.Hashes {
			return repro.Hashes{CatacombConfigHash: repro.ConfigHash(cfg)}
		})
		p, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
		require.NoError(t, d.Ingest("SessionStart", p))
		d.mu.Lock()
		defer d.mu.Unlock()
		r := d.graphs[d.execBySession["s1"]].Runs["s1"]
		if r == nil || r.Repro == nil {
			return ""
		}
		return r.Repro.CatacombConfigHash
	}

	h1 := makeHash("grpc://a:4317")
	h2 := makeHash("grpc://b:4317")
	h3 := makeHash("grpc://a:4317")

	require.NotEmpty(t, h1)
	require.NotEmpty(t, h2)
	assert.NotEqual(t, h1, h2)
	assert.Equal(t, h1, h3)
}

func TestSetReproCapture(t *testing.T) {
	d := New(tempStore(t))
	d.SetReproCapture(func(_ string, _ repro.Config) repro.Hashes {
		return repro.Hashes{}
	})
	d.mu.Lock()
	assert.NotNil(t, d.reproCapture)
	d.mu.Unlock()
}

func TestSetCatacombVersion(t *testing.T) {
	d := New(tempStore(t))
	d.SetCatacombVersion("v1.2.3")
	d.mu.Lock()
	assert.Equal(t, "v1.2.3", d.catacombVersion)
	d.mu.Unlock()
}

func TestCaptureReproIfReadyHappyPath(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetReproCapture(func(_ string, cfg repro.Config) repro.Hashes {
		return repro.Hashes{CatacombConfigHash: repro.ConfigHash(cfg)}
	})
	d.SetCatacombVersion("v1.0")
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1","cwd":"/repo"}`)))
	d.mu.Lock()
	captured := d.reproCaptured["s1"]
	r := d.graphs["exec1"].Runs["s1"]
	d.mu.Unlock()
	assert.True(t, captured)
	require.NotNil(t, r)
	require.NotNil(t, r.Repro)
	assert.Equal(t, "v1.0", r.Repro.CatacombVersion)
}

func TestCaptureReproNilGraph(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	d.execBySession["orphan"] = ""
	d.mu.Unlock()
	d.captureReproForTest("orphan")
	d.mu.Lock()
	captured := d.reproCaptured["orphan"]
	d.mu.Unlock()
	assert.False(t, captured)
}

func TestCaptureReproAlreadyCaptured(t *testing.T) {
	d := New(tempStore(t))
	d.SetReproCaptureCounterForTest("s1", true)
	initial := d.ReproCapturedCountForTest()
	d.captureReproForTest("s1")
	assert.Equal(t, initial, d.ReproCapturedCountForTest())
}

func TestCaptureReproStoreError(t *testing.T) {
	s := &failOnNthAppendStore{Store: tempStore(t), failN: 2}
	d := New(s)
	fixedExecID(d)
	d.SetReproCapture(func(_ string, _ repro.Config) repro.Hashes {
		return repro.Hashes{}
	})
	d.SetCatacombVersion("v1.0")
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1","cwd":"/repo"}`)))
}

func TestRecoverMarksRunsAsCaptured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d := New(s)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, s.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())
	d2.mu.Lock()
	captured := d2.reproCaptured["s1"]
	d2.mu.Unlock()
	assert.True(t, captured)
}

func TestCaptureReproNoCwdDeferred(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	d.mu.Lock()
	captured := d.reproCaptured["s1"]
	d.mu.Unlock()
	assert.False(t, captured)
}

func TestReproDefaultCapturePathNoSetter(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude\n"), 0o644))
	d := New(tempStore(t))
	fixedExecID(d)
	payload, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
	require.NoError(t, d.Ingest("SessionStart", payload))
	d.mu.Lock()
	r := d.graphs["exec1"].Runs["s1"]
	d.mu.Unlock()
	require.NotNil(t, r)
	require.NotNil(t, r.Repro)
	assert.Len(t, r.Repro.PromptsHash, 64)
	assert.NotEqual(t, repro.Absent, r.Repro.PromptsHash)
	assert.Len(t, r.Repro.CatacombConfigHash, 64)
}

func TestSummarizeRunExposesRepro(t *testing.T) {
	g := reduce.NewGraph()
	g.Runs["r1"] = &model.Run{
		ID:     "r1",
		Status: model.StatusOK,
		Repro: &model.ReproMeta{
			ClaudeCodeVersion: "1.2.3",
			Cwd:               "/work",
			PromptsHash:       "abc123",
		},
	}
	sum := SummarizeRun("r1", []*reduce.Graph{g})
	require.NotNil(t, sum.Repro)
	assert.Equal(t, "1.2.3", sum.Repro.ClaudeCodeVersion)
	assert.Equal(t, "/work", sum.Repro.Cwd)
	assert.Equal(t, "abc123", sum.Repro.PromptsHash)
}

func TestReproTwoRunsSameConfigEqualHashes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude\n"), 0o644))
	d := New(tempStore(t))
	p1, _ := json.Marshal(map[string]string{"session_id": "s1", "cwd": dir})
	p2, _ := json.Marshal(map[string]string{"session_id": "s2", "cwd": dir})
	require.NoError(t, d.Ingest("SessionStart", p1))
	require.NoError(t, d.Ingest("SessionStart", p2))
	d.mu.Lock()
	r1 := d.graphs[d.execBySession["s1"]].Runs["s1"]
	r2 := d.graphs[d.execBySession["s2"]].Runs["s2"]
	d.mu.Unlock()
	require.NotNil(t, r1.Repro)
	require.NotNil(t, r2.Repro)
	assert.Equal(t, r1.Repro.PromptsHash, r2.Repro.PromptsHash)
	assert.Equal(t, r1.Repro.CatacombConfigHash, r2.Repro.CatacombConfigHash)
}

func TestSetSinks(t *testing.T) {
	d := New(tempStore(t))
	sinks := []config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"}}
	d.SetSinks(sinks)
	d.mu.Lock()
	got := d.sinks
	d.mu.Unlock()
	require.Len(t, got, 1)
	assert.Equal(t, config.SinkOTLP, got[0].Type)
}

func TestSetSources(t *testing.T) {
	d := New(tempStore(t))
	enabled := true
	src := config.SourcesConfig{
		Hooks: config.SourceToggle{Enabled: &enabled},
		JSONL: config.JSONLSource{Enabled: &enabled, TranscriptDir: "/t"},
	}
	d.SetSources(src)
	d.mu.Lock()
	got := d.sources
	d.mu.Unlock()
	require.NotNil(t, got.Hooks.Enabled)
	assert.True(t, *got.Hooks.Enabled)
	assert.Equal(t, "/t", got.JSONL.TranscriptDir)
}
