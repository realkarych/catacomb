package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

func syntheticTranscript(pairs int) []byte {
	var buf bytes.Buffer
	base := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	fmt.Fprintf(&buf,
		`{"type":"user","uuid":"u0","sessionId":"s1","timestamp":%q,"message":{"role":"user","content":"run the basket"}}`+"\n",
		base.Format(time.RFC3339))
	output := strings.Repeat("x", 2048)
	for i := range pairs {
		call := base.Add(time.Duration(2*i+1) * time.Second)
		result := base.Add(time.Duration(2*i+2) * time.Second)
		fmt.Fprintf(&buf,
			`{"type":"assistant","uuid":"a%d","sessionId":"s1","timestamp":%q,"message":{"role":"assistant","id":"msg_%d","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_%d","name":"Bash","input":{"command":"step-%d"}}],"usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n",
			i, call.Format(time.RFC3339), i, i, i, 100+i%50, 20+i%10)
		fmt.Fprintf(&buf,
			`{"type":"user","uuid":"r%d","parentUuid":"a%d","sessionId":"s1","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":%q,"is_error":false}]}}`+"\n",
			i, i, result.Format(time.RFC3339), i, output)
	}
	return buf.Bytes()
}

func writeSyntheticTranscript(b *testing.B, pairs int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, syntheticTranscript(pairs), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func writeSyntheticEvidenceRun(b *testing.B, root, id, variant, src string) {
	b.Helper()
	m := evidence.Meta{
		RunID:       id,
		Task:        "t1",
		Variant:     variant,
		Rep:         1,
		SessionID:   "s1",
		Labels:      map[string]string{"variant": variant, "task": "t1"},
		MarkerName:  "task:t1",
		MarkerStart: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
		MarkerEnd:   time.Date(2026, 6, 20, 11, 0, 0, 0, time.UTC),
		FinishedAt:  time.Date(2026, 6, 20, 11, 0, 1, 0, time.UTC),
	}
	if err := evidence.Write(filepath.Join(root, id), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}); err != nil {
		b.Fatal(err)
	}
}

func syntheticEvidenceRoot(b *testing.B, variants []string, runsPerVariant, pairs int) string {
	b.Helper()
	root := b.TempDir()
	src := writeSyntheticTranscript(b, pairs)
	for _, variant := range variants {
		for r := range runsPerVariant {
			writeSyntheticEvidenceRun(b, root, fmt.Sprintf("%s-%03d", variant, r), variant, src)
		}
	}
	return root
}

func BenchmarkLoadGraphOffline(b *testing.B) {
	for _, pairs := range []int{250, 2500} {
		b.Run(fmt.Sprintf("pairs=%d", pairs), func(b *testing.B) {
			path := writeSyntheticTranscript(b, pairs)
			g, err := loadGraphOffline(path, nil, newExecutionID(), newPricer(), nil)
			if err != nil {
				b.Fatal(err)
			}
			nodes, _ := sortedGraphSnapshot(g)
			b.ReportMetric(float64(len(nodes)), "nodes")
			b.ReportAllocs()
			for b.Loop() {
				if _, lerr := loadGraphOffline(path, nil, newExecutionID(), newPricer(), nil); lerr != nil {
					b.Fatal(lerr)
				}
			}
		})
	}
}

func retainedHeapBytes(group *[]aggregate.RunGraph) float64 {
	runtime.GC()
	var with runtime.MemStats
	runtime.ReadMemStats(&with)
	*group = nil
	runtime.GC()
	var without runtime.MemStats
	runtime.ReadMemStats(&without)
	if with.HeapAlloc <= without.HeapAlloc {
		return 0
	}
	return float64(with.HeapAlloc - without.HeapAlloc)
}

func BenchmarkResolveRunsDirGroup(b *testing.B) {
	const runs, pairs = 8, 1000
	modes := []struct {
		name string
		mode runGraphLoadMode
	}{
		{"full", loadFullGraphs},
		{"stripped", loadForAggregation},
	}
	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			root := syntheticEvidenceRoot(b, []string{"base"}, runs, pairs)
			b.ReportAllocs()
			var group []aggregate.RunGraph
			for b.Loop() {
				var err error
				group, _, err = resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", m.mode)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(group[0].Nodes)), "nodes/run")
			b.ReportMetric(retainedHeapBytes(&group), "retained-B")
		})
	}
}

func BenchmarkRegressGate(b *testing.B) {
	const runsPerGroup, pairs = 8, 1000
	root := syntheticEvidenceRoot(b, []string{"base", "cand"}, runsPerGroup, pairs)
	f := regressFlags{
		runsDir:    root,
		baseline:   "label:variant=base",
		candidate:  "label:variant=cand",
		thresholds: regress.DefaultThresholds(),
	}
	group, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", loadForAggregation)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(group[0].Nodes)), "nodes/run")
	b.ReportMetric(2*runsPerGroup, "runs")
	b.ReportAllocs()
	for b.Loop() {
		if rerr := runRegress(io.Discard, io.Discard, store.OpenSQLiteReadOnly, newPricer, f); rerr != nil {
			b.Fatal(rerr)
		}
	}
}
