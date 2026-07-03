package redact

import (
	"bytes"
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
		pc.Input = canonicalizeJSON(Redact(p.Input).Data)
	}
	if len(p.Output) > 0 {
		pc.Output = canonicalizeJSON(Redact(p.Output).Data)
	}
	pc.Hash = model.HashPayload(&pc)
	return &pc
}

func canonicalizeJSON(data []byte) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return append(json.RawMessage(nil), data...)
	}
	return json.RawMessage(buf.Bytes())
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
	return k == "hash" || strings.HasSuffix(k, "_hash")
}

func isHexDigest(s string) bool {
	switch len(s) {
	case 40, 64, 96, 128:
	default:
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
