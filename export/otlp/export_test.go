package otlp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/realkarych/catacomb/model"
)

type recordExporter struct {
	batches  [][]sdktrace.ReadOnlySpan
	shutdown bool
}

func (r *recordExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.batches = append(r.batches, spans)
	return nil
}

func (r *recordExporter) Shutdown(_ context.Context) error {
	r.shutdown = true
	return nil
}

func TestNewRejectsSelfLoopGRPCAddr(t *testing.T) {
	_, err := New(context.Background(), "grpc://127.0.0.1:4317", "localhost:4317", "127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewRejectsSelfLoopHTTPAddr(t *testing.T) {
	_, err := New(context.Background(), "http://localhost:8080", "127.0.0.1:4317", "127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewGRPCSchemeConstructsClient(t *testing.T) {
	e, err := New(context.Background(), "grpc://collector.example:4317", "127.0.0.1:4317", "127.0.0.1:8080")
	require.NoError(t, err)
	assert.Equal(t, "otlp", e.Name())
	assert.NotNil(t, e.client)
}

func TestNewBareHostPortUsesGRPC(t *testing.T) {
	e, err := New(context.Background(), "collector.example:4317", "127.0.0.1:4318", "127.0.0.1:8081")
	require.NoError(t, err)
	assert.NotNil(t, e.client)
}

func TestNewHTTPSchemeConstructsClient(t *testing.T) {
	e, err := New(context.Background(), "https://collector.example:443", "127.0.0.1:4319", "127.0.0.1:8082")
	require.NoError(t, err)
	assert.NotNil(t, e.client)
}

func TestNewWithExporterUsesInjectedSeam(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	assert.Equal(t, "otlp", e.Name())
	assert.Same(t, spanExporter(rec), e.client)
}

func TestNormalizeAddrCanonicalisesLoopback(t *testing.T) {
	assert.Equal(t, normalizeAddr("grpc://localhost:4317"), normalizeAddr("127.0.0.1:4317"))
	assert.Equal(t, normalizeAddr("http://[::1]:8080"), normalizeAddr("localhost:8080"))
	assert.NotEqual(t, normalizeAddr("collector:4317"), normalizeAddr("127.0.0.1:4317"))
}

func TestNormalizeAddrNonLoopback(t *testing.T) {
	assert.Equal(t, "collector.example:4317", normalizeAddr("grpc://collector.example:4317"))
}

func TestNormalizeAddrBadHostPort(t *testing.T) {
	assert.Equal(t, "noport", normalizeAddr("noport"))
}

func TestRecordExporterRecordsShutdown(t *testing.T) {
	r := &recordExporter{}
	err := r.Shutdown(context.Background())
	require.NoError(t, err)
	assert.True(t, r.shutdown)
}

func TestRecordExporterRecordsBatches(t *testing.T) {
	r := &recordExporter{}
	err := r.ExportSpans(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, r.batches, 1)
}

func TestNormalizeAddrStripsPathAfterSlash(t *testing.T) {
	assert.Equal(t, normalizeAddr("collector.example:4317"), normalizeAddr("collector.example:4317/v1/traces"))
}

func TestNewLoopbackAliasMatchOnHTTP(t *testing.T) {
	_, err := New(context.Background(), "http://[::1]:8080", "127.0.0.1:4317", "localhost:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewFullPropagatesFactoryError(t *testing.T) {
	factoryErr := errors.New("factory failure")
	_, err := newFull(context.Background(), "grpc://collector.example:4317", "localhost:4317", "localhost:8080",
		func(_ context.Context, _ string) (spanExporter, error) {
			return nil, factoryErr
		},
	)
	require.ErrorIs(t, err, factoryErr)
}

func TestNormalizeAddrIPv6BracketedLoopback(t *testing.T) {
	assert.Equal(t, "loopback:8080", normalizeAddr("[::1]:8080"))
}

func attrMap(ro sdktrace.ReadOnlySpan) map[string]string {
	m := map[string]string{}
	for _, kv := range ro.Attributes() {
		m[string(kv.Key)] = kv.Value.String()
	}
	return m
}

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func TestSpanIDDeterministicAndParentMatches(t *testing.T) {
	a := spanID("node-A")
	b := spanID("node-A")
	assert.Equal(t, a, b)
	assert.NotEqual(t, spanID("node-A"), spanID("node-B"))
	assert.True(t, a.IsValid())
}

func TestTraceIDGroupsByRun(t *testing.T) {
	assert.Equal(t, traceID("run-1"), traceID("run-1"))
	assert.NotEqual(t, traceID("run-1"), traceID("run-2"))
	assert.True(t, traceID("run-1").IsValid())
}

func TestNodeToSpanLLMKindAndTokensAndCost(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeAssistantTurn, Name: "turn", Status: model.StatusOK, TokensIn: i64(11), TokensOut: i64(22), CostUSD: f64(0.5)}
	ro := e.nodeToSpan(n, "")
	m := attrMap(ro)
	assert.Equal(t, "LLM", m["openinference.span.kind"])
	assert.Equal(t, "anthropic", m["gen_ai.provider.name"])
	assert.Equal(t, "11", m["llm.token_count.prompt"])
	assert.Equal(t, "11", m["gen_ai.usage.input_tokens"])
	assert.Equal(t, "22", m["llm.token_count.completion"])
	assert.Equal(t, "22", m["gen_ai.usage.output_tokens"])
	assert.Equal(t, "0.5", m["llm.cost.total"])
	assert.Equal(t, traceID("r1"), ro.SpanContext().TraceID())
	assert.Equal(t, spanID("n1"), ro.SpanContext().SpanID())
	assert.False(t, ro.Parent().HasSpanID())
}

func TestNodeToSpanOmitsAbsentTokensAndCost(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusRunning}
	m := attrMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "TOOL", m["openinference.span.kind"])
	_, hasIn := m["llm.token_count.prompt"]
	_, hasOut := m["gen_ai.usage.output_tokens"]
	_, hasCost := m["llm.cost.total"]
	assert.False(t, hasIn)
	assert.False(t, hasOut)
	assert.False(t, hasCost)
}

