package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/model"
)

var (
	_ exportiface.Exporter    = (*Exporter)(nil)
	_ exportiface.RunExporter = (*Exporter)(nil)
)

type txExecer interface {
	Exec(ctx context.Context, sql string, args ...any) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) error
	BeginTx(ctx context.Context) (txExecer, error)
	Close()
}

type pgxQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	Close()
}

type poolAdapter struct {
	pool pgxQuerier
}

func (p *poolAdapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := p.pool.Exec(ctx, sql, args...)
	return err
}

func (p *poolAdapter) BeginTx(ctx context.Context) (txExecer, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	return &txAdapter{tx: tx}, nil
}

func (p *poolAdapter) Close() { p.pool.Close() }

type txAdapter struct {
	tx pgx.Tx
}

func (t *txAdapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.tx.Exec(ctx, sql, args...)
	return err
}

func (t *txAdapter) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *txAdapter) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

type Exporter struct {
	db              execer
	schemaReady     bool
	runsSchemaReady bool
}

func New(ctx context.Context, dsn string) (*Exporter, error) {
	return newFull(ctx, dsn, newPool)
}

func newFull(ctx context.Context, dsn string, factory func(context.Context, string) (execer, error)) (*Exporter, error) {
	db, err := factory(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres exporter: %w", err)
	}
	return ExporterWithExecer(db), nil
}

func newPool(ctx context.Context, dsn string) (execer, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &poolAdapter{pool: pool}, nil
}

var _ pgxQuerier = (*pgxpool.Pool)(nil)

func ExporterWithExecer(db execer) *Exporter {
	return &Exporter{db: db}
}

func ensureSchema(ctx context.Context, db execer) error {
	const nodesTable = `CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY,
		run_id TEXT,
		type TEXT,
		name TEXT,
		status TEXT,
		tier TEXT,
		parent_id TEXT,
		agent_id TEXT,
		t_start TIMESTAMPTZ,
		t_end TIMESTAMPTZ,
		duration_ms BIGINT,
		tokens_in BIGINT,
		tokens_out BIGINT,
		cost_usd DOUBLE PRECISION,
		payload_hash TEXT,
		attrs JSONB,
		annotations JSONB,
		rev BIGINT
	)`
	const edgesTable = `CREATE TABLE IF NOT EXISTS edges (
		id TEXT PRIMARY KEY,
		run_id TEXT,
		type TEXT,
		src TEXT,
		dst TEXT,
		attrs JSONB,
		rev BIGINT
	)`
	if err := db.Exec(ctx, nodesTable); err != nil {
		return fmt.Errorf("postgres exporter: create nodes table: %w", err)
	}
	if err := db.Exec(ctx, edgesTable); err != nil {
		return fmt.Errorf("postgres exporter: create edges table: %w", err)
	}
	return nil
}

func ensureRunsSchema(ctx context.Context, db execer) error {
	const runsTable = `CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		session_ids JSONB,
		model_id TEXT,
		status TEXT,
		end_reason TEXT,
		started_at TIMESTAMPTZ,
		ended_at TIMESTAMPTZ,
		meta JSONB,
		repro JSONB,
		last_seq BIGINT
	)`
	if err := db.Exec(ctx, runsTable); err != nil {
		return fmt.Errorf("postgres exporter: create runs table: %w", err)
	}
	return nil
}

func (e *Exporter) ensure(ctx context.Context) error {
	if e.schemaReady {
		return nil
	}
	if err := ensureSchema(ctx, e.db); err != nil {
		return err
	}
	e.schemaReady = true
	return nil
}

func (e *Exporter) ensureRuns(ctx context.Context) error {
	if e.runsSchemaReady {
		return nil
	}
	if err := ensureRunsSchema(ctx, e.db); err != nil {
		return err
	}
	e.runsSchemaReady = true
	return nil
}

func (e *Exporter) Name() string { return "postgres" }

func (e *Exporter) Shutdown(_ context.Context) error {
	e.db.Close()
	return nil
}

func (e *Exporter) FlushRun(_ context.Context, _ string) error { return nil }

func (e *Exporter) ApplyDelta(ctx context.Context, d cdc.GraphDelta) error {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		if d.Node == nil {
			return nil
		}
		if err := e.ensure(ctx); err != nil {
			return err
		}
		return e.upsertNode(ctx, d.Node)
	case cdc.DeltaEdgeUpsert:
		if d.Edge == nil {
			return nil
		}
		if err := e.ensure(ctx); err != nil {
			return err
		}
		return e.upsertEdge(ctx, d.Edge)
	case cdc.DeltaNodeMerge:
		if d.Node == nil {
			return nil
		}
		if err := e.ensure(ctx); err != nil {
			return err
		}
		if err := e.db.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, d.OldID); err != nil {
			return fmt.Errorf("postgres exporter node_merge delete: %w", err)
		}
		return e.upsertNode(ctx, d.Node)
	case cdc.DeltaEdgeDelete:
		if d.Edge == nil {
			return nil
		}
		if err := e.ensure(ctx); err != nil {
			return err
		}
		if err := e.db.Exec(ctx, `DELETE FROM edges WHERE id = $1`, d.Edge.ID); err != nil {
			return fmt.Errorf("postgres exporter edge_delete: %w", err)
		}
		return nil
	case cdc.DeltaRunStarted, cdc.DeltaRunEnded:
		if d.Run == nil {
			return nil
		}
		if err := e.ensureRuns(ctx); err != nil {
			return err
		}
		return e.upsertRun(ctx, d.Run)
	default:
		return nil
	}
}

