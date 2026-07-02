package aggregate

import (
	"sort"

	"github.com/realkarych/catacomb/model"
)

type RunGraph struct {
	Run   model.Run
	Nodes []*model.Node
	Edges []*model.Edge
}

type Options struct {
	AnnotationKeys []string
}

type MetricStats struct {
	N      int     `json:"n"`
	Median float64 `json:"median"`
	P90    float64 `json:"p90"`
}

type Row struct {
	Key          string                   `json:"key"`
	Name         string                   `json:"name,omitempty"`
	Present      int                      `json:"present"`
	PresenceRate float64                  `json:"presence_rate"`
	StatusRates  map[model.Status]float64 `json:"status_rates"`
	Occurrences  MetricStats              `json:"occurrences"`
	DurationMS   MetricStats              `json:"duration_ms"`
	CostUSD      MetricStats              `json:"cost_usd"`
	TokensIn     MetricStats              `json:"tokens_in"`
	TokensOut    MetricStats              `json:"tokens_out"`
	Annotations  map[string]MetricStats   `json:"annotations,omitempty"`
}

type RunTotals struct {
	DurationMS MetricStats `json:"duration_ms"`
	CostUSD    MetricStats `json:"cost_usd"`
	TokensIn   MetricStats `json:"tokens_in"`
	TokensOut  MetricStats `json:"tokens_out"`
	Nodes      MetricStats `json:"nodes"`
	ErrorRate  float64     `json:"error_rate"`
}

type Report struct {
	Runs   int       `json:"runs"`
	Steps  []Row     `json:"steps"`
	Phases []Row     `json:"phases"`
	Totals RunTotals `json:"totals"`
}

func Aggregate(group []RunGraph, _ Options) Report {
	return Report{
		Runs:   len(group),
		Steps:  buildRows(group, foldRunSteps),
		Phases: buildRows(group, foldRunPhases),
		Totals: runTotals(group),
	}
}

type metricSums struct {
	duration  float64
	cost      float64
	tokensIn  float64
	tokensOut float64
}

type runKey struct {
	count       float64
	sums        metricSums
	worst       model.Status
	firstNodeID string
	firstName   string
}

func included(n *model.Node) bool {
	return n.Status != model.StatusSuperseded && n.Status != model.StatusAbandoned
}

func severity(s model.Status) int {
	switch s {
	case model.StatusError:
		return 7
	case model.StatusBlocked:
		return 6
	case model.StatusCancelled:
		return 5
	case model.StatusUnknown:
		return 4
	case model.StatusRunning:
		return 3
	case model.StatusPending:
		return 2
	case model.StatusOK:
		return 1
	default:
		return 0
	}
}

func worse(a, b model.Status) model.Status {
	if severity(b) > severity(a) {
		return b
	}
	return a
}

func derefI(p *int64) float64 {
	if p == nil {
		return 0
	}
	return float64(*p)
}

