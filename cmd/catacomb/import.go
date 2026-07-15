package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/bench"
)

var errImportInput = errors.New("import: exactly one of --session-id or --transcript is required")

type importFlags struct {
	task        string
	variant     string
	sessionID   string
	transcript  string
	rep         int
	runID       string
	projectsDir string
	runsDir     string
	labels      string
}

func newImportCmd() *cobra.Command {
	var f importFlags
	cmd := &cobra.Command{
		Use:   "import <basket.yaml>",
		Short: "Ingest an already-finished session transcript as a bench-cell-shaped evidence dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.task, "task", "", "task id in the basket (selects verify/checkpoints/labels)")
	cmd.Flags().StringVar(&f.variant, "variant", "", "variant id in the basket")
	cmd.Flags().StringVar(&f.sessionID, "session-id", "", "session UUID resolved under --projects-dir")
	cmd.Flags().StringVar(&f.transcript, "transcript", "", "direct path to a main session .jsonl")
	cmd.Flags().IntVar(&f.rep, "rep", 1, "repetition index")
	cmd.Flags().StringVar(&f.runID, "run-id", "", "evidence dir name (default: import-<basket>-<task>-<variant>-r<rep>)")
	cmd.Flags().StringVar(&f.projectsDir, "projects-dir", benchDefaultDir(home, ".claude", "projects"), "Claude projects dir holding session transcripts")
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence output dir")
	cmd.Flags().StringVar(&f.labels, "label", "", "extra ambient labels merged under cell labels (k=v, comma-separated)")
	return cmd
}

func runImport(ctx context.Context, stdout, stderr io.Writer, basketPath string, f importFlags) error {
	if (f.sessionID == "") == (f.transcript == "") {
		return operational(errImportInput)
	}
	basket, hash, err := bench.LoadOffline(basketPath)
	if err != nil {
		return operational(err)
	}
	task, ok := indexTasks(basket.Tasks)[f.task]
	if !ok {
		return operational(fmt.Errorf("import: task %q not in basket", f.task))
	}
	if _, ok := indexVariants(basket.Variants)[f.variant]; !ok {
		return operational(fmt.Errorf("import: variant %q not in basket", f.variant))
	}
	return importEvidence(ctx, stdout, stderr, basket, hash, task, f)
}

func importTranscripts(f importFlags) (transcriptSet, string, error) {
	if f.sessionID != "" {
		ts, err := resolveTranscripts(f.projectsDir, f.sessionID)
		if err != nil {
			return transcriptSet{}, "", err
		}
		return ts, f.sessionID, nil
	}
	if _, err := os.Stat(f.transcript); err != nil {
		return transcriptSet{}, "", fmt.Errorf("import: transcript: %w", err)
	}
	sid := strings.TrimSuffix(filepath.Base(f.transcript), ".jsonl")
	subs, err := filepath.Glob(filepath.Join(filepath.Dir(f.transcript), sid, "subagents", "agent-*.jsonl"))
	if err != nil {
		return transcriptSet{}, "", fmt.Errorf("import: subagents: %w", err)
	}
	sort.Strings(subs)
	return transcriptSet{Main: f.transcript, Subagents: subs}, sid, nil
}

func importEvidence(_ context.Context, _, _ io.Writer, _ bench.Basket, _ string, _ bench.Task, _ importFlags) error {
	return nil
}
