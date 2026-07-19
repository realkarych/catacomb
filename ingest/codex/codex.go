package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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
	SessionID      string          `json:"session_id"`
	ID             string          `json:"id"`
	Cwd            string          `json:"cwd"`
	CLIVersion     string          `json:"cli_version"`
	ParentThreadID string          `json:"parent_thread_id"`
	AgentRole      string          `json:"agent_role"`
	Source         json.RawMessage `json:"source"`
}

type threadSpawn struct {
	ParentThreadID string `json:"parent_thread_id"`
	AgentRole      string `json:"agent_role"`
}

type spawnSubagent struct {
	ThreadSpawn threadSpawn `json:"thread_spawn"`
}

type spawnSource struct {
	Subagent spawnSubagent `json:"subagent"`
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
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Name      string          `json:"name"`
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Content   []contentItem   `json:"content"`
	Metadata  turnMetadata    `json:"internal_chat_message_metadata_passthrough"`
}

type tokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
}

type tokenInfo struct {
	LastTokenUsage *tokenUsage `json:"last_token_usage"`
}

type mcpInvocation struct {
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type eventMsgPayload struct {
	Type       string          `json:"type"`
	Message    string          `json:"message"`
	TurnID     string          `json:"turn_id"`
	CallID     string          `json:"call_id"`
	DurationMS *int64          `json:"duration_ms"`
	Info       *tokenInfo      `json:"info"`
	Invocation mcpInvocation   `json:"invocation"`
	Error      json.RawMessage `json:"error"`
	Result     json.RawMessage `json:"result"`
	ExitCode   *int            `json:"exit_code"`
	Success    *bool           `json:"success"`
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
	sessionID      string
	version        string
	cwd            string
	parentThreadID string
	agentRole      string
	currentTurnID  string
	turns          map[string]*turnState
	turnOrder      []string
	failedCalls    map[string]bool
	emissions      []emission
	counts         drift.Counts
}

func Parse(r io.Reader, mainRunID, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) ([]model.Observation, drift.Counts, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	p := &parser{turns: map[string]*turnState{}, failedCalls: map[string]bool{}}
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
	p.appendSubagentStop()
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
	p.parentThreadID = meta.ParentThreadID
	p.agentRole = meta.AgentRole
	p.applySpawnSource(meta.Source)
	return nil
}

func (p *parser) applySpawnSource(raw json.RawMessage) {
	var src spawnSource
	if err := json.Unmarshal(raw, &src); err != nil {
		return
	}
	if p.parentThreadID == "" {
		p.parentThreadID = src.Subagent.ThreadSpawn.ParentThreadID
	}
	if p.agentRole == "" {
		p.agentRole = src.Subagent.ThreadSpawn.AgentRole
	}
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
	switch item.Type {
	case "message":
		p.assistantMessage(item, ts)
	case "function_call":
		p.toolCall(item, item.Arguments, ts)
	case "custom_tool_call":
		p.toolCall(item, item.Input, ts)
	case "function_call_output", "custom_tool_call_output":
		p.toolResult(item, ts)
	default:
		if !knownResponseItemType(item.Type) {
			p.counts = p.counts.Bump(drift.ReasonUnknownRecordType)
		}
	}
	return nil
}

func (p *parser) toolCall(item responseItemPayload, args json.RawMessage, ts time.Time) {
	pl := &model.Payload{Input: decodedToolInput(args)}
	pl.Hash = model.HashPayload(pl)
	p.emissions = append(p.emissions, emission{
		kind: "assistant_tool_use",
		correlation: model.Correlation{
			SessionID: p.sessionID,
			ToolUseID: item.CallID,
			MessageID: p.orCurrent(item.Metadata.TurnID),
		},
		attrs:     map[string]any{"name": item.Name},
		payload:   pl,
		eventTime: ts,
	})
}

func decodedToolInput(raw json.RawMessage) json.RawMessage {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	return raw
}

func (p *parser) toolResult(item responseItemPayload, ts time.Time) {
	pl := &model.Payload{Output: item.Output}
	pl.Hash = model.HashPayload(pl)
	p.emissions = append(p.emissions, emission{
		kind:        "tool_result",
		correlation: model.Correlation{SessionID: p.sessionID, ToolUseID: item.CallID},
		attrs:       map[string]any{"status": string(outputStatus(proseString(item.Output)))},
		payload:     pl,
		eventTime:   ts,
	})
}

func proseString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

var exitCodeRe = regexp.MustCompile(`(?m)^Process exited with code (\d+)$`)

func outputStatus(output string) model.Status {
	for _, m := range exitCodeRe.FindAllStringSubmatch(output, -1) {
		if m[1] != "0" {
			return model.StatusError
		}
	}
	return model.StatusOK
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
	case "reasoning", "tool_search_call", "tool_search_output", "web_search_call":
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
	case "mcp_tool_call_begin":
		p.mcpToolCallBegin(ev, ts)
	case "mcp_tool_call_end":
		p.mcpToolCallEnd(ev, ts)
	case "exec_command_end", "patch_apply_end":
		p.toolCallEnd(ev)
	default:
		if !knownEventType(ev.Type) {
			p.counts = p.counts.Bump(drift.ReasonUnknownRecordType)
		}
	}
	return nil
}

