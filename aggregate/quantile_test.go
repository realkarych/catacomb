package aggregate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNearestRank(t *testing.T) {
	cases := []struct {
		name   string
		sorted []float64
		q      float64
		want   float64
	}{
		{"empty", nil, 0.5, 0},
		{"single median", []float64{5}, 0.5, 5},
		{"single p90", []float64{5}, 0.9, 5},
		{"even median", []float64{1, 2, 3, 4}, 0.5, 2},
		{"even p25", []float64{1, 2, 3, 4}, 0.25, 1},
		{"even p75", []float64{1, 2, 3, 4}, 0.75, 3},
		{"even p90", []float64{1, 2, 3, 4}, 0.9, 4},
		{"odd median", []float64{1, 2, 3}, 0.5, 2},
		{"odd p25", []float64{1, 2, 3}, 0.25, 1},
		{"odd p75", []float64{1, 2, 3}, 0.75, 3},
		{"odd p90", []float64{1, 2, 3}, 0.9, 3},
		{"clamp low", []float64{10, 20}, 0, 10},
		{"clamp high", []float64{10, 20}, 1.5, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, nearestRank(tc.sorted, tc.q))
		})
	}
}

func TestStats(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		want   MetricStats
	}{
		{"empty", nil, MetricStats{}},
		{"single", []float64{7}, MetricStats{N: 1, Median: 7, P25: 7, P75: 7, P90: 7}},
		{"unsorted even", []float64{4, 2, 1, 3}, MetricStats{N: 4, Median: 2, P25: 1, P75: 3, P90: 4}},
		{"odd", []float64{1, 2, 3}, MetricStats{N: 3, Median: 2, P25: 1, P75: 3, P90: 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, stats(tc.values))
		})
	}
}

func TestStatsDoesNotMutateInput(t *testing.T) {
	in := []float64{3, 1, 2}
	got := stats(in)
	require.Equal(t, MetricStats{N: 3, Median: 2, P25: 1, P75: 3, P90: 3}, got)
	assert.Equal(t, []float64{3, 1, 2}, in)
}
