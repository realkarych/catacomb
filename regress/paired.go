package regress

import (
	"fmt"
	"math"
	"sort"

	"github.com/realkarych/catacomb/aggregate"
)

const minUnanimousSearchCap = 1000

const (
	PairedTestSign     = "sign"
	PairedTestWilcoxon = "wilcoxon"
)

type signedRank struct {
	magnitude float64
	positive  bool
	doubled   int
}

func doubledSignedRanks(deltas []float64) []signedRank {
	ranks := make([]signedRank, len(deltas))
	for i, d := range deltas {
		ranks[i] = signedRank{magnitude: math.Abs(d), positive: d > 0}
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i].magnitude < ranks[j].magnitude })
	for lo := 0; lo < len(ranks); {
		hi := lo + 1
		for hi < len(ranks) && ranks[hi].magnitude == ranks[lo].magnitude {
			hi++
		}
		for i := lo; i < hi; i++ {
			ranks[i].doubled = lo + hi + 1
		}
		lo = hi
	}
	return ranks
}

func wilcoxonNullDist(ranks []signedRank, total int) []float64 {
	dist := make([]float64, total+1)
	dist[0] = 1
	for _, r := range ranks {
		next := make([]float64, total+1)
		for s, p := range dist {
			if p == 0 {
				continue
			}
			next[s] += p / 2
			next[s+r.doubled] += p / 2
		}
		dist = next
	}
	return dist
}

func wilcoxonPValues(deltas []float64) (wPlus, wTotal, pReg, pImp float64) {
	ranks := doubledSignedRanks(deltas)
	observed, total := 0, 0
	for _, r := range ranks {
		total += r.doubled
		if r.positive {
			observed += r.doubled
		}
	}
	for s, p := range wilcoxonNullDist(ranks, total) {
		if s >= observed {
			pReg += p
		}
		if s <= observed {
			pImp += p
		}
	}
	return float64(observed) / 2, float64(total) / 2, pReg, pImp
}

func wilcoxonDetail(wPlus, wTotal float64, n int, p float64) string {
	return fmt.Sprintf("W+ %g/%g over %d %s, p=%.4g", wPlus, wTotal, n, taskWord(n), p)
}

func wilcoxonFinding(metric string, deltas []float64, matched int, th Thresholds) Finding {
	f := Finding{Scope: "paired", Metric: metric}
	if matched < th.PairedMinTasks {
		f.Verdict = VerdictInsufficient
		f.Detail = pairedInsufficientDetail(matched, th)
		return f
	}
	wPlus, wTotal, pReg, pImp := wilcoxonPValues(deltas)
	switch {
	case pReg <= th.PairedAlpha:
		f.Verdict = VerdictRegression
		f.Detail = wilcoxonDetail(wPlus, wTotal, len(deltas), pReg)
	case pImp <= th.PairedAlpha:
		f.Verdict = VerdictImprovement
		f.Detail = wilcoxonDetail(wPlus, wTotal, len(deltas), pImp)
	default:
		f.Verdict = VerdictOK
		f.Detail = wilcoxonDetail(wPlus, wTotal, len(deltas), pReg)
	}
	return f
}

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

func metricDeltas(pairs []taskPair, sel func(aggregate.TaskStats) aggregate.MetricStats, minSupport int) []float64 {
	var deltas []float64
	for _, p := range pairs {
		bs, cs := sel(p.base), sel(p.cand)
		if !metricSupported(bs, minSupport) || !metricSupported(cs, minSupport) {
			continue
		}
		if d := cs.Median - bs.Median; d > 0 || d < 0 {
			deltas = append(deltas, d)
		}
	}
	return deltas
}

func signCounts(pairs []taskPair, sel func(aggregate.TaskStats) aggregate.MetricStats, minSupport int) (positive, nonzero int) {
	for _, d := range metricDeltas(pairs, sel, minSupport) {
		if d > 0 {
			positive++
		}
		nonzero++
	}
	return positive, nonzero
}

func pairedInsufficientDetail(matched int, th Thresholds) string {
	return fmt.Sprintf("matched %d %s below paired min %d", matched, taskWord(matched), th.PairedMinTasks)
}

func pairedFinding(metric string, nonzero, positive, matched int, th Thresholds) Finding {
	f := Finding{Scope: "paired", Metric: metric}
	if matched < th.PairedMinTasks {
		f.Verdict = VerdictInsufficient
		f.Detail = pairedInsufficientDetail(matched, th)
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
		if th.PairedTest == PairedTestWilcoxon {
			out = append(out, wilcoxonFinding(m.name, metricDeltas(pairs, m.sel, th.MinSupport), matched, th))
			continue
		}
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
