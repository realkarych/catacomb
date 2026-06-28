package evalview

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

type traceStart struct {
	Type             string `json:"type"`
	TraceID          string `json:"trace_id"`
	TraceSpecVersion string `json:"trace_spec_version"`
}

type traceEnd struct {
	Type    string `json:"type"`
	TraceID string `json:"trace_id"`
}

type llmInfo struct {
	Provider     string   `json:"provider"`
	Model        string   `json:"model,omitempty"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CostUSD      *float64 `json:"cost_usd,omitempty"`
}

type toolInfo struct {
	ToolName        string `json:"tool_name"`
	ToolArgsBytes   int    `json:"tool_args_bytes"`
	ToolResultBytes int    `json:"tool_result_bytes"`
	ToolSuccess     bool   `json:"tool_success"`
}

type span struct {
	Type         string    `json:"type"`
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	ParentSpanID string    `json:"parent_span_id,omitempty"`
	SpanType     string    `json:"span_type"`
	Name         string    `json:"name,omitempty"`
	StartTime    string    `json:"start_time,omitempty"`
	EndTime      string    `json:"end_time,omitempty"`
	LatencyMS    *float64  `json:"latency_ms,omitempty"`
	Status       string    `json:"status,omitempty"`
	LLM          *llmInfo  `json:"llm,omitempty"`
	Tool         *toolInfo `json:"tool,omitempty"`
}

func writeTrace(w io.Writer, runID string, nodes []*model.Node, edges []*model.Edge) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(traceStart{Type: "trace_start", TraceID: runID, TraceSpecVersion: "1.0"}); err != nil {
		return fmt.Errorf("evalview.writeTrace: %w", err)
	}

	pm := parentMap(edges)

	sorted := make([]*model.Node, len(nodes))
	copy(sorted, nodes)
	sortNodes(sorted)

	for _, n := range sorted {
		if err := enc.Encode(nodeToSpan(runID, n, pm)); err != nil {
			return fmt.Errorf("evalview.writeTrace: %w", err)
		}
	}

	if err := enc.Encode(traceEnd{Type: "trace_end", TraceID: runID}); err != nil {
		return fmt.Errorf("evalview.writeTrace: %w", err)
	}

	return nil
}

func WriteAll(w io.Writer, nodes []*model.Node, edges []*model.Edge) error {
	order, groups := groupByRun(nodes)
	for _, runID := range order {
		if err := writeTrace(w, runID, groups[runID], edges); err != nil {
			return err
		}
	}
	return nil
}

func nodeToSpan(traceID string, n *model.Node, pm map[string]string) span {
	s := span{
		Type:         "span",
		TraceID:      traceID,
		SpanID:       n.ID,
		ParentSpanID: pm[n.ID],
		SpanType:     spanType(n),
		Name:         n.Name,
		StartTime:    formatTime(n.TStart),
		EndTime:      formatTime(n.TEnd),
		LatencyMS:    latency(n),
		Status:       string(n.Status),
	}

	switch n.Type {
	case model.NodeAssistantTurn:
		s.LLM = &llmInfo{
			Provider:     "anthropic",
			Model:        attrString(n, "model"),
			InputTokens:  deref(n.TokensIn),
			OutputTokens: deref(n.TokensOut),
			CostUSD:      n.CostUSD,
		}
	case model.NodeToolCall, model.NodeMCPCall:
		s.Tool = &toolInfo{
			ToolName:        n.Name,
			ToolArgsBytes:   redactedLen(payloadInput(n)),
			ToolResultBytes: redactedLen(payloadOutput(n)),
			ToolSuccess:     n.Status == model.StatusOK,
		}
	}

	return s
}

func spanType(n *model.Node) string {
	switch n.Type {
	case model.NodeAssistantTurn:
		return "llm"
	case model.NodeToolCall:
		return "tool"
	case model.NodeMCPCall:
		return "mcp"
	default:
		return "agent"
	}
}

func attrString(n *model.Node, key string) string {
	if n.Attrs == nil {
		return ""
	}
	v, ok := n.Attrs[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func latency(n *model.Node) *float64 {
	if n.DurationMS != nil {
		v := float64(*n.DurationMS)
		return &v
	}
	if n.TEnd != nil && n.TStart != nil {
		v := float64(n.TEnd.Sub(*n.TStart).Milliseconds())
		return &v
	}
	return nil
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func redactedLen(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	return len(redact.Redact(raw).Data)
}

func payloadInput(n *model.Node) []byte {
	if n.Payload == nil {
		return nil
	}
	return n.Payload.Input
}

func payloadOutput(n *model.Node) []byte {
	if n.Payload == nil {
		return nil
	}
	return n.Payload.Output
}

func parentMap(edges []*model.Edge) map[string]string {
	pm := make(map[string]string)
	for _, e := range edges {
		if e.Type == model.EdgeParentChild {
			pm[e.Dst] = e.Src
		}
	}
	return pm
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func sortNodes(nodes []*model.Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		ti, tj := startUnix(nodes[i]), startUnix(nodes[j])
		if ti != tj {
			return ti < tj
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func startUnix(n *model.Node) int64 {
	if n.TStart == nil {
		return 0
	}
	return n.TStart.UnixNano()
}

func groupByRun(nodes []*model.Node) ([]string, map[string][]*model.Node) {
	groups := make(map[string][]*model.Node)
	seen := make(map[string]bool)
	var order []string
	for _, n := range nodes {
		if !seen[n.RunID] {
			seen[n.RunID] = true
			order = append(order, n.RunID)
		}
		groups[n.RunID] = append(groups[n.RunID], n)
	}
	sort.Strings(order)
	return order, groups
}