func derefF(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func foldRunSteps(rg RunGraph) map[string]runKey {
	acc := map[string]runKey{}
	for _, n := range rg.Nodes {
		if n.StepKey == "" || !included(n) {
			continue
		}
		rk := accumulate(acc[n.StepKey], n)
		rk.sums.duration += derefI(n.DurationMS)
		rk.sums.cost += derefF(n.CostUSD)
		rk.sums.tokensIn += derefI(n.TokensIn)
		rk.sums.tokensOut += derefI(n.TokensOut)
		acc[n.StepKey] = rk
	}
	return acc
}

func foldRunPhases(rg RunGraph) map[string]runKey {
	byID := make(map[string]*model.Node, len(rg.Nodes))
	for _, n := range rg.Nodes {
		byID[n.ID] = n
	}
	members := map[string][]string{}
	for _, e := range rg.Edges {
		if e.Type == model.EdgeMarkerSpan {
			members[e.Src] = append(members[e.Src], e.Dst)
		}
	}
	acc := map[string]runKey{}
	for _, n := range rg.Nodes {
		if n.Type != model.NodeMarker || n.PhaseKey == "" {
			continue
		}
		rk := accumulate(acc[n.PhaseKey], n)
		rk.sums.duration += markerDuration(n)
		for _, mid := range members[n.ID] {
			m := byID[mid]
			if m == nil || !included(m) {
				continue
			}
			rk.sums.cost += derefF(m.CostUSD)
			rk.sums.tokensIn += derefI(m.TokensIn)
			rk.sums.tokensOut += derefI(m.TokensOut)
		}
		acc[n.PhaseKey] = rk
	}
	return acc
}

func accumulate(rk runKey, n *model.Node) runKey {
	if rk.count == 0 {
		rk.worst = n.Status
		rk.firstNodeID = n.ID
		rk.firstName = n.Name
	} else {
		rk.worst = worse(rk.worst, n.Status)
		if n.ID < rk.firstNodeID {
			rk.firstNodeID = n.ID
			rk.firstName = n.Name
		}
	}
	rk.count++
	return rk
}

func markerDuration(n *model.Node) float64 {
	if n.TStart == nil || n.TEnd == nil {
		return 0
	}
	return float64(n.TEnd.Sub(*n.TStart).Milliseconds())
}

type rowAcc struct {
	key         string
	present     int
	counts      []float64
	duration    []float64
	cost        []float64
	tokensIn    []float64
	tokensOut   []float64
	statusCount map[model.Status]int
	name        string
	nameRunID   string
	nameSet     bool
}

func (a *rowAcc) add(runID string, rk runKey) {
	a.present++
	a.counts = append(a.counts, rk.count)
	a.duration = append(a.duration, rk.sums.duration)
	a.cost = append(a.cost, rk.sums.cost)
	a.tokensIn = append(a.tokensIn, rk.sums.tokensIn)
	a.tokensOut = append(a.tokensOut, rk.sums.tokensOut)
	a.statusCount[rk.worst]++
	if !a.nameSet || runID < a.nameRunID {
		a.nameSet = true
		a.nameRunID = runID
		a.name = rk.firstName
	}
}

func (a *rowAcc) row(runs int) Row {
	rates := make(map[model.Status]float64, len(a.statusCount))
	for s, c := range a.statusCount {
		rates[s] = float64(c) / float64(a.present)
	}
	return Row{
		Key:          a.key,
		Name:         a.name,
		Present:      a.present,
		PresenceRate: float64(a.present) / float64(runs),
		StatusRates:  rates,
		Occurrences:  stats(a.counts),
		DurationMS:   stats(a.duration),
		CostUSD:      stats(a.cost),
		TokensIn:     stats(a.tokensIn),
		TokensOut:    stats(a.tokensOut),
	}
}

func buildRows(group []RunGraph, fold func(RunGraph) map[string]runKey) []Row {
	accs := map[string]*rowAcc{}
	for _, rg := range group {
		for key, rk := range fold(rg) {
			a := accs[key]
			if a == nil {
				a = &rowAcc{key: key, statusCount: map[model.Status]int{}}
				accs[key] = a
			}
			a.add(rg.Run.ID, rk)
		}
	}
	rows := make([]Row, 0, len(accs))
	for _, a := range accs {
		rows = append(rows, a.row(len(group)))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

func runTotals(group []RunGraph) RunTotals {
	if len(group) == 0 {
		return RunTotals{}
	}
	var durations, costs, tokensIn, tokensOut, nodes []float64
	errorRuns := 0
	for _, rg := range group {
		var sums metricSums
		count := 0
		hasError := rg.Run.Status == model.StatusError
		for _, n := range rg.Nodes {
			if !included(n) {
				continue
			}
			count++
			sums.cost += derefF(n.CostUSD)
			sums.tokensIn += derefI(n.TokensIn)
			sums.tokensOut += derefI(n.TokensOut)
			if n.Status == model.StatusError {
				hasError = true
			}
		}
		durations = append(durations, runDuration(rg.Run))
		costs = append(costs, sums.cost)
		tokensIn = append(tokensIn, sums.tokensIn)
		tokensOut = append(tokensOut, sums.tokensOut)
		nodes = append(nodes, float64(count))
		if hasError {
			errorRuns++
		}
	}
	return RunTotals{
		DurationMS: stats(durations),
		CostUSD:    stats(costs),
		TokensIn:   stats(tokensIn),
		TokensOut:  stats(tokensOut),
		Nodes:      stats(nodes),
		ErrorRate:  float64(errorRuns) / float64(len(group)),
	}
}

func runDuration(r model.Run) float64 {
	if r.StartedAt == nil || r.EndedAt == nil {
		return 0
	}
	return float64(r.EndedAt.Sub(*r.StartedAt).Milliseconds())
}
