package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
)

const verifyHashWarning = "warning: basket hash differs from recorded runs (verifiers may be newer than the evidence)"

var errVerifyNoRunsDir = errors.New("verify: --runs-dir is required (home directory could not be resolved; set it explicitly)")

var errVerifyFailed = errors.New("verify: one or more cells failed re-verification")

type verifyFlags struct {
	runsDir string
	labels  string
}

func newVerifyCmd() *cobra.Command {
	var f verifyFlags
	cmd := &cobra.Command{
		Use:   "verify <basket.yaml>",
		Short: "Re-run basket verifiers offline over recorded evidence dirs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence dir holding recorded bench runs to re-verify")
	cmd.Flags().StringVar(&f.labels, "label", "", "restrict to runs whose labels match all comma-separated k=v terms")
	return cmd
}

func runVerify(ctx context.Context, stdout, stderr io.Writer, basketPath string, f verifyFlags) error {
	if f.runsDir == "" {
		return operational(errVerifyNoRunsDir)
	}
	basket, hash, err := bench.LoadOffline(basketPath)
	if err != nil {
		return operational(err)
	}
	runs, err := evidence.ScanRuns(f.runsDir)
	if err != nil {
		return operational(fmt.Errorf("verify --runs-dir: %w", err))
	}
	vf := &verifier{
		stdout:   stdout,
		stderr:   stderr,
		basket:   basket.Name,
		hash:     hash,
		tasks:    indexTasks(basket.Tasks),
		variants: indexVariants(basket.Variants),
		want:     model.ParseLabels(f.labels),
	}
	for _, r := range runs {
		vf.run(ctx, r)
	}
	if vf.matched == 0 {
		return operational(fmt.Errorf("verify: %w", ErrEmptyGroup))
	}
	if vf.failed > 0 {
		return errVerifyFailed
	}
	return nil
}

type verifier struct {
	stdout   io.Writer
	stderr   io.Writer
	basket   string
	hash     string
	tasks    map[string]bench.Task
	variants map[string]bench.Variant
	want     map[string]string
	matched  int
	failed   int
	warned   bool
}

func (vf *verifier) run(ctx context.Context, r evidence.Run) {
	m := r.Meta
	if m.Labels["basket"] != vf.basket {
		return
	}
	if !evidence.MatchLabels(m.Labels, vf.want) {
		return
	}
	task, ok := vf.tasks[m.Task]
	if !ok || task.Verify == nil {
		return
	}
	vf.matched++
	vf.maybeWarn(m.BasketHash)
	variant, ok := vf.variants[m.Labels["variant"]]
	if !ok {
		vf.fail(m.RunID, fmt.Sprintf("unknown variant %q", m.Labels["variant"]))
		return
	}
	rec := runVerifyCell(ctx, vf.stderr, *task.Verify, offlineVerifySpec(r.Dir, m, task, variant))
	if rec.Error != "" {
		vf.fail(m.RunID, rec.Error)
		return
	}
	fmt.Fprintf(vf.stdout, "verify %s: ok\n", m.RunID)
}

func (vf *verifier) maybeWarn(runHash string) {
	if vf.warned || runHash == vf.hash {
		return
	}
	fmt.Fprintln(vf.stderr, verifyHashWarning)
	vf.warned = true
}

func (vf *verifier) fail(runID, detail string) {
	vf.failed++
	fmt.Fprintf(vf.stdout, "verify %s: error (%s)\n", runID, detail)
}

func offlineVerifySpec(dir string, m evidence.Meta, task bench.Task, variant bench.Variant) verifySpec {
	return verifySpec{
		EvidenceDir: dir,
		RunID:       m.RunID,
		Basket:      m.Labels["basket"],
		Task:        task.ID,
		Variant:     variant.ID,
		Rep:         m.Rep,
		AgentExit:   m.ExitCode,
		Mode:        "offline",
		ExtraEnv:    cellEnv(bench.Cell{Task: task, Variant: variant}),
	}
}

func indexTasks(tasks []bench.Task) map[string]bench.Task {
	m := make(map[string]bench.Task, len(tasks))
	for _, t := range tasks {
		m[t.ID] = t
	}
	return m
}

func indexVariants(variants []bench.Variant) map[string]bench.Variant {
	m := make(map[string]bench.Variant, len(variants))
	for _, v := range variants {
		m[v.ID] = v
	}
	return m
}
