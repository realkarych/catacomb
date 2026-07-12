package regress

import (
	"fmt"
	"math"

	"github.com/realkarych/catacomb/aggregate"
)

type taskPair struct {
	base aggregate.TaskStats
	cand aggregate.TaskStats
}

var pairedMetrics = []struct {
	name string
	sel  func(aggregate.TaskStats) aggregate.MetricStats
}{
	{"duration_ms", func(t aggregate.TaskStats) aggregate.MetricStats { return t.DurationMS }},
	{"cost_usd", func(t aggregate.TaskStats) aggregate.MetricStats { return t.CostUSD }},
	{"tokens_in", func(t aggregate.TaskStats) aggregate.MetricStats { return t.TokensIn }},
	{"tokens_out", func(t aggregate.TaskStats) aggregate.MetricStats { return t.TokensOut }},
}

func binomTailGE(s, m int) float64 {
	if s <= 0 {
		return 1
	}
	if s > m {
		return 0
	}
	p := math.Ldexp(1, -m)
	total := 0.0
	for i := 0; i <= m; i++ {
		if i >= s {
			total += p
		}
		p = p * float64(m-i) / float64(i+1)
	}
	return total
}

func minUnanimousTasks(alpha float64) int {
	n := 1
	for math.Ldexp(1, -n) > alpha {
		n++
	}
	return n
}

func smallestFiringTasks(th Thresholds) int {
	u := minUnanimousTasks(th.PairedAlpha)
	if th.PairedMinTasks > u {
		return th.PairedMinTasks
	}
	return u
}

func pairedTasks(b, c []aggregate.TaskStats, minSupport int) []taskPair {
	bMap := make(map[string]aggregate.TaskStats, len(b))
	for _, t := range b {
		bMap[t.Task] = t
	}
	var pairs []taskPair
	for _, ct := range c {
		if ct.Runs < minSupport {
			continue
		}
		bt, ok := bMap[ct.Task]
		if !ok || bt.Runs < minSupport {
			continue
		}
		pairs = append(pairs, taskPair{base: bt, cand: ct})
	}
	return pairs
}

func signCounts(pairs []taskPair, sel func(aggregate.TaskStats) aggregate.MetricStats) (positive, nonzero int) {
	for _, p := range pairs {
		d := sel(p.cand).Median - sel(p.base).Median
		switch {
		case d > 0:
			positive++
			nonzero++
		case d < 0:
			nonzero++
		}
	}
	return positive, nonzero
}

func pairedFinding(metric string, nonzero, positive, matched int, th Thresholds) Finding {
	f := Finding{Scope: "paired", Metric: metric}
	if matched < th.PairedMinTasks {
		f.Verdict = VerdictInsufficient
		f.Detail = fmt.Sprintf("matched %d %s below paired min %d", matched, taskWord(matched), th.PairedMinTasks)
		return f
	}
	pReg := binomTailGE(positive, nonzero)
	pImp := binomTailGE(nonzero-positive, nonzero)
	switch {
	case pReg <= th.PairedAlpha:
		f.Verdict = VerdictRegression
		f.Detail = fmt.Sprintf("+%d/%d tasks, p=%.4g", positive, nonzero, pReg)
	case pImp <= th.PairedAlpha:
		f.Verdict = VerdictImprovement
		f.Detail = fmt.Sprintf("-%d/%d tasks, p=%.4g", nonzero-positive, nonzero, pImp)
	default:
		f.Verdict = VerdictOK
		f.Detail = fmt.Sprintf("+%d/%d tasks, p=%.4g", positive, nonzero, pReg)
	}
	return f
}

func pairedFindings(b, c aggregate.Report, th Thresholds) []Finding {
	if b.Tasks == nil || c.Tasks == nil {
		return nil
	}
	pairs := pairedTasks(b.Tasks, c.Tasks, th.MinSupport)
	matched := len(pairs)
	out := make([]Finding, 0, len(pairedMetrics))
	for _, m := range pairedMetrics {
		positive, nonzero := signCounts(pairs, m.sel)
		out = append(out, pairedFinding(m.name, nonzero, positive, matched, th))
	}
	return out
}

func pairedSensitivity(b, c aggregate.Report, th Thresholds) *PairedSensitivity {
	if b.Tasks == nil || c.Tasks == nil {
		return nil
	}
	matched := len(pairedTasks(b.Tasks, c.Tasks, th.MinSupport))
	smallest := smallestFiringTasks(th)
	return &PairedSensitivity{
		Reachable: matched >= smallest,
		MinTasks:  smallest,
	}
}
