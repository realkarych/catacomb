package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/model"
)

var _ exportiface.Exporter = (*Exporter)(nil)

type sqlCall struct {
	sql  string
	args []any
}

type recordExecer struct {
	calls   []sqlCall
	txCalls [][]sqlCall
	closed  bool
}

func (r *recordExecer) Exec(_ context.Context, sql string, args ...any) error {
	r.calls = append(r.calls, sqlCall{sql: sql, args: args})
	return nil
}

func (r *recordExecer) BeginTx(_ context.Context) (txExecer, error) {
	tx := &recordTx{parent: r}
	return tx, nil
}

func (r *recordExecer) Close() { r.closed = true }

type recordTx struct {
	parent *recordExecer
	calls  []sqlCall
}

func (t *recordTx) Exec(_ context.Context, sql string, args ...any) error {
	t.calls = append(t.calls, sqlCall{sql: sql, args: args})
	return nil
}

func (t *recordTx) Commit(_ context.Context) error {
	t.parent.txCalls = append(t.parent.txCalls, t.calls)
	return nil
}

func (t *recordTx) Rollback(_ context.Context) error { return nil }

type errorExecer struct {
	execErr    error
	beginTxErr error
	callN      int
	failOnCall int
}

func (e *errorExecer) Exec(_ context.Context, _ string, _ ...any) error {
	e.callN++
	if e.failOnCall > 0 && e.callN == e.failOnCall {
		return e.execErr
	}
	if e.failOnCall == 0 {
		return e.execErr
	}
	return nil
}

func (e *errorExecer) BeginTx(_ context.Context) (txExecer, error) {
	if e.beginTxErr != nil {
		return nil, e.beginTxErr
	}
	return &errorTx{execErr: e.execErr}, nil
}

func (e *errorExecer) Close() {}

type errorTx struct {
	execErr error
	callN   int
	failOn  int
}

func (t *errorTx) Exec(_ context.Context, _ string, _ ...any) error {
	t.callN++
	if t.failOn > 0 && t.callN == t.failOn {
		return t.execErr
	}
	if t.failOn == 0 {
		return t.execErr
	}
	return nil
}

func (t *errorTx) Commit(_ context.Context) error   { return nil }
func (t *errorTx) Rollback(_ context.Context) error { return nil }

type stubPgxTx struct {
	execErr     error
	commitErr   error
	rollbackErr error
}

func (s *stubPgxTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, nil }
func (s *stubPgxTx) Commit(_ context.Context) error          { return s.commitErr }
func (s *stubPgxTx) Rollback(_ context.Context) error        { return s.rollbackErr }
func (s *stubPgxTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (s *stubPgxTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (s *stubPgxTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (s *stubPgxTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (s *stubPgxTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, s.execErr
}
func (s *stubPgxTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (s *stubPgxTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }
func (s *stubPgxTx) Conn() *pgx.Conn                                               { return nil }

type stubPgxQuerier struct {
	execErr    error
	beginTxErr error
	tx         pgx.Tx
	closed     bool
}

func (s *stubPgxQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, s.execErr
}

func (s *stubPgxQuerier) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	if s.beginTxErr != nil {
		return nil, s.beginTxErr
	}
	return s.tx, nil
}

func (s *stubPgxQuerier) Close() { s.closed = true }

func TestNameIsPostgres(t *testing.T) {
	e := ExporterWithExecer(&recordExecer{})
	assert.Equal(t, "postgres", e.Name())
}

func TestShutdownClosesExecer(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.Shutdown(context.Background()))
	assert.True(t, r.closed)
}

func TestFlushRunIsNoop(t *testing.T) {
	e := ExporterWithExecer(&recordExecer{})
	require.NoError(t, e.FlushRun(context.Background(), "any-run"))
}

func TestApplyDeltaNodeUpsertEmitsSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusRunning, Rev: 3}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 3, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].sql, "ON CONFLICT")
	assert.Contains(t, r.calls[0].sql, "WHERE excluded.rev > nodes.rev")
}

func TestApplyDeltaNodeStatusEmitsSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, Rev: 5}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeStatus, Rev: 5, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].sql, "ON CONFLICT")
}

func TestApplyDeltaEdgeUpsertEmitsSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "c", Rev: 2}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeUpsert, Rev: 2, RunID: "r1", Edge: edge,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].sql, "edges")
	assert.Contains(t, r.calls[0].sql, "ON CONFLICT")
	assert.Contains(t, r.calls[0].sql, "WHERE excluded.rev > edges.rev")
}

func TestApplyDeltaNodeMergeDeletesThenUpserts(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	n := &model.Node{ID: "n-new", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 7}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 7, RunID: "r1", OldID: "n-old", NewID: "n-new", Node: n,
	}))
	require.Len(t, r.calls, 2)
	assert.Contains(t, strings.ToLower(r.calls[0].sql), "delete")
	assert.Contains(t, r.calls[0].sql, "$1")
	assert.Contains(t, r.calls[1].sql, "ON CONFLICT")
}

func TestApplyDeltaEdgeDeleteEmitsDelete(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1", Edge: edge,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, strings.ToLower(r.calls[0].sql), "delete")
	assert.Contains(t, r.calls[0].sql, "edges")
}

func TestApplyDeltaLifecycleKindsAreNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	for _, k := range []cdc.GraphDeltaKind{cdc.DeltaRunStarted, cdc.DeltaSessionEnded, cdc.DeltaRunEnded} {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: k, Rev: 1, RunID: "r1"}))
	}
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNilNodeIsNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1"}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNilEdgeIsNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1"}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNodeUpsertAttrsJSONEncoded(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	n := &model.Node{
		ID: "n3", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1,
		Attrs: map[string]any{"key": "val"},
	}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	var foundJSON bool
	for _, arg := range r.calls[0].args {
		if s, ok := arg.(string); ok {
			var m map[string]any
			if json.Unmarshal([]byte(s), &m) == nil {
				if m["key"] == "val" {
					foundJSON = true
				}
			}
		}
	}
	assert.True(t, foundJSON, "attrs must be JSON-encoded in args")
}

func TestApplyDeltaNodeUpsertPayloadNeverInSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	n := &model.Node{
		ID: "n4", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1,
		Payload: &model.Payload{Hash: "abc123", Input: []byte(`"UNIQUESENTINELXYZ"`)},
	}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	for _, call := range r.calls {
		assert.NotContains(t, call.sql, "UNIQUESENTINELXYZ", "raw payload content must never appear in SQL")
		for _, arg := range call.args {
			assert.NotContains(t, fmt.Sprintf("%v", arg), "UNIQUESENTINELXYZ", "raw payload content must never appear in args")
		}
	}
}

func TestSnapshotStateUpsertsBatched(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	e.schemaReady = true
	nodes := []*model.Node{
		{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1},
		{ID: "b", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 2},
	}
	edges := []*model.Edge{
		{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1},
	}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edges))
	require.Equal(t, 1, len(r.txCalls), "snapshot must use one transaction")
	totalCalls := len(r.txCalls[0])
	assert.Equal(t, 3, totalCalls, "two node upserts + one edge upsert in the tx")
}

func TestSnapshotStateEmptyIsNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.SnapshotState(context.Background(), nil, nil))
	assert.Empty(t, r.txCalls)
}

func TestApplyDeltaEnsuresSchemaOnceOnFirstWrite(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusRunning, Rev: 1}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 3, "2 CREATE TABLE + 1 upsert on first write")
	assert.Contains(t, strings.ToLower(r.calls[0].sql), "create table")
	assert.Contains(t, strings.ToLower(r.calls[1].sql), "create table")
	assert.Contains(t, r.calls[2].sql, "ON CONFLICT")

	n2 := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 2}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 2, RunID: "r1", Node: n2,
	}))
	require.Len(t, r.calls, 4, "only 1 more upsert on second write (ensure runs exactly once)")
}

