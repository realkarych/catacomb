package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

		mk := func(source model.Source, kind string, corr model.Correlation, attrs map[string]any) model.Observation {
			s := next()
			return model.Observation{
				ObsID: fmt.Sprintf("obs-%s-%d", execID, s), RunID: r.session, ExecutionID: execID,
				Source: source, Kind: kind, Correlation: corr, Attrs: attrs, Seq: s,
			}
		}
		msgID := "m-" + r.session

		ss := []model.Observation{mk(model.SourceHook, "session_start", model.Correlation{SessionID: r.session}, nil)}
		all = append(all, stampObs(ss, t0, r.labels, r.cwd)...)

		turn := []model.Observation{mk(model.SourceStreamJSON, "assistant_turn", model.Correlation{SessionID: r.session, MessageID: msgID}, map[string]any{"model": "claude-3", "tokens_in": r.tokens, "tokens_out": r.tokens})}
		for j := 0; j < r.tools; j++ {
			toolID := fmt.Sprintf("tu-%s-%d", r.session, j)
			turn = append(turn, mk(model.SourceStreamJSON, "assistant_tool_use", model.Correlation{SessionID: r.session, MessageID: msgID, ToolUseID: toolID}, map[string]any{"name": "Bash"}))
		}
		all = append(all, stampObs(turn, t0, r.labels, r.cwd)...)

		status := string(model.StatusOK)
		if r.isError {
			status = string(model.StatusError)
		}
		var res []model.Observation
		for j := 0; j < r.tools; j++ {
			toolID := fmt.Sprintf("tu-%s-%d", r.session, j)
			res = append(res, mk(model.SourceStreamJSON, "tool_result", model.Correlation{SessionID: r.session, ToolUseID: toolID}, map[string]any{"status": status}))
		}
		all = append(all, stampObs(res, t0, r.labels, r.cwd)...)

		se := []model.Observation{mk(model.SourceHook, "session_end", model.Correlation{SessionID: r.session}, map[string]any{"reason": "clear"})}
		all = append(all, stampObs(se, end, r.labels, r.cwd)...)
	}
	require.NoError(t, s.Persist(all, nil, nil))
	require.NoError(t, s.Close())
	return dbPath
}

func baseCandRuns() []seedRun {
	runs := make([]seedRun, 0, 10)
	for i := 0; i < 5; i++ {
		runs = append(runs,
			seedRun{session: fmt.Sprintf("base-%d", i), labels: "variant=base", tools: 1, tokens: 100, durationMS: 1000},
			seedRun{session: fmt.Sprintf("cand-%d", i), labels: "variant=cand", tools: 1, tokens: 100, durationMS: 1000},
		)
	}
	return runs
}

func openStore(s store.Store) storeOpener {
	return func(string) (store.Store, error) { return s, nil }
}

func seedV1RegressDB(t *testing.T) string {
	t.Helper()
	dbPath := seedRegressDB(t, baseCandRuns())
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE baselines")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return dbPath
}

func seedV2RegressDB(t *testing.T) string {
	t.Helper()
	dbPath := seedRegressDB(t, baseCandRuns())
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "golden", RunIDs: []string{"base-0"}, Stamps: currentStamps()})
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE regress_results")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 2")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return dbPath
}

func seedCurrentVersionDropTable(t *testing.T, table string) string {
	t.Helper()
	dbPath := seedRegressDB(t, baseCandRuns())
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("DROP TABLE " + table)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	return dbPath
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
		{"z", "1.96", func(th regress.Thresholds) string { return strconv.FormatFloat(th.Z, 'g', -1, 64) }},
	}
	for _, tc := range cases {
		var f regressFlags
		cmd := &cobra.Command{Use: "regress"}
		bindRegressFlags(cmd, &f)
		require.NoError(t, cmd.Flags().Set(tc.flag, tc.val))
		assert.Equal(t, tc.val, tc.got(f.thresholds), "flag %s", tc.flag)
	}
}

