package tui

import "encoding/json"

type Node struct {
	ID          string         `json:"id"`
	RunID       string         `json:"run_id"`
	Type        string         `json:"type"`
	ParentID    string         `json:"parent_id,omitempty"`
	Name        string         `json:"name,omitempty"`
	Status      string         `json:"status,omitempty"`
	TStart      *string        `json:"t_start,omitempty"`
	TEnd        *string        `json:"t_end,omitempty"`
	DurationMS  *int64         `json:"duration_ms,omitempty"`
	TokensIn    *int64         `json:"tokens_in,omitempty"`
	TokensOut   *int64         `json:"tokens_out,omitempty"`
	CostUSD     *float64       `json:"cost_usd,omitempty"`
	Attrs       map[string]any `json:"attrs,omitempty"`
	PayloadHash string         `json:"payload_hash,omitempty"`
	Sources     []SourceRef    `json:"sources,omitempty"`
	Tier        string         `json:"tier,omitempty"`
	Rev         uint64         `json:"rev,omitempty"`
}

type SourceRef struct {
	Source     string `json:"source"`
	ObsID      string `json:"obs_id"`
	ObservedAt string `json:"observed_at"`
}

type Edge struct {
	ID    string `json:"id"`
	RunID string `json:"run_id"`
	Type  string `json:"type"`
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Rev   uint64 `json:"rev,omitempty"`
}

type SseEvent struct {
	Kind        string `json:"kind"`
	Rev         uint64 `json:"rev"`
	RunID       string `json:"run_id,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	Node        *Node  `json:"node,omitempty"`
	Edge        *Edge  `json:"edge,omitempty"`
	OldID       string `json:"old_id,omitempty"`
	NewID       string `json:"new_id,omitempty"`
}

type SessionSummary struct {
	Session        string         `json:"session"`
	Status         string         `json:"status"`
	StartedAt      string         `json:"started_at,omitempty"`
	EndedAt        string         `json:"ended_at,omitempty"`
	DurationMS     *int64         `json:"duration_ms,omitempty"`
	TokensIn       int64          `json:"tokens_in"`
	TokensOut      int64          `json:"tokens_out"`
	CostUSD        *float64       `json:"cost_usd,omitempty"`
	CostSource     string         `json:"cost_source"`
	NodeCount      int            `json:"node_count"`
	ToolCount      int            `json:"tool_count"`
	ErrorCount     int            `json:"error_count"`
	ModelID        string         `json:"model_id,omitempty"`
	RunIDs         []string       `json:"run_ids"`
	CountsByType   map[string]int `json:"counts_by_type"`
	CountsByStatus map[string]int `json:"counts_by_status"`
	ErrorRate      float64        `json:"error_rate"`
}

type RedactionFinding struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type PayloadView struct {
	NodeID      string             `json:"node_id"`
	PayloadHash string             `json:"payload_hash,omitempty"`
	Input       json.RawMessage    `json:"input,omitempty"`
	Output      json.RawMessage    `json:"output,omitempty"`
	Redactions  []RedactionFinding `json:"redactions"`
	Redacted    bool               `json:"redacted"`
}