func TestSnapshotStateEnsuresSchemaOnFirstWrite(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	nodes := []*model.Node{
		{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1},
	}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, nil))
	require.Len(t, r.calls, 2, "2 CREATE TABLE calls before BeginTx")
	assert.Contains(t, strings.ToLower(r.calls[0].sql), "create table")
	assert.Contains(t, strings.ToLower(r.calls[1].sql), "create table")
	require.Equal(t, 1, len(r.txCalls), "one transaction with node upsert")
}

func TestNewWithValidDSNConstructsPool(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, "postgres://localhost:5432/catacomb_test")
	require.NoError(t, err)
	assert.Equal(t, "postgres", e.Name())
	_ = e.Shutdown(ctx)
}

func TestNewWithMalformedDSNReturnsError(t *testing.T) {
	_, err := New(context.Background(), "not-a-dsn://\x00invalid")
	require.Error(t, err)
}

func TestEnsureSchemaNodesTableError(t *testing.T) {
	errDB := errors.New("db error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create nodes table")
}

func TestEnsureSchemaEdgesTableError(t *testing.T) {
	errDB := errors.New("edges table error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 2})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create edges table")
}

func TestApplyDeltaNodeUpsertEnsureError(t *testing.T) {
	errDB := errors.New("ensure error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	})
	require.Error(t, err)
}

func TestApplyDeltaNodeUpsertExecError(t *testing.T) {
	errDB := errors.New("upsert error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 3})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert node")
}

func TestApplyDeltaNodeStatusEnsureError(t *testing.T) {
	errDB := errors.New("ensure error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeStatus, Rev: 1, RunID: "r1", Node: n,
	})
	require.Error(t, err)
}

func TestApplyDeltaEdgeUpsertEnsureError(t *testing.T) {
	errDB := errors.New("ensure error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1", Edge: edge,
	})
	require.Error(t, err)
}

func TestApplyDeltaEdgeUpsertExecError(t *testing.T) {
	errDB := errors.New("edge upsert error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 3})
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1", Edge: edge,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert edge")
}

func TestApplyDeltaNodeMergeNilNode(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 1, RunID: "r1",
	}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNodeMergeEnsureError(t *testing.T) {
	errDB := errors.New("ensure error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	n := &model.Node{ID: "n-new", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 1, RunID: "r1", OldID: "n-old", Node: n,
	})
	require.Error(t, err)
}

