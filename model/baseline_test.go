package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaselineJSONRoundTrip(t *testing.T) {
	b := Baseline{
		Name:      "nightly",
		RunIDs:    []string{"r1", "r2"},
		Selector:  map[string]string{"suite": "checkout"},
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	}
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"name":"nightly"`)
	assert.Contains(t, string(raw), `"run_ids":["r1","r2"]`)
	assert.Contains(t, string(raw), `"selector":{"suite":"checkout"}`)
	var back Baseline
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, b, back)
}

func TestBaselineSelectorOmitemptyWhenUnset(t *testing.T) {
	raw, err := json.Marshal(Baseline{Name: "n", CreatedAt: time.Unix(0, 0).UTC()})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "selector")
}
