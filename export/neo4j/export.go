package neo4j

import (
	"context"
	"encoding/json"
	"fmt"

	neo4japi "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/model"
)

var (
	_ exportiface.Exporter = (*Exporter)(nil)
	_ neo4jDriver          = (*driverAdapter)(nil)
)

type neo4jSession interface {
	Run(ctx context.Context, cypher string, params map[string]any, configurers ...func(*neo4japi.TransactionConfig)) (neo4japi.ResultWithContext, error)
	Close(ctx context.Context) error
}

type neo4jDriver interface {
	NewSession(ctx context.Context, config neo4japi.SessionConfig) neo4jSession
	Close(ctx context.Context) error
}

type driverAdapter struct {
	d neo4japi.DriverWithContext
}

func (a *driverAdapter) NewSession(ctx context.Context, config neo4japi.SessionConfig) neo4jSession {
	return a.d.NewSession(ctx, config)
}

func (a *driverAdapter) Close(ctx context.Context) error {
	return a.d.Close(ctx)
}

type runner interface {
	Run(ctx context.Context, cypher string, params map[string]any) error
	Close(ctx context.Context) error
}

type sessionRunner struct {
	d neo4jDriver
}

func (s *sessionRunner) Run(ctx context.Context, cypher string, params map[string]any) error {
	sess := s.d.NewSession(ctx, neo4japi.SessionConfig{})
	defer func() { _ = sess.Close(ctx) }()
	_, err := sess.Run(ctx, cypher, params)
	return err
}

func (s *sessionRunner) Close(ctx context.Context) error {
	return s.d.Close(ctx)
}

type Exporter struct {
	r runner
}

func New(ctx context.Context, uri, user, password string) (*Exporter, error) {
	return newFull(ctx, uri, user, password, newDriver)
}

func newFull(ctx context.Context, uri, user, password string, factory func(context.Context, string, string, string) (runner, error)) (*Exporter, error) {
	r, err := factory(ctx, uri, user, password)
	if err != nil {
		return nil, fmt.Errorf("neo4j exporter: %w", err)
	}
	return ExporterWithRunner(r), nil
}

func newDriver(_ context.Context, uri, user, password string) (runner, error) {
	drv, err := neo4japi.NewDriverWithContext(uri, neo4japi.BasicAuth(user, password, ""))
	if err != nil {
		return nil, err
	}
	return &sessionRunner{d: &driverAdapter{d: drv}}, nil
}

func ExporterWithRunner(r runner) *Exporter {
	return &Exporter{r: r}
}

func nodeLabel(t model.NodeType) string {
	switch t {
	case model.NodeSession:
		return "Session"
	case model.NodeUserPrompt:
		return "UserPrompt"
	case model.NodeAssistantTurn:
		return "AssistantTurn"
	case model.NodeToolCall:
		return "ToolCall"
	case model.NodeSubagent:
		return "Subagent"
	case model.NodeMCPCall:
		return "McpCall"
	case model.NodeHookEvent:
		return "HookEvent"
	default:
		return "Marker"
	}
}

func edgeRelType(t model.EdgeType) string {
	switch t {
	case model.EdgeParentChild:
		return "PARENT_OF"
	case model.EdgeSequence:
		return "NEXT"
	case model.EdgeMarkerSpan:
		return "IN_PHASE"
	default:
		return "DATA_DEP"
	}
}

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

func nodeProps(n *model.Node) map[string]any {
	return map[string]any{
		"run_id":       n.RunID,
		"type":         string(n.Type),
		"name":         n.Name,
		"status":       string(n.Status),
		"tier":         n.Tier,
		"parent_id":    n.ParentID,
		"agent_id":     n.AgentID,
		"payload_hash": n.PayloadHash,
		"attrs":        jsonMarshal(n.Attrs),
		"annotations":  jsonMarshal(n.Annotations),
		"rev":          int64(n.Rev),
	}
}

