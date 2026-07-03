package redact

import (
	"encoding/json"

	"github.com/realkarych/catacomb/model"
)

func Observation(o model.Observation) model.Observation {
	o.Attrs = redactAttrs(o.Attrs)
	o.Payload = redactPayload(o.Payload)
	return o
}

func redactPayload(p *model.Payload) *model.Payload {
	if p == nil {
		return nil
	}
	pc := *p
	if len(p.Input) > 0 {
		pc.Input = append(json.RawMessage(nil), Redact(p.Input).Data...)
	}
	if len(p.Output) > 0 {
		pc.Output = append(json.RawMessage(nil), Redact(p.Output).Data...)
	}
	pc.Hash = model.HashPayload(&pc)
	return &pc
}

func redactAttrs(attrs map[string]any) map[string]any {
	if attrs == nil {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if sv, ok := v.(string); ok {
			out[k] = redactString(sv)
		} else {
			out[k] = v
		}
	}
	return out
}
