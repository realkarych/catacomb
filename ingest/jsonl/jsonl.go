package jsonl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/ingest/drift"
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
	Version         string          `json:"version"`
	Cwd             string          `json:"cwd"`
}

type message struct {
	Role    string          `json:"role"`
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
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

var nowFn = time.Now

func ParseReader(r io.Reader, executionID string) ([]model.Observation, error) {
	var seq uint64
	next := func() uint64 {
		s := seq
		seq++
		return s
	}
	obs, _, err := Parse(r, executionID, next, func(eventTime time.Time) time.Time { return eventTime })
	return obs, err
}

func Parse(r io.Reader, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) ([]model.Observation, drift.Counts, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var out []model.Observation
	var dc drift.Counts
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		ln, parts, lineCounts, err := decodeLine([]byte(raw))
		if err != nil {
			return nil, nil, err
		}
		dc = dc.Merge(lineCounts)
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
		return nil, nil, fmt.Errorf("jsonl.ParseReader: %w", err)
	}
	return out, dc, nil
}

func decodeLine(raw []byte) (line, []partial, drift.Counts, error) {
	var ln line
	if err := json.Unmarshal(raw, &ln); err != nil {
		return ln, nil, nil, fmt.Errorf("jsonl.decodeLine: %w", err)
	}
	var dc drift.Counts
	if !knownLineType(ln.Type) {
		dc = dc.Bump(drift.ReasonUnknownRecordType)
	}
	if len(ln.Message) == 0 {
		return ln, nil, dc, nil
	}
	var msg message
	if err := json.Unmarshal(ln.Message, &msg); err != nil {
		return ln, nil, nil, fmt.Errorf("jsonl.decodeLine.message: %w", err)
	}
	text, blocks, err := decodeContent(msg.Content)
	if err != nil {
		return ln, nil, nil, err
	}
	base := model.Correlation{SessionID: ln.SessionID, UUID: ln.UUID, ParentToolUseID: ln.ParentToolUseID, AgentID: ln.AgentID}
	var parts []partial
	switch ln.Type {
	case "user":
		parts = userParts(base, text, blocks)
		dc = dc.Merge(unknownBlockCounts(blocks, knownUserBlock))
	case "assistant":
		parts = assistantParts(base, msg, text, blocks)
		dc = dc.Merge(unknownBlockCounts(blocks, knownAssistantBlock))
	}
	if ln.IsSidechain || ln.AgentID != "" {
		parts = append(parts, partial{
			kind:        "subagent_stop",
			correlation: model.Correlation{AgentID: ln.AgentID, ParentToolUseID: ln.ParentToolUseID, SessionID: ln.SessionID},
		})
	}
	if ln.Version != "" || ln.Cwd != "" {
		for i := range parts {
			if parts[i].attrs == nil {
				parts[i].attrs = map[string]any{}
			}
			if ln.Version != "" {
				parts[i].attrs["claude_code_version"] = ln.Version
			}
			if ln.Cwd != "" {
				parts[i].attrs["cwd"] = ln.Cwd
			}
		}
	}
	return ln, parts, dc, nil
}

func knownLineType(t string) bool {
	switch t {
	case "user", "assistant", "summary", "system", "file-history-snapshot",
		"attachment", "last-prompt", "mode", "ai-title", "permission-mode",
		"pr-link", "queue-operation", "worktree-state", "relocated":
		return true
	default:
		return false
	}
}

func unknownBlockCounts(blocks []block, known func(string) bool) drift.Counts {
	var dc drift.Counts
	for _, b := range blocks {
		if !known(b.Type) {
			dc = dc.Bump(drift.ReasonUnknownContentBlock)
		}
	}
	return dc
}

func knownAssistantBlock(t string) bool {
	switch t {
	case "text", "tool_use", "thinking", "redacted_thinking":
		return true
	default:
		return false
	}
}

func knownUserBlock(t string) bool {
	switch t {
	case "text", "tool_result", "image", "document":
		return true
	default:
		return false
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
		return "", nil, fmt.Errorf("jsonl.decodeContent: %w", err)
	}
	return "", blocks, nil
}

func userParts(base model.Correlation, text string, blocks []block) []partial {
	var parts []partial
	if text != "" {
		encoded, _ := json.Marshal(text)
		pl := &model.Payload{Input: encoded}
		pl.Hash = model.HashPayload(pl)
		c := base
		c.UUID = model.PromptUUID(base.SessionID, text)
		parts = append(parts, partial{kind: "user_prompt", correlation: c, attrs: map[string]any{"prompt_kind": model.PromptKind(text)}, payload: pl})
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
		attrs["cache_read_in"] = msg.Usage.CacheReadInputTokens
		attrs["cache_write"] = msg.Usage.CacheCreationInputTokens
	}
	turnPart := partial{kind: "assistant_turn", correlation: turn, attrs: attrs}
	if text == "" {
		text = assistantTextFromBlocks(blocks)
	}
	if text != "" {
		enc, err := json.Marshal(text)
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
