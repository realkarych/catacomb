package redact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func secretObservation() model.Observation {
	return model.Observation{
		ObsID: "o1",
		Attrs: map[string]any{"prompt": "use AKIAIOSFODNN7EXAMPLE", "count": 3},
		Payload: &model.Payload{
			Input:  json.RawMessage(`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}`),
			Output: json.RawMessage(`{"password":"hunter2"}`),
			Hash:   "stale-pre-redaction",
		},
	}
}

func TestObservationRedactsPayloadAndAttrs(t *testing.T) {
	o := secretObservation()
	r := redact.Observation(o)
	assert.Equal(t, "use ‹redacted:aws-key›", r.Attrs["prompt"])
	assert.Equal(t, 3, r.Attrs["count"])
	assert.Contains(t, string(r.Payload.Input), "‹redacted:connection-string›")
	assert.Contains(t, string(r.Payload.Output), "‹redacted:")
	assert.NotContains(t, string(r.Payload.Input), "kesha_dev_password")
}

func TestObservationRecomputesPostRedactionHash(t *testing.T) {
	r := redact.Observation(secretObservation())
	require.NotNil(t, r.Payload)
	assert.Equal(t, model.HashPayload(r.Payload), r.Payload.Hash)
	assert.NotEqual(t, "stale-pre-redaction", r.Payload.Hash)
}

func TestObservationPreservesHashWhenAllNonEmptySidesAreTypedRefs(t *testing.T) {
	o := model.Observation{
		ObsID: "o-ref",
		Payload: &model.Payload{
			Input: json.RawMessage(`"‹ref:64,00112233aabbccdd›"`),
			Hash:  "content-hash-by-design",
		},
	}
	r := redact.Observation(o)
	require.NotNil(t, r.Payload)
	assert.Equal(t, "content-hash-by-design", r.Payload.Hash)
	assert.Equal(t, string(o.Payload.Input), string(r.Payload.Input))
}

func TestObservationRecomputesHashWhenOnlyOneSideIsTypedRef(t *testing.T) {
	o := model.Observation{
		ObsID: "o-mixed",
		Payload: &model.Payload{
			Input:  json.RawMessage(`"‹ref:64,00112233aabbccdd›"`),
			Output: json.RawMessage(`{"result":"ok"}`),
			Hash:   "content-hash-by-design",
		},
	}
	r := redact.Observation(o)
	require.NotNil(t, r.Payload)
	assert.Equal(t, model.HashPayload(r.Payload), r.Payload.Hash)
	assert.NotEqual(t, "content-hash-by-design", r.Payload.Hash)
}

func TestObservationDoesNotMutateInput(t *testing.T) {
	o := secretObservation()
	_ = redact.Observation(o)
	assert.Equal(t, "use AKIAIOSFODNN7EXAMPLE", o.Attrs["prompt"])
	assert.Contains(t, string(o.Payload.Input), "kesha_dev_password")
	assert.Equal(t, "stale-pre-redaction", o.Payload.Hash)
}

func TestObservationNilPayloadAndAttrs(t *testing.T) {
	r := redact.Observation(model.Observation{ObsID: "o2"})
	assert.Nil(t, r.Payload)
	assert.Nil(t, r.Attrs)
}

func TestObservationIdempotent(t *testing.T) {
	once := redact.Observation(secretObservation())
	twice := redact.Observation(once)
	assert.Equal(t, once, twice)
}

func TestObservationPreservesChecksumAttrs(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	o := model.Observation{Attrs: map[string]any{
		"prompts_hash":         digest,
		"catacomb_config_hash": strings.Repeat("0f", 20),
		"boot_hash":            "use AKIAIOSFODNN7EXAMPLE",
		"tree_hash":            "gK3xZ7tRv9yB2kW5jN8mC4pD6fH1sL0qGt3Xz7WrV9Yb2Uw5Jn8Mc4Pd6Fh1Sl0e",
		"entropy":              "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
	}}
	r := redact.Observation(o)
	assert.Equal(t, digest, r.Attrs["prompts_hash"])
	assert.Equal(t, strings.Repeat("0f", 20), r.Attrs["catacomb_config_hash"])
	assert.Equal(t, "use ‹redacted:aws-key›", r.Attrs["boot_hash"])
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["tree_hash"])
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["entropy"])
}

func TestChecksumCarveoutExemptsBareHashKey(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	r := redact.Observation(model.Observation{Attrs: map[string]any{"hash": digest}})
	assert.Equal(t, digest, r.Attrs["hash"])
}

func TestChecksumCarveoutRejectsUppercaseHex(t *testing.T) {
	upper := "9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"
	r := redact.Observation(model.Observation{Attrs: map[string]any{"prompts_hash": upper}})
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["prompts_hash"])
}

func TestChecksumCarveoutRejectsMixedCaseHex(t *testing.T) {
	mixed := "9f86D081884c7D659a2Feaa0c55aD015a3bF4f1b2b0b822cD15d6c15b0f00A08"
	r := redact.Observation(model.Observation{Attrs: map[string]any{"skills_hash": mixed}})
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["skills_hash"])
}

func TestChecksumCarveoutRejectsNonDigestLengths(t *testing.T) {
	hex50 := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd1"
	hex129 := "ee26b0dd4af7e749aa1a8ee3c10ae9923f618980772e473f8819a5d4940e0db27ac185f8a0e1d5f84f88bc887fd67b143732c304cc5fa9ad8e6f57f50028a8ff0"
	r := redact.Observation(model.Observation{Attrs: map[string]any{
		"prompts_hash": hex50,
		"skills_hash":  hex129,
	}})
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["prompts_hash"])
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["skills_hash"])
}

func TestChecksumCarveoutLowEntropyNonDigestSurvivesEntropyGate(t *testing.T) {
	r := redact.Observation(model.Observation{Attrs: map[string]any{
		"prompts_hash": strings.Repeat("a", 50),
	}})
	assert.Equal(t, strings.Repeat("a", 50), r.Attrs["prompts_hash"])
}

func TestChecksumCarveoutAppliesToAnyHashKeyByDesign(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	r := redact.Observation(model.Observation{Attrs: map[string]any{"password_hash": digest}})
	assert.Equal(t, digest, r.Attrs["password_hash"])
}

func TestObservationCanonicalizesWhitespaceJSONBeforeHash(t *testing.T) {
	o := model.Observation{Payload: &model.Payload{
		Input: json.RawMessage(`{"file": "main.go"}`),
		Hash:  "stale",
	}}
	r := redact.Observation(o)
	require.NotNil(t, r.Payload)
	assert.Equal(t, `{"file":"main.go"}`, string(r.Payload.Input))
	assert.Equal(t, model.HashPayload(r.Payload), r.Payload.Hash)
}
