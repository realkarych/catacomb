package pricing

import (
	"regexp"
	"strings"
)

type Tier struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheReadPerMTok  float64
	CacheWritePerMTok float64
}

type Inputs struct {
	ModelID     string
	TokensIn    int64
	TokensOut   int64
	CacheReadIn int64
	CacheWrite  int64
}

type Result struct {
	USD    float64
	Source string
}

type family struct {
	prefix string
	tier   Tier
}

type Engine struct {
	table    map[string]Tier
	families []family
}

func New() *Engine {
	return newEngineWithFamilies(defaultTable(), defaultFamilies())
}

func newEngineWithFamilies(t map[string]Tier, fams []family) *Engine {
	return &Engine{table: t, families: fams}
}

func (e *Engine) Cost(in Inputs) (Result, bool) {
	tier, ok := e.tierFor(in.ModelID)
	if !ok {
		return Result{}, false
	}
	usd := perMTok(in.TokensIn, tier.InputPerMTok) +
		perMTok(in.TokensOut, tier.OutputPerMTok) +
		perMTok(in.CacheReadIn, tier.CacheReadPerMTok) +
		perMTok(in.CacheWrite, tier.CacheWritePerMTok)
	return Result{USD: usd, Source: "estimated"}, true
}

func (e *Engine) tierFor(id string) (Tier, bool) {
	if tier, ok := e.lookup(id); ok {
		return tier, true
	}
	if norm := normalizeModelID(id); norm != id {
		return e.lookup(norm)
	}
	return Tier{}, false
}

func (e *Engine) lookup(id string) (Tier, bool) {
	if tier, ok := e.table[id]; ok {
		return tier, true
	}
	return e.familyTier(id)
}

var dateSnapshotRE = regexp.MustCompile(`[@-]\d{8}$`)

func normalizeModelID(id string) string {
	for stripped := true; stripped; {
		stripped = false
		for _, p := range []string{"anthropic.", "vertex_ai/", "bedrock/"} {
			if strings.HasPrefix(id, p) {
				id = strings.TrimPrefix(id, p)
				stripped = true
			}
		}
	}
	return dateSnapshotRE.ReplaceAllString(id, "")
}

func (e *Engine) familyTier(id string) (Tier, bool) {
	best := -1
	var chosen Tier
	for _, f := range e.families {
		if len(f.prefix) > best && strings.HasPrefix(id, f.prefix) {
			best = len(f.prefix)
			chosen = f.tier
		}
	}
	if best < 0 {
		return Tier{}, false
	}
	return chosen, true
}

func perMTok(tokens int64, perM float64) float64 {
	return float64(tokens) / 1_000_000 * perM
}

func defaultFamilies() []family {
	return []family{
		{prefix: "claude-opus-", tier: Tier{InputPerMTok: 5.00, OutputPerMTok: 25.00, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25}},
		{prefix: "claude-sonnet-", tier: Tier{InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75}},
		{prefix: "claude-haiku-", tier: Tier{InputPerMTok: 1.00, OutputPerMTok: 5.00, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25}},
		{prefix: "claude-fable-", tier: Tier{InputPerMTok: 10.00, OutputPerMTok: 50.00, CacheReadPerMTok: 1.00, CacheWritePerMTok: 12.50}},
	}
}

func defaultTable() map[string]Tier {
	return map[string]Tier{
		"claude-fable-5":    {InputPerMTok: 10.00, OutputPerMTok: 50.00, CacheReadPerMTok: 1.00, CacheWritePerMTok: 12.50},
		"claude-opus-4-8":   {InputPerMTok: 5.00, OutputPerMTok: 25.00, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25},
		"claude-opus-4-7":   {InputPerMTok: 5.00, OutputPerMTok: 25.00, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25},
		"claude-opus-4-6":   {InputPerMTok: 5.00, OutputPerMTok: 25.00, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25},
		"claude-opus-4-5":   {InputPerMTok: 5.00, OutputPerMTok: 25.00, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25},
		"claude-sonnet-4-6": {InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
		"claude-sonnet-4-5": {InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75},
		"claude-haiku-4-5":  {InputPerMTok: 1.00, OutputPerMTok: 5.00, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25},
	}
}
