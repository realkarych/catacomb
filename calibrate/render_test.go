package calibrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/regress"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func renderedHuman(t *testing.T, r CalibrateReport) string {
	t.Helper()
	var buf bytes.Buffer
	RenderHuman(r, &buf)
	return buf.String()
}

func TestRenderHumanInsufficientStopsAfterDetail(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 10000), regress.DefaultThresholds())
	got := renderedHuman(t, r)
	assert.Equal(t,
		"self-check: insufficient · runs 4 · min-support 3\n"+
			"order: r00 r01 r02 r03\n"+
			"self-check needs k>=6 runs (have 4)\n",
		got)
}

func TestRenderHumanCleanSplitSkippedInfluence(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 10000, 10000, 10000), regress.DefaultThresholds())
	got := renderedHuman(t, r)
	assert.Equal(t,
		"self-check: sufficient · runs 6 · min-support 3\n"+
			"order: r00 r01 r02 r03 r04 r05\n"+
			"A/A ok (first 3 vs second 3)\n"+
			"influence: leave-one-out needs k>=7 runs (have 6)\n",
		got)
}

func TestRenderHumanNoteLinesUnderInsufficientAA(t *testing.T) {
	r := Calibrate(withTaskLabels(fixtureGroup(10000, 10000, 10000, 10000, 10000, 10000), "sql"), regress.DefaultThresholds())
	got := renderedHuman(t, r)
	assert.Contains(t, got, "A/A insufficient (first 3 vs second 3)\n")
	assert.Contains(t, got, "note: matched 1 task below paired min 5\n")
}

func TestRenderHumanSufficientWithNilBlocksRendersHeader(t *testing.T) {
	got := renderedHuman(t, CalibrateReport{Runs: 6, MinSupport: 3, Sufficient: true})
	assert.Equal(t, "self-check: sufficient · runs 6 · min-support 3\n", got)
}

func TestRenderHumanDriftLines(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	got := renderedHuman(t, r)
	assert.Contains(t, got, "self-check: sufficient · runs 6 · min-support 3\n")
	assert.Contains(t, got, "A/A regression (first 3 vs second 3)\n")
	assert.Contains(t, got, "drift: total duration_ms regression 10000.00 -> 14000.00\n")
}

func TestRenderHumanInfluenceFlipLines(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 30000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	got := renderedHuman(t, r)
	assert.Contains(t, got, "A/A ok (first 3 vs second 4)\n")
	assert.Contains(t, got, "influence: dropping run r02 (#2) flips ok -> regression\n")
}

func TestRenderHumanInfluenceNoFlip(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 10000, 10000, 10000, 10000), regress.DefaultThresholds())
	require.NotNil(t, r.Influence)
	require.True(t, r.Influence.Evaluated)
	got := renderedHuman(t, r)
	assert.Contains(t, got, "influence: no single run flips the verdict\n")
}

func TestRenderHumanDeterministic(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 30000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	require.Equal(t, renderedHuman(t, r), renderedHuman(t, r))
}

func TestRenderJSONRoundTrip(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(r, &buf))
	var got CalibrateReport
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, r, got)
}

func TestRenderJSONThresholdsKeysAreSnakeCase(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(r, &buf))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &raw))
	var th map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["thresholds"], &th))
	want := []string{
		"presence_delta", "error_delta", "metric_rel_delta", "iqr_factor",
		"min_support", "coverage_floor", "z", "fail_on_notable",
		"annotation_rate_delta", "paired_alpha", "paired_min_tasks",
		"paired_test", "audit_iqr_factor", "audit_rel_delta",
	}
	assert.Len(t, th, len(want))
	for _, key := range want {
		assert.Contains(t, th, key)
	}
	assert.JSONEq(t, "3", string(th["min_support"]))
}

func TestRenderJSONWriteError(t *testing.T) {
	r := Calibrate(fixtureGroup(10000, 10000, 10000, 10000), regress.DefaultThresholds())
	require.Error(t, RenderJSON(r, failWriter{}))
}
