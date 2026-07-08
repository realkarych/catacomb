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
	assert.NotContains(t, string(raw), "runs_dir")
	assert.NotContains(t, string(raw), "stamps")
	assert.NotContains(t, string(raw), "catacomb_version")
	assert.NotContains(t, string(raw), "stepkey_scheme")
}

func TestBaselineRoundTripWithRunsDirAndStamps(t *testing.T) {
	b := Baseline{
		Name:      "nightly",
		RunIDs:    []string{"r1"},
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		RunsDir:   "/tmp/runs",
		Stamps:    Stamps{CatacombVersion: "v1.2.3", StepKeyScheme: "stepkey/v1"},
	}
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"runs_dir":"/tmp/runs"`)
	assert.Contains(t, string(raw), `"stamps":{"catacomb_version":"v1.2.3","stepkey_scheme":"stepkey/v1"}`)
	var back Baseline
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, b, back)
}

func TestBaselineUnmarshalLegacyBodyWithoutNewFields(t *testing.T) {
	legacy := `{"name":"old","run_ids":["r1"],"created_at":"2023-11-14T22:13:20Z"}`
	var b Baseline
	require.NoError(t, json.Unmarshal([]byte(legacy), &b))
	assert.Equal(t, "old", b.Name)
	assert.Equal(t, []string{"r1"}, b.RunIDs)
	assert.Empty(t, b.RunsDir)
	assert.True(t, b.Stamps.Zero())
}

func TestBaselineUnmarshalLegacyEmptyStampsObject(t *testing.T) {
	legacy := `{"name":"old","run_ids":["r1"],"created_at":"2023-11-14T22:13:20Z","stamps":{}}`
	var b Baseline
	require.NoError(t, json.Unmarshal([]byte(legacy), &b))
	assert.True(t, b.Stamps.Zero())
}

func TestBaselineUnmarshalLegacyBodyWithReproKey(t *testing.T) {
	legacy := `{"name":"old","run_ids":["r1"],"created_at":"2023-11-14T22:13:20Z","repro":{"r1":{"cwd":"/work","prompts_hash":"abc123"}}}`
	var b Baseline
	require.NoError(t, json.Unmarshal([]byte(legacy), &b))
	assert.Equal(t, "old", b.Name)
	assert.Equal(t, []string{"r1"}, b.RunIDs)
}
