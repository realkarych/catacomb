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

	"github.com/realkarych/catacomb/model"
)

type spanExporter interface {
	ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
	Shutdown(ctx context.Context) error
}

type Exporter struct {
	client spanExporter
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
	return &Exporter{client: exp}
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