func knownEventType(t string) bool {
	switch t {
	case "agent_message", "error", "session_error", "stream_error",
		"turn_aborted", "context_compacted", "exec_command_begin",
		"exec_command_end", "patch_apply_begin", "patch_apply_end":
		return true
	default:
		return false
	}
}

func (p *parser) toolCallEnd(ev eventMsgPayload) {
	if ev.CallID == "" || !endEventReportedFailure(ev) {
		return
	}
	p.failedCalls[ev.CallID] = true
}

func endEventReportedFailure(ev eventMsgPayload) bool {
	if ev.ExitCode != nil && *ev.ExitCode != 0 {
		return true
	}
	return ev.Success != nil && !*ev.Success
}

func (p *parser) applyFailedCallStatus() {
	for i := range p.emissions {
		e := &p.emissions[i]
		if e.kind == "tool_result" && p.failedCalls[e.correlation.ToolUseID] {
			e.attrs["status"] = string(model.StatusError)
		}
	}
}

func (p *parser) mcpToolCallBegin(ev eventMsgPayload, ts time.Time) {
	pl := &model.Payload{Input: ev.Invocation.Arguments}
	pl.Hash = model.HashPayload(pl)
	p.emissions = append(p.emissions, emission{
		kind: "assistant_tool_use",
		correlation: model.Correlation{
			SessionID: p.sessionID,
			ToolUseID: ev.CallID,
			MessageID: p.orCurrent(ev.TurnID),
		},
		attrs:     map[string]any{"name": "mcp__" + ev.Invocation.Server + "__" + ev.Invocation.Tool},
		payload:   pl,
		eventTime: ts,
	})
}

func (p *parser) mcpToolCallEnd(ev eventMsgPayload, ts time.Time) {
	var pl *model.Payload
	if jsonValuePresent(ev.Result) {
		pl = &model.Payload{Output: ev.Result}
		pl.Hash = model.HashPayload(pl)
	}
	p.emissions = append(p.emissions, emission{
		kind:        "tool_result",
		correlation: model.Correlation{SessionID: p.sessionID, ToolUseID: ev.CallID},
		attrs:       map[string]any{"status": string(mcpStatus(ev))},
		payload:     pl,
		eventTime:   ts,
	})
}

func mcpStatus(ev eventMsgPayload) model.Status {
	if jsonValuePresent(ev.Error) {
		return model.StatusError
	}
	var res struct {
		IsError bool `json:"is_error"`
	}
	if err := json.Unmarshal(ev.Result, &res); err == nil && res.IsError {
		return model.StatusError
	}
	return model.StatusOK
}

func jsonValuePresent(raw json.RawMessage) bool {
	s := string(raw)
	return s != "" && s != "null" && s != `""`
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
		attrs["tokens_in"] = max(int64(0), t.usage.InputTokens-t.usage.CachedInputTokens)
		attrs["tokens_out"] = t.usage.OutputTokens
		attrs["cache_read_in"] = t.usage.CachedInputTokens
		if t.usage.CacheWriteInputTokens > 0 {
			attrs["cache_write"] = t.usage.CacheWriteInputTokens
		}
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

func (p *parser) appendSubagentStop() {
	if p.parentThreadID == "" {
		return
	}
	role := p.agentRole
	if role == "" {
		role = "codex-agent"
	}
	var ts time.Time
	if n := len(p.emissions); n > 0 {
		ts = p.emissions[n-1].eventTime
	}
	p.emissions = append(p.emissions, emission{
		kind:      "subagent_stop",
		attrs:     map[string]any{"subagent_type": role},
		eventTime: ts,
	})
}

func (p *parser) observations(mainRunID, executionID string, nextSeq func() uint64, observedAt func(eventTime time.Time) time.Time) []model.Observation {
	runID := mainRunID
	if runID == "" {
		runID = p.sessionID
	}
	p.applyFailedCallStatus()
	var out []model.Observation
	for _, e := range p.emissions {
		if p.parentThreadID != "" {
			e.correlation.AgentID = p.sessionID
			e.correlation.SessionID = runID
		}
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
