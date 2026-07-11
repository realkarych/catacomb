package redact

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/realkarych/catacomb/model"
)

const DefaultMaxBytes = 262144

type Policy struct {
	MaxBytes int
}

func DefaultPolicy() Policy {
	return Policy{MaxBytes: DefaultMaxBytes}
}

func (p Policy) normalized() Policy {
	if p.MaxBytes <= 0 {
		p.MaxBytes = DefaultMaxBytes
	}
	return p
}

func (p Policy) Observation(o model.Observation) model.Observation {
	p = p.normalized()
	o.Attrs = redactAttrs(o.Attrs)
	o.Payload = p.payload(o.Payload)
	return o
}

var reTypedRef = regexp.MustCompile(`^"` + typedRefCorePattern + `"$`)

func (p Policy) payload(pl *model.Payload) *model.Payload {
	if pl == nil {
		return nil
	}
	in := json.RawMessage(Redact(pl.Input).Data)
	out := json.RawMessage(Redact(pl.Output).Data)
	in = canonicalizeJSON(in)
	out = canonicalizeJSON(out)
	pc := model.Payload{Hash: pl.Hash}
	if !preserveIncomingHash(pl.Input, pl.Output) {
		hp := model.Payload{Input: in, Output: out}
		pc.Hash = model.HashPayload(&hp)
	}
	pc.Input = p.capSide(in)
	pc.Output = p.capSide(out)
	return &pc
}

func preserveIncomingHash(in, out json.RawMessage) bool {
	return (len(in) > 0 || len(out) > 0) && sideIsRefOrEmpty(in) && sideIsRefOrEmpty(out)
}

func sideIsRefOrEmpty(data json.RawMessage) bool {
	return len(data) == 0 || reTypedRef.Match(data)
}

func (p Policy) capSide(data json.RawMessage) json.RawMessage {
	if sideIsRefOrEmpty(data) {
		return data
	}
	if len(data) > p.MaxBytes {
		return typedRef(data)
	}
	return data
}

func typedRef(data []byte) json.RawMessage {
	h := sha256.Sum256(data)
	return fmt.Appendf(nil, `"‹ref:%d,%x›"`, len(data), h[:8])
}
