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
	e := newEngineWithTable(testTable())
	reported := 0.42
	r, ok := e.Cost(Inputs{ModelID: "model-x", TokensIn: 999999, ReportedUSD: &reported})
	require.True(t, ok)
	assert.Equal(t, "reported", r.Source)
	assert.InDelta(t, 0.42, r.USD, 1e-9)
}

func TestCostEstimateFromTiers(t *testing.T) {
	e := newEngineWithTable(testTable())
	r, ok := e.Cost(Inputs{ModelID: "model-x", TokensIn: 1_000_000, TokensOut: 1_000_000, CacheReadIn: 1_000_000, CacheWrite: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 1+5+0.1+1.25, r.USD, 1e-9)
}

func TestCostUnknownModel(t *testing.T) {
	e := newEngineWithTable(testTable())
	_, ok := e.Cost(Inputs{ModelID: "nope", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostZeroTokensKnownModel(t *testing.T) {
	e := newEngineWithTable(testTable())
	r, ok := e.Cost(Inputs{ModelID: "model-x"})
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