func (e *Exporter) SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error {
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}
	if err := e.ensure(ctx); err != nil {
		return err
	}
	tx, err := e.db.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("postgres exporter snapshot begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, n := range nodes {
		if err := upsertNodeTx(ctx, tx, n); err != nil {
			return err
		}
	}
	for _, edge := range edges {
		if err := upsertEdgeTx(ctx, tx, edge); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (e *Exporter) SnapshotRuns(ctx context.Context, runs []model.Run) error {
	if len(runs) == 0 {
		return nil
	}
	if err := e.ensureRuns(ctx); err != nil {
		return err
	}
	for i := range runs {
		if err := e.upsertRun(ctx, &runs[i]); err != nil {
			return err
		}
	}
	return nil
}

const nodeUpsertSQL = `INSERT INTO nodes (id, run_id, type, name, status, tier, parent_id, agent_id,
	t_start, t_end, duration_ms, tokens_in, tokens_out, cost_usd, payload_hash, attrs, annotations, rev)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (id) DO UPDATE SET
	run_id=$2, type=$3, name=$4, status=$5, tier=$6, parent_id=$7, agent_id=$8,
	t_start=$9, t_end=$10, duration_ms=$11, tokens_in=$12, tokens_out=$13, cost_usd=$14,
	payload_hash=$15, attrs=$16, annotations=$17, rev=$18
WHERE excluded.rev > nodes.rev`

const edgeUpsertSQL = `INSERT INTO edges (id, run_id, type, src, dst, attrs, rev)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (id) DO UPDATE SET
	run_id=$2, type=$3, src=$4, dst=$5, attrs=$6, rev=$7
WHERE excluded.rev > edges.rev`

const runUpsertSQL = `INSERT INTO runs (id, session_ids, model_id, status, end_reason, started_at, ended_at, meta, repro, last_seq)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (id) DO UPDATE SET
	session_ids=$2, model_id=$3, status=$4, end_reason=$5, started_at=$6, ended_at=$7, meta=$8, repro=$9, last_seq=$10
WHERE excluded.last_seq >= runs.last_seq`

func jsonMarshal(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func nodeArgs(n *model.Node) []any {
	return []any{
		n.ID, n.RunID, string(n.Type), n.Name, string(n.Status), n.Tier,
		n.ParentID, n.AgentID,
		n.TStart, n.TEnd, n.DurationMS, n.TokensIn, n.TokensOut, n.CostUSD,
		n.PayloadHash,
		jsonMarshal(n.Attrs),
		jsonMarshal(n.Annotations),
		n.Rev,
	}
}

func edgeArgs(edge *model.Edge) []any {
	return []any{
		edge.ID, edge.RunID, string(edge.Type), edge.Src, edge.Dst,
		jsonMarshal(edge.Attrs),
		edge.Rev,
	}
}

func runArgs(r *model.Run) []any {
	return []any{
		r.ID,
		jsonMarshal(r.SessionIDs),
		r.ModelID,
		string(r.Status),
		r.EndReason,
		r.StartedAt,
		r.EndedAt,
		jsonMarshal(r.Meta),
		jsonMarshal(r.Repro),
		int64(r.LastSeq),
	}
}

func (e *Exporter) upsertNode(ctx context.Context, n *model.Node) error {
	if err := e.db.Exec(ctx, nodeUpsertSQL, nodeArgs(n)...); err != nil {
		return fmt.Errorf("postgres exporter upsert node: %w", err)
	}
	return nil
}

func (e *Exporter) upsertEdge(ctx context.Context, edge *model.Edge) error {
	if err := e.db.Exec(ctx, edgeUpsertSQL, edgeArgs(edge)...); err != nil {
		return fmt.Errorf("postgres exporter upsert edge: %w", err)
	}
	return nil
}

func (e *Exporter) upsertRun(ctx context.Context, r *model.Run) error {
	if err := e.db.Exec(ctx, runUpsertSQL, runArgs(r)...); err != nil {
		return fmt.Errorf("postgres exporter upsert run: %w", err)
	}
	return nil
}

func upsertNodeTx(ctx context.Context, tx txExecer, n *model.Node) error {
	if err := tx.Exec(ctx, nodeUpsertSQL, nodeArgs(n)...); err != nil {
		return fmt.Errorf("postgres exporter snapshot upsert node: %w", err)
	}
	return nil
}

func upsertEdgeTx(ctx context.Context, tx txExecer, edge *model.Edge) error {
	if err := tx.Exec(ctx, edgeUpsertSQL, edgeArgs(edge)...); err != nil {
		return fmt.Errorf("postgres exporter snapshot upsert edge: %w", err)
	}
	return nil
}