func TestApplyDeltaNodeMergeDeleteError(t *testing.T) {
	errDB := errors.New("delete error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 3})
	n := &model.Node{ID: "n-new", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 1, RunID: "r1", OldID: "n-old", Node: n,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node_merge delete")
}

func TestApplyDeltaEdgeDeleteNilEdge(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1",
	}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaEdgeDeleteEnsureError(t *testing.T) {
	errDB := errors.New("ensure error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1", Edge: edge,
	})
	require.Error(t, err)
}

func TestApplyDeltaEdgeDeleteExecError(t *testing.T) {
	errDB := errors.New("delete error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 3})
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1", Edge: edge,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edge_delete")
}

func TestSnapshotStateEnsureError(t *testing.T) {
	errDB := errors.New("ensure error")
	e := ExporterWithExecer(&errorExecer{execErr: errDB, failOnCall: 1})
	nodes := []*model.Node{{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1}}
	err := e.SnapshotState(context.Background(), nodes, nil)
	require.Error(t, err)
}

func TestSnapshotStateBeginTxError(t *testing.T) {
	errTx := errors.New("begin tx error")
	e := ExporterWithExecer(&errorExecer{beginTxErr: errTx})
	e.schemaReady = true
	nodes := []*model.Node{{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1}}
	err := e.SnapshotState(context.Background(), nodes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot begin")
}

func TestSnapshotStateUpsertNodeTxError(t *testing.T) {
	errExec := errors.New("upsert node tx error")
	e := ExporterWithExecer(&errorExecer{execErr: errExec})
	e.schemaReady = true
	nodes := []*model.Node{{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1}}
	err := e.SnapshotState(context.Background(), nodes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot upsert node")
}

func TestSnapshotStateUpsertEdgeTxError(t *testing.T) {
	errExec := errors.New("upsert edge tx error")
	e := ExporterWithExecer(&errorExecer{execErr: errExec})
	e.schemaReady = true
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1}}
	err := e.SnapshotState(context.Background(), nil, edges)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot upsert edge")
}

func TestPoolAdapterExec(t *testing.T) {
	q := &stubPgxQuerier{}
	p := &poolAdapter{pool: q}
	require.NoError(t, p.Exec(context.Background(), "SELECT 1"))
}

func TestPoolAdapterExecError(t *testing.T) {
	errExec := errors.New("exec error")
	q := &stubPgxQuerier{execErr: errExec}
	p := &poolAdapter{pool: q}
	err := p.Exec(context.Background(), "SELECT 1")
	require.Error(t, err)
}

func TestPoolAdapterBeginTx(t *testing.T) {
	stub := &stubPgxTx{}
	q := &stubPgxQuerier{tx: stub}
	p := &poolAdapter{pool: q}
	tx, err := p.BeginTx(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tx)
}

func TestPoolAdapterBeginTxError(t *testing.T) {
	errTx := errors.New("begin tx error")
	q := &stubPgxQuerier{beginTxErr: errTx}
	p := &poolAdapter{pool: q}
	_, err := p.BeginTx(context.Background())
	require.Error(t, err)
}

func TestPoolAdapterClose(t *testing.T) {
	q := &stubPgxQuerier{}
	p := &poolAdapter{pool: q}
	p.Close()
	assert.True(t, q.closed)
}

func TestTxAdapterExec(t *testing.T) {
	stub := &stubPgxTx{}
	ta := &txAdapter{tx: stub}
	require.NoError(t, ta.Exec(context.Background(), "SELECT 1"))
}

func TestTxAdapterExecError(t *testing.T) {
	errExec := errors.New("exec error")
	stub := &stubPgxTx{execErr: errExec}
	ta := &txAdapter{tx: stub}
	err := ta.Exec(context.Background(), "SELECT 1")
	require.Error(t, err)
}

func TestTxAdapterCommit(t *testing.T) {
	stub := &stubPgxTx{}
	ta := &txAdapter{tx: stub}
	require.NoError(t, ta.Commit(context.Background()))
}

func TestTxAdapterCommitError(t *testing.T) {
	errCommit := errors.New("commit error")
	stub := &stubPgxTx{commitErr: errCommit}
	ta := &txAdapter{tx: stub}
	err := ta.Commit(context.Background())
	require.Error(t, err)
}

func TestTxAdapterRollback(t *testing.T) {
	stub := &stubPgxTx{}
	ta := &txAdapter{tx: stub}
	require.NoError(t, ta.Rollback(context.Background()))
}

func TestTxAdapterRollbackError(t *testing.T) {
	errRollback := errors.New("rollback error")
	stub := &stubPgxTx{rollbackErr: errRollback}
	ta := &txAdapter{tx: stub}
	err := ta.Rollback(context.Background())
	require.Error(t, err)
}

func TestJsonMarshalNil(t *testing.T) {
	assert.Equal(t, "null", jsonMarshal(nil))
}

func TestJsonMarshalChannel(t *testing.T) {
	ch := make(chan struct{})
	result := jsonMarshal(ch)
	assert.Equal(t, "null", result)
}

func TestNodeArgsTimestamps(t *testing.T) {
	now := time.Now()
	dur := int64(100)
	tokIn := int64(10)
	tokOut := int64(20)
	cost := 0.5
	n := &model.Node{
		ID: "n1", RunID: "r1", Rev: 1,
		TStart: &now, TEnd: &now, DurationMS: &dur,
		TokensIn: &tokIn, TokensOut: &tokOut, CostUSD: &cost,
	}
	args := nodeArgs(n)
	assert.Len(t, args, 18)
}
