package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

func fp(v float64) *float64 { return &v }

func bp(v bool) *bool { return &v }

func paretoRecord(seq int, selector string, created, baselineCreated time.Time, findings ...regress.Finding) seqRecord {
	return seqRecord{seq: seq, rec: regress.Record{
		V:                 regress.RecordVersion,
		CandidateSelector: selector,
		CreatedAt:         created,
		BaselineCreatedAt: baselineCreated,
		Report:            regress.Report{Findings: findings},
	}}
}

func verifierFinding(baseline, candidate float64) regress.Finding {
	return regress.Finding{Scope: "total", Metric: "ann:verifier.pass", Baseline: baseline, Candidate: candidate}
}

func costFinding(baseline, candidate float64) regress.Finding {
	return regress.Finding{Scope: "total", Metric: "cost_usd", Baseline: baseline, Candidate: candidate}
}

func TestBuildParetoReportExtraction(t *testing.T) {
	base := time.Date(2026, 7, 12, 17, 0, 0, 0, time.UTC)
	created := base.Add(time.Hour)
	cases := []struct {
		name    string
		records []seqRecord
		want    []paretoPoint
	}{
		{
			name:    "no records",
			records: nil,
			want:    []paretoPoint{},
		},
		{
			name: "both findings",
			records: []seqRecord{
				paretoRecord(1, "label:variant=cand", created, base, verifierFinding(0.9, 0.8), costFinding(0.02, 0.01)),
			},
			want: []paretoPoint{
				{Source: "record", Seq: 1, Candidate: "label:variant=cand", CreatedAt: created, Accuracy: fp(0.8), CostUSD: fp(0.01), Dominated: bp(false), Spliced: bp(false)},
				{Source: "baseline", Accuracy: fp(0.9), CostUSD: fp(0.02), Dominated: bp(false)},
			},
		},
		{
			name: "missing verifier",
			records: []seqRecord{
				paretoRecord(1, "label:variant=cand", created, base, costFinding(0.02, 0.01)),
			},
			want: []paretoPoint{
				{Source: "baseline", CostUSD: fp(0.02)},
				{Source: "record", Seq: 1, Candidate: "label:variant=cand", CreatedAt: created, CostUSD: fp(0.01), Spliced: bp(false)},
			},
		},
		{
			name: "missing cost",
			records: []seqRecord{
				paretoRecord(1, "label:variant=cand", created, base, verifierFinding(0.9, 0.8)),
			},
			want: []paretoPoint{
				{Source: "baseline", Accuracy: fp(0.9)},
				{Source: "record", Seq: 1, Candidate: "label:variant=cand", CreatedAt: created, Accuracy: fp(0.8), Spliced: bp(false)},
			},
		},
		{
			name: "baseline point from newest record",
			records: []seqRecord{
				paretoRecord(1, "label:variant=a", created, base, verifierFinding(0.5, 0.5), costFinding(0.05, 0.05)),
				paretoRecord(2, "label:variant=b", created.Add(time.Hour), base, verifierFinding(0.9, 0.9), costFinding(0.01, 0.01)),
			},
			want: []paretoPoint{
				{Source: "baseline", Accuracy: fp(0.9), CostUSD: fp(0.01), Dominated: bp(false)},
				{Source: "record", Seq: 2, Candidate: "label:variant=b", CreatedAt: created.Add(time.Hour), Accuracy: fp(0.9), CostUSD: fp(0.01), Dominated: bp(false), Spliced: bp(false)},
				{Source: "record", Seq: 1, Candidate: "label:variant=a", CreatedAt: created, Accuracy: fp(0.5), CostUSD: fp(0.05), Dominated: bp(true), Spliced: bp(false)},
			},
		},
		{
			name: "spliced record",
			records: []seqRecord{
				paretoRecord(1, "label:variant=cand", created, base.Add(-time.Hour), verifierFinding(1, 1), costFinding(0.01, 0.01)),
			},
			want: []paretoPoint{
				{Source: "baseline", Accuracy: fp(1), CostUSD: fp(0.01), Dominated: bp(false)},
				{Source: "record", Seq: 1, Candidate: "label:variant=cand", CreatedAt: created, Accuracy: fp(1), CostUSD: fp(0.01), Dominated: bp(false), Spliced: bp(true)},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := buildParetoReport("golden", tc.records, base)
			assert.Equal(t, "golden", rep.Baseline)
			assert.Equal(t, tc.want, rep.Points)
		})
	}
}