func edgeProps(edge *model.Edge) map[string]any {
	return map[string]any{
		"run_id": edge.RunID,
		"type":   string(edge.Type),
		"src":    edge.Src,
		"dst":    edge.Dst,
		"attrs":  jsonMarshal(edge.Attrs),
		"rev":    int64(edge.Rev),
	}
}

func (e *Exporter) Name() string { return "neo4j" }

func (e *Exporter) Shutdown(ctx context.Context) error {
	return e.r.Close(ctx)
}

func (e *Exporter) FlushRun(_ context.Context, _ string) error { return nil }

func (e *Exporter) upsertNode(ctx context.Context, n *model.Node) error {
	label := nodeLabel(n.Type)
	cypher := fmt.Sprintf(
		`MERGE (n:%s {id:$id}) WITH n WHERE coalesce(n.rev,-1) < $rev SET n += $props`,
		label,
	)
	return e.r.Run(ctx, cypher, map[string]any{
		"id":    n.ID,
		"rev":   int64(n.Rev),
		"props": nodeProps(n),
	})
}

func (e *Exporter) upsertEdge(ctx context.Context, edge *model.Edge) error {
	rel := edgeRelType(edge.Type)
	cypher := fmt.Sprintf(
		`MATCH (src {id:$src}) MATCH (dst {id:$dst}) MERGE (src)-[r:%s {id:$id}]->(dst) WITH r WHERE coalesce(r.rev,-1) < $rev SET r += $props`,
		rel,
	)
	return e.r.Run(ctx, cypher, map[string]any{
		"src":   edge.Src,
		"dst":   edge.Dst,
		"id":    edge.ID,
		"rev":   int64(edge.Rev),
		"props": edgeProps(edge),
	})
}

func (e *Exporter) ApplyDelta(ctx context.Context, d cdc.GraphDelta) error {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		if d.Node == nil {
			return nil
		}
		return e.upsertNode(ctx, d.Node)
	case cdc.DeltaEdgeUpsert:
		if d.Edge == nil {
			return nil
		}
		return e.upsertEdge(ctx, d.Edge)
	case cdc.DeltaNodeMerge:
		if d.Node == nil {
			return nil
		}
		if err := e.r.Run(ctx, `MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": d.OldID}); err != nil {
			return fmt.Errorf("neo4j exporter node_merge delete: %w", err)
		}
		return e.upsertNode(ctx, d.Node)
	case cdc.DeltaEdgeDelete:
		if d.Edge == nil {
			return nil
		}
		if err := e.r.Run(ctx, `MATCH ()-[r {id:$id}]->() DELETE r`, map[string]any{"id": d.Edge.ID}); err != nil {
			return fmt.Errorf("neo4j exporter edge_delete: %w", err)
		}
		return nil
	default:
		return nil
	}
}

func (e *Exporter) SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error {
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}
	for _, n := range nodes {
		label := nodeLabel(n.Type)
		cypher := fmt.Sprintf(
			`MERGE (n:%s {id:$id}) WITH n WHERE coalesce(n.rev,-1) < $rev SET n += $props`,
			label,
		)
		if err := e.r.Run(ctx, cypher, map[string]any{
			"id":    n.ID,
			"rev":   int64(n.Rev),
			"props": nodeProps(n),
		}); err != nil {
			return fmt.Errorf("neo4j exporter snapshot upsert node: %w", err)
		}
	}
	for _, edge := range edges {
		rel := edgeRelType(edge.Type)
		cypher := fmt.Sprintf(
			`MATCH (src {id:$src}) MATCH (dst {id:$dst}) MERGE (src)-[r:%s {id:$id}]->(dst) WITH r WHERE coalesce(r.rev,-1) < $rev SET r += $props`,
			rel,
		)
		if err := e.r.Run(ctx, cypher, map[string]any{
			"src":   edge.Src,
			"dst":   edge.Dst,
			"id":    edge.ID,
			"rev":   int64(edge.Rev),
			"props": edgeProps(edge),
		}); err != nil {
			return fmt.Errorf("neo4j exporter snapshot upsert edge: %w", err)
		}
	}
	return nil
}
