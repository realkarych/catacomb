package hook

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/model"
)

type envelope struct {
	HookEventName      string          `json:"hook_event_name"`
	SessionID          string          `json:"session_id"`
	ToolName           string          `json:"tool_name"`
	ToolUseID          string          `json:"tool_use_id"`
	ToolInput          json.RawMessage `json:"tool_input"`
	ToolResponse       json.RawMessage `json:"tool_response"`
	PermissionDecision string          `json:"permission_decision"`
	Prompt             string          `json:"prompt"`
	Source             string          `json:"source"`
	Reason             string          `json:"reason"`
	Trigger            string          `json:"trigger"`
	Message            string          `json:"message"`
	AgentID            string          `json:"agent_id"`
	AgentType          string          `json:"agent_type"`
	Cwd                string          `json:"cwd"`
}

var nowFn = time.Now

func Parse(hookType string, payload []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error) {
	var e envelope
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, fmt.Errorf("hook.Parse: %w", err)
	}
	p := build(hookType, e)
	if p == nil {
		return nil, nil
	}
	seq := nextSeq()
	if p.kind == "user_prompt" {
		p.correlation.UUID = model.PromptUUID(e.SessionID, e.Prompt)
	}
	ts := nowFn().UTC()
	return []model.Observation{{
		ObsID:       ulid.Make().String(),
		RunID:       e.SessionID,
		ExecutionID: executionID,
		Source:      model.SourceHook,
		Kind:        p.kind,
		Correlation: p.correlation,
		Attrs:       p.attrs,
		Payload:     p.payload,
		EventTime:   ts,
		ObservedAt:  ts,
		Seq:         seq,
	}}, nil
}

type partial struct {
	kind        string
	correlation model.Correlation
	attrs       map[string]any
	payload     *model.Payload
}

func build(hookType string, e envelope) *partial {
	base := model.Correlation{SessionID: e.SessionID}
	switch hookType {
	case "SessionStart":
		attrs := map[string]any{"source": e.Source}
		if e.Cwd != "" {
			attrs["cwd"] = e.Cwd
		}
		return &partial{kind: "session_start", correlation: base, attrs: attrs}
	case "SessionEnd":
		return &partial{kind: "session_end", correlation: base, attrs: map[string]any{"reason": e.Reason}}
	case "Stop":
		return &partial{kind: "stop", correlation: base}
	case "UserPromptSubmit":
		return &partial{kind: "user_prompt", correlation: base, attrs: map[string]any{"prompt": e.Prompt}}
	case "PreToolUse":
		c := base
		c.ToolUseID = e.ToolUseID
		status := model.StatusRunning
		if e.PermissionDecision == "deny" {
			status = model.StatusBlocked
		}
		pl := &model.Payload{Input: e.ToolInput}
		pl.Hash = model.HashPayload(pl)
		return &partial{kind: "assistant_tool_use", correlation: c, attrs: map[string]any{"name": e.ToolName, "status": string(status)}, payload: pl}
	case "PostToolUse":
		c := base
		c.ToolUseID = e.ToolUseID
		pl := &model.Payload{Output: e.ToolResponse}
		pl.Hash = model.HashPayload(pl)
		return &partial{kind: "tool_result", correlation: c, attrs: map[string]any{"name": e.ToolName, "status": string(model.StatusOK)}, payload: pl}
	case "SubagentStop":
		c := base
		c.AgentID = e.AgentID
		return &partial{kind: "subagent_stop", correlation: c, attrs: map[string]any{"subagent_type": e.AgentType}}
	case "PreCompact", "Notification":
		attrs := map[string]any{"hook_event": hookType}
		if e.Trigger != "" {
			attrs["trigger"] = e.Trigger
		}
		if e.Message != "" {
			attrs["message"] = e.Message
		}
		return &partial{kind: "marker", correlation: base, attrs: attrs}
	default:
		return nil
	}
}
