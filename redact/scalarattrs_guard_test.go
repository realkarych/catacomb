package redact_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/realkarych/catacomb/ingest/hook"
	"github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/ingest/otel"
	"github.com/realkarych/catacomb/ingest/streamjson"
	"github.com/realkarych/catacomb/model"
)

func nextSeq() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func requireScalarTopLevelAttrs(t *testing.T, parser string, obs []model.Observation) {
	t.Helper()
	require.NotEmpty(t, obs, "parser %s must produce observations for this fixture", parser)
	for _, o := range obs {
		for k, v := range o.Attrs {
			switch v.(type) {
			case string, bool, int, int64, uint64, float64:
			default:
				t.Fatalf("redact.redactAttrs redacts only top-level scalar attr values: parser %s emitted non-scalar attr %q (%T) on kind %q — nested attr values would bypass write-path redaction; extend redactAttrs to walk nested values before landing this parser change", parser, k, v, o.Kind)
			}
		}
	}
}

func TestIngestParsersEmitOnlyScalarTopLevelAttrs(t *testing.T) {
	t.Run("hook", func(t *testing.T) {
		payloads := map[string]string{
			"SessionStart":     `{"session_id":"s1","source":"startup","cwd":"/home/kesha"}`,
			"SessionEnd":       `{"session_id":"s1","reason":"done"}`,
			"UserPromptSubmit": `{"session_id":"s1","prompt":"hi"}`,
			"PreToolUse":       `{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"ls"},"permission_decision":"deny"}`,
			"PostToolUse":      `{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{"ok":true}}`,
			"SubagentStop":     `{"session_id":"s1","agent_id":"a1","agent_type":"researcher"}`,
			"PreCompact":       `{"session_id":"s1","trigger":"auto","message":"compacting"}`,
		}
		var all []model.Observation
		for hookType, payload := range payloads {
			obs, _, err := hook.Parse(hookType, []byte(payload), "e1", nextSeq())
			require.NoError(t, err)
			all = append(all, obs...)
		}
		requireScalarTopLevelAttrs(t, "ingest/hook", all)
	})

	t.Run("streamjson", func(t *testing.T) {
		lines := []string{
			`{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-8","cwd":"/home/kesha"}`,
			`{"type":"assistant","session_id":"s1","message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":2},"content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
			`{"type":"user","session_id":"s1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok","is_error":false}]}}`,
			`{"type":"result","session_id":"s1","total_cost_usd":0.42,"usage":{"input_tokens":7,"output_tokens":9,"cache_read_input_tokens":1,"cache_creation_input_tokens":1}}`,
		}
		var all []model.Observation
		for _, line := range lines {
			obs, _, err := streamjson.Parse([]byte(line), "e1", nextSeq())
			require.NoError(t, err)
			all = append(all, obs...)
		}
		requireScalarTopLevelAttrs(t, "ingest/streamjson", all)
	})

	t.Run("jsonl", func(t *testing.T) {
		lines := strings.Join([]string{
			`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"list files"}}`,
			`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","isSidechain":true,"version":"1.2.3","cwd":"/home/kesha","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}}`,
			`{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a.txt","is_error":false}]}}`,
		}, "\n") + "\n"
		obs, _, err := jsonl.Parse(strings.NewReader(lines), "e1", nextSeq(), func(ts time.Time) time.Time { return ts })
		require.NoError(t, err)
		requireScalarTopLevelAttrs(t, "ingest/jsonl", obs)
	})

	t.Run("otel", func(t *testing.T) {
		strAttr := func(key, val string) *commonv1.KeyValue {
			return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: val}}}
		}
		intAttr := func(key string, val int64) *commonv1.KeyValue {
			return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: val}}}
		}
		req := &collectorv1.ExportTraceServiceRequest{
			ResourceSpans: []*tracev1.ResourceSpans{{
				Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{
					strAttr("session.id", "s1"),
					strAttr("claude_code.version", "1.2.3"),
				}},
				ScopeSpans: []*tracev1.ScopeSpans{{Spans: []*tracev1.Span{
					{
						Name:   "claude_code.llm_request",
						SpanId: []byte{0, 0, 0, 0, 0, 0, 0, 1},
						Attributes: []*commonv1.KeyValue{
							strAttr("gen_ai.request.model", "claude-opus-4-8"),
							strAttr("message.id", "m1"),
							intAttr("gen_ai.usage.input_tokens", 10),
							intAttr("gen_ai.usage.output_tokens", 5),
							intAttr("gen_ai.usage.cache_read_input_tokens", 3),
							intAttr("gen_ai.usage.cache_creation_input_tokens", 2),
						},
					},
					{
						Name:   "claude_code.tool",
						SpanId: []byte{0, 0, 0, 0, 0, 0, 0, 2},
						Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
						Attributes: []*commonv1.KeyValue{
							strAttr("gen_ai.tool.name", "Bash"),
							strAttr("gen_ai.tool.call.id", "t1"),
						},
					},
					{
						Name:       "invoke_agent researcher",
						SpanId:     []byte{0, 0, 0, 0, 0, 0, 0, 3},
						Attributes: []*commonv1.KeyValue{strAttr("subagent_type", "researcher"), strAttr("agent_id", "a1")},
					},
					{
						Name:       "claude_code.hook",
						SpanId:     []byte{0, 0, 0, 0, 0, 0, 0, 4},
						Attributes: []*commonv1.KeyValue{strAttr("hook_event", "PreCompact")},
					},
				}}},
			}},
		}
		obs, _, err := otel.Parse(req, "e1", nextSeq())
		require.NoError(t, err)
		requireScalarTopLevelAttrs(t, "ingest/otel", obs)
	})
}
