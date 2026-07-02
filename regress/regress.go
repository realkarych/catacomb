package regress

import (
	"fmt"
	"math"
	"sort"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
)

type Input struct {
	Baseline  aggregate.Report
	Candidate aggregate.Report
}

type Coverage struct {
	Steps  float64 `json:"steps"`
	Phases float64 `json:"phases"`
}

type Report struct {
	BaselineRuns   int       `json:"baseline_runs"`
	CandidateRuns  int       `json:"candidate_runs"`
	Coverage       Coverage  `json:"coverage"`
	StepsTrusted   bool      `json:"steps_trusted"`
	Findings       []Finding `json:"findings"`
	Regressions    int       `json:"regressions"`
	Insufficient   int       `json:"insufficient"`
	OverallVerdict Verdict   `json:"overall_verdict"`
}

var scopeOrder = map[string]int{"total": 0, "phase": 1, "step": 2}

func Compare(in Input, th Thresholds) Report {
	b, c := in.Baseline, in.Candidate
	rep := Report{
		BaselineRuns:  b.Runs,
		CandidateRuns: c.Runs,
		Coverage: Coverage{
			Steps:  coverageFraction(b.Steps, c.Steps),
			Phases: coverageFraction(b.Phases, c.Phases),
		},
	}
	rep.StepsTrusted = rep.Coverage.Steps >= th.CoverageFloor

	findings := totalsFindings(b, c, th)
	findings = append(findings, rowFindings("phase", b.Phases, c.Phases, b.Runs, c.Runs, th, false, 0)...)
	findings = append(findings, rowFindings("step", b.Steps, c.Steps, b.Runs, c.Runs, th, !rep.StepsTrusted, rep.Coverage.Steps)...)

	findings = filterFindings(findings)
	sortFindings(findings)

	rep.Findings = findings
	rep.Regressions = countVerdict(findings, VerdictRegression)
	rep.Insufficient = countVerdict(findings, VerdictInsufficient)
	rep.OverallVerdict = overallVerdict(findings, rep.Regressions, rep.Insufficient)
	return rep
}

func coverageFraction(baseline, candidate []aggregate.Row) float64 {
	if len(baseline) == 0 {
		return 1
	}
	present := make(map[string]struct{}, len(candidate))
	for _, r := range candidate {
		present[r.Key] = struct{}{}
	}
	matched := 0
	for _, r := range baseline {
		if _, ok := present[r.Key]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(baseline))
}

func totalsFindings(b, c aggregate.Report, th Thresholds) []Finding {
	out := []Finding{
		compareMetric("total", "", "", "duration_ms", b.Totals.DurationMS, c.Totals.DurationMS, th),
		compareMetric("total", "", "", "cost_usd", b.Totals.CostUSD, c.Totals.CostUSD, th),
		compareMetric("total", "", "", "tokens_in", b.Totals.TokensIn, c.Totals.TokensIn, th),
		compareMetric("total", "", "", "tokens_out", b.Totals.TokensOut, c.Totals.TokensOut, th),
		compareMetric("total", "", "", "nodes", b.Totals.Nodes, c.Totals.Nodes, th),
	}
	bErr := int(math.Round(b.Totals.ErrorRate * float64(b.Runs)))
	cErr := int(math.Round(c.Totals.ErrorRate * float64(c.Runs)))
	out = append(out, compareRate("total", "", "", "error_rate", bErr, b.Runs, cErr, c.Runs, th.ErrorRateDelta, th))
	return out
}