func TestRegressFailOnNotableFlagMaps(t *testing.T) {
	var f regressFlags
	cmd := &cobra.Command{Use: "regress"}
	bindRegressFlags(cmd, &f)
	require.NoError(t, cmd.Flags().Set("fail-on-notable", "true"))
	assert.True(t, f.thresholds.FailOnNotable)
}

func TestRegressRunsDirDefault(t *testing.T) {
	var f regressFlags
	cmd := &cobra.Command{Use: "regress"}
	bindRegressFlags(cmd, &f)
	rd := cmd.Flags().Lookup("runs-dir")
	require.NotNil(t, rd)
	assert.True(t, strings.HasSuffix(rd.DefValue, filepath.Join(".catacomb", "runs")) || rd.DefValue == "")
}

func TestRegressRequiresRunsDir(t *testing.T) {
	f := regressFlags{baseline: "label:variant=base", candidate: "label:variant=cand", thresholds: regress.DefaultThresholds()}
	err := runRegress(io.Discard, io.Discard, openStore(nil), newPricer, f)
	require.ErrorIs(t, err, errRegressNoRunsDir)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRegressMinSupportGuard(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--min-support", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "min-support")
}

func TestRegressZFlagRejectsNonPositive(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--z", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "--z must be > 0")
}

func TestRegressStrictInsufficientExitOne(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "overall insufficient")

	out.Reset()
	errBuf.Reset()
	code = run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--strict"}, &out, &errBuf)
	assert.Equal(t, 1, code)
}

func TestRegressBadAnnotationExitTwo(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--annotation", "deepeval.tool_correctness:sideways"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "sideways")
}

func TestRegressUnfiredAnnotationWarns(t *testing.T) {
	root := scoresEvidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", filepath.Join(t.TempDir(), "nope.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
		"--annotation", "owner.never",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.NotContains(t, out.String(), "ann:")
	assert.Contains(t, errBuf.String(), `annotation "owner.never" produced no findings`)
	assert.Contains(t, errBuf.String(), "step-key-eligible")
}

func TestParseAnnotationFlags(t *testing.T) {
	specs, keys, err := parseAnnotationFlags([]string{"deepeval.tool_correctness"})
	require.NoError(t, err)
	assert.Equal(t, []regress.AnnotationSpec{{Key: "deepeval.tool_correctness", HigherBetter: true}}, specs)
	assert.Equal(t, []string{"deepeval.tool_correctness"}, keys)

	specs, keys, err = parseAnnotationFlags([]string{"a.b:higher-better", "c.d:lower-better"})
	require.NoError(t, err)
	assert.Equal(t, []regress.AnnotationSpec{
		{Key: "a.b", HigherBetter: true},
		{Key: "c.d", HigherBetter: false},
	}, specs)
	assert.Equal(t, []string{"a.b", "c.d"}, keys)
}

func TestParseSelectorUnknownPrefix(t *testing.T) {
	_, _, err := parseSelector("phase:x=y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prefix")
}

func TestParseAnnotationFlagsEmpty(t *testing.T) {
	specs, keys, err := parseAnnotationFlags(nil)
	require.NoError(t, err)
	assert.Empty(t, specs)
	assert.Empty(t, keys)
}

func TestParseAnnotationFlagsErrors(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"bad suffix", []string{"a.b:sideways"}, "sideways"},
		{"trailing colon", []string{"owner.key:"}, `unknown direction ""`},
		{"empty key", []string{":higher-better"}, "empty key"},
		{"no dot", []string{"nodot:lower-better"}, "owner.key"},
		{"no dot default", []string{"nodot"}, "owner.key"},
		{"empty owner segment", []string{".b"}, "owner.key"},
		{"empty key segment", []string{"a."}, "owner.key"},
		{"double dot", []string{"a..b"}, "owner.key"},
		{"two dots", []string{"a.b.c"}, "owner.key"},
		{"duplicate", []string{"a.b", "a.b"}, "duplicate --annotation key"},
		{"duplicate mixed direction", []string{"a.b:higher-better", "a.b:lower-better"}, "duplicate --annotation key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseAnnotationFlags(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRegressCmdWired(t *testing.T) {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["regress"])
}

