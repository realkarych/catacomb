package regress

import (
	"fmt"
	"math"

	"github.com/realkarych/catacomb/aggregate"
)

type Thresholds struct {
	PresenceDelta       float64
	ErrorRateDelta      float64
	MetricRelDelta      float64
	IQRFactor           float64
	MinSupport          int
	CoverageFloor       float64
	Z                   float64
	FailOnNotable       bool
	AnnotationRateDelta float64
	PairedAlpha         float64
	PairedMinTasks      int
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		PresenceDelta:       0.2,
		ErrorRateDelta:      0.1,
		MetricRelDelta:      0.25,
		IQRFactor:           1.5,
		MinSupport:          3,
		CoverageFloor:       0.7,
		Z:                   1.645,
		AnnotationRateDelta: 0.1,
		PairedAlpha:         0.05,
		PairedMinTasks:      5,
	}
}

type Verdict string

const (
	VerdictOK           Verdict = "ok"
	VerdictRegression   Verdict = "regression"
	VerdictImprovement  Verdict = "improvement"
	VerdictNotable      Verdict = "notable"
	VerdictInsufficient Verdict = "insufficient"
)

type Finding struct {
	Scope     string  `json:"scope"`
	Key       string  `json:"key,omitempty"`
	Name      string  `json:"name,omitempty"`
	Metric    string  `json:"metric"`
	Verdict   Verdict `json:"verdict"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Delta     float64 `json:"delta"`
	BandLo    float64 `json:"band_lo,omitempty"`
	BandHi    float64 `json:"band_hi,omitempty"`
	Detail    string  `json:"detail,omitempty"`
}

func rate(successes, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(successes) / float64(n)
}

func insufficientDetail(bN, cN, minSupport int) string {
	switch {
	case bN < minSupport && cN < minSupport:
		return fmt.Sprintf("baseline n=%d and candidate n=%d below min support %d", bN, cN, minSupport)
	case bN < minSupport:
		return fmt.Sprintf("baseline n=%d below min support %d", bN, minSupport)
	default:
		return fmt.Sprintf("candidate n=%d below min support %d", cN, minSupport)
	}
}

func compareRate(scope, key, name, metric string, bSucc, bN, cSucc, cN int, delta float64, th Thresholds) Finding {
	pb := rate(bSucc, bN)
	pc := rate(cSucc, cN)
	f := Finding{
		Scope:     scope,
		Key:       key,
		Name:      name,
		Metric:    metric,
		Baseline:  pb,
		Candidate: pc,
		Delta:     pc - pb,
	}
	if bN < th.MinSupport || cN < th.MinSupport {
		f.Verdict = VerdictInsufficient
		f.Detail = insufficientDetail(bN, cN, th.MinSupport)
		return f
	}
	bLo, bHi := wilson(bSucc, bN, th.Z)
	cLo, cHi := wilson(cSucc, cN, th.Z)
	f.BandLo = bLo
	f.BandHi = bHi
	switch {
	case bHi < cLo && pc-pb > delta:
		f.Verdict = VerdictRegression
	case pc-pb > delta:
		f.Verdict = VerdictNotable
	case cHi < bLo && pb-pc > delta:
		f.Verdict = VerdictImprovement
	default:
		f.Verdict = VerdictOK
	}
	return f
}

func compareMetric(scope, key, name, metric string, b, c aggregate.MetricStats, th Thresholds) Finding {
	f := Finding{
		Scope:     scope,
		Key:       key,
		Name:      name,
		Metric:    metric,
		Baseline:  b.Median,
		Candidate: c.Median,
		Delta:     c.Median - b.Median,
	}
	if b.N < th.MinSupport || c.N < th.MinSupport {
		f.Verdict = VerdictInsufficient
		f.Detail = insufficientDetail(b.N, c.N, th.MinSupport)
		return f
	}
	iqr := b.P75 - b.P25
	band := math.Max(th.MetricRelDelta*math.Abs(b.Median), th.IQRFactor*iqr)
	f.BandLo = b.Median - band
	f.BandHi = b.Median + band
	switch {
	case c.Median > f.BandHi:
		f.Verdict = VerdictRegression
	case c.Median < f.BandLo:
		f.Verdict = VerdictImprovement
	default:
		f.Verdict = VerdictOK
	}
	return f
}

func compareAnnotation(scope, key, name string, spec AnnotationSpec, b, c aggregate.MetricStats, th Thresholds) Finding {
	f := compareMetric(scope, key, name, "ann:"+spec.Key, b, c, th)
	if !spec.HigherBetter {
		return f
	}
	switch f.Verdict {
	case VerdictRegression:
		f.Verdict = VerdictImprovement
	case VerdictImprovement:
		f.Verdict = VerdictRegression
	}
	return f
}
