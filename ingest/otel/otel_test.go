package otel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/realkarych/catacomb/model"
)

func strAttr(key, val string) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: val}},
	}
}

func intAttr(key string, val int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: val}},
	}
}

func spanID(b byte) []byte {
	id := make([]byte, 8)
	id[7] = b
	return id
}

func parentSpanID(b byte) []byte {
	id := make([]byte, 8)
	id[7] = b
	return id
}

func makeReq(resource *resourcev1.Resource, spans ...*tracev1.Span) *collectorv1.ExportTraceServiceRequest {
	return &collectorv1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: resource,
				ScopeSpans: []*tracev1.ScopeSpans{
					{Spans: spans},
				},
			},
		},
	}
}

func seq() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func fixedNow(t time.Time) {
	nowFn = func() time.Time { return t }
}

func TestParseEmptyRequest(t *testing.T) {
	obs, err := Parse(&collectorv1.ExportTraceServiceRequest{}, "exec1", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseLLMSpan(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fixedNow(now)

	start := uint64(1000000000)
	span := &tracev1.Span{
		SpanId:            spanID(1),
		ParentSpanId:      parentSpanID(0),
		Name:              "claude_code.llm_request",
		StartTimeUnixNano: start,
		Attributes: []*commonv1.KeyValue{
			strAttr("message.id", "msg_abc"),
			intAttr("gen_ai.usage.input_tokens", 100),
			intAttr("gen_ai.usage.output_tokens", 50),
			strAttr("gen_ai.request.model", "claude-opus-4-5"),
		},
	}
	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			strAttr("session.id", "sess_001"),
		},
	}
	req := makeReq(resource, span)

	obs, err := Parse(req, "exec1", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "assistant_turn", o.Kind)
	assert.Equal(t, model.SourceOTel, o.Source)
	assert.Equal(t, "exec1", o.ExecutionID)
	assert.Equal(t, "sess_001", o.RunID)
	assert.Equal(t, "sess_001", o.Correlation.SessionID)
	assert.Equal(t, "msg_abc", o.Correlation.MessageID)
	assert.Equal(t, int64(100), o.Attrs["tokens_in"])
	assert.Equal(t, int64(50), o.Attrs["tokens_out"])
	assert.Equal(t, "claude-opus-4-5", o.Attrs["model"])
	assert.Equal(t, now.UTC(), o.ObservedAt)
	assert.Equal(t, uint64(1), o.Seq)
	assert.NotEmpty(t, o.ObsID)
	assert.Equal(t, "0000000000000001", o.Correlation.SpanID)

	expectedEventTime := time.Unix(0, int64(start)).UTC()
	assert.Equal(t, expectedEventTime, o.EventTime)
}

func TestParseToolSpan(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(2),
		Name:   "claude_code.tool",
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
		Attributes: []*commonv1.KeyValue{
			strAttr("tool_use_id", "toolu_xyz"),
			strAttr("tool_name", "Read"),
		},
	}
	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			strAttr("session.id", "sess_002"),
		},
	}
	req := makeReq(resource, span)

	obs, err := Parse(req, "exec2", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "assistant_tool_use", o.Kind)
	assert.Equal(t, "toolu_xyz", o.Correlation.ToolUseID)
	assert.Equal(t, "Read", o.Attrs["name"])
	assert.Equal(t, "ok", o.Attrs["status"])
}

