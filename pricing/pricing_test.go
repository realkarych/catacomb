package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTable() map[string]Tier {
	return map[string]Tier{
		"model-x": {InputPerMTok: 1, OutputPerMTok: 5, CacheReadPerMTok: 0.1, CacheWritePerMTok: 1.25},
	}
}

func TestCostEstimateFromTiers(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-haiku-4-5", TokensIn: 1_000_000, TokensOut: 1_000_000, CacheReadIn: 1_000_000, CacheWrite: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 1+5+0.1+1.25, r.USD, 1e-9)
}

func TestCostUnknownModel(t *testing.T) {
	e := New()
	_, ok := e.Cost(Inputs{ModelID: "nope", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostZeroTokensKnownModel(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-haiku-4-5"})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 0, r.USD, 1e-9)
}

func TestNewHasRealTableEntry(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-8", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.Greater(t, r.USD, 0.0)
}

func TestCostPrefixFamilyFallbackEstimated(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-9", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 5.00, r.USD, 1e-9)
}

func TestCostUnknownFamilyYieldsNothing(t *testing.T) {
	e := New()
	_, ok := e.Cost(Inputs{ModelID: "gpt-5-turbo", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostExactMatchTakesPrecedenceOverFamily(t *testing.T) {
	fams := []family{
		{prefix: "model-", tier: Tier{InputPerMTok: 99, OutputPerMTok: 99, CacheReadPerMTok: 99, CacheWritePerMTok: 99}},
	}
	e := newEngineWithFamilies(testTable(), fams)
	r, ok := e.Cost(Inputs{ModelID: "model-x", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 1.00, r.USD, 1e-9)
}

func TestCostPrefixLongestFamilyWins(t *testing.T) {
	fams := []family{
		{prefix: "claude-opus-", tier: Tier{InputPerMTok: 5}},
		{prefix: "claude-opus-4-", tier: Tier{InputPerMTok: 9}},
	}
	e := newEngineWithFamilies(testTable(), fams)
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-9", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 9.0, r.USD, 1e-9)
}

func TestCostNoFamiliesStillMissesUnknown(t *testing.T) {
	e := newEngineWithFamilies(testTable(), nil)
	_, ok := e.Cost(Inputs{ModelID: "claude-opus-4-9", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostNormalizesVendorFormsToKnownIDs(t *testing.T) {
	cases := []struct {
		name    string
		modelID string
		wantUSD float64
	}{
		{name: "bedrock dotted prefix", modelID: "anthropic.claude-opus-4-8", wantUSD: 5.00},
		{name: "vertex slash prefix", modelID: "vertex_ai/claude-sonnet-4-5", wantUSD: 3.00},
		{name: "bedrock slash prefix", modelID: "bedrock/claude-haiku-4-5", wantUSD: 1.00},
		{name: "stacked prefixes", modelID: "bedrock/anthropic.claude-opus-4-8", wantUSD: 5.00},
		{name: "at date snapshot", modelID: "claude-opus-4-5@20251101", wantUSD: 5.00},
		{name: "prefix plus date snapshot", modelID: "anthropic.claude-opus-4-5@20251101", wantUSD: 5.00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			r, ok := e.Cost(Inputs{ModelID: tc.modelID, TokensIn: 1_000_000})
			require.True(t, ok)
			assert.Equal(t, "estimated", r.Source)
			assert.InDelta(t, tc.wantUSD, r.USD, 1e-9)
		})
	}
}

func TestCostBedrockPrefixPricesIdenticallyToBareID(t *testing.T) {
	e := New()
	in := Inputs{TokensIn: 123_456, TokensOut: 78_910, CacheReadIn: 11_213, CacheWrite: 14_151}
	in.ModelID = "claude-opus-4-8"
	want, ok := e.Cost(in)
	require.True(t, ok)
	in.ModelID = "anthropic.claude-opus-4-8"
	got, ok := e.Cost(in)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestCostDashDateSnapshotHitsExactTable(t *testing.T) {
	e := newEngineWithFamilies(defaultTable(), nil)
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-5-20251101", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 5.00, r.USD, 1e-9)
}

func TestCostNormalizedIDFallsBackToFamily(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "anthropic.claude-opus-4-9", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 5.00, r.USD, 1e-9)
}

func TestCostNormalizedIDStillUnknown(t *testing.T) {
	e := New()
	_, ok := e.Cost(Inputs{ModelID: "anthropic.gpt-9@20260101", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostShortNumericSuffixNotStripped(t *testing.T) {
	e := newEngineWithFamilies(testTable(), nil)
	_, ok := e.Cost(Inputs{ModelID: "model-x-1234567", TokensIn: 10})
	assert.False(t, ok)
}
