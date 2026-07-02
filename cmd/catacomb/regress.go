package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type regressFlags struct {
	baseline    string
	candidate   string
	dbPath      string
	asJSON      bool
	strict      bool
	record      bool
	annotations []string
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
	cmd.Flags().StringVar(&f.baseline, "baseline", "", "baseline selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.candidate, "candidate", "", "candidate selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "output JSON")
	cmd.Flags().BoolVar(&f.strict, "strict", false, "treat insufficient data as failure (exit 1)")
	cmd.Flags().BoolVar(&f.record, "record", false, "append this result to the baseline's longitudinal history (requires --baseline name:<x>)")
	cmd.Flags().StringArrayVar(&f.annotations, "annotation", nil, "numeric annotation to gate on: owner.key[:higher-better|lower-better] (repeatable)")
	cmd.Flags().IntVar(&f.thresholds.MinSupport, "min-support", def.MinSupport, "minimum runs per group for a trusted comparison")
	cmd.Flags().Float64Var(&f.thresholds.PresenceDelta, "presence-delta", def.PresenceDelta, "presence rate delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.ErrorRateDelta, "error-delta", def.ErrorRateDelta, "error rate delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.MetricRelDelta, "metric-rel-delta", def.MetricRelDelta, "relative metric delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.IQRFactor, "iqr-factor", def.IQRFactor, "IQR band factor")
	cmd.Flags().Float64Var(&f.thresholds.CoverageFloor, "coverage-floor", def.CoverageFloor, "step alignment coverage floor")
}

func runRegress(out, errOut io.Writer, open storeOpener, mkPricer func() reduce.Pricer, f regressFlags) error {
	if f.thresholds.MinSupport < 1 {
		return operational(fmt.Errorf("regress: --min-support must be >= 1, got %d", f.thresholds.MinSupport))
	}
	specs, keys, err := parseAnnotationFlags(f.annotations)
	if err != nil {
		return operational(err)
	}
	baselineName, err := recordBaselineName(f)
	if err != nil {
		return operational(err)
	}
	s, err := openReadStore(open, f.dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()

	pricer := mkPricer()
	baseGroup, baseline, err := resolveSelector(errOut, s, pricer, f.baseline)
	if err != nil {
		return err
	}
	if len(baseGroup) == 0 {
		return operational(fmt.Errorf("regress baseline %q: %w", f.baseline, ErrEmptyGroup))
	}
	candGroup, _, err := resolveSelector(errOut, s, pricer, f.candidate)
	if err != nil {
		return err
	}
	if len(candGroup) == 0 {
		return operational(fmt.Errorf("regress candidate %q: %w", f.candidate, ErrEmptyGroup))
	}

	opts := aggregate.Options{AnnotationKeys: keys}
	baseAgg := aggregate.Aggregate(baseGroup, opts)
	candAgg := aggregate.Aggregate(candGroup, opts)
	rep := regress.Compare(regress.Input{
		Baseline:    baseAgg,
		Candidate:   candAgg,
		Annotations: specs,
	}, f.thresholds)
	warnUnfiredAnnotations(errOut, specs, baseAgg, candAgg)

	if f.asJSON {
		if err := regress.RenderJSON(rep, out); err != nil {
			return operational(err)
		}
	} else {
		regress.RenderHuman(rep, out)
	}
	if f.record {
		if err := appendRecord(s, baselineName, baseline.CreatedAt, f, specs, rep); err != nil {
			return operational(err)
		}
	}
	return verdictError(rep, f.strict)
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
	owner, rest, found := strings.Cut(key, ".")
	if !found || owner == "" || rest == "" || strings.Contains(rest, ".") {
		return regress.AnnotationSpec{}, fmt.Errorf("invalid --annotation %q: key %q must be owner.key", v, key)
	}
	return regress.AnnotationSpec{Key: key, HigherBetter: higherBetter}, nil
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

func resolveSelector(errOut io.Writer, s store.Store, pricer reduce.Pricer, sel string) ([]aggregate.RunGraph, model.Baseline, error) {
	kind, val, err := parseSelector(sel)
	if err != nil {
		return nil, model.Baseline{}, operational(err)
	}
	if kind == selectorName {
		return resolveNameSelector(errOut, s, pricer, val)
	}
	group, err := resolveLabelSelector(s, pricer, val)
	return group, model.Baseline{}, err
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

func resolveLabelSelector(s store.Store, pricer reduce.Pricer, val string) ([]aggregate.RunGraph, error) {
	if err := validateLabelTerms(strings.Split(val, ",")); err != nil {
		return nil, operational(err)
	}
	group, err := loadRunGroup(s, pricer, model.ParseLabels(val))
	if err != nil {
		return nil, operational(err)
	}
	return group, nil
}

func resolveNameSelector(errOut io.Writer, s store.Store, pricer reduce.Pricer, name string) ([]aggregate.RunGraph, model.Baseline, error) {
	b, ok, err := s.GetBaseline(name)
	if err != nil {
		if errors.Is(err, store.ErrSchemaOutdated) {
			return nil, model.Baseline{}, operational(store.ErrSchemaOutdated)
		}
		return nil, model.Baseline{}, operational(fmt.Errorf("regress get baseline %q: %w", name, err))
	}
	if !ok {
		return nil, model.Baseline{}, operational(fmt.Errorf("%w: %q", ErrBaselineNotFound, name))
	}
	group, err := loadRunGroupByIDs(s, pricer, b.RunIDs)
	if err != nil {
		return nil, model.Baseline{}, operational(err)
	}
	if len(group) < len(b.RunIDs) {
		fmt.Fprintf(errOut, "warning: baseline %q resolved %d < stored %d runs\n", name, len(group), len(b.RunIDs))
	}
	return group, b, nil
}
