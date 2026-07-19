package phasekey

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeIsDeterministicAndSixteenHexBytes(t *testing.T) {
	got := Compute("step1", "review", 0)

	assert.Equal(t, got, Compute("step1", "review", 0))
	raw, err := hex.DecodeString(got)
	require.NoError(t, err, "a phase key must be hex so it can be stored and compared as text")
	assert.Len(t, raw, 16)
}

func TestComputePinsSchemeAndInputLayout(t *testing.T) {
	assert.Equal(t, "phasekey/v1", scheme)
	assert.Equal(t, "b9bf32a7f1ed8a442f6cf22a8065efd7", Compute("step1", "review", 0),
		"phase keys are persisted in baselines, so the v1 scheme must not drift without a version bump")
}

func TestComputeSeparatesEveryInput(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"base", Compute("step1", "review", 0)},
		{"occurrence", Compute("step1", "review", 1)},
		{"marker name", Compute("step1", "build", 0)},
		{"enclosing step key", Compute("step2", "review", 0)},
		{"marker name absorbing the occurrence digit", Compute("step1", "review0", 0)},
		{"enclosing key absorbing the marker name", Compute("step1review", "", 0)},
		{"pipe inside the marker name", Compute("step1", "review|0", 0)},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev, dup := seen[tc.got]
			assert.False(t, dup, "collides with %q: distinct phases must never share a key", prev)
			seen[tc.got] = tc.name
		})
	}
}
