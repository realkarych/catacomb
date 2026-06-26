package streamjson

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/model"
)

var nowFn = time.Now

type envelope struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	Model           string          `json:"model"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	UUID            string          `json:"uuid"`
	Message         json.RawMessage `json:"message"`
	Usage           *usage          `json:"usage"`
	TotalCostUSD    *float64        `json:"total_cost_usd"`
}

type message struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type partial struct {
	kind        string
	correlation model.Correlation
	attrs       map[string]any
	payload     *model.Payload
}

func Parse(line []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error) {
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, fmt.Errorf("streamjson.Parse: %w", err)
	}
	parts, err := build(e)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return []model.Observation{}, nil
	}
	ts := nowFn().UTC()
	out := make([]model.Observation, 0, len(parts))
	for _, p := range parts {
		out = append(out, model.Observation{
			ObsID:       ulid.Make().String(),
			RunID:       e.SessionID,
			ExecutionID: executionID,
			Source:      model.SourceStreamJSON,
			Kind:        p.kind,
			Correlation: p.correlation,
			Attrs:       p.attrs,
			Payload:     p.payload,
			EventTime:   ts,
			ObservedAt:  ts,
			Seq:         nextSeq(),
		})
	}
	return out, nil
}

func build(e envelope) ([]partial, error) {
	base := model.Correlation{SessionID: e.SessionID, ParentToolUseID: e.ParentToolUseID}
	switch e.Type {
	case "system":
		if e.Subtype != "init" {
			return nil, nil
		}
		return []partial{{kind: "session_start", correlation: base, attrs: map[string]any{"model": e.Model}}}, nil
	case "assistant":
		var msg message
		if len(e.Message) > 0 {
			if err := json.Unmarshal(e.Message, &msg); err != nil {
				return nil, fmt.Errorf("streamjson.build.assistant: %w", err)
			}
		}
		text, blocks, err := decodeContent(msg.Content)
		if err != nil {
			return nil, err
		}
		return assistantParts(base, msg, text, blocks), nil
	case "user":
		var msg message
		if len(e.Message) > 0 {
			if err := json.Unmarshal(e.Message, &msg); err != nil {
				return nil, fmt.Errorf("streamjson.build.user: %w", err)
			}
		}
		text, blocks, err := decodeContent(msg.Content)
		if err != nil {
			return nil, err
		}
		return userParts(base, text, blocks), nil
	case "stream_event":
		return nil, nil
	case "result":
		attrs := map[string]any{}
		if e.Usage != nil {
			attrs["tokens_in"] = e.Usage.InputTokens
			attrs["tokens_out"] = e.Usage.OutputTokens
		}
		if e.TotalCostUSD != nil {
			attrs["cost_usd"] = *e.TotalCostUSD
		}
		return []partial{{kind: "assistant_turn", correlation: base, attrs: attrs}}, nil
	default:
		return nil, nil
	}
}

func decodeContent(raw json.RawMessage) (string, []block, error) {
	if len(raw) == 0 {
		return "", nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, nil
	}
	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil, fmt.Errorf("streamjson.decodeContent: %w", err)
	}
	return "", blocks, nil
}

func assistantTextFromBlocks(blocks []block) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "")
}

func assistantParts(base model.Correlation, msg message, text string, blocks []block) []partial {
	turn := base
	turn.MessageID = msg.ID
	attrs := map[string]any{"model": msg.Model}
	if msg.Usage != nil {
		attrs["tokens_in"] = msg.Usage.InputTokens
		attrs["tokens_out"] = msg.Usage.OutputTokens
	}
	turnPart := partial{kind: "assistant_turn", correlation: turn, attrs: attrs}
	resolved := text
	if resolved == "" {
		resolved = assistantTextFromBlocks(blocks)
	}
	if resolved != "" {
		enc, err := json.Marshal(resolved)
		if err == nil {
			pl := &model.Payload{Output: enc}
			pl.Hash = model.HashPayload(pl)
			turnPart.payload = pl
		}
	}
	parts := []partial{turnPart}
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		c := base
		c.ToolUseID = b.ID
		c.MessageID = msg.ID
		pl := &model.Payload{Input: b.Input}
		pl.Hash = model.HashPayload(pl)
		parts = append(parts, partial{
			kind:        "assistant_tool_use",
			correlation: c,
			attrs:       map[string]any{"name": b.Name},
			payload:     pl,
		})
	}
	return parts
}

func userParts(base model.Correlation, text string, blocks []block) []partial {
	var parts []partial
	if text != "" {
		p := partial{kind: "user_prompt", correlation: base, attrs: map[string]any{"prompt": text, "prompt_kind": model.PromptKind(text)}}
		enc, err := json.Marshal(text)
		if err == nil {
			pl := &model.Payload{Input: enc}
			pl.Hash = model.HashPayload(pl)
			p.payload = pl
		}
		parts = append(parts, p)
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		status := model.StatusOK
		if b.IsError {
			status = model.StatusError
		}
		c := base
		c.ToolUseID = b.ToolUseID
		pl := &model.Payload{Output: b.Content}
		pl.Hash = model.HashPayload(pl)
		parts = append(parts, partial{
			kind:        "tool_result",
			correlation: c,
			attrs:       map[string]any{"status": string(status)},
			payload:     pl,
		})
	}
	return parts
}
