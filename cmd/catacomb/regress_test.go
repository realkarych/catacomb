package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/hook"
	"github.com/realkarych/catacomb/ingest/streamjson"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

type seedRun struct {
	session    string
	labels     string
	tools      int
	isError    bool
	tokens     int64
	durationMS int64
	cwd        string
}

func stampObs(obs []model.Observation, at time.Time, labels, cwd string) []model.Observation {
	for i := range obs {
		obs[i].EventTime = at
		obs[i].ObservedAt = at
		if labels == "" && cwd == "" {
			continue
		}
		if obs[i].Attrs == nil {
			obs[i].Attrs = map[string]any{}
		}
		if labels != "" {
			obs[i].Attrs["catacomb.labels"] = labels
		}
		if cwd != "" {
			obs[i].Attrs["cwd"] = cwd
		}
	}
	return obs
}

func seedRegressDB(t *testing.T, runs []seedRun) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "r.db")
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	var seq uint64
	next := func() uint64 { seq++; return seq }
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var all []model.Observation
	for i, r := range runs {
		execID := fmt.Sprintf("exec-%03d", i)
		end := t0.Add(time.Duration(r.durationMS) * time.Millisecond)

		ss, err := hook.Parse("SessionStart", []byte(fmt.Sprintf(`{"session_id":%q}`, r.session)), execID, next)
		require.NoError(t, err)
		all = append(all, stampObs(ss, t0, r.labels, r.cwd)...)

		blocks, results := "", ""
		for j := 0; j < r.tools; j++ {
			if j > 0 {
				blocks += ","
				results += ","
			}
			blocks += fmt.Sprintf(`{"type":"tool_use","id":"tu-%s-%d","name":"Bash","input":{}}`, r.session, j)
			results += fmt.Sprintf(`{"type":"tool_result","tool_use_id":"tu-%s-%d","is_error":%t,"content":"x"}`, r.session, j, r.isError)
		}
		asst := fmt.Sprintf(`{"type":"assistant","session_id":%q,"message":{"id":"m-%s","model":"claude-3","content":[%s],"usage":{"input_tokens":%d,"output_tokens":%d}}}`, r.session, r.session, blocks, r.tokens, r.tokens)
		ao, err := streamjson.Parse([]byte(asst), execID, next)
		require.NoError(t, err)
		all = append(all, stampObs(ao, t0, r.labels, r.cwd)...)

		usr := fmt.Sprintf(`{"type":"user","session_id":%q,"message":{"content":[%s]}}`, r.session, results)
		uo, err := streamjson.Parse([]byte(usr), execID, next)
		require.NoError(t, err)
		all = append(all, stampObs(uo, t0, r.labels, r.cwd)...)

		se, err := hook.Parse("SessionEnd", []byte(fmt.Sprintf(`{"session_id":%q,"reason":"clear"}`, r.session)), execID, next)
		require.NoError(t, err)
		all = append(all, stampObs(se, end, r.labels, r.cwd)...)
	}
	require.NoError(t, s.Persist(all, nil, nil))
	require.NoError(t, s.Close())
	return dbPath
}

func baseCandRuns(count int, candErr bool, candTok int64) []seedRun {
	runs := make([]seedRun, 0, count*2)
	for i := 0; i < count; i++ {
		runs = append(runs,
			seedRun{session: fmt.Sprintf("base-%d", i), labels: "variant=base", tools: 1, tokens: 100, durationMS: 1000},
			seedRun{session: fmt.Sprintf("cand-%d", i), labels: "variant=cand", tools: 1, isError: candErr, tokens: candTok, durationMS: 1000},
		)
	}
	return runs
}

func openStore(s store.Store) storeOpener {
	return func(string) (store.Store, error) { return s, nil }
}

func defaultRegressFlags(dbPath string) regressFlags {
	return regressFlags{
		baseline:   "label:variant=base",
		candidate:  "label:variant=cand",
		dbPath:     dbPath,
		thresholds: regress.DefaultThresholds(),
	}
}

func TestRegressIdenticalGroupsOK(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var buf strings.Builder
	err := runRegress(&buf, io.Discard, store.OpenSQLiteReadOnly, newPricer, defaultRegressFlags(dbPath))
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "overall ok")
}

func TestRegressIdenticalExitZeroViaRun(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Empty(t, errBuf.String())
}

func TestRegressErrorJumpRegressionExitOne(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, true, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "overall regression")
	assert.Empty(t, errBuf.String())
}

func TestRegressMetricRelDeltaFlagReachesCompare(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 5000))

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 1, code)

	out.Reset()
	errBuf.Reset()
	code = run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--metric-rel-delta", "1000"}, &out, &errBuf)
	assert.Equal(t, 0, code)
}

