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
		"tree_hash":            strings.Repeat("g", 64),
		"entropy":              strings.Repeat("ab", 32),
	}}
	r := redact.Observation(o)
	assert.Equal(t, digest, r.Attrs["prompts_hash"])
	assert.Equal(t, strings.Repeat("0f", 20), r.Attrs["catacomb_config_hash"])
	assert.Equal(t, "use ‹redacted:aws-key›", r.Attrs["boot_hash"])
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["tree_hash"])
	assert.Equal(t, "‹redacted:high-entropy›", r.Attrs["entropy"])
}
