package reduce

type PriceInputs struct {
	ModelID     string
	TokensIn    int64
	TokensOut   int64
	CacheReadIn int64
	CacheWrite  int64
}

type PriceResult struct {
	USD    float64
	Source string
}

type Pricer interface {
	Cost(in PriceInputs) (PriceResult, bool)
}

type PricerFunc func(PriceInputs) (PriceResult, bool)

func (f PricerFunc) Cost(in PriceInputs) (PriceResult, bool) { return f(in) }
