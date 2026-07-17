package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/calibrate"
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
	runsInRecordedOrder, _, err := resolveSelectorRunsDir(errOut, "calibrate", f.dbPath, f.runsDir, mkPricer(), f.group)
	if err != nil {
		return err
	}
	rep := calibrate.Calibrate(runsInRecordedOrder, f.thresholds)
	if rerr := render(rep, out); rerr != nil {
		return operational(rerr)
	}
	return nil
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