func TestMarkParetoDominated(t *testing.T) {
	cases := []struct {
		name   string
		points []paretoPoint
		want   []*bool
	}{
		{
			name: "strictly better on one axis dominates",
			points: []paretoPoint{
				{Seq: 1, Accuracy: fp(1), CostUSD: fp(1)},
				{Seq: 2, Accuracy: fp(1), CostUSD: fp(2)},
			},
			want: []*bool{bp(false), bp(true)},
		},
		{
			name: "strictly better on both axes dominates",
			points: []paretoPoint{
				{Seq: 1, Accuracy: fp(1), CostUSD: fp(1)},
				{Seq: 2, Accuracy: fp(0.5), CostUSD: fp(2)},
			},
			want: []*bool{bp(false), bp(true)},
		},
		{
			name: "equal on both axes dominates neither",
			points: []paretoPoint{
				{Seq: 1, Accuracy: fp(1), CostUSD: fp(1)},
				{Seq: 2, Accuracy: fp(1), CostUSD: fp(1)},
			},
			want: []*bool{bp(false), bp(false)},
		},
		{
			name: "chain of three",
			points: []paretoPoint{
				{Seq: 1, Accuracy: fp(1), CostUSD: fp(0.5)},
				{Seq: 2, Accuracy: fp(0.9), CostUSD: fp(0.6)},
				{Seq: 3, Accuracy: fp(0.8), CostUSD: fp(0.7)},
			},
			want: []*bool{bp(false), bp(true), bp(true)},
		},
		{
			name: "trade-off frontier is non-dominated",
			points: []paretoPoint{
				{Seq: 1, Accuracy: fp(1), CostUSD: fp(2)},
				{Seq: 2, Accuracy: fp(0.5), CostUSD: fp(1)},
			},
			want: []*bool{bp(false), bp(false)},
		},
		{
			name: "non-comparable excluded from both sides",
			points: []paretoPoint{
				{Seq: 1, Accuracy: fp(1), CostUSD: fp(1)},
				{Seq: 2, CostUSD: fp(0.0001)},
				{Seq: 3, Accuracy: fp(2)},
			},
			want: []*bool{bp(false), nil, nil},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			markParetoDominated(tc.points)
			got := make([]*bool, 0, len(tc.points))
			for _, p := range tc.points {
				got = append(got, p.Dominated)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParetoLess(t *testing.T) {
	cases := []struct {
		name string
		a, b paretoPoint
		want bool
	}{
		{name: "comparable before non-comparable", a: paretoPoint{Seq: 9, Accuracy: fp(1), CostUSD: fp(1)}, b: paretoPoint{Seq: 1, CostUSD: fp(1)}, want: true},
		{name: "non-comparable after comparable", a: paretoPoint{Seq: 1, CostUSD: fp(1)}, b: paretoPoint{Seq: 9, Accuracy: fp(1), CostUSD: fp(1)}, want: false},
		{name: "non-comparable by seq", a: paretoPoint{Seq: 1}, b: paretoPoint{Seq: 2}, want: true},
		{name: "cost ascending", a: paretoPoint{Seq: 9, Accuracy: fp(0), CostUSD: fp(1)}, b: paretoPoint{Seq: 1, Accuracy: fp(1), CostUSD: fp(2)}, want: true},
		{name: "accuracy descending on equal cost", a: paretoPoint{Seq: 9, Accuracy: fp(1), CostUSD: fp(1)}, b: paretoPoint{Seq: 1, Accuracy: fp(0.5), CostUSD: fp(1)}, want: true},
		{name: "seq ascending on equal axes", a: paretoPoint{Seq: 1, Accuracy: fp(1), CostUSD: fp(1)}, b: paretoPoint{Seq: 2, Accuracy: fp(1), CostUSD: fp(1)}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, paretoLess(tc.a, tc.b))
		})
	}
}

func TestSortParetoPointsNonComparableSink(t *testing.T) {
	points := []paretoPoint{
		{Source: "record", Seq: 4, Spliced: bp(false)},
		{Source: "record", Seq: 1, CostUSD: fp(0.011), Spliced: bp(false)},
		{Source: "record", Seq: 2, Accuracy: fp(0), CostUSD: fp(0.0102), Spliced: bp(false)},
		{Source: "record", Seq: 3, Accuracy: fp(1), CostUSD: fp(0.0102), Spliced: bp(false)},
		{Source: "baseline", Accuracy: fp(1), CostUSD: fp(0.0102)},
		{Source: "record", Seq: 5, Accuracy: fp(0.5), CostUSD: fp(0.001), Spliced: bp(false)},
	}
	sortParetoPoints(points)
	order := make([]int, 0, len(points))
	for _, p := range points {
		order = append(order, p.Seq)
	}
	assert.Equal(t, []int{5, 0, 3, 2, 1, 4}, order)
}

func paretoTrendsDB(t *testing.T) string {
	t.Helper()
	dbPath := emptyStoreDB(t)
	base := time.Date(2026, 7, 12, 17, 0, 0, 0, time.UTC)
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "golden", RunIDs: []string{"base-0"}, CreatedAt: base, Stamps: currentStamps()})
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	records := []regress.Record{
		{
			V: regress.RecordVersion, CandidateSelector: "label:variant=old", CreatedAt: base.Add(time.Hour), BaselineCreatedAt: base,
			Report: regress.Report{Findings: []regress.Finding{costFinding(0.0104, 0.011)}},
		},
		{
			V: regress.RecordVersion, CandidateSelector: "label:variant=degraded", CreatedAt: base.Add(2 * time.Hour), BaselineCreatedAt: base.Add(-time.Hour),
			Report: regress.Report{Findings: []regress.Finding{verifierFinding(1, 0), costFinding(0.0102, 0.0102)}},
		},
		{
			V: regress.RecordVersion, CandidateSelector: "label:variant=cand", CreatedAt: base.Add(3 * time.Hour), BaselineCreatedAt: base,
			Report: regress.Report{Findings: []regress.Finding{verifierFinding(1, 1), costFinding(0.0102, 0.0102)}},
		},
	}
	for _, rec := range records {
		body, err := json.Marshal(rec)
		require.NoError(t, err)
		_, err = s.AppendRegressResult("golden", body)
		require.NoError(t, err)
	}
	require.NoError(t, s.Close())
	return dbPath
}

