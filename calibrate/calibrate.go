package calibrate

import (
	"fmt"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/regress"
)

type CalibrateReport struct {
	Runs       int              `json:"runs"`
	MinSupport int              `json:"min_support"`
	RunIDs     []string         `json:"run_ids,omitempty"`
	Thresholds ThresholdsEcho   `json:"thresholds"`
	Sufficient bool             `json:"sufficient"`
	Detail     string           `json:"detail,omitempty"`
	Split      *SplitResult     `json:"split,omitempty"`
	Influence  *InfluenceResult `json:"influence,omitempty"`
}

type ThresholdsEcho struct {
	PresenceDelta       float64 `json:"presence_delta"`
	ErrorRateDelta      float64 `json:"error_delta"`
	MetricRelDelta      float64 `json:"metric_rel_delta"`
	IQRFactor           float64 `json:"iqr_factor"`
	MinSupport          int     `json:"min_support"`
	CoverageFloor       float64 `json:"coverage_floor"`
	Z                   float64 `json:"z"`
	FailOnNotable       bool    `json:"fail_on_notable"`
	AnnotationRateDelta float64 `json:"annotation_rate_delta"`
	PairedAlpha         float64 `json:"paired_alpha"`
	PairedMinTasks      int     `json:"paired_min_tasks"`
	PairedTest          string  `json:"paired_test"`
	AuditIQRFactor      float64 `json:"audit_iqr_factor"`
	AuditRelDelta       float64 `json:"audit_rel_delta"`
}

type SplitResult struct {
	FirstN  int             `json:"first_n"`
	SecondN int             `json:"second_n"`
	Verdict regress.Verdict `json:"verdict"`
	Notes   []string        `json:"notes,omitempty"`
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
	RunID        string          `json:"run_id"`
	From         regress.Verdict `json:"from"`
	To           regress.Verdict `json:"to"`
}

func Calibrate(runs []aggregate.RunGraph, th regress.Thresholds) CalibrateReport {
	k := len(runs)
	rep := CalibrateReport{Runs: k, MinSupport: th.MinSupport, RunIDs: runIDs(runs), Thresholds: ThresholdsEcho(th)}
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
	if split.OverallVerdict == regress.VerdictInsufficient {
		rep.Split.Notes = insufficientNotes(split.Findings)
	}
	rep.Influence = leaveOneOut(runs, th, split.OverallVerdict)
	return rep
}

func runIDs(runs []aggregate.RunGraph) []string {
	var ids []string
	for _, rg := range runs {
		ids = append(ids, rg.Run.ID)
	}
	return ids
}

func insufficientNotes(findings []regress.Finding) []string {
	var notes []string
	seen := map[string]struct{}{}
	for _, f := range findings {
		if f.Verdict != regress.VerdictInsufficient || f.Detail == "" {
			continue
		}
		if _, dup := seen[f.Detail]; dup {
			continue
		}
		seen[f.Detail] = struct{}{}
		notes = append(notes, f.Detail)
	}
	return notes
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
				RunID:        runs[i].Run.ID,
				From:         splitVerdict,
				To:           verdict,
			})
		}
	}
	return res
}