func rowFindings(scope string, bRows, cRows []aggregate.Row, bRuns, cRuns int, th Thresholds, active bool, cov float64) []Finding {
	bMap := rowMap(bRows)
	cMap := rowMap(cRows)
	var out []Finding
	for _, key := range unionKeys(bMap, cMap) {
		br, bok := bMap[key]
		cr, cok := cMap[key]
		name := rowName(br, bok, cr, cok)
		bPresent := rowPresent(br, bok)
		cPresent := rowPresent(cr, cok)

		presence := compareRate(scope, key, name, "presence", bRuns-bPresent, bRuns, cRuns-cPresent, cRuns, th.PresenceDelta, th)
		presence.Detail = fmt.Sprintf("present %d/%d -> %d/%d", bPresent, bRuns, cPresent, cRuns)
		applyDowngrade(active, &presence, cov, th.CoverageFloor)
		out = append(out, presence)

		if !bok || !cok {
			out = append(out, missingMetricsFinding(scope, key, name, bok))
			continue
		}

		errRate := compareRate(scope, key, name, "error_rate", errorCount(br), br.Present, errorCount(cr), cr.Present, th.ErrorRateDelta, th)
		applyDowngrade(active, &errRate, cov, th.CoverageFloor)
		out = append(out, errRate)

		for _, m := range rowMetrics(br, cr) {
			f := compareMetric(scope, key, name, m.name, m.baseline, m.candidate, th)
			applyDowngrade(active, &f, cov, th.CoverageFloor)
			out = append(out, f)
		}
	}
	return out
}

func missingMetricsFinding(scope, key, name string, bok bool) Finding {
	detail := "absent in candidate"
	if !bok {
		detail = "absent in baseline"
	}
	return Finding{
		Scope:   scope,
		Key:     key,
		Name:    name,
		Metric:  "metrics",
		Verdict: VerdictInsufficient,
		Detail:  detail,
	}
}

func errorCount(r aggregate.Row) int {
	return int(math.Round(r.StatusRates[model.StatusError] * float64(r.Present)))
}

type metricPair struct {
	name      string
	baseline  aggregate.MetricStats
	candidate aggregate.MetricStats
}

func rowMetrics(b, c aggregate.Row) []metricPair {
	return []metricPair{
		{"occurrences", b.Occurrences, c.Occurrences},
		{"duration_ms", b.DurationMS, c.DurationMS},
		{"cost_usd", b.CostUSD, c.CostUSD},
		{"tokens_in", b.TokensIn, c.TokensIn},
		{"tokens_out", b.TokensOut, c.TokensOut},
	}
}

func applyDowngrade(active bool, f *Finding, cov, floor float64) {
	if !active || f.Verdict != VerdictRegression {
		return
	}
	f.Verdict = VerdictNotable
	note := fmt.Sprintf("step alignment coverage %.2f below floor %.2f", cov, floor)
	if f.Detail == "" {
		f.Detail = note
		return
	}
	f.Detail = f.Detail + "; " + note
}

func rowMap(rows []aggregate.Row) map[string]aggregate.Row {
	m := make(map[string]aggregate.Row, len(rows))
	for _, r := range rows {
		m[r.Key] = r
	}
	return m
}

func unionKeys(a, b map[string]aggregate.Row) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := seen[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func rowName(br aggregate.Row, bok bool, cr aggregate.Row, cok bool) string {
	if bok && br.Name != "" {
		return br.Name
	}
	if cok && cr.Name != "" {
		return cr.Name
	}
	return ""
}

func rowPresent(r aggregate.Row, ok bool) int {
	if !ok {
		return 0
	}
	return r.Present
}

func filterFindings(fs []Finding) []Finding {
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		if f.Scope == "total" || f.Verdict != VerdictOK {
			out = append(out, f)
		}
	}
	return out
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if ra, rb := scopeOrder[a.Scope], scopeOrder[b.Scope]; ra != rb {
			return ra < rb
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Metric < b.Metric
	})
}

func countVerdict(fs []Finding, v Verdict) int {
	n := 0
	for _, f := range fs {
		if f.Verdict == v {
			n++
		}
	}
	return n
}

func overallVerdict(fs []Finding, regressions, insufficient int) Verdict {
	switch {
	case regressions > 0:
		return VerdictRegression
	case insufficient > 0 && allNonInsufficientOK(fs):
		return VerdictInsufficient
	default:
		return VerdictOK
	}
}

func allNonInsufficientOK(fs []Finding) bool {
	for _, f := range fs {
		if f.Verdict != VerdictInsufficient && f.Verdict != VerdictOK {
			return false
		}
	}
	return true
}
