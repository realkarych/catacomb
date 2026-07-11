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
	assert.Equal(t, redact.DefaultMaxBytes, p.MaxBytes)
}

func TestPolicyRedactModeRedactsUnderCap(t *testing.T) {
	r := redact.DefaultPolicy().Observation(secretObservation())
	want := redact.Observation(secretObservation())
	assert.Equal(t, want, r)
}

func TestPolicyCapsOversizedSideAfterRedaction(t *testing.T) {
	p := redact.Policy{MaxBytes: 16}
	r := p.Observation(secretObservation())
	redacted := redact.Observation(secretObservation())
	assert.Equal(t, refFor(redacted.Payload.Input), string(r.Payload.Input))
	assert.Equal(t, refFor(redacted.Payload.Output), string(r.Payload.Output))
	assert.Equal(t, redacted.Payload.Hash, r.Payload.Hash)
}

func TestPolicyNilPayload(t *testing.T) {
	r := redact.DefaultPolicy().Observation(model.Observation{ObsID: "o2"})
	assert.Nil(t, r.Payload)
}

func TestPolicyZeroValueNormalizesToDefault(t *testing.T) {
	var p redact.Policy
	r := p.Observation(secretObservation())
	assert.Equal(t, redact.DefaultPolicy().Observation(secretObservation()), r)
}

func TestPolicyIdempotent(t *testing.T) {
	policies := []redact.Policy{
		redact.DefaultPolicy(),
		{MaxBytes: 8},
	}
	for _, p := range policies {
		once := p.Observation(secretObservation())
		twice := p.Observation(once)
		assert.Equal(t, once, twice, "max %d", p.MaxBytes)
	}
}

func TestPolicyWrapsRefLookalikeAsRefOfRedactedLength(t *testing.T) {
	in := json.RawMessage(`"‹ref:1,ab›garbage-AKIAIOSFODNN7EXAMPLE"`)
	p := redact.Policy{MaxBytes: 8}
	r := p.Observation(model.Observation{Payload: &model.Payload{Input: in}})

	redacted := redact.Redact(in).Data
	assert.Equal(t, refFor(redacted), string(r.Payload.Input))
	assert.NotContains(t, string(r.Payload.Input), "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, string(r.Payload.Input), fmt.Sprintf("‹ref:%d,", len(redacted)))
}

func TestPolicyDropsPreRedactionHashWhenOneSideIsForgedRef(t *testing.T) {
	rawInput := json.RawMessage(`{"command":"psql postgres://user:pw@host/db"}`)
	forgedRef := json.RawMessage(`"‹ref:5,0123456789abcdef›"`)

	incomingHash := model.HashPayload(&model.Payload{Input: rawInput, Output: forgedRef})
	wantHash := model.HashPayload(&model.Payload{
		Input:  json.RawMessage(redact.Redact(rawInput).Data),
		Output: json.RawMessage(redact.Redact(forgedRef).Data),
	})
	require.NotEqual(t, incomingHash, wantHash)

	p := redact.Policy{MaxBytes: redact.DefaultMaxBytes}
	o := p.Observation(model.Observation{Payload: &model.Payload{Input: rawInput, Output: forgedRef, Hash: incomingHash}})
	assert.Equal(t, wantHash, o.Payload.Hash)
	assert.NotEqual(t, incomingHash, o.Payload.Hash)
}

func TestPolicyRecomputesHashWhenOtherSideIsRawSecret(t *testing.T) {
	forgedRef := json.RawMessage(`"‹ref:5,0123456789abcdef›"`)
	rawOut := json.RawMessage(`{"password":"hunter2"}`)
	incomingHash := model.HashPayload(&model.Payload{Input: forgedRef, Output: rawOut})
	wantHash := model.HashPayload(&model.Payload{
		Input:  json.RawMessage(redact.Redact(forgedRef).Data),
		Output: json.RawMessage(redact.Redact(rawOut).Data),
	})
	require.NotEqual(t, incomingHash, wantHash)

	p := redact.Policy{MaxBytes: redact.DefaultMaxBytes}
	o := p.Observation(model.Observation{Payload: &model.Payload{Input: forgedRef, Output: rawOut, Hash: incomingHash}})
	assert.Equal(t, wantHash, o.Payload.Hash)
	assert.NotEqual(t, incomingHash, o.Payload.Hash)
}

func TestPolicyForgedHighEntropyRefRecomputesHashAndStaysIdempotent(t *testing.T) {
	smuggled := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	forged := json.RawMessage(`"‹ref:1,` + smuggled + `›"`)
	rawSecret := json.RawMessage(`{"password":"hunter2"}`)

	cases := []struct {
		name string
		out  json.RawMessage
	}{
		{"empty-other-side", nil},
		{"raw-secret-other-side", rawSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantHash := model.HashPayload(&model.Payload{
				Input:  json.RawMessage(redact.Redact(forged).Data),
				Output: json.RawMessage(redact.Redact(tc.out).Data),
			})

			mk := func() *model.Payload {
				return &model.Payload{Input: forged, Output: tc.out, Hash: "incoming-stale-hash"}
			}

			p := redact.Policy{MaxBytes: redact.DefaultMaxBytes}
			once := p.Observation(model.Observation{Payload: mk()})
			assert.Equal(t, wantHash, once.Payload.Hash)
			assert.NotEqual(t, "incoming-stale-hash", once.Payload.Hash)
			assert.NotContains(t, string(once.Payload.Input), smuggled)
			twice := p.Observation(once)
			assert.Equal(t, once, twice)

			ro := redact.Observation(model.Observation{Payload: mk()})
			assert.Equal(t, wantHash, ro.Payload.Hash)
			assert.Equal(t, ro, redact.Observation(ro))
		})
	}
}

func TestPolicyCanonicalizesWhitespaceJSONBeforeHash(t *testing.T) {
	spacey := json.RawMessage(`{"command": "ls -la"}`)
	p := redact.DefaultPolicy()
	r := p.Observation(model.Observation{Payload: &model.Payload{Input: spacey, Hash: "stale"}})
	require.NotNil(t, r.Payload)
	assert.Equal(t, `{"command":"ls -la"}`, string(r.Payload.Input))
	assert.Equal(t, model.HashPayload(r.Payload), r.Payload.Hash)
}

func TestPolicyPassesThroughNonJSONPayloadUnchanged(t *testing.T) {
	free := json.RawMessage("plain text, not json")
	p := redact.DefaultPolicy()
	r := p.Observation(model.Observation{Payload: &model.Payload{Input: free}})
	require.NotNil(t, r.Payload)
	assert.Equal(t, "plain text, not json", string(r.Payload.Input))
	assert.Equal(t, model.HashPayload(r.Payload), r.Payload.Hash)
}

func TestPolicyBinaryPayloadBecomesBinaryRefNotDoubleWrapped(t *testing.T) {
	p := redact.Policy{MaxBytes: redact.DefaultMaxBytes}
	o := model.Observation{Payload: &model.Payload{Input: json.RawMessage{0xff, 0xfe, 0x01}}}
	once := p.Observation(o)
	assert.True(t, strings.HasPrefix(string(once.Payload.Input), `"‹binary:`))
	twice := p.Observation(once)
	assert.Equal(t, once, twice)
}
