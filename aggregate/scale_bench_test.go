package aggregate

import (
	"fmt"
	"testing"
	"time"

	"github.com/realkarych/catacomb/model"
)

func syntheticRunGraph(runID string, nodes int) RunGraph {
	start := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Duration(nodes) * time.Second)
	rg := RunGraph{
		Run: model.Run{
			ID:        runID,
			Status:    model.StatusOK,
			Labels:    map[string]string{"task": "t1", "variant": "base"},
			StartedAt: tp(start),
			EndedAt:   tp(end),
		},
		Nodes:       make([]*model.Node, 0, nodes+1),
		Edges:       make([]*model.Edge, 0, nodes),
		Annotations: map[string]float64{"verifier.pass": 1},
	}
	marker := &model.Node{
		ID:       "n-marker",
		Type:     model.NodeMarker,
		Name:     "task:t1",
		Status:   model.StatusOK,
		PhaseKey: "phase-task-t1",
		TStart:   tp(start),
		TEnd:     tp(end),
	}
	rg.Nodes = append(rg.Nodes, marker)
	for i := range nodes {
		id := fmt.Sprintf("n-%06d", i)
		rg.Nodes = append(rg.Nodes, &model.Node{
			ID:          id,
			Type:        model.NodeToolCall,
			Name:        "Bash",
			Status:      model.StatusOK,
			StepKey:     fmt.Sprintf("step-%06d", i),
			DurationMS:  i64(int64(50 + i%200)),
			TokensIn:    i64(int64(100 + i%50)),
			TokensOut:   i64(int64(20 + i%10)),
			CostUSD:     f64(0.0001 * float64(1+i%5)),
			Annotations: map[string]any{"verifier.pass": float64(i % 2)},
		})
		rg.Edges = append(rg.Edges, &model.Edge{ID: "e-" + id, Type: model.EdgeMarkerSpan, Src: marker.ID, Dst: id})
	}
	return rg
}

func syntheticGroup(runs, nodes int) []RunGraph {
	group := make([]RunGraph, 0, runs)
	for r := range runs {
		group = append(group, syntheticRunGraph(fmt.Sprintf("run-%03d", r), nodes))
	}
	return group
}

func BenchmarkAggregate(b *testing.B) {
	sizes := []struct{ runs, nodes int }{{8, 500}, {8, 2000}, {32, 2000}}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("runs=%d/nodes=%d", size.runs, size.nodes), func(b *testing.B) {
			group := syntheticGroup(size.runs, size.nodes)
			opts := Options{AnnotationKeys: []string{"verifier.pass"}}
			b.ReportAllocs()
			for b.Loop() {
				rep := Aggregate(group, opts)
				if rep.Runs != size.runs {
					b.Fatalf("unexpected run count %d", rep.Runs)
				}
			}
		})
	}
}
