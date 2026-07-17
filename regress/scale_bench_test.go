package regress

import (
	"fmt"
	"testing"
	"time"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
)

func benchI64(v int64) *int64 { return &v }

func benchF64(v float64) *float64 { return &v }

func benchTime(t time.Time) *time.Time { return &t }

func benchRunGraph(runID string, nodes, jitter int) aggregate.RunGraph {
	start := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Duration(nodes+jitter) * time.Second)
	rg := aggregate.RunGraph{
		Run: model.Run{
			ID:        runID,
			Status:    model.StatusOK,
			Labels:    map[string]string{"task": "t1"},
			StartedAt: benchTime(start),
			EndedAt:   benchTime(end),
		},
		Nodes:       make([]*model.Node, 0, nodes),
		Annotations: map[string]float64{"verifier.pass": 1},
	}
	for i := range nodes {
		rg.Nodes = append(rg.Nodes, &model.Node{
			ID:          fmt.Sprintf("n-%06d", i),
			Type:        model.NodeToolCall,
			Name:        "Bash",
			Status:      model.StatusOK,
			StepKey:     fmt.Sprintf("step-%06d", i),
			DurationMS:  benchI64(int64(50 + (i+jitter)%200)),
			TokensIn:    benchI64(int64(100 + i%50)),
			TokensOut:   benchI64(int64(20 + i%10)),
			CostUSD:     benchF64(0.0001 * float64(1+i%5)),
			Annotations: map[string]any{"verifier.pass": float64(i % 2)},
		})
	}
	return rg
}

func benchGroup(runs, nodes, jitter int) []aggregate.RunGraph {
	group := make([]aggregate.RunGraph, 0, runs)
	for r := range runs {
		group = append(group, benchRunGraph(fmt.Sprintf("run-%03d", r), nodes, jitter+r))
	}
	return group
}

func BenchmarkCompare(b *testing.B) {
	sizes := []struct{ runs, nodes int }{{8, 2000}, {32, 2000}}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("runs=%d/nodes=%d", size.runs, size.nodes), func(b *testing.B) {
			baseGroup := benchGroup(size.runs, size.nodes, 0)
			candGroup := benchGroup(size.runs, size.nodes, 7)
			opts := aggregate.Options{AnnotationKeys: []string{"verifier.pass"}}
			in := Input{
				Baseline:       aggregate.Aggregate(baseGroup, opts),
				Candidate:      aggregate.Aggregate(candGroup, opts),
				Annotations:    []AnnotationSpec{{Key: "verifier.pass", HigherBetter: true}},
				BaselineCells:  aggregate.Cells(baseGroup),
				CandidateCells: aggregate.Cells(candGroup),
			}
			th := DefaultThresholds()
			b.ReportAllocs()
			for b.Loop() {
				rep := Compare(in, th)
				if rep.OverallVerdict == "" {
					b.Fatal("empty verdict")
				}
			}
		})
	}
}
