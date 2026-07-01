package redact

import (
	"encoding/json"

	"github.com/realkarych/catacomb/model"
)

func Node(n *model.Node) *model.Node {
	if n == nil {
		return nil
	}
	nc := *n
	nc.Name = redactString(n.Name)
	if n.Attrs != nil {
		nc.Attrs = make(map[string]any, len(n.Attrs))
		for k, v := range n.Attrs {
			if sv, ok := v.(string); ok {
				nc.Attrs[k] = redactString(sv)
			} else {
				nc.Attrs[k] = v
			}
		}
	}
	if n.Payload != nil {
		pc := *n.Payload
		if len(n.Payload.Input) > 0 {
			pc.Input = append(json.RawMessage(nil), Redact(n.Payload.Input).Data...)
		}
		if len(n.Payload.Output) > 0 {
			pc.Output = append(json.RawMessage(nil), Redact(n.Payload.Output).Data...)
		}
		nc.Payload = &pc
	}
	return &nc
}

func redactString(s string) string {
	if s == "" {
		return s
	}
	return string(Redact([]byte(s)).Data)
}
