package redact

import "github.com/realkarych/catacomb/model"

func Node(n *model.Node) *model.Node {
	if n == nil {
		return nil
	}
	nc := *n
	if n.Payload != nil {
		pc := *n.Payload
		if len(n.Payload.Input) > 0 {
			pc.Input = Redact(n.Payload.Input).Data
		}
		if len(n.Payload.Output) > 0 {
			pc.Output = Redact(n.Payload.Output).Data
		}
		nc.Payload = &pc
	}
	return &nc
}
