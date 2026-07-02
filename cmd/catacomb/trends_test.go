package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

type trendsEntry struct {
	Seq    int            `json:"seq"`
	Record regress.Record `json:"record"`
}

func trendsSeedRuns() []seedRun {
	runs := make([]seedRun, 0, 15)
	for i := 0; i < 5; i++ {
		runs = append(runs,
			seedRun{session: fmt.Sprintf("base-%d", i), labels: "variant=base", tools: 1, tokens: 100, durationMS: 1000},
			seedRun{session: fmt.Sprintf("c1-%d", i), labels: "variant=cand1", tools: 1, tokens: 100, durationMS: 1000},
			seedRun{session: fmt.Sprintf("c2-%d", i), labels: "variant=cand2", tools: 1, tokens: 5000, durationMS: 1000},
		)
	}
	return runs
}

func recordedTrendsDB(t *testing.T) (string, time.Time) {
	t.Helper()
	dbPath := seedRegressDB(t, trendsSeedRuns())
	ts := pinBaselineNow(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))
	var out, errBuf bytes.Buffer
	require.Equal(t, 0, run([]string{"regress", "--record", "--db", dbPath, "--baseline", "name:golden", "--candidate", "label:variant=cand1"}, &out, &errBuf))
	out.Reset()
	errBuf.Reset()
	require.Equal(t, 1, run([]string{"regress", "--record", "--db", dbPath, "--baseline", "name:golden", "--candidate", "label:variant=cand2"}, &out, &errBuf))
	return dbPath, ts
}

func TestTrendsRecordFlowEndToEnd(t *testing.T) {
	dbPath, ts := recordedTrendsDB(t)

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "SEQ")
	assert.Contains(t, lines[0], "DURATION_MS")
	assert.Contains(t, lines[0], "ERROR_RATE")
	assert.Contains(t, lines[1], "label:variant=cand1")
	assert.Contains(t, lines[1], "ok")
	assert.Contains(t, lines[1], ts.Format(time.RFC3339))
	assert.Contains(t, lines[2], "label:variant=cand2")
	assert.Contains(t, lines[2], "regression")

	out.Reset()
	errBuf.Reset()
	code = run([]string{"trends", "golden", "--db", dbPath, "--json"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	var entries []trendsEntry
	require.NoError(t, json.Unmarshal(out.Bytes(), &entries))
	require.Len(t, entries, 2)
	assert.Equal(t, 1, entries[0].Seq)
	assert.Equal(t, 2, entries[1].Seq)
	assert.Equal(t, "label:variant=cand1", entries[0].Record.CandidateSelector)
	assert.Equal(t, "label:variant=cand2", entries[1].Record.CandidateSelector)
	assert.Equal(t, regress.VerdictOK, entries[0].Record.Report.OverallVerdict)
	assert.Equal(t, regress.VerdictRegression, entries[1].Record.Report.OverallVerdict)
	assert.Equal(t, ts.UTC(), entries[0].Record.CreatedAt.UTC())

	f, ok := totalFinding(entries[1].Record.Report, "tokens_in")
	require.True(t, ok)
	assert.InDelta(t, 5000, f.Candidate, 0.001)
}

func TestTrendsRecordAnnotationsRoundTrip(t *testing.T) {
	dbPath := seedRegressAnnDB(t, "tool_correctness", 0.9)
	pinBaselineNow(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--record", "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
		"--annotation", "deepeval.tool_correctness:higher-better",
	}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	out.Reset()
	errBuf.Reset()
	code = run([]string{"trends", "golden", "--db", dbPath, "--json"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	var entries []trendsEntry
	require.NoError(t, json.Unmarshal(out.Bytes(), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, []regress.AnnotationSpec{{Key: "deepeval.tool_correctness", HigherBetter: true}}, entries[0].Record.Annotations)
}

func TestTrendsNarrowedTable(t *testing.T) {
	dbPath, _ := recordedTrendsDB(t)

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath, "--metric", "tokens_in"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "BASELINE-VALUE")
	assert.Contains(t, lines[0], "CANDIDATE-VALUE")
	assert.Contains(t, lines[0], "BAND")
	assert.Contains(t, lines[1], "100")
	assert.Contains(t, lines[2], "5000")
	assert.Contains(t, lines[2], "[")
}

func TestTrendsMetricValidation(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath, "--metric", "bogus"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "unknown --metric")
	assert.Contains(t, errBuf.String(), "duration_ms")
}

func TestTrendsUnknownBaseline(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "nope", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "baseline not found")
}

func TestTrendsZeroRecordsDistinctMessage(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))
	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.NotContains(t, errBuf.String(), "baseline not found")
	assert.Contains(t, errBuf.String(), "no recorded")
}