type appendErrStore struct {
	store.Store
}

func (a *appendErrStore) AppendRegressResult(string, json.RawMessage) (int, error) {
	return 0, errors.New("boom-append")
}

func TestRegressRecordBadBaselineSelector(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", filepath.Join(t.TempDir(), "b.db"),
		"--record", "--baseline", "bogus", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "invalid selector")
}

func TestRegressRecordMarshalError(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := emptyStoreDB(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))
	orig := marshalRecord
	marshalRecord = func(any) ([]byte, error) { return nil, errors.New("boom-marshal") }
	t.Cleanup(func() { marshalRecord = orig })

	f := regressFlags{runsDir: root, dbPath: dbPath, baseline: "name:golden", candidate: "label:variant=cand", thresholds: regress.DefaultThresholds(), record: true}
	err := runRegress(io.Discard, io.Discard, store.OpenSQLite, newPricer, f)
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, err.Error(), "boom-marshal")
}

func scoresEvidenceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		writeEvidenceRun(t, root, fmt.Sprintf("base-%d", i), "base", "session.jsonl")
		writeEvidenceRun(t, root, fmt.Sprintf("cand-%d", i), "cand", "session.jsonl")
	}
	return root
}

func fixtureStepKey(t *testing.T) string {
	t.Helper()
	g, err := loadGraphOffline(filepath.Join("testdata", "session.jsonl"), nil, newExecutionID(), newPricer(), nil)
	require.NoError(t, err)
	nodes, _ := g.Snapshot()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for _, n := range nodes {
		if n.StepKey != "" {
			return n.StepKey
		}
	}
	t.Fatal("fixture has no step-key-eligible node")
	return ""
}

func TestRegressScoresOfflineAnnotationRegression(t *testing.T) {
	root := scoresEvidenceRoot(t)
	sk := fixtureStepKey(t)
	lines := make([]string, 0, 6)
	for i := 0; i < 3; i++ {
		lines = append(lines,
			fmt.Sprintf(`{"step_key":%q,"key":"owner.quality","value":1,"run_id":"base-%d"}`, sk, i),
			fmt.Sprintf(`{"step_key":%q,"key":"owner.quality","value":0,"run_id":"cand-%d"}`, sk, i),
		)
	}
	scores := writeScoresFile(t, strings.Join(lines, "\n"))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", filepath.Join(t.TempDir(), "nope.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
		"--scores", scores, "--annotation", "owner.quality:higher-better",
	}, &out, &errBuf)
	assert.Equal(t, 1, code, out.String()+errBuf.String())
	assert.Contains(t, out.String(), "overall regression")
	assert.Contains(t, out.String(), "ann:owner.quality")
	assert.Empty(t, errBuf.String())
}

func TestRegressScoresOfflineAvsAOK(t *testing.T) {
	root := scoresEvidenceRoot(t)
	sk := fixtureStepKey(t)
	scores := writeScoresFile(t, fmt.Sprintf(`{"step_key":%q,"key":"owner.quality","value":1}`, sk))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", filepath.Join(t.TempDir(), "nope.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
		"--scores", scores, "--annotation", "owner.quality:higher-better",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, out.String()+errBuf.String())
	assert.Contains(t, out.String(), "overall ok")
	assert.Empty(t, errBuf.String())
}

func TestRegressScoresOfflineBadFileExitTwo(t *testing.T) {
	root := evidenceRoot(t)
	scores := writeScoresFile(t, `{"step_key":"sk","key":"owner.quality","value":1}`+"\n"+"{bad")

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", filepath.Join(t.TempDir(), "nope.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
		"--scores", scores,
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "line 2")
}

func TestRegressScoresMatchedNoNodeWarns(t *testing.T) {
	root := scoresEvidenceRoot(t)
	scores := writeScoresFile(t, `{"step_key":"ghost","key":"owner.quality","value":1}`)

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", filepath.Join(t.TempDir(), "nope.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
		"--scores", scores,
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "overall ok")
	assert.Contains(t, errBuf.String(), "matched no node")
}
