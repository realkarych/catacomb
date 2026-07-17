package calibrate

import (
	"fmt"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/regress"
)

type CalibrateReport struct {
	Runs       int              `json:"runs"`
	MinSupport int              `json:"min_support"`
	Sufficient bool             `json:"sufficient"`
	Detail     string           `json:"detail,omitempty"`
	Split      *SplitResult     `json:"split,omitempty"`
	Influence  *InfluenceResult `json:"influence,omitempty"`
}

type SplitResult struct {
	FirstN  int             `json:"first_n"`
	SecondN int             `json:"second_n"`
	Verdict regress.Verdict `json:"verdict"`
	Drift   []DriftFinding  `json:"drift,omitempty"`
}

type DriftFinding struct {
	Scope     string          `json:"scope"`
	Metric    string          `json:"metric"`
	Verdict   regress.Verdict `json:"verdict"`
	Baseline  float64         `json:"baseline"`
	Candidate float64         `json:"candidate"`
	Detail    string          `json:"detail,omitempty"`
}

type InfluenceResult struct {
	Evaluated    bool          `json:"evaluated"`
	Detail       string        `json:"detail,omitempty"`
	FlippingRuns []FlipFinding `json:"flipping_runs,omitempty"`
}

type FlipFinding struct {
	DroppedIndex int             `json:"dropped_index"`
	From         regress.Verdict `json:"from"`
	To           regress.Verdict `json:"to"`
}

func Calibrate(runs []aggregate.RunGraph, th regress.Thresholds) CalibrateReport {
	k := len(runs)
	rep := CalibrateReport{Runs: k, MinSupport: th.MinSupport}
	need := 2 * th.MinSupport
	if k < need {
		rep.Detail = fmt.Sprintf("self-check needs k>=%d runs (have %d)", need, k)
		return rep
	}
	rep.Sufficient = true
	firstN := k / 2
	split := compareGroups(runs[:firstN], runs[firstN:], th)
	rep.Split = &SplitResult{
		FirstN:  firstN,
		SecondN: k - firstN,
		Verdict: split.OverallVerdict,
		Drift:   driftFindings(split.Findings),
	}
	rep.Influence = leaveOneOut(runs, th, split.OverallVerdict)
	return rep
}

func compareGroups(first, second []aggregate.RunGraph, th regress.Thresholds) regress.Report {
	opts := aggregate.Options{}
	return regress.Compare(regress.Input{
		Baseline:       aggregate.Aggregate(first, opts),
		Candidate:      aggregate.Aggregate(second, opts),
		BaselineCells:  aggregate.Cells(first),
		CandidateCells: aggregate.Cells(second),
	}, th)
}

func driftFindings(findings []regress.Finding) []DriftFinding {
	var out []DriftFinding
	for _, f := range findings {
		if f.Verdict != regress.VerdictRegression && f.Verdict != regress.VerdictNotable {
			continue
		}
		out = append(out, DriftFinding{
			Scope:     f.Scope,
			Metric:    f.Metric,
			Verdict:   f.Verdict,
			Baseline:  f.Baseline,
			Candidate: f.Candidate,
			Detail:    f.Detail,
		})
	}
	return out
}

func leaveOneOut(runs []aggregate.RunGraph, th regress.Thresholds, splitVerdict regress.Verdict) *InfluenceResult {
	k := len(runs)
	need := 2 * th.MinSupport
	if k-1 < need {
		return &InfluenceResult{
			Detail: fmt.Sprintf("leave-one-out needs k>=%d runs (have %d)", need+1, k),
		}
	}
	res := &InfluenceResult{Evaluated: true}
	for i := range k {
		remaining := make([]aggregate.RunGraph, 0, k-1)
		remaining = append(remaining, runs[:i]...)
		remaining = append(remaining, runs[i+1:]...)
		verdict := compareGroups(remaining[:len(remaining)/2], remaining[len(remaining)/2:], th).OverallVerdict
		if verdict != splitVerdict {
			res.FlippingRuns = append(res.FlippingRuns, FlipFinding{
				DroppedIndex: i,
				From:         splitVerdict,
				To:           verdict,
			})
		}
	}
	return res
}
