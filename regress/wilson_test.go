package regress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func wilsonUpperBoundAtZeroSuccesses(n int, z float64) float64 {
	return z * z / (float64(n) + z*z)
}

func wilsonLowerBoundAtAllSuccesses(n int, z float64) float64 {
	return float64(n) / (float64(n) + z*z)
}

var wilsonZs = []float64{1.0, 1.645, 1.96, 2.576}

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

func TestWilsonAtZeroSuccessesEqualsClosedFormZSquaredOverNPlusZSquared(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 3, 5, 10, 97} {
		for _, z := range wilsonZs {
			lo, hi := wilson(0, n, z)
			require.InDelta(t, 0.0, lo, 1e-12, "n=%d z=%g", n, z)
			require.InDelta(t, wilsonUpperBoundAtZeroSuccesses(n, z), hi, 1e-12, "n=%d z=%g", n, z)
		}
	}
}

func TestWilsonAtAllSuccessesEqualsClosedFormNOverNPlusZSquared(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 3, 5, 10, 97} {
		for _, z := range wilsonZs {
			lo, hi := wilson(n, n, z)
			require.InDelta(t, wilsonLowerBoundAtAllSuccesses(n, z), lo, 1e-12, "n=%d z=%g", n, z)
			require.InDelta(t, 1.0, hi, 1e-12, "n=%d z=%g", n, z)
		}
	}
}

func TestWilsonIntervalIsMirroredWhenSuccessesAndFailuresSwap(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 3, 5, 10, 97} {
		for successes := 0; successes <= n; successes++ {
			for _, z := range wilsonZs {
				lo, hi := wilson(successes, n, z)
				mirroredLo, mirroredHi := wilson(n-successes, n, z)
				require.InDelta(t, 1-mirroredHi, lo, 1e-12, "n=%d k=%d z=%g", n, successes, z)
				require.InDelta(t, 1-mirroredLo, hi, 1e-12, "n=%d k=%d z=%g", n, successes, z)
			}
		}
	}
}

func TestWilsonIntervalWidensWithZAndAlwaysContainsPointEstimate(t *testing.T) {
	t.Parallel()
	const n = 10
	for successes := 0; successes <= n; successes++ {
		phat := float64(successes) / float64(n)
		var prevWidth float64
		for _, z := range wilsonZs {
			lo, hi := wilson(successes, n, z)
			require.LessOrEqual(t, lo, phat+1e-12, "k=%d z=%g", successes, z)
			require.GreaterOrEqual(t, hi, phat-1e-12, "k=%d z=%g", successes, z)
			require.Greater(t, hi-lo, prevWidth, "k=%d z=%g", successes, z)
			prevWidth = hi - lo
		}
	}
}
