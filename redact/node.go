package redact

import (
	"github.com/realkarych/catacomb/model"
)

func Node(n *model.Node) *model.Node {
	if n == nil {
		return nil
	}
	nc := *n
	nc.Name = redactString(n.Name)
	nc.SubagentType = redactString(n.SubagentType)
	nc.Attrs = redactAttrs(n.Attrs)
	if n.Payload != nil {
		nc.Payload = redactPayload(n.Payload)
		nc.PayloadHash = nc.Payload.Hash
	}
	return &nc
}

func redactString(s string) string {
	if s == "" {
		return s
	}
	return string(Redact([]byte(s)).Data)
}
