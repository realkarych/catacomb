package regress

import (
	"fmt"
	"math"

	"github.com/realkarych/catacomb/aggregate"
)

const minUnanimousSearchCap = 1000

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

func logChoose(m, i int) float64 {
	lm, _ := math.Lgamma(float64(m) + 1)
	li, _ := math.Lgamma(float64(i) + 1)
	lr, _ := math.Lgamma(float64(m-i) + 1)
	return lm - li - lr
}

func binomTailGE(s, m int) float64 {
	if s <= 0 {
		return 1
	}
	if s > m {
		return 0
	}
	logs := make([]float64, 0, m-s+1)
	maxLog := math.Inf(-1)
	for i := s; i <= m; i++ {
		l := logChoose(m, i) - float64(m)*math.Ln2
		logs = append(logs, l)
		if l > maxLog {
			maxLog = l
		}
	}
	sum := 0.0
	for _, l := range logs {
		sum += math.Exp(l - maxLog)
	}
	return math.Exp(maxLog + math.Log(sum))
}

func minUnanimousTasks(alpha float64) int {
	for n := 1; n < minUnanimousSearchCap; n++ {
		if math.Ldexp(1, -n) <= alpha {
			return n
		}
	}
	return minUnanimousSearchCap
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

func metricSupported(ms aggregate.MetricStats, minSupport int) bool {
	return ms.N > 0 && ms.N >= minSupport
}

func signCounts(pairs []taskPair, sel func(aggregate.TaskStats) aggregate.MetricStats, minSupport int) (positive, nonzero int) {
	for _, p := range pairs {
		bs, cs := sel(p.base), sel(p.cand)
		if !metricSupported(bs, minSupport) || !metricSupported(cs, minSupport) {
			continue
		}
		d := cs.Median - bs.Median
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
		positive, nonzero := signCounts(pairs, m.sel, th.MinSupport)
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
