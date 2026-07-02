package aggregate

import (
	"math"
	"slices"
)

func nearestRank(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(math.Ceil(q * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

func stats(values []float64) MetricStats {
	s := slices.Clone(values)
	slices.Sort(s)
	return MetricStats{
		N:      len(s),
		Median: nearestRank(s, 0.5),
		P90:    nearestRank(s, 0.9),
	}
}
