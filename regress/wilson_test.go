package regress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWilson(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		successes int
		n         int
		z         float64
		wantLo    float64
		wantHi    float64
	}{
		{"all_success_small", 3, 3, 1.96, 0.4385, 1.0},
		{"no_success_small", 0, 3, 1.96, 0.0, 0.5615},
		{"eight_of_ten", 8, 10, 1.96, 0.4901, 0.9433},
		{"no_success_five", 0, 5, 1.96, 0.0, 0.4345},
		{"all_success_five", 5, 5, 1.96, 0.5655, 1.0},
		{"half_of_five", 2, 5, 1.96, 0.1176, 0.7693},
		{"z_one", 5, 10, 1.0, 0.3492, 0.6508},
		{"zero_n", 0, 0, 1.96, 0.0, 1.0},
		{"zero_n_success", 4, 0, 1.96, 0.0, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lo, hi := wilson(tc.successes, tc.n, tc.z)
			require.InDelta(t, tc.wantLo, lo, 1e-3)
			require.InDelta(t, tc.wantHi, hi, 1e-3)
		})
	}
}
