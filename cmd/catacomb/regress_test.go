package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
}

func stampObs(obs []model.Observation, at time.Time, labels string) []model.Observation {
	for i := range obs {
		obs[i].EventTime = at
		obs[i].ObservedAt = at
		if labels != "" {
			if obs[i].Attrs == nil {
				obs[i].Attrs = map[string]any{}
			}
			obs[i].Attrs["catacomb.labels"] = labels
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
		all = append(all, stampObs(ss, t0, r.labels)...)

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
		all = append(all, stampObs(ao, t0, r.labels)...)

		usr := fmt.Sprintf(`{"type":"user","session_id":%q,"message":{"content":[%s]}}`, r.session, results)
		uo, err := streamjson.Parse([]byte(usr), execID, next)
		require.NoError(t, err)
		all = append(all, stampObs(uo, t0, r.labels)...)

		se, err := hook.Parse("SessionEnd", []byte(fmt.Sprintf(`{"session_id":%q,"reason":"clear"}`, r.session)), execID, next)
		require.NoError(t, err)
		all = append(all, stampObs(se, end, r.labels)...)
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
	err := runRegress(&buf, store.OpenSQLiteReadOnly, newPricer, defaultRegressFlags(dbPath))
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
	err := runRegress(&buf, store.OpenSQLiteReadOnly, newPricer, f)
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
	err := runRegress(failWriter{}, store.OpenSQLiteReadOnly, newPricer, f)
	require.Error(t, err)
	assert.NotErrorIs(t, err, errRegressionDetected)
}

func TestRegressNameSelectorResolves(t *testing.T) {
	dbPath := seedRegressDB(t, baseCandRuns(5, false, 100))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))

	var buf strings.Builder
	err := runRegress(&buf, store.OpenSQLiteReadOnly, newPricer, regressFlags{
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
	_, err := resolveSelector(nil, newPricer(), "phase:x=y")
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
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runRegress(io.Discard, openStore(&obsErrStore{}), newPricer, defaultRegressFlags(f.Name()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
	assert.NotErrorIs(t, err, errRegressionDetected)
}

type getBaselineErrStore struct {
	fakeStore
}

func (g *getBaselineErrStore) GetBaseline(string) (model.Baseline, bool, error) {
	return model.Baseline{}, false, errors.New("boom")
}

func TestRegressGetBaselineError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	flags := defaultRegressFlags(f.Name())
	flags.baseline = "name:x"
	err = runRegress(io.Discard, openStore(&getBaselineErrStore{}), newPricer, flags)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get baseline")
}

func TestRegressCmdWiredAndGrouped(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "advanced", groups["regress"])
}
