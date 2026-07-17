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
	_, ok := e.Cost(Inputs{ModelID: "grok-4", TokensIn: 10})
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

func rateUSD(t *testing.T, e *Engine, in Inputs) float64 {
	t.Helper()
	r, ok := e.Cost(in)
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	return r.USD
}

func TestCostOpenAIExactTableRates(t *testing.T) {
	cases := []struct {
		modelID    string
		in         float64
		cacheRead  float64
		out        float64
		cacheWrite float64
	}{
		{modelID: "gpt-5.6-sol", in: 5.00, cacheRead: 0.50, out: 30.00, cacheWrite: 6.25},
		{modelID: "gpt-5.6-terra", in: 2.50, cacheRead: 0.25, out: 15.00, cacheWrite: 3.125},
		{modelID: "gpt-5.6-luna", in: 1.00, cacheRead: 0.10, out: 6.00, cacheWrite: 1.25},
		{modelID: "gpt-5.5", in: 5.00, cacheRead: 0.50, out: 30.00, cacheWrite: 0},
		{modelID: "gpt-5.5-pro", in: 30.00, cacheRead: 0, out: 180.00, cacheWrite: 0},
		{modelID: "gpt-5.5-cyber", in: 12.50, cacheRead: 1.25, out: 75.00, cacheWrite: 0},
		{modelID: "gpt-5.4", in: 2.50, cacheRead: 0.25, out: 15.00, cacheWrite: 0},
		{modelID: "gpt-5.4-mini", in: 0.75, cacheRead: 0.075, out: 4.50, cacheWrite: 0},
		{modelID: "gpt-5.4-nano", in: 0.20, cacheRead: 0.02, out: 1.25, cacheWrite: 0},
		{modelID: "gpt-5.4-pro", in: 30.00, cacheRead: 0, out: 180.00, cacheWrite: 0},
		{modelID: "gpt-5.2", in: 1.75, cacheRead: 0.175, out: 14.00, cacheWrite: 0},
		{modelID: "gpt-5.2-pro", in: 21.00, cacheRead: 0, out: 168.00, cacheWrite: 0},
		{modelID: "gpt-5.1", in: 1.25, cacheRead: 0.125, out: 10.00, cacheWrite: 0},
		{modelID: "gpt-5", in: 1.25, cacheRead: 0.125, out: 10.00, cacheWrite: 0},
		{modelID: "gpt-5-mini", in: 0.25, cacheRead: 0.025, out: 2.00, cacheWrite: 0},
		{modelID: "gpt-5-nano", in: 0.05, cacheRead: 0.005, out: 0.40, cacheWrite: 0},
		{modelID: "gpt-5-pro", in: 15.00, cacheRead: 0, out: 120.00, cacheWrite: 0},
		{modelID: "gpt-5.3-codex", in: 1.75, cacheRead: 0.175, out: 14.00, cacheWrite: 0},
		{modelID: "gpt-5.2-codex", in: 1.75, cacheRead: 0.175, out: 14.00, cacheWrite: 0},
		{modelID: "gpt-5.1-codex-max", in: 1.25, cacheRead: 0.125, out: 10.00, cacheWrite: 0},
		{modelID: "gpt-5.1-codex", in: 1.25, cacheRead: 0.125, out: 10.00, cacheWrite: 0},
		{modelID: "gpt-5-codex", in: 1.25, cacheRead: 0.125, out: 10.00, cacheWrite: 0},
		{modelID: "gpt-5.1-codex-mini", in: 0.25, cacheRead: 0.025, out: 2.00, cacheWrite: 0},
		{modelID: "codex-mini-latest", in: 1.50, cacheRead: 0.375, out: 6.00, cacheWrite: 0},
	}
	for _, tc := range cases {
		t.Run(tc.modelID, func(t *testing.T) {
			e := newEngineWithFamilies(defaultTable(), nil)
			assert.InDelta(t, tc.in, rateUSD(t, e, Inputs{ModelID: tc.modelID, TokensIn: 1_000_000}), 1e-9)
			assert.InDelta(t, tc.cacheRead, rateUSD(t, e, Inputs{ModelID: tc.modelID, CacheReadIn: 1_000_000}), 1e-9)
			assert.InDelta(t, tc.out, rateUSD(t, e, Inputs{ModelID: tc.modelID, TokensOut: 1_000_000}), 1e-9)
			assert.InDelta(t, tc.cacheWrite, rateUSD(t, e, Inputs{ModelID: tc.modelID, CacheWrite: 1_000_000}), 1e-9)
		})
	}
}

