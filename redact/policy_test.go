package redact_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func refFor(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf(`"‹ref:%d,%x›"`, len(data), h[:8])
}

func TestDefaultPolicy(t *testing.T) {
	p := redact.DefaultPolicy()
	assert.Equal(t, redact.ModeRedact, p.Mode)
	assert.Equal(t, redact.DefaultMaxBytes, p.MaxBytes)
}

func TestPolicyRedactModeRedactsUnderCap(t *testing.T) {
	r := redact.DefaultPolicy().Observation(secretObservation())
	want := redact.Observation(secretObservation())
	assert.Equal(t, want, r)
}

func TestPolicyCapsOversizedSideAfterRedaction(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRedact, MaxBytes: 16}
	r := p.Observation(secretObservation())
	redacted := redact.Observation(secretObservation())
	assert.Equal(t, refFor(redacted.Payload.Input), string(r.Payload.Input))
	assert.Equal(t, refFor(redacted.Payload.Output), string(r.Payload.Output))
	assert.Equal(t, redacted.Payload.Hash, r.Payload.Hash)
}

func TestPolicyRefsModeStoresRefsAndHashOnly(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRefs, MaxBytes: redact.DefaultMaxBytes}
	r := p.Observation(secretObservation())
	redacted := redact.Observation(secretObservation())
	assert.Equal(t, refFor(redacted.Payload.Input), string(r.Payload.Input))
	assert.Equal(t, refFor(redacted.Payload.Output), string(r.Payload.Output))
	assert.Equal(t, redacted.Payload.Hash, r.Payload.Hash)
	assert.NotContains(t, string(r.Payload.Input), "redacted:connection-string")
}

func TestPolicyAllModeSkipsRedactionButCaps(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeAll, MaxBytes: redact.DefaultMaxBytes}
	o := secretObservation()
	r := p.Observation(o)
	assert.Equal(t, o, r)

	tiny := redact.Policy{Mode: redact.ModeAll, MaxBytes: 8}
	capped := tiny.Observation(o)
	assert.Equal(t, refFor(o.Payload.Input), string(capped.Payload.Input))
	assert.Equal(t, "stale-pre-redaction", capped.Payload.Hash)
	assert.Equal(t, "use AKIAIOSFODNN7EXAMPLE", capped.Attrs["prompt"])
}

func TestPolicyZeroValueNormalizesToRedact(t *testing.T) {
	var p redact.Policy
	r := p.Observation(secretObservation())
	assert.Equal(t, redact.DefaultPolicy().Observation(secretObservation()), r)
}

func TestPolicyIdempotentAcrossModes(t *testing.T) {
	policies := []redact.Policy{
		redact.DefaultPolicy(),
		{Mode: redact.ModeRedact, MaxBytes: 8},
		{Mode: redact.ModeRefs, MaxBytes: 64},
		{Mode: redact.ModeAll, MaxBytes: 8},
	}
	for _, p := range policies {
		once := p.Observation(secretObservation())
		twice := p.Observation(once)
		assert.Equal(t, once, twice, "mode %q max %d", p.Mode, p.MaxBytes)
	}
}

func TestPolicyNode(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRedact, MaxBytes: 16}
	assert.Nil(t, p.Node(nil))

	bare := &model.Node{Name: "clean"}
	assert.Equal(t, "clean", p.Node(bare).Name)

	n := &model.Node{
		Name:    "Bash",
		Payload: &model.Payload{Input: json.RawMessage(`{"command":"psql postgres://kesha:pw@localhost/db"}`)},
	}
	rn := p.Node(n)
	require.NotNil(t, rn.Payload)
	assert.True(t, strings.HasPrefix(string(rn.Payload.Input), `"‹ref:`))
	assert.Equal(t, rn.Payload.Hash, rn.PayloadHash)
	assert.Contains(t, string(n.Payload.Input), "kesha")
	again := p.Node(rn)
	assert.Equal(t, rn, again)
}

func TestPolicyNodeAllModeSkipsRedactionButCaps(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeAll, MaxBytes: 8}
	n := &model.Node{
		Name:         "AKIAIOSFODNN7EXAMPLE",
		SubagentType: "reviewer",
		Attrs:        map[string]any{"prompt": "use AKIAIOSFODNN7EXAMPLE"},
		Payload: &model.Payload{
			Input: json.RawMessage(`{"command":"psql postgres://kesha:pw@localhost/db"}`),
			Hash:  "stale",
		},
	}
	rn := p.Node(n)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", rn.Name)
	assert.Equal(t, "reviewer", rn.SubagentType)
	assert.Equal(t, "use AKIAIOSFODNN7EXAMPLE", rn.Attrs["prompt"])
	assert.True(t, strings.HasPrefix(string(rn.Payload.Input), `"‹ref:`))
	assert.Equal(t, "stale", rn.Payload.Hash)
	assert.Equal(t, "stale", rn.PayloadHash)

	twice := p.Node(rn)
	assert.Equal(t, rn, twice)
}

func TestPolicyBinaryPayloadBecomesBinaryRefNotDoubleWrapped(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRefs, MaxBytes: redact.DefaultMaxBytes}
	o := model.Observation{Payload: &model.Payload{Input: json.RawMessage{0xff, 0xfe, 0x01}}}
	once := p.Observation(o)
	assert.True(t, strings.HasPrefix(string(once.Payload.Input), `"‹binary:`))
	twice := p.Observation(once)
	assert.Equal(t, once, twice)
}
