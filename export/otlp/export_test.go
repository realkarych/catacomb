package otlp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/realkarych/catacomb/cdc"
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

func TestExporterShutdownDelegatesToClient(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.Shutdown(context.Background()))
	assert.True(t, rec.shutdown)
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

func TestApplyDeltaBuffersUntilLifecycleFlush(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	assert.Empty(t, rec.batches)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, spanID("n1"), rec.batches[0][0].SpanContext().SpanID())
}

func TestApplyDeltaRevGuardDropsStale(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	apply := func(rev uint64, st model.Status) {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: rev, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: st}}))
	}
	apply(5, model.StatusError)
	apply(3, model.StatusRunning)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 9, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, codes.Error, rec.batches[0][0].Status().Code)
}

func TestApplyDeltaNodeStatusIsFullStateUpsert(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: 1, RunID: "r1", Node: &model.Node{ID: "only", RunID: "r1", Type: model.NodeAssistantTurn, Name: "t", Status: model.StatusOK, TokensIn: i64(7)}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	m := attrMap(rec.batches[0][0])
	assert.Equal(t, "7", m["gen_ai.usage.input_tokens"])
}

func TestApplyDeltaProvisionalHeldThenFlushedOnClose(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: 4, RunID: "r1", Node: &model.Node{ID: "c1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusCancelled}}))
	assert.Empty(t, rec.batches)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 6, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
}

func TestApplyDeltaEdgeUpsertLinksParent(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "p", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 2, RunID: "r1", Node: &model.Node{ID: "ch", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 2, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "ch", Rev: 2}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 3, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	byID := map[trace.SpanID]sdktrace.ReadOnlySpan{}
	for _, ro := range rec.batches[0] {
		byID[ro.SpanContext().SpanID()] = ro
	}
	child := byID[spanID("ch")]
	require.NotNil(t, child)
	assert.Equal(t, spanID("p"), child.Parent().SpanID())
}

func TestApplyDeltaEdgeRevGuardKeepsNewerParent(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 9, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "newp", Dst: "ch", Rev: 9}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 4, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "oldp", Dst: "ch", Rev: 4}}))
	assert.Equal(t, "newp", e.parents["ch"])
}

func TestApplyDeltaIgnoresNonParentEdge(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1", Edge: &model.Edge{ID: "e2", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}}))
	_, ok := e.parents["b"]
	assert.False(t, ok)
}

func TestApplyDeltaLifecycleNoopsAndUnknownKind(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	for _, k := range []cdc.GraphDeltaKind{cdc.DeltaRunStarted, cdc.DeltaNodeMerge, cdc.DeltaEdgeDelete} {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: k, Rev: 1, RunID: "r1"}))
	}
	assert.Empty(t, rec.batches)
}

func TestFlushUnknownRunIsNoop(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 1, RunID: "ghost"}))
	assert.Empty(t, rec.batches)
}

func TestFlushPropagatesExportError(t *testing.T) {
	e := newWithExporter(&errExporter{})
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"})
	require.Error(t, err)
}

func TestStaleSpanAfterCloseReEmittedOnNextClose(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: 3, RunID: "r1", Node: &model.Node{ID: "late", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusError}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 4, RunID: "r1"}))
	require.Len(t, rec.batches, 2)
	assert.Equal(t, spanID("late"), rec.batches[1][0].SpanContext().SpanID())
}

func TestSnapshotMapsAndFlushesTerminalRuns(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	nodes := []*model.Node{
		{ID: "p", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK},
		{ID: "ch", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK},
	}
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "ch", Rev: 1}}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edges))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 9, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 2)
}

func TestSnapshotFilterByRunID(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	nodes := []*model.Node{
		{ID: "a", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK},
		{ID: "b", RunID: "r2", Type: model.NodeToolCall, Status: model.StatusOK},
	}
	require.NoError(t, e.Snapshot(context.Background(), RunFilter{RunID: "r1"}))
	_ = nodes
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edgesNil()))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 5, RunID: "r2"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, spanID("b"), rec.batches[0][0].SpanContext().SpanID())
}

func TestApplyDeltaNodeUpsertNilNodeIsNoop(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1"}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	assert.Empty(t, rec.batches)
}

func TestSnapshotStateSkipsNonParentEdge(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}}
	require.NoError(t, e.SnapshotState(context.Background(), nil, edges))
	_, ok := e.parents["b"]
	assert.False(t, ok)
}

func TestSnapshotStateNodeRevGuardDropsStale(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	nodes := []*model.Node{
		{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 5},
		{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusError, Rev: 3},
	}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, nil))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 9, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, codes.Ok, rec.batches[0][0].Status().Code)
}

func TestFlushRunPublicMethodExportsAndPrunesMaps(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "root", Dst: "n1", Rev: 1}}))
	require.NoError(t, e.FlushRun(context.Background(), "r1"))
	require.Len(t, rec.batches, 1)
	_, hasParent := e.parents["n1"]
	assert.False(t, hasParent)
	_, hasRev := e.edgeRev["n1"]
	assert.False(t, hasRev)
	_, hasRun := e.runs["r1"]
	assert.False(t, hasRun)
}

func TestFlushRunUnknownRunIsNoop(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.FlushRun(context.Background(), "ghost"))
	assert.Empty(t, rec.batches)
}