func TestRegressJSONParses(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, true, 100))
	f := defaultRegressFlags(dbPath)
	f.asJSON = true
	var buf bytes.Buffer
	err := runRegress(&buf, io.Discard, store.OpenSQLiteReadOnly, newPricer, f)
	require.ErrorIs(t, err, errRegressionDetected)
	var rep regress.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rep))
	assert.Equal(t, regress.VerdictRegression, rep.OverallVerdict)
	assert.Equal(t, 5, rep.BaselineRuns)
}

func TestRegressJSONRenderError(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	f := defaultRegressFlags(dbPath)
	f.asJSON = true
	err := runRegress(failWriter{}, io.Discard, store.OpenSQLiteReadOnly, newPricer, f)
	require.Error(t, err)
	assert.NotErrorIs(t, err, errRegressionDetected)
}

func TestRegressNameSelectorResolves(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))

	var buf strings.Builder
	err := runRegress(&buf, io.Discard, store.OpenSQLiteReadOnly, newPricer, regressFlags{
		baseline:   "name:golden",
		candidate:  "label:variant=cand",
		dbPath:     dbPath,
		thresholds: regress.DefaultThresholds(),
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "baseline runs 5")
}

func TestRegressUnknownNameOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "name:nope", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "baseline not found")
}

func TestRegressStrictInsufficientExitOne(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(2, false, 100))

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "overall insufficient")

	out.Reset()
	errBuf.Reset()
	code = run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--strict"}, &out, &errBuf)
	assert.Equal(t, 1, code)
}

func TestRegressStoreMissingOperational(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", missing, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "no catacomb store")
}

func TestRegressBadBaselineSelectorOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "bogus", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "invalid selector")
}

func TestRegressBadCandidateSelectorOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "bogus"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "invalid selector")
}

func TestRegressUnknownPrefixOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	_, err := resolveSelector(io.Discard, nil, newPricer(), "phase:x=y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prefix")
	_ = dbPath
}

func TestRegressBadLabelSelectorOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:BAD=x", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "invalid --label")
}

func TestRegressEmptyBaselineGroupOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=none", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "matched no runs")
}

func TestRegressEmptyCandidateGroupOperational(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=none"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "matched no runs")
}

func TestRegressLabelLoadError(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE observations")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "store read")
}

func TestRegressGetBaselineError(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO baselines(name, body) VALUES('x','not-json')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "name:x", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "get baseline")
}

func TestRegressMinSupportGuard(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--min-support", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "min-support")
}

func TestRegressThresholdFlagsMapToFields(t *testing.T) {
	cases := []struct {
		flag string
		val  string
		got  func(regress.Thresholds) string
	}{
		{"min-support", "9", func(th regress.Thresholds) string { return strconv.Itoa(th.MinSupport) }},
		{"presence-delta", "0.42", func(th regress.Thresholds) string { return strconv.FormatFloat(th.PresenceDelta, 'g', -1, 64) }},
		{"error-delta", "0.31", func(th regress.Thresholds) string { return strconv.FormatFloat(th.ErrorRateDelta, 'g', -1, 64) }},
		{"metric-rel-delta", "0.53", func(th regress.Thresholds) string { return strconv.FormatFloat(th.MetricRelDelta, 'g', -1, 64) }},
		{"iqr-factor", "2.5", func(th regress.Thresholds) string { return strconv.FormatFloat(th.IQRFactor, 'g', -1, 64) }},
		{"coverage-floor", "0.8", func(th regress.Thresholds) string { return strconv.FormatFloat(th.CoverageFloor, 'g', -1, 64) }},
	}
	for _, tc := range cases {
		var f regressFlags
		cmd := &cobra.Command{Use: "regress"}
		bindRegressFlags(cmd, &f)
		require.NoError(t, cmd.Flags().Set(tc.flag, tc.val))
		assert.Equal(t, tc.val, tc.got(f.thresholds), "flag %s", tc.flag)
	}
}

func seedV1RegressDB(t *testing.T) string {
	t.Helper()
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE baselines")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return dbPath
}

func TestRegressNameSelectorLoadError(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE observations")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "name:golden", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "store read")
}

func TestRegressNameSelectorV1StoreHint(t *testing.T) {
	dbPath := seedV1RegressDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "name:golden", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "older than this binary")
}

func TestRegressLabelOnlyV1StoreWorks(t *testing.T) {
	dbPath := seedV1RegressDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "overall ok")
	assert.Empty(t, errBuf.String())
}

func TestRegressNameSelectorFewerRunsWarns(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "golden", RunIDs: []string{"base-0", "ghost-1"}}))
	require.NoError(t, s.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--db", dbPath, "--baseline", "name:golden", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Contains(t, errBuf.String(), "resolved 1 < stored 2")
}

func TestRegressCmdWiredAndGrouped(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "advanced", groups["regress"])
}
