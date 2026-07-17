package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/calibrate"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/regress"
)

const (
	calibrateFormatHuman = "human"
	calibrateFormatJSON  = "json"
)

var errCalibrateNoRunsDir = errors.New("calibrate: --runs-dir is required (home directory could not be resolved; set it explicitly)")

type calibrateFlags struct {
	group      string
	dbPath     string
	runsDir    string
	format     string
	thresholds regress.Thresholds
}

func newCalibrateCmd() *cobra.Command {
	f := calibrateFlags{}
	cmd := &cobra.Command{
		Use:   "calibrate",
		Short: "Self-check gate power over one run group (A/A split + leave-one-out influence)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCalibrate(cmd.OutOrStdout(), cmd.ErrOrStderr(), newPricer, f)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.group, "group", "", "run group selector: label:k=v[,k=v...] or name:<baseline>")
	cmd.Flags().StringVar(&f.dbPath, "db", defaultDBPath(), "SQLite database path for name: selectors (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence dir to resolve --group from: label: scans it, name: reads --db's baselines table")
	cmd.Flags().StringVar(&f.format, "format", calibrateFormatHuman, "output format: human|json")
	bindThresholdFlags(cmd, &f.thresholds)
	cmd.Flags().Lookup("fail-on-notable").Usage = "count notable findings toward the A/A split verdict (calibrate always exits 0)"
	return cmd
}

func runCalibrate(out, errOut io.Writer, mkPricer func() reduce.Pricer, f calibrateFlags) error {
	render, err := resolveCalibrateFormat(f.format)
	if err != nil {
		return err
	}
	if verr := validateThresholds("calibrate", f.thresholds); verr != nil {
		return verr
	}
	if f.runsDir == "" {
		return operational(errCalibrateNoRunsDir)
	}
	group, _, err := resolveSelectorRunsDir(errOut, "calibrate", f.dbPath, f.runsDir, mkPricer(), f.group, loadForAggregation)
	if err != nil {
		return err
	}
	ordered := orderRunsByTime(group)
	warnCalibrateTaskMix(errOut, ordered)
	rep := calibrate.Calibrate(ordered, f.thresholds)
	if rerr := render(rep, out); rerr != nil {
		return operational(rerr)
	}
	return nil
}

func orderRunsByTime(runs []aggregate.RunGraph) []aggregate.RunGraph {
	if !anyRunTimed(runs) {
		return runs
	}
	ordered := make([]aggregate.RunGraph, len(runs))
	copy(ordered, runs)
	sort.SliceStable(ordered, func(i, j int) bool { return runTimeLess(ordered[i].Run, ordered[j].Run) })
	return ordered
}

func anyRunTimed(runs []aggregate.RunGraph) bool {
	for _, rg := range runs {
		if !timeOrZero(rg.Run.StartedAt).IsZero() || !timeOrZero(rg.Run.EndedAt).IsZero() {
			return true
		}
	}
	return false
}

func runTimeLess(a, b model.Run) bool {
	if as, bs := timeOrZero(a.StartedAt), timeOrZero(b.StartedAt); !as.Equal(bs) {
		return as.Before(bs)
	}
	if ae, be := timeOrZero(a.EndedAt), timeOrZero(b.EndedAt); !ae.Equal(be) {
		return ae.Before(be)
	}
	return a.ID < b.ID
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func warnCalibrateTaskMix(errOut io.Writer, runs []aggregate.RunGraph) {
	tasks := map[string]struct{}{}
	for _, rg := range runs {
		if task := rg.Run.Labels["task"]; task != "" {
			tasks[task] = struct{}{}
		}
	}
	if len(tasks) > 1 {
		fmt.Fprintf(errOut, "warning: calibrate group spans %d tasks; drift may reflect task composition — prefer a per-task selector (label:...,task=<id>)\n", len(tasks))
	}
}

func resolveCalibrateFormat(format string) (func(calibrate.CalibrateReport, io.Writer) error, error) {
	switch format {
	case calibrateFormatHuman:
		return func(r calibrate.CalibrateReport, w io.Writer) error {
			calibrate.RenderHuman(r, w)
			return nil
		}, nil
	case calibrateFormatJSON:
		return calibrate.RenderJSON, nil
	default:
		return nil, operational(fmt.Errorf("calibrate: unknown --format %q (want human or json)", format))
	}
}
