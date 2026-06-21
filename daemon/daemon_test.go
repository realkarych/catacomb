package daemon

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
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
	d := New(tempStore(t))
	require.Error(t, d.Ingest("PreToolUse", []byte("{not json}")))
}

func TestIngestSeqMonotonic(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("UserPromptSubmit", []byte(`{"session_id":"s1","prompt":"hi"}`)))
	assert.Equal(t, uint64(2), d.seq)
}

func TestIngestStoreError(t *testing.T) {
	d := New(&errStore{failAppend: true})
	require.Error(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
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

type errStore struct {
	failAppend bool
	failSince  bool
}

func (e *errStore) Persist([]model.Observation, []*model.Node, []*model.Edge) error { return nil }

func (e *errStore) AppendAndApply(model.Observation, []*model.Node, []*model.Edge) error {
	if e.failAppend {
		return errors.New("append")
	}
	return nil
}

func (e *errStore) MaxSeq() (uint64, error) { return 0, nil }

func (e *errStore) ObservationsSince(uint64) ([]model.Observation, error) {
	if e.failSince {
		return nil, errors.New("since")
	}
	return nil, nil
}

func (e *errStore) UpsertRun(model.Run) error               { return nil }
func (e *errStore) ListOpenRuns() ([]model.Run, error)      { return nil, nil }
func (e *errStore) Runs() ([]model.Run, error)              { return nil, nil }
func (e *errStore) Quarantine(model.QuarantineRecord) error { return nil }
func (e *errStore) QuarantineCount() (int64, error)         { return 0, nil }
func (e *errStore) Close() error                            { return nil }
