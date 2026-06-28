package agentevals

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func Build(nodes []*model.Node, edges []*model.Edge) []Message {
	byID := make(map[string]*model.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}

	parentOf := make(map[string]string)
	children := make(map[string][]*model.Node)
	for _, e := range edges {
		if e.Type != model.EdgeParentChild {
			continue
		}
		parentOf[e.Dst] = e.Src
		if p, ok := byID[e.Src]; ok {
			_ = p
		}
		if child, ok := byID[e.Dst]; ok {
			children[e.Src] = append(children[e.Src], child)
		}
	}

	var topLevel []*model.Node
	for _, n := range nodes {
		switch n.Type {
		case model.NodeUserPrompt, model.NodeAssistantTurn:
			topLevel = append(topLevel, n)
		case model.NodeToolCall, model.NodeMCPCall:
			pid := parentOf[n.ID]
			if p, ok := byID[pid]; ok && p.Type == model.NodeAssistantTurn {
				continue
			}
			topLevel = append(topLevel, n)
		}
	}

	sortNodes(topLevel)

	var msgs []Message
	for _, n := range topLevel {
		switch n.Type {
		case model.NodeUserPrompt:
			msgs = append(msgs, Message{Role: "user", Content: textOf(payloadInput(n))})
		case model.NodeAssistantTurn:
			msgs = append(msgs, assistantMessages(n, children[n.ID])...)
		case model.NodeToolCall, model.NodeMCPCall:
			msgs = append(msgs, Message{Role: "tool", Content: textOf(payloadOutput(n)), ToolCallID: n.ID})
		}
	}
	return msgs
}

func assistantMessages(turn *model.Node, toolNodes []*model.Node) []Message {
	sortNodes(toolNodes)

	calls := make([]ToolCall, 0, len(toolNodes))
	for _, tn := range toolNodes {
		calls = append(calls, ToolCall{
			ID:   tn.ID,
			Type: "function",
			Function: ToolFunction{
				Name:      tn.Name,
				Arguments: string(redactRaw(payloadInput(tn))),
			},
		})
	}

	var out []Message
	assistantMsg := Message{
		Role:    "assistant",
		Content: textOf(payloadOutput(turn)),
	}
	if len(calls) > 0 {
		assistantMsg.ToolCalls = calls
	}
	out = append(out, assistantMsg)

	for _, tn := range toolNodes {
		out = append(out, Message{
			Role:       "tool",
			Content:    textOf(payloadOutput(tn)),
			ToolCallID: tn.ID,
		})
	}
	return out
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

func redactRaw(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	return redact.Redact(raw).Data
}

func textOf(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	r := redactRaw(raw)
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return s
	}
	return string(r)
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
	var order []string
	seen := make(map[string]bool)
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

func WriteAll(w io.Writer, nodes []*model.Node, edges []*model.Edge) error {
	order, groups := groupByRun(nodes)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	var result [][]Message
	for _, runID := range order {
		msgs := Build(groups[runID], edges)
		if msgs == nil {
			msgs = []Message{}
		}
		result = append(result, msgs)
	}
	if result == nil {
		result = [][]Message{}
	}

	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("agentevals.WriteAll: %w", err)
	}
	return nil
}
