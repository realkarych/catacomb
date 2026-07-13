package regress

import (
	"math"
	"slices"
	"sort"

	"github.com/realkarych/catacomb/aggregate"
)

type CellFlag struct {
	RunID  string  `json:"run_id"`
	Task   string  `json:"task,omitempty"`
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Median float64 `json:"median"`
	Band   float64 `json:"band"`
}

type Audit struct {
	Baseline  []CellFlag `json:"baseline,omitempty"`
	Candidate []CellFlag `json:"candidate,omitempty"`
}

type auditMetric struct {
	name  string
	value func(aggregate.Cell) float64
}

var auditMetrics = []auditMetric{
	{"duration_ms", func(c aggregate.Cell) float64 { return c.DurationMS }},
	{"cost_usd", func(c aggregate.Cell) float64 { return c.CostUSD }},
	{"tokens_in", func(c aggregate.Cell) float64 { return c.TokensIn }},
	{"tokens_out", func(c aggregate.Cell) float64 { return c.TokensOut }},
	{"turns", func(c aggregate.Cell) float64 { return c.Turns }},
}

func computeAudit(baseline, candidate []aggregate.Cell, th Thresholds) *Audit {
	b := groupFlags(baseline, th)
	c := groupFlags(candidate, th)
	if len(b) == 0 && len(c) == 0 {
		return nil
	}
	return &Audit{Baseline: b, Candidate: c}
}

func groupFlags(cells []aggregate.Cell, th Thresholds) []CellFlag {
	if len(cells) < 3 {
		return nil
	}
	var out []CellFlag
	for _, m := range auditMetrics {
		values := make([]float64, len(cells))
		for i, c := range cells {
			values[i] = m.value(c)
		}
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		median := auditRank(sorted, 0.5)
		iqr := auditRank(sorted, 0.75) - auditRank(sorted, 0.25)
		band := math.Max(th.AuditRelDelta*math.Abs(median), th.AuditIQRFactor*iqr)
		for i, c := range cells {
			if math.Abs(values[i]-median) > band {
				out = append(out, CellFlag{
					RunID:  c.RunID,
					Task:   c.Labels["task"],
					Metric: m.name,
					Value:  values[i],
					Median: median,
					Band:   band,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out
}

func auditRank(sorted []float64, q float64) float64 {
	return sorted[int(math.Ceil(q*float64(len(sorted))))-1]
}
