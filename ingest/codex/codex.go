package codex

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

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	SessionID  string `json:"session_id"`
	ID         string `json:"id"`
	Cwd        string `json:"cwd"`
	CLIVersion string `json:"cli_version"`
}

type turnContextPayload struct {
	TurnID string `json:"turn_id"`
	Model  string `json:"model"`
}

type turnMetadata struct {
	TurnID string `json:"turn_id"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseItemPayload struct {
	Type     string        `json:"type"`
	Role     string        `json:"role"`
	Content  []contentItem `json:"content"`
	Metadata turnMetadata  `json:"internal_chat_message_metadata_passthrough"`
}

type tokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

type tokenInfo struct {
	LastTokenUsage *tokenUsage `json:"last_token_usage"`
}

type eventMsgPayload struct {
	Type       string     `json:"type"`
	Message    string     `json:"message"`
	TurnID     string     `json:"turn_id"`
	DurationMS *int64     `json:"duration_ms"`
	Info       *tokenInfo `json:"info"`
}

type turnState struct {
	id            string
	model         string
	usage         *tokenUsage
	durationMS    *int64
	assistantText string
	eventTime     time.Time
	done          bool
}

type emission struct {
	kind        string
	correlation model.Correlation
	attrs       map[string]any
	payload     *model.Payload
	eventTime   time.Time
}

type parser struct {
	sessionID     string
	version       string
	cwd           string
	currentTurnID string
	turns         map[string]*turnState
	turnOrder     []string
	emissions     []emission
	counts        drift.Counts
}

func Parse(r io.Reader, mainRunID, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) ([]model.Observation, drift.Counts, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	p := &parser{turns: map[string]*turnState{}}
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		if err := p.line([]byte(raw)); err != nil {
			return nil, nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("codex.Parse: %w", err)
	}
	p.flushOpenTurns()
	return p.observations(mainRunID, executionID, nextSeq, observedAt), p.counts, nil
}

func (p *parser) line(raw []byte) error {
	var ln rolloutLine
	if err := json.Unmarshal(raw, &ln); err != nil {
		return fmt.Errorf("codex.decodeLine: %w", err)
	}
	ts, tsErr := time.Parse(time.RFC3339, ln.Timestamp)
	if tsErr != nil && ln.Timestamp != "" {
		p.counts = p.counts.Bump(drift.ReasonBadTimestamp)
	}
	switch ln.Type {
	case "session_meta":
		return p.sessionMeta(ln.Payload)
	case "turn_context":
		return p.turnContext(ln.Payload, ts)
	case "response_item":
		return p.responseItem(ln.Payload, ts)
	case "event_msg":
		return p.eventMsg(ln.Payload, ts)
	case "compacted", "world_state":
		return nil
	default:
		p.counts = p.counts.Bump(drift.ReasonUnknownRecordType)
		return nil
	}
}

func (p *parser) sessionMeta(raw json.RawMessage) error {
	var meta sessionMetaPayload
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("codex.sessionMeta: %w", err)
	}
	p.sessionID = meta.SessionID
	if p.sessionID == "" {
		p.sessionID = meta.ID
	}
	p.version = meta.CLIVersion
	p.cwd = meta.Cwd
	return nil
}

func (p *parser) turnContext(raw json.RawMessage, ts time.Time) error {
	var tc turnContextPayload
	if err := json.Unmarshal(raw, &tc); err != nil {
		return fmt.Errorf("codex.turnContext: %w", err)
	}
	p.currentTurnID = tc.TurnID
	p.turn(tc.TurnID, ts).model = tc.Model
	return nil
}

func (p *parser) responseItem(raw json.RawMessage, ts time.Time) error {
	var item responseItemPayload
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("codex.responseItem: %w", err)
	}
	if item.Type == "message" {
		p.assistantMessage(item, ts)
		return nil
	}
	if !knownResponseItemType(item.Type) {
		p.counts = p.counts.Bump(drift.ReasonUnknownRecordType)
	}
	return nil
}

func (p *parser) assistantMessage(item responseItemPayload, ts time.Time) {
	if item.Role != "assistant" {
		return
	}
	text := outputText(item.Content)
	if text == "" {
		return
	}
	p.turn(p.orCurrent(item.Metadata.TurnID), ts).assistantText = text
}

func outputText(content []contentItem) string {
	var parts []string
	for _, c := range content {
		if c.Type == "output_text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "")
}

func knownResponseItemType(t string) bool {
	switch t {
	case "reasoning", "function_call", "function_call_output",
		"custom_tool_call", "custom_tool_call_output",
		"tool_search_call", "tool_search_output", "web_search_call":
		return true
	default:
		return strings.HasPrefix(t, "mcp_tool_call")
	}
}

func (p *parser) eventMsg(raw json.RawMessage, ts time.Time) error {
	var ev eventMsgPayload
	if err := json.Unmarshal(raw, &ev); err != nil {
		return fmt.Errorf("codex.eventMsg: %w", err)
	}
	switch ev.Type {
	case "user_message":
		p.userMessage(ev.Message, ts)
	case "task_started", "turn_started":
		p.currentTurnID = ev.TurnID
	case "token_count":
		p.tokenCount(ev, ts)
	case "task_complete":
		p.taskComplete(ev, ts)
	default:
		if !knownEventType(ev.Type) {
			p.counts = p.counts.Bump(drift.ReasonUnknownRecordType)
		}
	}
	return nil
}

func knownEventType(t string) bool {
	switch t {
	case "agent_message", "mcp_tool_call_begin", "mcp_tool_call_end",
		"error", "session_error", "stream_error", "turn_aborted",
		"context_compacted", "exec_command_begin", "exec_command_end",
		"patch_apply_begin", "patch_apply_end":
		return true
	default:
		return false
	}
}

func (p *parser) userMessage(text string, ts time.Time) {
	if text == "" {
		return
	}
	encoded, _ := json.Marshal(text)
	pl := &model.Payload{Input: encoded}
	pl.Hash = model.HashPayload(pl)
	p.emissions = append(p.emissions, emission{
		kind: "user_prompt",
		correlation: model.Correlation{
			SessionID: p.sessionID,
			UUID:      model.PromptUUID(p.sessionID, text),
		},
		attrs:     map[string]any{"prompt_kind": model.PromptKind(text)},
		payload:   pl,
		eventTime: ts,
	})
}

func (p *parser) tokenCount(ev eventMsgPayload, ts time.Time) {
	if ev.Info == nil || ev.Info.LastTokenUsage == nil {
		return
	}
	p.turn(p.orCurrent(ev.TurnID), ts).usage = ev.Info.LastTokenUsage
}

func (p *parser) taskComplete(ev eventMsgPayload, ts time.Time) {
	t := p.turn(p.orCurrent(ev.TurnID), ts)
	if ev.DurationMS != nil {
		t.durationMS = ev.DurationMS
	}
	p.flushTurn(t)
	p.currentTurnID = ""
}

func (p *parser) turn(id string, ts time.Time) *turnState {
	t, ok := p.turns[id]
	if !ok {
		t = &turnState{id: id}
		p.turns[id] = t
		p.turnOrder = append(p.turnOrder, id)
	}
	t.eventTime = ts
	return t
}

func (p *parser) orCurrent(turnID string) string {
	if turnID == "" {
		return p.currentTurnID
	}
	return turnID
}

func (p *parser) flushTurn(t *turnState) {
	if t.done {
		return
	}
	t.done = true
	attrs := map[string]any{}
	if t.model != "" {
		attrs["model"] = t.model
	}
	if t.usage != nil {
		attrs["tokens_in"] = t.usage.InputTokens
		attrs["tokens_out"] = t.usage.OutputTokens
		attrs["cache_read_in"] = t.usage.CachedInputTokens
	}
	if t.durationMS != nil {
		attrs["duration_ms"] = *t.durationMS
	}
	var pl *model.Payload
	if t.assistantText != "" {
		encoded, _ := json.Marshal(t.assistantText)
		pl = &model.Payload{Output: encoded}
		pl.Hash = model.HashPayload(pl)
	}
	p.emissions = append(p.emissions, emission{
		kind:        "assistant_turn",
		correlation: model.Correlation{SessionID: p.sessionID, MessageID: t.id},
		attrs:       attrs,
		payload:     pl,
		eventTime:   t.eventTime,
	})
}

func (p *parser) flushOpenTurns() {
	for _, id := range p.turnOrder {
		p.flushTurn(p.turns[id])
	}
}

func (p *parser) observations(mainRunID, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) []model.Observation {
	runID := mainRunID
	if runID == "" {
		runID = p.sessionID
	}
	var out []model.Observation
	for _, e := range p.emissions {
		e.attrs["agent_runtime"] = drift.RuntimeCodex
		if p.version != "" {
			e.attrs["codex_version"] = p.version
		}
		if p.cwd != "" {
			e.attrs["cwd"] = p.cwd
		}
		out = append(out, model.Observation{
			ObsID:       ulid.Make().String(),
			RunID:       runID,
			ExecutionID: executionID,
			Source:      model.SourceJSONL,
			Kind:        e.kind,
			Correlation: e.correlation,
			Attrs:       e.attrs,
			Payload:     e.payload,
			EventTime:   e.eventTime,
			ObservedAt:  observedAt(e.eventTime),
			Seq:         nextSeq(),
		})
	}
	return out
}