func TestCostOpenAIHandCheckedVectors(t *testing.T) {
	cases := []struct {
		name    string
		inputs  Inputs
		wantUSD float64
	}{
		{
			name:    "gpt-5.4-mini cached bulk",
			inputs:  Inputs{ModelID: "gpt-5.4-mini", TokensIn: 6159, CacheReadIn: 5504, TokensOut: 16},
			wantUSD: 0.00510405,
		},
		{
			name:    "gpt-5.6-sol all four token kinds",
			inputs:  Inputs{ModelID: "gpt-5.6-sol", TokensIn: 1000, TokensOut: 100, CacheReadIn: 2000, CacheWrite: 500},
			wantUSD: 0.012125,
		},
		{
			name:    "gpt-5-pro dated snapshot at the pro tier",
			inputs:  Inputs{ModelID: "gpt-5-pro-2025-10-06", TokensIn: 1000, TokensOut: 100},
			wantUSD: 0.027,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.wantUSD, rateUSD(t, New(), tc.inputs), 1e-9)
		})
	}
}

func TestCostOpenAIDatedSnapshotResolvesBaseTier(t *testing.T) {
	cases := []struct {
		modelID string
		wantUSD float64
	}{
		{modelID: "gpt-5-pro-2025-10-06", wantUSD: 15.00},
		{modelID: "gpt-5.5-pro-2026-04-23", wantUSD: 30.00},
		{modelID: "gpt-5.2-pro-2025-12-11", wantUSD: 21.00},
		{modelID: "gpt-5.5-cyber-2026-05-01", wantUSD: 12.50},
		{modelID: "gpt-5.6-sol-2026-05-12", wantUSD: 5.00},
		{modelID: "gpt-5.6-terra-2026-05-12", wantUSD: 2.50},
		{modelID: "gpt-5.6-luna-2026-05-12", wantUSD: 1.00},
		{modelID: "gpt-5.5-2026-04-23", wantUSD: 5.00},
		{modelID: "gpt-5.4-mini-2026-03-17", wantUSD: 0.75},
		{modelID: "gpt-5.4-nano-2026-03-17", wantUSD: 0.20},
		{modelID: "gpt-5.4-2026-03-17", wantUSD: 2.50},
		{modelID: "gpt-5.3-codex-2026-02-05", wantUSD: 1.75},
		{modelID: "gpt-5.2-codex-2025-12-11", wantUSD: 1.75},
		{modelID: "gpt-5.2-2025-12-11", wantUSD: 1.75},
		{modelID: "gpt-5.1-codex-max-2025-11-19", wantUSD: 1.25},
		{modelID: "gpt-5.1-codex-mini-2025-11-13", wantUSD: 0.25},
		{modelID: "gpt-5.1-codex-2025-11-13", wantUSD: 1.25},
		{modelID: "gpt-5.1-2025-11-13", wantUSD: 1.25},
		{modelID: "gpt-5-codex-2025-09-15", wantUSD: 1.25},
		{modelID: "gpt-5-mini-2025-08-07", wantUSD: 0.25},
		{modelID: "gpt-5-nano-2025-08-07", wantUSD: 0.05},
		{modelID: "gpt-5-2025-08-07", wantUSD: 1.25},
	}
	for _, tc := range cases {
		t.Run(tc.modelID, func(t *testing.T) {
			assert.InDelta(t, tc.wantUSD, rateUSD(t, New(), Inputs{ModelID: tc.modelID, TokensIn: 1_000_000}), 1e-9)
		})
	}
}

func TestCostOpenAIFamilyClaimsByLongestPrefix(t *testing.T) {
	cases := []struct {
		name    string
		modelID string
		wantUSD float64
	}{
		{name: "codex-mini variant beats gpt-5.1-codex prefix", modelID: "gpt-5.1-codex-mini-x", wantUSD: 0.25},
		{name: "gpt-5 fallback claims unknown variant", modelID: "gpt-5-turbo", wantUSD: 1.25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.wantUSD, rateUSD(t, New(), Inputs{ModelID: tc.modelID, TokensIn: 1_000_000}), 1e-9)
		})
	}
}

func TestCostOpenAIDeliberatelyUnpricedIDs(t *testing.T) {
	for _, modelID := range []string{"codex-auto-review", "gpt-5.3-codex-spark", "gpt-5.3-codex-spark-2026-01-01"} {
		t.Run(modelID, func(t *testing.T) {
			e := New()
			_, ok := e.Cost(Inputs{ModelID: modelID, TokensIn: 1_000_000})
			assert.False(t, ok)
		})
	}
}

func TestCostGPT54CyberUnpricedUpstreamStillFallsToGPT54FamilyTradeoff(t *testing.T) {
	assert.InDelta(t, 2.50, rateUSD(t, New(), Inputs{ModelID: "gpt-5.4-cyber", TokensIn: 1_000_000}), 1e-9)
}
