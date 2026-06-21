package otlp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type spanExporter interface {
	ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
	Shutdown(ctx context.Context) error
}

type nodeState struct {
	node *model.Node
	rev  uint64
}

type RunFilter struct {
	RunID string
}

type Exporter struct {
	client  spanExporter
	runs    map[string]map[string]*nodeState
	parents map[string]string
	edgeRev map[string]uint64
	filter  RunFilter
}

func New(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string) (*Exporter, error) {
	return newFull(ctx, endpoint, daemonGRPCAddr, daemonHTTPAddr, newClient)
}

func newFull(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string, factory func(context.Context, string) (spanExporter, error)) (*Exporter, error) {
	norm := normalizeAddr(endpoint)
	if norm == normalizeAddr(daemonGRPCAddr) || norm == normalizeAddr(daemonHTTPAddr) {
		return nil, fmt.Errorf("otlp exporter: endpoint %q targets the daemon's own receiver — self-loop refused", endpoint)
	}
	client, err := factory(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return newWithExporter(client), nil
}

func newWithExporter(exp spanExporter) *Exporter {
	return &Exporter{
		client:  exp,
		runs:    map[string]map[string]*nodeState{},
		parents: map[string]string{},
		edgeRev: map[string]uint64{},
	}
}

func (e *Exporter) Name() string { return "otlp" }

func newClient(ctx context.Context, endpoint string) (spanExporter, error) {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	}
	host := strings.TrimPrefix(endpoint, "grpc://")
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(host), otlptracegrpc.WithInsecure())
}

func normalizeAddr(addr string) string {
	a := addr
	for _, p := range []string{"grpc://", "http://", "https://"} {
		a = strings.TrimPrefix(a, p)
	}
	if i := strings.IndexByte(a, '/'); i >= 0 {
		a = a[:i]
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil {
		return a
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "":
		host = "loopback"
	}
	return host + ":" + port
}

func spanID(nodeID string) trace.SpanID {
	h := sha256.Sum256([]byte(nodeID))
	var id trace.SpanID
	copy(id[:], h[:8])
	return id
}

func traceID(runID string) trace.TraceID {
	h := sha256.Sum256([]byte(runID))
	var id trace.TraceID
	copy(id[:], h[:16])
	return id
}

func openInferenceKind(t model.NodeType) string {
	switch t {
	case model.NodeSubagent:
		return "AGENT"
	case model.NodeToolCall, model.NodeMCPCall:
		return "TOOL"
	case model.NodeAssistantTurn:
		return "LLM"
	default:
		return "CHAIN"
	}
}

func spanStatus(s model.Status) sdktrace.Status {
	switch s {
	case model.StatusError, model.StatusBlocked:
		return sdktrace.Status{Code: codes.Error}
	case model.StatusOK:
		return sdktrace.Status{Code: codes.Ok}
	default:
		return sdktrace.Status{Code: codes.Unset}
	}
}

func (e *Exporter) ApplyDelta(ctx context.Context, d cdc.GraphDelta) error {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		e.upsertNode(d)
		return nil
	case cdc.DeltaEdgeUpsert:
		if d.Edge != nil && d.Edge.Type == model.EdgeParentChild && d.Rev >= e.edgeRev[d.Edge.Dst] {
			e.edgeRev[d.Edge.Dst] = d.Rev
			e.parents[d.Edge.Dst] = d.Edge.Src
		}
		return nil
	case cdc.DeltaSessionEnded, cdc.DeltaRunEnded:
		return e.flushRun(ctx, d.RunID)
	default:
		return nil
	}
}

func (e *Exporter) upsertNode(d cdc.GraphDelta) {
	if d.Node == nil {
		return
	}
	run, ok := e.runs[d.RunID]
	if !ok {
		run = map[string]*nodeState{}
		e.runs[d.RunID] = run
	}
	st, ok := run[d.Node.ID]
	if ok && d.Rev <= st.rev {
		return
	}
	if !ok {
		st = &nodeState{}
		run[d.Node.ID] = st
	}
	st.node = d.Node
	st.rev = d.Rev
}

func (e *Exporter) flushRun(ctx context.Context, runID string) error {
	run, ok := e.runs[runID]
	if !ok || len(run) == 0 {
		return nil
	}
	spans := make([]sdktrace.ReadOnlySpan, 0, len(run))
	for id, st := range run {
		spans = append(spans, e.nodeToSpan(st.node, e.parents[id]))
	}
	delete(e.runs, runID)
	return e.client.ExportSpans(ctx, spans)
}

func (e *Exporter) Snapshot(_ context.Context, filter RunFilter) error {
	e.filter = filter
	return nil
}

func (e *Exporter) SnapshotState(_ context.Context, nodes []*model.Node, edges []*model.Edge) error {
	for _, edge := range edges {
		if edge.Type != model.EdgeParentChild {
			continue
		}
		if edge.Rev >= e.edgeRev[edge.Dst] {
			e.edgeRev[edge.Dst] = edge.Rev
			e.parents[edge.Dst] = edge.Src
		}
	}
	for _, n := range nodes {
		if e.filter.RunID != "" && n.RunID == e.filter.RunID {
			continue
		}
		run, ok := e.runs[n.RunID]
		if !ok {
			run = map[string]*nodeState{}
			e.runs[n.RunID] = run
		}
		st, ok := run[n.ID]
		if ok && n.Rev <= st.rev {
			continue
		}
		if !ok {
			st = &nodeState{}
			run[n.ID] = st
		}
		st.node = n
		st.rev = n.Rev
	}
	return nil
}

func (e *Exporter) nodeToSpan(n *model.Node, parentNodeID string) sdktrace.ReadOnlySpan {
	attrs := []attribute.KeyValue{
		attribute.String("openinference.span.kind", openInferenceKind(n.Type)),
		attribute.String("gen_ai.provider.name", "anthropic"),
	}
	if n.TokensIn != nil {
		attrs = append(attrs,
			attribute.Int64("llm.token_count.prompt", *n.TokensIn),
			attribute.Int64("gen_ai.usage.input_tokens", *n.TokensIn),
		)
	}
	if n.TokensOut != nil {
		attrs = append(attrs,
			attribute.Int64("llm.token_count.completion", *n.TokensOut),
			attribute.Int64("gen_ai.usage.output_tokens", *n.TokensOut),
		)
	}
	if n.CostUSD != nil {
		attrs = append(attrs, attribute.String("llm.cost.total", strconv.FormatFloat(*n.CostUSD, 'g', -1, 64)))
	}
	var parent trace.SpanContext
	if parentNodeID != "" {
		parent = trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID(n.RunID),
			SpanID:     spanID(parentNodeID),
			TraceFlags: trace.FlagsSampled,
		})
	}
	var start, end time.Time
	if n.TStart != nil {
		start = *n.TStart
	}
	if n.TEnd != nil {
		end = *n.TEnd
	}
	stub := tracetest.SpanStub{
		Name: n.Name,
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID(n.RunID),
			SpanID:     spanID(n.ID),
			TraceFlags: trace.FlagsSampled,
		}),
		Parent:     parent,
		SpanKind:   trace.SpanKindInternal,
		StartTime:  start,
		EndTime:    end,
		Attributes: attrs,
		Status:     spanStatus(n.Status),
	}
	return stub.Snapshot()
}
