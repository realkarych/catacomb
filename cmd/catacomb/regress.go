package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

const (
	selectorLabel = "label"
	selectorName  = "name"
)

var marshalRecord = json.Marshal

var errRegressNoRunsDir = errors.New("regress: --runs-dir is required (home directory could not be resolved; set it explicitly)")

type regressFlags struct {
	baseline    string
	candidate   string
	dbPath      string
	runsDir     string
	asJSON      bool
	strict      bool
	record      bool
	annotations []string
	scores      string
	thresholds  regress.Thresholds
}

func newRegressCmd() *cobra.Command {
	f := regressFlags{}
	cmd := &cobra.Command{
		Use:   "regress",
		Short: "Compare a candidate run group against a baseline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			open := store.OpenSQLiteReadOnly
			if f.record {
				open = store.OpenSQLite
			}
			return runRegress(cmd.OutOrStdout(), cmd.ErrOrStderr(), open, newPricer, f)
		},
	}
	bindRegressFlags(cmd, &f)
	return cmd
}

func bindRegressFlags(cmd *cobra.Command, f *regressFlags) {
	def := regress.DefaultThresholds()
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.baseline, "baseline", "", "baseline selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.candidate, "candidate", "", "candidate selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.dbPath, "db", defaultDBPath(), "SQLite database path for name:/--record (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence dir to resolve selectors from: label: scans it, name: reads --db's baselines table, --record appends there")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "output JSON")
	cmd.Flags().BoolVar(&f.strict, "strict", false, "fail on insufficient data (exit 1); also refuse baselines with missing or mismatched version stamps (exit 2)")
	cmd.Flags().BoolVar(&f.record, "record", false, "append this result to the baseline's longitudinal history (requires --baseline name:<x>)")
	cmd.Flags().StringArrayVar(&f.annotations, "annotation", nil, "numeric annotation to gate on: owner.key[:higher-better|lower-better] (repeatable)")
	cmd.Flags().StringVar(&f.scores, "scores", "", "JSONL file of external scores applied as node annotations before comparison; one {\"step_key\",\"key\",\"value\"[,\"run_id\"]} object per line")
	cmd.Flags().IntVar(&f.thresholds.MinSupport, "min-support", def.MinSupport, "minimum runs per group for a trusted comparison")
	cmd.Flags().Float64Var(&f.thresholds.PresenceDelta, "presence-delta", def.PresenceDelta, "presence rate delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.ErrorRateDelta, "error-delta", def.ErrorRateDelta, "error rate delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.MetricRelDelta, "metric-rel-delta", def.MetricRelDelta, "relative metric delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.IQRFactor, "iqr-factor", def.IQRFactor, "IQR band factor")
	cmd.Flags().Float64Var(&f.thresholds.CoverageFloor, "coverage-floor", def.CoverageFloor, "step alignment coverage floor")
	cmd.Flags().Float64Var(&f.thresholds.Z, "z", def.Z, "one-sided Wilson z for rate gates (1.645 = 95% one-sided)")
	cmd.Flags().Float64Var(&f.thresholds.AnnotationRateDelta, "annotation-rate-delta", def.AnnotationRateDelta, "run-level binary annotation rate delta threshold (e.g. verifier.pass)")
	cmd.Flags().Float64Var(&f.thresholds.PairedAlpha, "paired-alpha", def.PairedAlpha, "paired sign-test significance level for per-task median deltas (0,1)")
	cmd.Flags().IntVar(&f.thresholds.PairedMinTasks, "paired-min-tasks", def.PairedMinTasks, "minimum matched tasks before the paired sign test gates")
	cmd.Flags().Float64Var(&f.thresholds.AuditIQRFactor, "audit-iqr-factor", def.AuditIQRFactor, "per-cell audit IQR band factor for outlier flags")
	cmd.Flags().Float64Var(&f.thresholds.AuditRelDelta, "audit-rel-delta", def.AuditRelDelta, "per-cell audit relative delta floor for outlier flags")
	cmd.Flags().BoolVar(&f.thresholds.FailOnNotable, "fail-on-notable", def.FailOnNotable, "count notable findings toward the gate (exit 1)")
}

