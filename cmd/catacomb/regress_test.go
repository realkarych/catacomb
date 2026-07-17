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

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

func seedRegressDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "r.db")
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	return dbPath
}

func openStore(s store.Store) storeOpener {
	return func(string) (store.Store, error) { return s, nil }
}

func seedV1RegressDB(t *testing.T) string {
	t.Helper()
	dbPath := seedRegressDB(t)
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
	dbPath := seedRegressDB(t)
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
	dbPath := seedRegressDB(t)
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
		{"annotation-rate-delta", "0.15", func(th regress.Thresholds) string { return strconv.FormatFloat(th.AnnotationRateDelta, 'g', -1, 64) }},
		{"paired-alpha", "0.02", func(th regress.Thresholds) string { return strconv.FormatFloat(th.PairedAlpha, 'g', -1, 64) }},
		{"paired-min-tasks", "7", func(th regress.Thresholds) string { return strconv.Itoa(th.PairedMinTasks) }},
		{"audit-iqr-factor", "4.5", func(th regress.Thresholds) string { return strconv.FormatFloat(th.AuditIQRFactor, 'g', -1, 64) }},
		{"audit-rel-delta", "0.7", func(th regress.Thresholds) string { return strconv.FormatFloat(th.AuditRelDelta, 'g', -1, 64) }},
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

func TestRegressAnnotationRateDeltaRejectsNonPositive(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--annotation-rate-delta", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "--annotation-rate-delta must be > 0")
}

func TestRegressPairedAlphaRejectsOutOfRange(t *testing.T) {
	root := evidenceRoot(t)
	for _, val := range []string{"0", "1"} {
		var out, errBuf bytes.Buffer
		code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--paired-alpha", val}, &out, &errBuf)
		assert.Equal(t, 2, code)
		assert.Contains(t, errBuf.String(), "--paired-alpha must be in (0,1)")
	}
}

func TestRegressPairedMinTasksRejectsNonPositive(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--paired-min-tasks", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "--paired-min-tasks must be > 0")
}

func TestRegressAuditIQRFactorRejectsNonPositive(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--audit-iqr-factor", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "--audit-iqr-factor must be > 0")
}

func TestRegressAuditRelDeltaRejectsNonPositive(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--audit-rel-delta", "-1"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "--audit-rel-delta must be > 0")
}

func TestRegressAuditBlockEndToEnd(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		writeTokenEvidenceRun(t, root, fmt.Sprintf("base-%d", i), "base", 10)
		writeTokenEvidenceRun(t, root, fmt.Sprintf("cand-%d", i), "cand", 10)
	}
	writeTokenEvidenceRun(t, root, "cand-3", "cand", 1000)

	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand", "--json"}, &out, &errBuf)
	assert.Equal(t, 0, code, out.String()+errBuf.String())
	var rep regress.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	require.NotNil(t, rep.Audit)
	assert.Empty(t, rep.Audit.Baseline)
	require.NotEmpty(t, rep.Audit.Candidate)
	metrics := make([]string, 0, len(rep.Audit.Candidate))
	for _, fl := range rep.Audit.Candidate {
		assert.Equal(t, "cand-3", fl.RunID)
		metrics = append(metrics, fl.Metric)
	}
	assert.Contains(t, metrics, "tokens_in")
}

func TestRegressJSONOmitsAuditAvsA(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=base", "--json"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.NotContains(t, out.String(), `"audit"`)
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

func TestWarnUnfiredAnnotationRunLevelOnly(t *testing.T) {
	specs := []regress.AnnotationSpec{{Key: "judge.groundedness", HigherBetter: true}}
	base := aggregate.Report{
		Totals: aggregate.RunTotals{
			Annotations: map[string]aggregate.AnnotationTotals{
				"judge.groundedness": {N: 5, Binary: false},
			},
		},
	}
	var errBuf bytes.Buffer
	warnUnfiredAnnotations(&errBuf, specs, base, aggregate.Report{})
	assert.Empty(t, errBuf.String())
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

func runRegressCLI(t *testing.T, root string, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"regress", "--runs-dir", root, "--baseline", "label:variant=base", "--candidate", "label:variant=cand"}, extra...)
	var out, errBuf bytes.Buffer
	code := run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestRegressFormatFlagsBound(t *testing.T) {
	var f regressFlags
	cmd := &cobra.Command{Use: "regress"}
	bindRegressFlags(cmd, &f)
	ff := cmd.Flags().Lookup("format")
	require.NotNil(t, ff)
	assert.Equal(t, "human", ff.DefValue)
	jf := cmd.Flags().Lookup("json")
	require.NotNil(t, jf)
	assert.Equal(t, "use --format json", jf.Deprecated)
	assert.True(t, jf.Hidden)
}

func TestRegressFormatJSONMatchesDeprecatedJSON(t *testing.T) {
	root := evidenceRoot(t)
	codeFormat, outFormat, _ := runRegressCLI(t, root, "--format", "json")
	codeJSON, outJSON, errJSON := runRegressCLI(t, root, "--json")
	assert.Equal(t, 0, codeFormat)
	assert.Equal(t, 0, codeJSON)
	require.NotEmpty(t, outFormat)
	assert.Equal(t, outFormat, outJSON)
	assert.Contains(t, errJSON, "use --format json")
	var rep regress.Report
	require.NoError(t, json.Unmarshal([]byte(outJSON), &rep))
}

func TestRegressFormatHumanMatchesDefault(t *testing.T) {
	root := evidenceRoot(t)
	codeDefault, outDefault, _ := runRegressCLI(t, root)
	codeHuman, outHuman, _ := runRegressCLI(t, root, "--format", "human")
	assert.Equal(t, 0, codeDefault)
	assert.Equal(t, 0, codeHuman)
	require.NotEmpty(t, outDefault)
	assert.Equal(t, outDefault, outHuman)
}

func TestRegressFormatExplicitWinsOverDeprecatedJSON(t *testing.T) {
	root := evidenceRoot(t)
	_, outHuman, _ := runRegressCLI(t, root)
	code, out, _ := runRegressCLI(t, root, "--format", "human", "--json")
	assert.Equal(t, 0, code)
	require.NotEmpty(t, outHuman)
	assert.Equal(t, outHuman, out)
}

func TestRegressFormatUnknownExitTwo(t *testing.T) {
	root := evidenceRoot(t)
	code, _, errOut := runRegressCLI(t, root, "--format", "bogus")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, `regress --format: unknown format "bogus" (want human|json|markdown)`)
}

func TestRegressFormatMarkdownRenders(t *testing.T) {
	root := evidenceRoot(t)
	code, out, _ := runRegressCLI(t, root, "--format", "markdown")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "**Verdict:")
	assert.Contains(t, out, "| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |")
	assert.Contains(t, out, "|---|---|---|---|---|---|---|---|---|")
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
