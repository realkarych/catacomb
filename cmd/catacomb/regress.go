package main

import (
	"fmt"
	"io"
	"strings"

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

type regressFlags struct {
	baseline   string
	candidate  string
	dbPath     string
	asJSON     bool
	strict     bool
	thresholds regress.Thresholds
}

func newRegressCmd() *cobra.Command {
	f := regressFlags{}
	def := regress.DefaultThresholds()
	cmd := &cobra.Command{
		Use:   "regress",
		Short: "Compare a candidate run group against a baseline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRegress(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, newPricer, f)
		},
	}
	cmd.Flags().StringVar(&f.baseline, "baseline", "", "baseline selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.candidate, "candidate", "", "candidate selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "output JSON")
	cmd.Flags().BoolVar(&f.strict, "strict", false, "treat insufficient data as failure (exit 1)")
	cmd.Flags().IntVar(&f.thresholds.MinSupport, "min-support", def.MinSupport, "minimum runs per group for a trusted comparison")
	cmd.Flags().Float64Var(&f.thresholds.PresenceDelta, "presence-delta", def.PresenceDelta, "presence rate delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.ErrorRateDelta, "error-delta", def.ErrorRateDelta, "error rate delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.MetricRelDelta, "metric-rel-delta", def.MetricRelDelta, "relative metric delta threshold")
	cmd.Flags().Float64Var(&f.thresholds.IQRFactor, "iqr-factor", def.IQRFactor, "IQR band factor")
	cmd.Flags().Float64Var(&f.thresholds.CoverageFloor, "coverage-floor", def.CoverageFloor, "step alignment coverage floor")
	return cmd
}

func runRegress(out io.Writer, open storeOpener, mkPricer func() reduce.Pricer, f regressFlags) error {
	s, err := openReadStore(open, f.dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()

	pricer := mkPricer()
	baseGroup, err := resolveSelector(s, pricer, f.baseline)
	if err != nil {
		return err
	}
	if len(baseGroup) == 0 {
		return operational(fmt.Errorf("regress baseline %q: %w", f.baseline, ErrEmptyGroup))
	}
	candGroup, err := resolveSelector(s, pricer, f.candidate)
	if err != nil {
		return err
	}
	if len(candGroup) == 0 {
		return operational(fmt.Errorf("regress candidate %q: %w", f.candidate, ErrEmptyGroup))
	}

	rep := regress.Compare(regress.Input{
		Baseline:  aggregate.Aggregate(baseGroup, aggregate.Options{}),
		Candidate: aggregate.Aggregate(candGroup, aggregate.Options{}),
	}, f.thresholds)

	if f.asJSON {
		if err := regress.RenderJSON(rep, out); err != nil {
			return err
		}
	} else {
		regress.RenderHuman(rep, out)
	}
	return verdictError(rep, f.strict)
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

func resolveSelector(s store.Store, pricer reduce.Pricer, sel string) ([]aggregate.RunGraph, error) {
	kind, val, err := parseSelector(sel)
	if err != nil {
		return nil, operational(err)
	}
	if kind == selectorName {
		return resolveNameSelector(s, pricer, val)
	}
	return resolveLabelSelector(s, pricer, val)
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
	return loadRunGroup(s, pricer, model.ParseLabels(val))
}

func resolveNameSelector(s store.Store, pricer reduce.Pricer, name string) ([]aggregate.RunGraph, error) {
	b, ok, err := s.GetBaseline(name)
	if err != nil {
		return nil, fmt.Errorf("regress get baseline %q: %w", name, err)
	}
	if !ok {
		return nil, operational(fmt.Errorf("%w: %q", ErrBaselineNotFound, name))
	}
	return loadRunGroupByIDs(s, pricer, b.RunIDs)
}
