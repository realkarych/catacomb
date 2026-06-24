package pricing

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
	ReportedUSD *float64
}

type Result struct {
	USD    float64
	Source string
}

type Engine struct {
	table map[string]Tier
}

func New() *Engine {
	return newEngineWithTable(defaultTable())
}

func newEngineWithTable(t map[string]Tier) *Engine {
	return &Engine{table: t}
}

func (e *Engine) Cost(in Inputs) (Result, bool) {
	if in.ReportedUSD != nil {
		return Result{USD: *in.ReportedUSD, Source: "reported"}, true
	}
	tier, ok := e.table[in.ModelID]
	if !ok {
		return Result{}, false
	}
	usd := perMTok(in.TokensIn, tier.InputPerMTok) +
		perMTok(in.TokensOut, tier.OutputPerMTok) +
		perMTok(in.CacheReadIn, tier.CacheReadPerMTok) +
		perMTok(in.CacheWrite, tier.CacheWritePerMTok)
	return Result{USD: usd, Source: "estimated"}, true
}

func perMTok(tokens int64, perM float64) float64 {
	return float64(tokens) / 1_000_000 * perM
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