func TestTrendsMalformedBodyNamesSeq(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	_, err = s.AppendRegressResult("golden", json.RawMessage(`not-json`))
	require.NoError(t, err)
	require.NoError(t, s.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "seq 1")
	assert.Contains(t, errBuf.String(), "malformed")
}

func TestTrendsStoreMissing(t *testing.T) {
	missing := "/nonexistent/dir/nope.db"
	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", missing}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "no catacomb store")
}

func TestTrendsGetBaselineError(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO baselines(name, body) VALUES('x','not-json')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "x", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "get baseline")
}

func TestTrendsV1SchemaOutdated(t *testing.T) {
	dbPath := seedV1RegressDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "older than this binary")
}

func TestTrendsResultsScanError(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE regress_results")
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE regress_results (baseline TEXT NOT NULL, seq TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY (baseline, seq))")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO regress_results VALUES('golden','not-an-int','{}')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "RegressResultsFor")
}

func TestTrendsV2StoreReportsOutdated(t *testing.T) {
	dbPath := seedV2RegressDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"trends", "golden", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "older than this binary")
}

func TestTrendsCmdWiredAndGrouped(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "advanced", groups["trends"])
}

func TestTotalFindingAndMetricCandidate(t *testing.T) {
	assert.Equal(t, "-", totalMetricCandidate(regress.Report{}, "duration_ms"))
	rep := regress.Report{Findings: []regress.Finding{
		{Scope: "step", Metric: "duration_ms", Candidate: 9},
		{Scope: "total", Metric: "duration_ms", Candidate: 1234},
	}}
	f, ok := totalFinding(rep, "duration_ms")
	require.True(t, ok)
	assert.InDelta(t, 1234, f.Candidate, 0.001)
	assert.Equal(t, "1234", totalMetricCandidate(rep, "duration_ms"))
}

func TestFormatTrendBand(t *testing.T) {
	assert.Equal(t, "-", formatTrendBand(regress.Finding{}))
	assert.Equal(t, "[1, 3.5]", formatTrendBand(regress.Finding{BandLo: 1, BandHi: 3.5}))
}

func TestRenderTrendsMetricAbsentFinding(t *testing.T) {
	recs := []seqRecord{{seq: 1, rec: regress.Record{
		CandidateSelector: "label:variant=cand",
		CreatedAt:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Report:            regress.Report{OverallVerdict: regress.VerdictOK},
	}}}
	var buf bytes.Buffer
	require.NoError(t, renderTrendsMetric(&buf, recs, "duration_ms"))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	fields := strings.Fields(lines[1])
	assert.Equal(t, "-", fields[len(fields)-1])
}

func TestDecodeRecordsRoundTrip(t *testing.T) {
	rec := regress.Record{CandidateSelector: "label:x=y", CreatedAt: time.Unix(0, 0).UTC()}
	body, err := json.Marshal(rec)
	require.NoError(t, err)
	out, err := decodeRecords([]model.RegressResult{{Baseline: "g", Seq: 7, Body: body}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, 7, out[0].seq)
	assert.Equal(t, "label:x=y", out[0].rec.CandidateSelector)
}
