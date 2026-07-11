package regress

const minFullFlipSearchCap = 1000

type RateSensitivity struct {
	Reachable       bool `json:"reachable"`
	MinFullFlipRuns int  `json:"min_full_flip_runs"`
}

type Sensitivity struct {
	Presence   RateSensitivity  `json:"presence"`
	ErrorRate  RateSensitivity  `json:"error_rate"`
	Annotation *RateSensitivity `json:"annotation,omitempty"`
}

func rateGateReachable(bN, cN int, delta float64, th Thresholds) bool {
	return compareRate("", "", "", "", 0, bN, cN, cN, delta, th).Verdict == VerdictRegression
}

func minFullFlipRuns(th Thresholds, delta float64) int {
	for n := 1; n <= minFullFlipSearchCap; n++ {
		if rateGateReachable(n, n, delta, th) {
			return n
		}
	}
	return 0
}

func computeSensitivity(bRuns, cRuns int, th Thresholds, withAnnotations bool) *Sensitivity {
	s := Sensitivity{
		Presence: RateSensitivity{
			Reachable:       rateGateReachable(bRuns, cRuns, th.PresenceDelta, th),
			MinFullFlipRuns: minFullFlipRuns(th, th.PresenceDelta),
		},
		ErrorRate: RateSensitivity{
			Reachable:       rateGateReachable(bRuns, cRuns, th.ErrorRateDelta, th),
			MinFullFlipRuns: minFullFlipRuns(th, th.ErrorRateDelta),
		},
	}
	if withAnnotations {
		s.Annotation = &RateSensitivity{
			Reachable:       rateGateReachable(bRuns, cRuns, th.AnnotationRateDelta, th),
			MinFullFlipRuns: minFullFlipRuns(th, th.AnnotationRateDelta),
		}
	}
	if s.Presence.Reachable && s.ErrorRate.Reachable && (s.Annotation == nil || s.Annotation.Reachable) {
		return nil
	}
	return &s
}