func runRegress(out, errOut io.Writer, open storeOpener, mkPricer func() reduce.Pricer, f regressFlags) error {
	if f.thresholds.MinSupport < 1 {
		return operational(fmt.Errorf("regress: --min-support must be >= 1, got %d", f.thresholds.MinSupport))
	}
	if f.thresholds.Z <= 0 {
		return operational(fmt.Errorf("regress: --z must be > 0, got %g", f.thresholds.Z))
	}
	if f.thresholds.AnnotationRateDelta <= 0 {
		return operational(fmt.Errorf("regress: --annotation-rate-delta must be > 0, got %g", f.thresholds.AnnotationRateDelta))
	}
	if f.thresholds.PairedAlpha <= 0 || f.thresholds.PairedAlpha >= 1 {
		return operational(fmt.Errorf("regress: --paired-alpha must be in (0,1), got %g", f.thresholds.PairedAlpha))
	}
	if f.thresholds.PairedMinTasks < 1 {
		return operational(fmt.Errorf("regress: --paired-min-tasks must be > 0, got %d", f.thresholds.PairedMinTasks))
	}
	if f.thresholds.AuditIQRFactor <= 0 {
		return operational(fmt.Errorf("regress: --audit-iqr-factor must be > 0, got %g", f.thresholds.AuditIQRFactor))
	}
	if f.thresholds.AuditRelDelta <= 0 {
		return operational(fmt.Errorf("regress: --audit-rel-delta must be > 0, got %g", f.thresholds.AuditRelDelta))
	}
	if f.runsDir == "" {
		return operational(errRegressNoRunsDir)
	}
	specs, keys, err := parseAnnotationFlags(f.annotations)
	if err != nil {
		return operational(err)
	}
	baselineName, err := recordBaselineName(f)
	if err != nil {
		return operational(err)
	}
	pricer := mkPricer()
	baseGroup, baseline, err := resolveSelectorRunsDir(errOut, f.dbPath, f.runsDir, pricer, f.baseline)
	if err != nil {
		return err
	}
	if serr := checkBaselineStamps(errOut, baseline, f.strict); serr != nil {
		return serr
	}
	candGroup, _, err := resolveSelectorRunsDir(errOut, f.dbPath, f.runsDir, pricer, f.candidate)
	if err != nil {
		return err
	}
	if serr := applyScoresFile(errOut, f.scores, baseGroup, candGroup); serr != nil {
		return serr
	}
	rep, err := regressReport(out, errOut, f, specs, keys, baseGroup, candGroup)
	if err != nil {
		return err
	}
	if f.record {
		if aerr := appendRecordOffline(open, f, baselineName, baseline.CreatedAt, specs, rep); aerr != nil {
			return operational(aerr)
		}
	}
	return verdictError(rep, f.strict)
}

func checkBaselineStamps(errOut io.Writer, b model.Baseline, strict bool) error {
	if b.Name == "" {
		return nil
	}
	if b.Stamps.Zero() {
		return stampIssue(errOut, strict, fmt.Errorf("baseline %s has no version stamps (pre-PV-2)", b.Name))
	}
	cur := currentStamps()
	if b.Stamps.Mismatch(cur) {
		return stampIssue(errOut, strict, fmt.Errorf(
			"baseline %s version stamps differ: recorded catacomb=%s stepkey=%s, current catacomb=%s stepkey=%s",
			b.Name, b.Stamps.CatacombVersion, b.Stamps.StepKeyScheme, cur.CatacombVersion, cur.StepKeyScheme))
	}
	return nil
}

func stampIssue(errOut io.Writer, strict bool, err error) error {
	if strict {
		return operational(err)
	}
	fmt.Fprintf(errOut, "warning: %s\n", err)
	return nil
}

