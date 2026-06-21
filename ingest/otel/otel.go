package otel

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/realkarych/catacomb/model"
)

var nowFn = time.Now

func Parse(req *collectorv1.ExportTraceServiceRequest, executionID string, nextSeq func() uint64) ([]model.Observation, error) {
	var out []model.Observation
	for _, rs := range req.GetResourceSpans() {
		sessionID := extractSessionID(rs.GetResource().GetAttributes())
		for _, ss := range rs.GetScopeSpans() {
			for _, span := range ss.GetSpans() {
				obs, ok := spanToObservation(span, executionID, sessionID, nextSeq)
				if !ok {
					continue
				}
				out = append(out, obs)
			}
		}
	}
	if out == nil {
		out = []model.Observation{}
	}
	return out, nil
}

func extractSessionID(attrs []*commonv1.KeyValue) string {
	if v, ok := lookupString(attrs, "session.id", "session_id", "gen_ai.conversation.id"); ok {
		return v
	}
	return ""
}

func spanToObservation(span *tracev1.Span, executionID, sessionID string, nextSeq func() uint64) (model.Observation, bool) {
	kind, ok := classifySpan(span)
	if !ok {
		return model.Observation{}, false
	}

	attrs := buildAttrs(kind, span)
	corr := buildCorrelation(kind, sessionID, span)

	ts := nowFn().UTC()
	eventTime := time.Unix(0, int64(span.GetStartTimeUnixNano())).UTC()

	return model.Observation{
		ObsID:       ulid.Make().String(),
		RunID:       sessionID,
		ExecutionID: executionID,
		Source:      model.SourceOTel,
		Kind:        kind,
		Correlation: corr,
		Attrs:       attrs,
		EventTime:   eventTime,
		ObservedAt:  ts,
		Seq:         nextSeq(),
	}, true
}

func classifySpan(span *tracev1.Span) (string, bool) {
	name := span.GetName()
	switch {
	case name == "claude_code.interaction" || name == "claude_code.llm_request" || strings.HasPrefix(name, "chat "):
		return "assistant_turn", true
	case name == "claude_code.tool" || name == "claude_code.tool.execution" || strings.HasPrefix(name, "execute_tool "):
		return "assistant_tool_use", true
	case strings.HasPrefix(name, "invoke_agent "):
		return "subagent_stop", true
	case name == "claude_code.hook":
		return "marker", true
	}

	if v, ok := lookupString(span.GetAttributes(), "openinference.span.kind"); ok {
		switch v {
		case "LLM":
			return "assistant_turn", true
		case "TOOL":
			return "assistant_tool_use", true
		case "AGENT":
			return "subagent_stop", true
		case "CHAIN":
			return "marker", true
		}
	}

	return "", false
}

func buildCorrelation(kind, sessionID string, span *tracev1.Span) model.Correlation {
	c := model.Correlation{
		SessionID:    sessionID,
		SpanID:       hex.EncodeToString(span.GetSpanId()),
		ParentSpanID: hex.EncodeToString(span.GetParentSpanId()),
	}
	if c.ParentSpanID == "0000000000000000" {
		c.ParentSpanID = ""
	}

	attrs := span.GetAttributes()
	switch kind {
	case "assistant_turn":
		c.MessageID, _ = lookupString(attrs, "message.id")
	case "assistant_tool_use":
		c.ToolUseID, _ = lookupString(attrs, "gen_ai.tool.call.id", "tool_use_id")
	case "subagent_stop":
		c.AgentID, _ = lookupString(attrs, "agent_id")
	}
	return c
}

func buildAttrs(kind string, span *tracev1.Span) map[string]any {
	attrs := span.GetAttributes()
	m := map[string]any{}
	switch kind {
	case "assistant_turn":
		if v, ok := lookupString(attrs, "gen_ai.request.model", "gen_ai.response.model", "model"); ok {
			m["model"] = v
		}
		if v, ok := lookupInt(attrs, "gen_ai.usage.input_tokens"); ok {
			m["tokens_in"] = v
		}
		if v, ok := lookupInt(attrs, "gen_ai.usage.output_tokens"); ok {
			m["tokens_out"] = v
		}
	case "assistant_tool_use":
		if v, ok := lookupString(attrs, "gen_ai.tool.name", "tool_name"); ok {
			m["name"] = v
		}
		m["status"] = spanStatus(span)
	case "subagent_stop":
		if v, ok := lookupString(attrs, "subagent_type"); ok {
			m["subagent_type"] = v
		}
	case "marker":
		if v, ok := lookupString(attrs, "hook_event"); ok {
			m["hook_event"] = v
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func spanStatus(span *tracev1.Span) string {
	s := span.GetStatus()
	if s == nil {
		return string(model.StatusRunning)
	}
	switch s.GetCode() {
	case tracev1.Status_STATUS_CODE_OK:
		return string(model.StatusOK)
	case tracev1.Status_STATUS_CODE_ERROR:
		return string(model.StatusError)
	default:
		return string(model.StatusRunning)
	}
}

func lookupString(attrs []*commonv1.KeyValue, keys ...string) (string, bool) {
	for _, key := range keys {
		for _, kv := range attrs {
			if kv.GetKey() == key {
				v := kv.GetValue().GetStringValue()
				if v != "" {
					return v, true
				}
			}
		}
	}
	return "", false
}

func lookupInt(attrs []*commonv1.KeyValue, keys ...string) (int64, bool) {
	for _, key := range keys {
		for _, kv := range attrs {
			if kv.GetKey() == key {
				v := kv.GetValue().GetIntValue()
				return v, true
			}
		}
	}
	return 0, false
}