func TestNodeToSpanParentLinkage(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "child", RunID: "r1", Type: model.NodeMCPCall, Status: model.StatusOK}
	ro := e.nodeToSpan(n, "parent")
	assert.True(t, ro.Parent().HasSpanID())
	assert.Equal(t, spanID("parent"), ro.Parent().SpanID())
	assert.Equal(t, traceID("r1"), ro.Parent().TraceID())
}

func TestNodeToSpanTiming(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	start := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	n := &model.Node{ID: "n3", RunID: "r1", Type: model.NodeSubagent, Status: model.StatusOK, TStart: &start, TEnd: &end}
	ro := e.nodeToSpan(n, "")
	assert.Equal(t, start, ro.StartTime())
	assert.Equal(t, end, ro.EndTime())
}

func TestNodeToSpanUnfinalizedTimingIsZero(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n4", RunID: "r1", Type: model.NodeUserPrompt, Status: model.StatusRunning}
	ro := e.nodeToSpan(n, "")
	assert.True(t, ro.EndTime().IsZero())
}

func TestOpenInferenceKindTable(t *testing.T) {
	assert.Equal(t, "AGENT", openInferenceKind(model.NodeSubagent))
	assert.Equal(t, "TOOL", openInferenceKind(model.NodeToolCall))
	assert.Equal(t, "TOOL", openInferenceKind(model.NodeMCPCall))
	assert.Equal(t, "LLM", openInferenceKind(model.NodeAssistantTurn))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeMarker))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeSession))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeUserPrompt))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeHookEvent))
}

func TestSpanStatusMapping(t *testing.T) {
	assert.Equal(t, codes.Error, spanStatus(model.StatusError).Code)
	assert.Equal(t, codes.Error, spanStatus(model.StatusBlocked).Code)
	assert.Equal(t, codes.Ok, spanStatus(model.StatusOK).Code)
	assert.Equal(t, codes.Unset, spanStatus(model.StatusRunning).Code)
}
