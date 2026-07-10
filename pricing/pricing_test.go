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

func TestCostReportedFirst(t *testing.T) {
	e := New()
	reported := 0.42
	r, ok := e.Cost(Inputs{ModelID: "model-x", TokensIn: 999999, ReportedUSD: &reported})
	require.True(t, ok)
	assert.Equal(t, "reported", r.Source)
	assert.InDelta(t, 0.42, r.USD, 1e-9)
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
