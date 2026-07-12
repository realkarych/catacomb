package aggregate

import "sort"

const (
	taskLabelKey       = "task"
	verifierOutcomeKey = "verifier.pass"
)

type TaskOutcome struct {
	N    int `json:"n"`
	Ones int `json:"ones"`
}

type TaskStats struct {
	Task       string       `json:"task"`
	Runs       int          `json:"runs"`
	Outcome    *TaskOutcome `json:"outcome,omitempty"`
	DurationMS MetricStats  `json:"duration_ms"`
	CostUSD    MetricStats  `json:"cost_usd"`
	TokensIn   MetricStats  `json:"tokens_in"`
	TokensOut  MetricStats  `json:"tokens_out"`
}

type taskAcc struct {
	task      string
	runs      int
	duration  []float64
	cost      []float64
	tokensIn  []float64
	tokensOut []float64
	outN      int
	outOnes   int
}

func (a *taskAcc) add(rg RunGraph) {
	a.runs++
	sums := runNodeSums(rg)
	if d, ok := runDuration(rg.Run); ok {
		a.duration = append(a.duration, d)
	}
	a.cost = append(a.cost, sums.cost)
	a.tokensIn = append(a.tokensIn, sums.tokensIn)
	a.tokensOut = append(a.tokensOut, sums.tokensOut)
	if v, ok := rg.Annotations[verifierOutcomeKey]; ok {
		a.outN++
		if v == 1 {
			a.outOnes++
		}
	}
}

func (a *taskAcc) stats() TaskStats {
	ts := TaskStats{
		Task:       a.task,
		Runs:       a.runs,
		DurationMS: stats(a.duration),
		CostUSD:    stats(a.cost),
		TokensIn:   stats(a.tokensIn),
		TokensOut:  stats(a.tokensOut),
	}
	if a.outN > 0 {
		ts.Outcome = &TaskOutcome{N: a.outN, Ones: a.outOnes}
	}
	return ts
}

func taskStats(group []RunGraph) []TaskStats {
	accs := map[string]*taskAcc{}
	for _, rg := range group {
		task := rg.Run.Labels[taskLabelKey]
		if task == "" {
			continue
		}
		a := accs[task]
		if a == nil {
			a = &taskAcc{task: task}
			accs[task] = a
		}
		a.add(rg)
	}
	if len(accs) == 0 {
		return nil
	}
	tasks := make([]TaskStats, 0, len(accs))
	for _, a := range accs {
		tasks = append(tasks, a.stats())
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Task < tasks[j].Task })
	return tasks
}
