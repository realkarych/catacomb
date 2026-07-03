package regress

import "math"

func wilson(successes, n int, z float64) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	nf := float64(n)
	phat := float64(successes) / nf
	z2 := z * z
	denom := 1 + z2/nf
	center := (phat + z2/(2*nf)) / denom
	half := z * math.Sqrt(phat*(1-phat)/nf+z2/(4*nf*nf)) / denom
	return math.Max(0, center-half), math.Min(1, center+half)
}
