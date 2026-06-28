package model

import (
	"encoding/json"
	"time"
)

type Source string

const (
	SourceHook       Source = "hook"
	SourceOTel       Source = "otel"
	SourceStreamJSON Source = "stream_json"
	SourceJSONL      Source = "jsonl"
)

type NodeType string

const (
	NodeSession       NodeType = "session"
	NodeUserPrompt    NodeType = "user_prompt"
	NodeAssistantTurn NodeType = "assistant_turn"
	NodeToolCall      NodeType = "tool_call"
	NodeSubagent      NodeType = "subagent"
	NodeMCPCall       NodeType = "mcp_call"
	NodeHookEvent     NodeType = "hook_event"
	NodeMarker        NodeType = "marker"
)

type EdgeType string

const (
	EdgeParentChild EdgeType = "parent_child"
	EdgeSequence    EdgeType = "sequence"
	EdgeMarkerSpan  EdgeType = "marker_span"
	EdgeDataDep     EdgeType = "data_dep"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusOK         Status = "ok"
	StatusError      Status = "error"
	StatusBlocked    Status = "blocked"
	StatusCancelled  Status = "cancelled"
	StatusUnknown    Status = "unknown"
	StatusSuperseded Status = "superseded"
	StatusAbandoned  Status = "abandoned"
)

type Correlation struct {
	SessionID       string `json:"session_id,omitempty"`
	ToolUseID       string `json:"tool_use_id,omitempty"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	SpanID          string `json:"span_id,omitempty"`
	ParentSpanID    string `json:"parent_span_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	ParentAgentID   string `json:"parent_agent_id,omitempty"`
	MessageID       string `json:"message_id,omitempty"`
	UUID            string `json:"uuid,omitempty"`
}

type Payload struct {
	Input  json.RawMessage `json:"input,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
	Hash   string          `json:"hash,omitempty"`
}

type Observation struct {
	ObsID       string         `json:"obs_id"`
	RunID       string         `json:"run_id"`
	ExecutionID string         `json:"execution_id"`
	Source      Source         `json:"source"`
	Kind        string         `json:"kind"`
	Correlation Correlation    `json:"correlation"`
	Attrs       map[string]any `json:"attrs,omitempty"`
	Payload     *Payload       `json:"payload,omitempty"`
	EventTime   time.Time      `json:"event_time"`
	ObservedAt  time.Time      `json:"observed_at"`
	Seq         uint64         `json:"seq"`
}

type SourceRef struct {
	Source     Source    `json:"source"`
	ObsID      string    `json:"obs_id"`
	ObservedAt time.Time `json:"observed_at"`
}

type Node struct {
	ID            string         `json:"id"`
	RunID         string         `json:"run_id"`
	Type          NodeType       `json:"type"`
	ParentID      string         `json:"parent_id,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	ParentAgentID string         `json:"parent_agent_id,omitempty"`
	SubagentType  string         `json:"subagent_type,omitempty"`
	Name          string         `json:"name,omitempty"`
	Status        Status         `json:"status,omitempty"`
	TStart        *time.Time     `json:"t_start,omitempty"`
	TEnd          *time.Time     `json:"t_end,omitempty"`
	DurationMS    *int64         `json:"duration_ms,omitempty"`
	TokensIn      *int64         `json:"tokens_in,omitempty"`
	TokensOut     *int64         `json:"tokens_out,omitempty"`
	CostUSD       *float64       `json:"cost_usd,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
	Payload       *Payload       `json:"payload,omitempty"`
	PayloadHash   string         `json:"payload_hash,omitempty"`
	Sources       []SourceRef    `json:"sources,omitempty"`
	Tier          string         `json:"tier,omitempty"`
	StepKey       string         `json:"step_key,omitempty"`
	StepKeyMethod string         `json:"step_key_method,omitempty"`
	Annotations   map[string]any `json:"annotations,omitempty"`
	Rev           uint64         `json:"rev,omitempty"`
}

type Edge struct {
	ID    string         `json:"id"`
	RunID string         `json:"run_id"`
	Type  EdgeType       `json:"type"`
	Src   string         `json:"src"`
	Dst   string         `json:"dst"`
	Attrs map[string]any `json:"attrs,omitempty"`
	Rev   uint64         `json:"rev,omitempty"`
}

type Run struct {
	ID         string         `json:"id"`
	SessionIDs []string       `json:"session_ids,omitempty"`
	ModelID    string         `json:"model_id,omitempty"`
	Status     Status         `json:"status,omitempty"`
	EndReason  string         `json:"end_reason,omitempty"`
	LastSeq    uint64         `json:"last_seq,omitempty"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	EndedAt    *time.Time     `json:"ended_at,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
}

type QuarantineRecord struct {
	Raw      []byte    `json:"raw,omitempty"`
	HookType string    `json:"hook_type,omitempty"`
	Err      string    `json:"err,omitempty"`
	At       time.Time `json:"at"`
}

type TailCursor struct {
	Path        string `json:"path"`
	Offset      int64  `json:"offset"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Size        int64  `json:"size"`
	Mtime       int64  `json:"mtime"`
}

type SubagentMeta struct {
	SessionID   string
	AgentID     string
	ToolUseID   string
	AgentType   string
	Description string
}