func appendRecordOffline(open storeOpener, f regressFlags, baselineName string, baselineCreatedAt time.Time, specs []regress.AnnotationSpec, rep regress.Report) error {
	s, err := openWriteStore(open, f.dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return appendRecord(s, baselineName, baselineCreatedAt, f, specs, rep)
}

func regressReport(out, errOut io.Writer, f regressFlags, specs []regress.AnnotationSpec, keys []string, baseGroup, candGroup []aggregate.RunGraph) (regress.Report, error) {
	opts := aggregate.Options{AnnotationKeys: keys}
	baseAgg := aggregate.Aggregate(baseGroup, opts)
	candAgg := aggregate.Aggregate(candGroup, opts)
	rep := regress.Compare(regress.Input{
		Baseline:       baseAgg,
		Candidate:      candAgg,
		Annotations:    specs,
		BaselineCells:  aggregate.Cells(baseGroup),
		CandidateCells: aggregate.Cells(candGroup),
	}, f.thresholds)
	warnUnfiredAnnotations(errOut, specs, baseAgg, candAgg)
	if f.asJSON {
		if err := regress.RenderJSON(rep, out); err != nil {
			return regress.Report{}, operational(err)
		}
	} else {
		regress.RenderHuman(rep, out)
	}
	return rep, nil
}

func recordBaselineName(f regressFlags) (string, error) {
	if !f.record {
		return "", nil
	}
	kind, val, err := parseSelector(f.baseline)
	if err != nil {
		return "", err
	}
	if kind != selectorName {
		return "", fmt.Errorf("regress --record requires --baseline in name:<baseline> form, got %q", f.baseline)
	}
	return val, nil
}

func appendRecord(s store.Store, baselineName string, baselineCreatedAt time.Time, f regressFlags, specs []regress.AnnotationSpec, rep regress.Report) error {
	body, err := marshalRecord(regress.Record{
		V:                 regress.RecordVersion,
		CandidateSelector: f.candidate,
		Thresholds:        f.thresholds,
		Annotations:       specs,
		Report:            rep,
		CreatedAt:         nowFn().UTC(),
		BaselineCreatedAt: baselineCreatedAt,
		Stamps:            currentStamps(),
	})
	if err != nil {
		return fmt.Errorf("regress --record marshal: %w", err)
	}
	if _, err := s.AppendRegressResult(baselineName, body); err != nil {
		return fmt.Errorf("regress --record: %w", err)
	}
	return nil
}

func parseAnnotationFlags(raw []string) ([]regress.AnnotationSpec, []string, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	specs := make([]regress.AnnotationSpec, 0, len(raw))
	keys := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, v := range raw {
		spec, err := parseAnnotationSpec(v)
		if err != nil {
			return nil, nil, err
		}
		if _, dup := seen[spec.Key]; dup {
			return nil, nil, fmt.Errorf("duplicate --annotation key %q", spec.Key)
		}
		seen[spec.Key] = struct{}{}
		specs = append(specs, spec)
		keys = append(keys, spec.Key)
	}
	return specs, keys, nil
}

func parseAnnotationSpec(v string) (regress.AnnotationSpec, error) {
	key := v
	higherBetter := true
	if i := strings.LastIndex(v, ":"); i >= 0 {
		switch v[i+1:] {
		case "higher-better":
			key = v[:i]
		case "lower-better":
			higherBetter = false
			key = v[:i]
		default:
			return regress.AnnotationSpec{}, fmt.Errorf("invalid --annotation %q: unknown direction %q (want higher-better or lower-better)", v, v[i+1:])
		}
	}
	if key == "" {
		return regress.AnnotationSpec{}, fmt.Errorf("invalid --annotation %q: empty key", v)
	}
	if !validAnnotationKey(key) {
		return regress.AnnotationSpec{}, fmt.Errorf("invalid --annotation %q: key %q must be owner.key", v, key)
	}
	return regress.AnnotationSpec{Key: key, HigherBetter: higherBetter}, nil
}

func validAnnotationKey(key string) bool {
	owner, rest, found := strings.Cut(key, ".")
	return found && owner != "" && rest != "" && !strings.Contains(rest, ".")
}

func warnUnfiredAnnotations(errOut io.Writer, specs []regress.AnnotationSpec, base, cand aggregate.Report) {
	for _, spec := range specs {
		if annotationFired(base, spec.Key) || annotationFired(cand, spec.Key) {
			continue
		}
		fmt.Fprintf(errOut, "warning: annotation %q produced no findings (key never fired on any step; check the key and that scores landed on step-key-eligible nodes)\n", spec.Key)
	}
}

func annotationFired(rep aggregate.Report, key string) bool {
	if _, ok := rep.Totals.Annotations[key]; ok {
		return true
	}
	for _, r := range rep.Steps {
		if _, ok := r.Annotations[key]; ok {
			return true
		}
	}
	return false
}

func verdictError(rep regress.Report, strict bool) error {
	if rep.OverallVerdict == regress.VerdictRegression {
		return errRegressionDetected
	}
	if strict && rep.OverallVerdict == regress.VerdictInsufficient {
		return errRegressionDetected
	}
	return nil
}

func parseSelector(sel string) (string, string, error) {
	kind, val, found := strings.Cut(sel, ":")
	if !found || val == "" {
		return "", "", fmt.Errorf("invalid selector %q: expected label:k=v[,k=v...] or name:<baseline>", sel)
	}
	if kind != selectorLabel && kind != selectorName {
		return "", "", fmt.Errorf("invalid selector %q: unknown prefix %q (want label: or name:)", sel, kind)
	}
	return kind, val, nil
}