func TestParseToolSpanGenAIToolCallID(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(3),
		Name:   "claude_code.tool",
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR},
		Attributes: []*commonv1.KeyValue{
			strAttr("gen_ai.tool.call.id", "toolu_gen"),
			strAttr("gen_ai.tool.name", "Write"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec3", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "assistant_tool_use", o.Kind)
	assert.Equal(t, "toolu_gen", o.Correlation.ToolUseID)
	assert.Equal(t, "Write", o.Attrs["name"])
	assert.Equal(t, "error", o.Attrs["status"])
}

func TestParseAgentSpan(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(4),
		Name:   "invoke_agent myagent",
		Attributes: []*commonv1.KeyValue{
			strAttr("agent_id", "agent_abc"),
			strAttr("subagent_type", "task"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec4", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "subagent_stop", o.Kind)
	assert.Equal(t, "agent_abc", o.Correlation.AgentID)
	assert.Equal(t, "task", o.Attrs["subagent_type"])
}

func TestParseHookSpan(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(5),
		Name:   "claude_code.hook",
		Attributes: []*commonv1.KeyValue{
			strAttr("hook_event", "PreToolUse"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec5", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "marker", o.Kind)
	assert.Equal(t, "PreToolUse", o.Attrs["hook_event"])
}

func TestParseUnknownSpanSkipped(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(6),
		Name:   "some.unknown.span",
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec6", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseSessionIDPropagatedToAllSpans(t *testing.T) {
	fixedNow(time.Now())
	spans := []*tracev1.Span{
		{
			SpanId: spanID(7),
			Name:   "claude_code.llm_request",
		},
		{
			SpanId: spanID(8),
			Name:   "claude_code.hook",
			Attributes: []*commonv1.KeyValue{
				strAttr("hook_event", "Notification"),
			},
		},
	}
	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			strAttr("session.id", "sess_shared"),
		},
	}
	req := makeReq(resource, spans...)

	obs, err := Parse(req, "exec7", seq())
	require.NoError(t, err)
	require.Len(t, obs, 2)

	for _, o := range obs {
		assert.Equal(t, "sess_shared", o.RunID)
		assert.Equal(t, "sess_shared", o.Correlation.SessionID)
	}
}

func TestParseOpenInferenceLLMKind(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(9),
		Name:   "llm_call",
		Attributes: []*commonv1.KeyValue{
			strAttr("openinference.span.kind", "LLM"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec8", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "assistant_turn", obs[0].Kind)
}

func TestParseOpenInferenceTOOLKind(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(10),
		Name:   "tool_call",
		Attributes: []*commonv1.KeyValue{
			strAttr("openinference.span.kind", "TOOL"),
			strAttr("tool_use_id", "toolu_oi"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec9", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_tool_use", o.Kind)
	assert.Equal(t, "toolu_oi", o.Correlation.ToolUseID)
}

func TestParseOpenInferenceAGENTKind(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(11),
		Name:   "agent_run",
		Attributes: []*commonv1.KeyValue{
			strAttr("openinference.span.kind", "AGENT"),
			strAttr("agent_id", "agent_oi"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec10", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "subagent_stop", o.Kind)
	assert.Equal(t, "agent_oi", o.Correlation.AgentID)
}

func TestParseOpenInferenceCHAINKind(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(12),
		Name:   "chain_step",
		Attributes: []*commonv1.KeyValue{
			strAttr("openinference.span.kind", "CHAIN"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec11", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "marker", obs[0].Kind)
}

func TestParseSessionIDFallbackSessionIDUnderscore(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(13),
		Name:   "claude_code.llm_request",
	}
	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			strAttr("session_id", "sess_underscore"),
		},
	}
	req := makeReq(resource, span)

	obs, err := Parse(req, "exec12", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "sess_underscore", obs[0].RunID)
}

func TestParseSessionIDFallbackGenAIConversationID(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(14),
		Name:   "claude_code.llm_request",
	}
	resource := &resourcev1.Resource{
		Attributes: []*commonv1.KeyValue{
			strAttr("gen_ai.conversation.id", "sess_genai"),
		},
	}
	req := makeReq(resource, span)

	obs, err := Parse(req, "exec13", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "sess_genai", obs[0].RunID)
}

func TestParseParentSpanIDHex(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId:       spanID(1),
		ParentSpanId: parentSpanID(2),
		Name:         "claude_code.llm_request",
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec14", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "0000000000000001", obs[0].Correlation.SpanID)
	assert.Equal(t, "0000000000000002", obs[0].Correlation.ParentSpanID)
}

func TestParseToolSpanStatusUnset(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(15),
		Name:   "claude_code.tool",
		Attributes: []*commonv1.KeyValue{
			strAttr("tool_use_id", "toolu_unset"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec15", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "running", obs[0].Attrs["status"])
}

func TestParseLLMSpanChatModel(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(16),
		Name:   "chat claude-opus-4-5",
		Attributes: []*commonv1.KeyValue{
			strAttr("model", "claude-opus-4-5"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec16", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "assistant_turn", obs[0].Kind)
	assert.Equal(t, "claude-opus-4-5", obs[0].Attrs["model"])
}

func TestParseLLMSpanInteraction(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(17),
		Name:   "claude_code.interaction",
		Attributes: []*commonv1.KeyValue{
			strAttr("message.id", "msg_interact"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec17", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_turn", o.Kind)
	assert.Equal(t, "msg_interact", o.Correlation.MessageID)
}

func TestParseMultipleResourceSpans(t *testing.T) {
	fixedNow(time.Now())
	span1 := &tracev1.Span{SpanId: spanID(18), Name: "claude_code.llm_request"}
	span2 := &tracev1.Span{SpanId: spanID(19), Name: "claude_code.hook", Attributes: []*commonv1.KeyValue{strAttr("hook_event", "Stop")}}

	req := &collectorv1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource:   &resourcev1.Resource{Attributes: []*commonv1.KeyValue{strAttr("session.id", "sess_multi_a")}},
				ScopeSpans: []*tracev1.ScopeSpans{{Spans: []*tracev1.Span{span1}}},
			},
			{
				Resource:   &resourcev1.Resource{Attributes: []*commonv1.KeyValue{strAttr("session.id", "sess_multi_b")}},
				ScopeSpans: []*tracev1.ScopeSpans{{Spans: []*tracev1.Span{span2}}},
			},
		},
	}

	obs, err := Parse(req, "exec18", seq())
	require.NoError(t, err)
	require.Len(t, obs, 2)
	assert.Equal(t, "sess_multi_a", obs[0].RunID)
	assert.Equal(t, "sess_multi_b", obs[1].RunID)
}

func TestParseToolSpanStatusUnsetCode(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(19),
		Name:   "claude_code.tool",
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_UNSET},
		Attributes: []*commonv1.KeyValue{
			strAttr("tool_use_id", "toolu_unsetcode"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec19", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "running", obs[0].Attrs["status"])
}

func TestParseLLMSpanTokensWrongType(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(20),
		Name:   "claude_code.llm_request",
		Attributes: []*commonv1.KeyValue{
			strAttr("gen_ai.usage.input_tokens", "not_an_int"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec20", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	_, ok := obs[0].Attrs["tokens_in"]
	assert.False(t, ok)
}

func TestParseLLMSpanResponseModelFallback(t *testing.T) {
	fixedNow(time.Now())
	span := &tracev1.Span{
		SpanId: spanID(21),
		Name:   "claude_code.llm_request",
		Attributes: []*commonv1.KeyValue{
			strAttr("gen_ai.response.model", "claude-opus-4-5-resp"),
		},
	}
	req := makeReq(nil, span)

	obs, err := Parse(req, "exec21", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "claude-opus-4-5-resp", obs[0].Attrs["model"])
}
