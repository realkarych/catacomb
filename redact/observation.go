package redact

import (
	"encoding/json"
	"strings"

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
		sv, ok := v.(string)
		switch {
		case !ok:
			out[k] = v
		case isChecksumKey(k) && isHexDigest(sv):
			out[k] = sv
		default:
			out[k] = redactString(sv)
		}
	}
	return out
}

func isChecksumKey(k string) bool {
	return strings.HasSuffix(normalizeKey(k), "hash")
}

func isHexDigest(s string) bool {
	if len(s) < 40 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		isDigit := r >= '0' && r <= '9'
		isLower := r >= 'a' && r <= 'f'
		isUpper := r >= 'A' && r <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}
