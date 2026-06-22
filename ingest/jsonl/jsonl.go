package jsonl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/model"
)

type line struct {
	Type            string          `json:"type"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"sessionId"`
	Timestamp       string          `json:"timestamp"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	IsSidechain     bool            `json:"isSidechain"`
	AgentID         string          `json:"agentId"`
	Message         json.RawMessage `json:"message"`
}

type message struct {
	Role    string          `json:"role"`
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

var nowFn = time.Now

func ParseReader(r io.Reader, executionID string) ([]model.Observation, error) {
	var seq uint64
	next := func() uint64 {
		s := seq
		seq++
		return s
	}
	return Parse(r, executionID, next, func(eventTime time.Time) time.Time { return eventTime })
}

func Parse(r io.Reader, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) ([]model.Observation, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var out []model.Observation
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		ln, parts, err := decodeLine([]byte(raw))
		if err != nil {
			return nil, err
		}
		ts, _ := time.Parse(time.RFC3339, ln.Timestamp)
		for _, p := range parts {
			out = append(out, model.Observation{
				ObsID:       ulid.Make().String(),
				RunID:       ln.SessionID,
				ExecutionID: executionID,
				Source:      model.SourceJSONL,
				Kind:        p.kind,
				Correlation: p.correlation,
				Attrs:       p.attrs,
				Payload:     p.payload,
				EventTime:   ts,
				ObservedAt:  observedAt(ts),
				Seq:         nextSeq(),
			})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("jsonl.ParseReader: %w", err)
	}
	return out, nil
}

func decodeLine(raw []byte) (line, []partial, error) {
	var ln line
	if err := json.Unmarshal(raw, &ln); err != nil {
		return ln, nil, fmt.Errorf("jsonl.decodeLine: %w", err)
	}
	if len(ln.Message) == 0 {
		return ln, nil, nil
	}
	var msg message
	if err := json.Unmarshal(ln.Message, &msg); err != nil {
		return ln, nil, fmt.Errorf("jsonl.decodeLine.message: %w", err)
	}
	text, blocks, err := decodeContent(msg.Content)
	if err != nil {
		return ln, nil, err
	}
	base := model.Correlation{SessionID: ln.SessionID, UUID: ln.UUID, ParentToolUseID: ln.ParentToolUseID}
	var parts []partial
	switch ln.Type {
	case "user":
		parts = userParts(base, text, blocks)
	case "assistant":
		parts = assistantParts(base, msg, blocks)
	}
	if ln.IsSidechain || ln.AgentID != "" {
		parts = append(parts, partial{
			kind:        "subagent_stop",
			correlation: model.Correlation{AgentID: ln.AgentID, ParentToolUseID: ln.ParentToolUseID, SessionID: ln.SessionID},
		})
	}
	return ln, parts, nil
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
		return "", nil, fmt.Errorf("jsonl.decodeContent: %w", err)
	}
	return "", blocks, nil
}

func userParts(base model.Correlation, text string, blocks []block) []partial {
	var parts []partial
	if text != "" {
		parts = append(parts, partial{kind: "user_prompt", correlation: base})
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

func assistantParts(base model.Correlation, msg message, blocks []block) []partial {
	turn := base
	turn.MessageID = msg.ID
	attrs := map[string]any{"model": msg.Model}
	if msg.Usage != nil {
		attrs["tokens_in"] = msg.Usage.InputTokens
		attrs["tokens_out"] = msg.Usage.OutputTokens
	}
	parts := []partial{{kind: "assistant_turn", correlation: turn, attrs: attrs}}
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
