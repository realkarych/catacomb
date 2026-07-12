package regress

import "github.com/realkarych/catacomb/aggregate"

type TaskReliability struct {
	Task  string    `json:"task"`
	N     int       `json:"n"`
	Ones  int       `json:"ones"`
	PassK []float64 `json:"pass_k"`
}

type GroupReliability struct {
	Tasks []TaskReliability `json:"tasks"`
	KMax  int               `json:"k_max"`
	Mean  []float64         `json:"mean"`
}

type Reliability struct {
	Baseline  GroupReliability `json:"baseline"`
	Candidate GroupReliability `json:"candidate"`
}

func passK(c, n, k int) float64 {
	if c < k {
		return 0
	}
	r := 1.0
	for i := 0; i < k; i++ {
		r *= float64(c-i) / float64(n-i)
	}
	return r
}

func groupReliability(tasks []aggregate.TaskStats) (GroupReliability, bool) {
	var outcomes []aggregate.TaskStats
	for _, t := range tasks {
		if t.Outcome != nil {
			outcomes = append(outcomes, t)
		}
	}
	if len(outcomes) == 0 {
		return GroupReliability{}, false
	}
	kMax := outcomes[0].Outcome.N
	for _, t := range outcomes {
		if t.Outcome.N < kMax {
			kMax = t.Outcome.N
		}
	}
	gr := GroupReliability{KMax: kMax, Mean: make([]float64, kMax)}
	for _, t := range outcomes {
		tr := TaskReliability{Task: t.Task, N: t.Outcome.N, Ones: t.Outcome.Ones, PassK: make([]float64, kMax)}
		for i := 0; i < kMax; i++ {
			p := passK(t.Outcome.Ones, t.Outcome.N, i+1)
			tr.PassK[i] = p
			gr.Mean[i] += p
		}
		gr.Tasks = append(gr.Tasks, tr)
	}
	for i := range gr.Mean {
		gr.Mean[i] /= float64(len(outcomes))
	}
	return gr, true
}

func computeReliability(b, c aggregate.Report) *Reliability {
	bg, bok := groupReliability(b.Tasks)
	cg, cok := groupReliability(c.Tasks)
	if !bok || !cok {
		return nil
	}
	return &Reliability{Baseline: bg, Candidate: cg}
}