func TestFlushRunEmptyRunIsNoop(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	e.runs["empty"] = map[string]*nodeState{}
	require.NoError(t, e.FlushRun(context.Background(), "empty"))
	assert.Empty(t, rec.batches)
}

func TestNodeToSpanGraphAndSessionAttrs(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "child", RunID: "run-hash", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK}
	m := attrMap(e.nodeToSpan(n, "parent"))
	assert.Equal(t, "child", m["graph.node.id"])
	assert.Equal(t, "parent", m["graph.node.parent_id"])
	assert.Equal(t, "Bash", m["graph.node.name"])
	assert.Equal(t, "run-hash", m["session.id"])
}

func TestNodeToSpanRootHasNoParentAttr(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "root", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK}
	m := attrMap(e.nodeToSpan(n, ""))
	_, ok := m["graph.node.parent_id"]
	assert.False(t, ok)
}

func TestNodeToSpanLLMModelName(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, Attrs: map[string]any{"model": "claude-opus-4-8"}}
	m := attrMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "claude-opus-4-8", m["llm.model_name"])
	assert.Equal(t, "claude-opus-4-8", m["gen_ai.request.model"])
}

func TestNodeToSpanModelAbsentWhenNoAttr(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	m := attrMap(e.nodeToSpan(n, ""))
	_, ok := m["llm.model_name"]
	assert.False(t, ok)
}

func TestNodeToSpanToolNameAndArguments(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{
		ID: "n1", RunID: "r1", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK,
		Payload: &model.Payload{Input: json.RawMessage(`{"command":"ls"}`), Output: json.RawMessage(`"ok"`)},
	}
	m := attrMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "Bash", m["tool.name"])
	assert.Equal(t, "Bash", m["tool_call.function.name"])
	assert.Equal(t, `{"command":"ls"}`, m["tool_call.function.arguments"])
	assert.Equal(t, `{"command":"ls"}`, m["input.value"])
	assert.Equal(t, "application/json", m["input.mime_type"])
	assert.Equal(t, `"ok"`, m["output.value"])
	assert.Equal(t, "application/json", m["output.mime_type"])
}

func TestNodeToSpanRedactsInputValue(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{
		ID: "n1", RunID: "r1", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK,
		Payload: &model.Payload{Input: json.RawMessage(`{"command":"git clone ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij foo"}`)},
	}
	m := attrMap(e.nodeToSpan(n, ""))
	assert.NotContains(t, m["input.value"], "ghp_")
	assert.Contains(t, m["input.value"], "redacted")
}

func TestNodeToSpanCapsLargeValue(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	large := json.RawMessage("[" + strings.Repeat("1,", maxIOValueBytes) + "0]")
	n := &model.Node{
		ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK,
		Payload: &model.Payload{Output: large},
	}
	m := attrMap(e.nodeToSpan(n, ""))
	assert.Equal(t, maxIOValueBytes, len(m["output.value"]))
}

func TestCapBytesRuneSafe(t *testing.T) {
	prefix := strings.Repeat("a", maxIOValueBytes-1)
	s := prefix + "世"
	result := capBytes(s)
	assert.LessOrEqual(t, len(result), maxIOValueBytes)
	assert.True(t, utf8.ValidString(result))
	assert.Equal(t, prefix, result)
}

func TestNodeToSpanModelAbsentWhenAttrWrongType(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, Attrs: map[string]any{"model": 42}}
	m := attrMap(e.nodeToSpan(n, ""))
	_, ok := m["llm.model_name"]
	assert.False(t, ok)
}

func TestNodeToSpanToolNameNotSetForNonToolKind(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSubagent, Name: "researcher", Status: model.StatusOK}
	m := attrMap(e.nodeToSpan(n, ""))
	_, ok := m["tool.name"]
	assert.False(t, ok)
}

func TestNodeToSpanNoPayloadOmitsIO(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}
	m := attrMap(e.nodeToSpan(n, ""))
	_, hasIn := m["input.value"]
	_, hasOut := m["output.value"]
	assert.False(t, hasIn)
	assert.False(t, hasOut)
}

func TestNodeToSpanEmptyOutputOmitsOutputValue(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{
		ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK,
		Payload: &model.Payload{Input: json.RawMessage(`{"cmd":"ls"}`)},
	}
	m := attrMap(e.nodeToSpan(n, ""))
	_, ok := m["output.value"]
	assert.False(t, ok)
}

func resourceMap(ro sdktrace.ReadOnlySpan) map[string]string {
	m := map[string]string{}
	for _, kv := range ro.Resource().Attributes() {
		m[string(kv.Key)] = kv.Value.AsString()
	}
	return m
}

func TestNodeToSpanDefaultProjectResource(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	rm := resourceMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "catacomb", rm["openinference.project.name"])
}

func TestSetProjectOverridesResource(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	e.SetProject("my-project")
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}
	rm := resourceMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "my-project", rm["openinference.project.name"])
}

func TestSetProjectEmptyKeepsDefault(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	e.SetProject("")
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeMarker, Status: model.StatusOK}
	rm := resourceMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "catacomb", rm["openinference.project.name"])
}

func edgesNil() []*model.Edge { return nil }

type errExporter struct{}

func (errExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return assert.AnError
}
func (errExporter) Shutdown(context.Context) error { return nil }
