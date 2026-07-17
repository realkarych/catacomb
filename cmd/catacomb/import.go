package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
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
	sessionsDir string
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
	cmd.Flags().StringVar(&f.transcript, "transcript", "", "direct path to a main session transcript (.jsonl, or a Codex rollout .jsonl / .jsonl.zst)")
	cmd.Flags().IntVar(&f.rep, "rep", 1, "repetition index")
	cmd.Flags().StringVar(&f.runID, "run-id", "", "evidence dir name (default: import-<basket>-<task>-<variant>-r<rep>)")
	cmd.Flags().StringVar(&f.projectsDir, "projects-dir", benchDefaultDir(home, ".claude", "projects"), "Claude projects dir holding session transcripts")
	cmd.Flags().StringVar(&f.sessionsDir, "sessions-dir", benchDefaultDir(home, ".codex", "sessions"), "Codex sessions dir holding rollout transcripts (runtime: codex)")
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

func importTranscripts(rt string, f importFlags) (transcriptSet, string, error) {
	if f.sessionID != "" {
		ts, err := resolveImportSession(rt, f)
		if err != nil {
			return transcriptSet{}, "", err
		}
		return ts, f.sessionID, nil
	}
	if _, err := os.Stat(f.transcript); err != nil {
		return transcriptSet{}, "", fmt.Errorf("transcript: %w", err)
	}
	if rt == drift.RuntimeCodex {
		return codexTranscriptByPath(f.transcript)
	}
	sid := strings.TrimSuffix(filepath.Base(f.transcript), ".jsonl")
	subs, err := filepath.Glob(filepath.Join(filepath.Dir(f.transcript), sid, "subagents", "agent-*.jsonl"))
	if err != nil {
		return transcriptSet{}, "", fmt.Errorf("subagents: %w", err)
	}
	sort.Strings(subs)
	return transcriptSet{Main: f.transcript, Subagents: subs}, sid, nil
}

func resolveImportSession(rt string, f importFlags) (transcriptSet, error) {
	if rt == drift.RuntimeCodex {
		return resolveCodexTranscripts(f.sessionsDir, f.sessionID)
	}
	return resolveTranscripts(f.projectsDir, f.sessionID)
}

func codexTranscriptByPath(path string) (transcriptSet, string, error) {
	sid := codexThreadIDFromFilename(filepath.Base(path))
	if sid == "" {
		return transcriptSet{}, "", fmt.Errorf("transcript %s: cannot derive thread id (expected rollout-<timestamp>-<thread-id>.jsonl[.zst])", path)
	}
	subs, err := codexChildTranscripts(filepath.Dir(path), sid)
	if err != nil {
		return transcriptSet{}, "", err
	}
	return transcriptSet{Main: path, Subagents: subs}, sid, nil
}

func importEvidence(_ context.Context, stdout, stderr io.Writer, basket bench.Basket, hash string, task bench.Task, f importFlags) error {
	rt := basket.EffectiveRuntime()
	ts, sessionID, err := importTranscripts(rt, f)
	if err != nil {
		return operational(fmt.Errorf("import: %w", err))
	}
	execID := newExecutionID()
	obs, err := parseTranscriptsFor(rt, ts.Main, ts.Subagents, sessionID, execID)
	if err != nil {
		return operational(fmt.Errorf("import: %w", err))
	}
	start, end, ok := transcriptTimeBounds(obs)
	if !ok {
		return operational(fmt.Errorf("import: transcript %s has no timestamped records", ts.Main))
	}
	boundary := boundaryObservations(sessionID, "task:"+task.ID, start, end)
	g := graphFromObservations(obs, execID, newPricer(), boundary)
	marks := graphMarkerNames(g)
	warnMissingCheckpoints(stderr, task, marks, importRunID(f, basket.Name))
	env := importEnvStamps(rt, g.RunsSnapshot(), sessionID, obs)
	runID := importRunID(f, basket.Name)
	meta := importMeta(runID, task.ID, f.variant, f.rep, sessionID, hash, importLabels(f, basket.Name), start, end, env)
	dir := filepath.Join(f.runsDir, runID)
	if err := evidence.Write(dir, meta, offlineFilesFor(rt, ts)); err != nil {
		return operational(fmt.Errorf("import: evidence write: %w", err))
	}
	fmt.Fprintf(stdout, "import %s: %s\n", runID, dir)
	return nil
}

func warnMissingCheckpoints(stderr io.Writer, task bench.Task, marks map[string]struct{}, runID string) {
	var missing []string
	for _, name := range task.Checkpoints {
		if _, ok := marks[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "import %s: missing checkpoints: %s\n", runID, strings.Join(missing, ", "))
	}
}

func importRunID(f importFlags, basketName string) string {
	if f.runID != "" {
		return f.runID
	}
	return fmt.Sprintf("import-%s-%s-%s-r%d", basketName, f.task, f.variant, f.rep)
}

func importLabels(f importFlags, basketName string) map[string]string {
	cell := map[string]string{
		"basket":  basketName,
		"task":    f.task,
		"variant": f.variant,
		"rep":     strconv.Itoa(f.rep),
	}
	return model.MergeLabels(model.ParseLabels(f.labels), cell)
}

func importMeta(runID, task, variant string, rep int, sessionID, hash string, labels map[string]string, start, end time.Time, env *evidence.EnvStamps) evidence.Meta {
	return evidence.Meta{
		RunID:       runID,
		Task:        task,
		Variant:     variant,
		Rep:         rep,
		SessionID:   sessionID,
		Labels:      labels,
		ExitCode:    0,
		CostUSD:     nil,
		BasketHash:  hash,
		MarkerName:  "task:" + task,
		MarkerStart: start.UTC(),
		MarkerEnd:   end.UTC(),
		FinishedAt:  nowFn().UTC(),
		Env:         env,
	}
}
