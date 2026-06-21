package daemon

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
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

func (s *appendErrStore) AppendAndApply(model.Observation, []*model.Node, []*model.Edge) error {
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

func (e *errStore) AppendAndApply(model.Observation, []*model.Node, []*model.Edge) error {
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
func (e *errStore) Close() error                                                 { return nil }

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