func TestTrendsParetoTableEndToEnd(t *testing.T) {
	dbPath := paretoTrendsDB(t)

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath, "--pareto"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 7)
	assert.Equal(t, []string{"SEQ", "CREATED", "CANDIDATE", "ACCURACY", "COST_USD", "DOMINATED"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"-", "-", "baseline", "1.00", "0.0102", "no"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"3", "2026-07-12T20:00:00Z", "label:variant=cand", "1.00", "0.0102", "no"}, strings.Fields(lines[2]))
	assert.Equal(t, []string{"2", "*", "2026-07-12T19:00:00Z", "label:variant=degraded", "0.00", "0.0102", "yes"}, strings.Fields(lines[3]))
	assert.Equal(t, []string{"1", "2026-07-12T18:00:00Z", "label:variant=old", "-", "0.0110", "-"}, strings.Fields(lines[4]))
	assert.Equal(t, spliceFootnote, lines[5])
	assert.Equal(t, "pareto: 1 row(s) lack an accuracy axis (no ann:verifier.pass finding) and are not compared", lines[6])
}

func TestRenderParetoTableWithoutEpilogues(t *testing.T) {
	base := time.Date(2026, 7, 12, 17, 0, 0, 0, time.UTC)
	records := []seqRecord{
		paretoRecord(1, "label:variant=cand", base.Add(time.Hour), base, verifierFinding(1, 1), costFinding(0.01, 0.01)),
	}
	var buf bytes.Buffer
	require.NoError(t, renderParetoTable(&buf, buildParetoReport("golden", records, base)))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	assert.NotContains(t, buf.String(), "*")
	assert.NotContains(t, buf.String(), "pareto:")
}

func TestRenderParetoTableMissingCostCell(t *testing.T) {
	base := time.Date(2026, 7, 12, 17, 0, 0, 0, time.UTC)
	records := []seqRecord{
		paretoRecord(1, "label:variant=cand", base.Add(time.Hour), base, verifierFinding(0.9, 0.8)),
	}
	var buf bytes.Buffer
	require.NoError(t, renderParetoTable(&buf, buildParetoReport("golden", records, base)))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, []string{"-", "-", "baseline", "0.90", "-", "-"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"1", "2026-07-12T18:00:00Z", "label:variant=cand", "0.80", "-", "-"}, strings.Fields(lines[2]))
	assert.Equal(t, "pareto: 2 row(s) lack an accuracy axis (no ann:verifier.pass finding) and are not compared", lines[3])
}

func TestTrendsParetoJSONEndToEnd(t *testing.T) {
	dbPath := paretoTrendsDB(t)

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath, "--pareto", "--json"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	assert.JSONEq(t, `{
		"baseline": "golden",
		"points": [
			{"source": "baseline", "accuracy": 1, "cost_usd": 0.0102, "dominated": false},
			{"source": "record", "seq": 3, "candidate": "label:variant=cand", "created_at": "2026-07-12T20:00:00Z", "accuracy": 1, "cost_usd": 0.0102, "dominated": false, "spliced": false},
			{"source": "record", "seq": 2, "candidate": "label:variant=degraded", "created_at": "2026-07-12T19:00:00Z", "accuracy": 0, "cost_usd": 0.0102, "dominated": true, "spliced": true},
			{"source": "record", "seq": 1, "candidate": "label:variant=old", "created_at": "2026-07-12T18:00:00Z", "cost_usd": 0.011, "spliced": false}
		]
	}`, out.String())

	var doc struct {
		Points []map[string]any `json:"points"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Len(t, doc.Points, 4)
	baselineKeys := doc.Points[0]
	assert.NotContains(t, baselineKeys, "seq")
	assert.NotContains(t, baselineKeys, "candidate")
	assert.NotContains(t, baselineKeys, "created_at")
	assert.NotContains(t, baselineKeys, "spliced")
	nonComparable := doc.Points[3]
	assert.NotContains(t, nonComparable, "accuracy")
	assert.NotContains(t, nonComparable, "dominated")
	assert.Contains(t, nonComparable, "cost_usd")
	assert.Contains(t, nonComparable, "spliced")
	degraded := doc.Points[2]
	assert.Equal(t, float64(0), degraded["accuracy"])
	assert.Equal(t, true, degraded["dominated"])
}

func TestTrendsParetoMetricConflict(t *testing.T) {
	dbPath := paretoTrendsDB(t)

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath, "--pareto", "--metric", "cost_usd"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "trends: --pareto and --metric are mutually exclusive")
}
