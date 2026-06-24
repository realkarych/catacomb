package tui

func NodeTypeLabel(t string) string {
	switch t {
	case "session":
		return "session"
	case "user_prompt":
		return "user prompt"
	case "assistant_turn":
		return "assistant turn"
	case "tool_call":
		return "tool call"
	case "subagent":
		return "subagent"
	case "mcp_call":
		return "mcp call"
	case "hook_event":
		return "hook event"
	default:
		return "marker"
	}
}

func Provenance(n Node) string {
	if n.CostUSD == nil {
		return "unknown"
	}
	if src, ok := n.Attrs["cost_source"]; ok {
		if s, ok2 := src.(string); ok2 && s == "reported" {
			return "reported"
		}
	}
	return "estimated"
}

func StatusLabel(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func StatusGlyph(s string) string {
	switch s {
	case "ok":
		return "●"
	case "running":
		return "◐"
	case "error":
		return "✗"
	case "pending":
		return "○"
	case "blocked":
		return "⊘"
	case "cancelled":
		return "⊘"
	default:
		return "·"
	}
}
