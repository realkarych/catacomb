package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

var errBenchRerun = errors.New("bench: manifest already has entries: pass --resume to continue or --manifest for a fresh run")

var errBenchFailFast = errors.New("bench: stopped after a failing cell (--fail-fast)")

var errBenchOfflineDirs = errors.New("bench: --projects-dir and --runs-dir are required (home directory could not be resolved; set them explicitly)")

type benchFlags struct {
	manifest       string
	resume         bool
	failFast       bool
	dryRun         bool
	projectsDir    string
	runsDir        string
	workspacesDir  string
	keepWorkspaces bool
}

func newBenchCmd() *cobra.Command {
	var f benchFlags
	cmd := &cobra.Command{
		Use:   "bench <basket.yaml>",
		Short: "Run a benchmark basket: expand cells, execute, mark phases, record a manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBench(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.manifest, "manifest", "", "manifest path (default: <basket>.manifest.jsonl)")
	cmd.Flags().BoolVar(&f.resume, "resume", false, "skip cells already recorded in the manifest")
	cmd.Flags().BoolVar(&f.failFast, "fail-fast", false, "stop at the first failing cell")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print the cell expansion and exit without executing")
	cmd.Flags().StringVar(&f.projectsDir, "projects-dir", benchDefaultDir(home, ".claude", "projects"), "Claude projects dir holding session transcripts")
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence output dir for bench runs")
	cmd.Flags().StringVar(&f.workspacesDir, "workspaces-dir", "", "base dir for per-cell workspace dirs (default: OS temp dir)")
	cmd.Flags().BoolVar(&f.keepWorkspaces, "keep-workspaces", false, "keep per-cell workspace dirs after teardown (paths printed to stderr)")
	return cmd
}

func benchDefaultDir(home string, parts ...string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

type cellRunner func(ctx context.Context, cell bench.Cell, ambient map[string]string) (bench.ManifestEntry, bool, bool)

func runBench(ctx context.Context, stdout, stderr io.Writer, basketPath string, f benchFlags) error {
	basket, hash, err := bench.Load(basketPath)
	if err != nil {
		return operational(err)
	}
	cells := basket.Cells()
	if len(basket.Variants) == 1 {
		fmt.Fprintln(stderr, "note: basket has 1 variant; bench records evidence, but regress needs >= 2 variants to gate")
	}
	if f.dryRun {
		printDryRun(stdout, cells)
		return nil
	}
	cellFn, err := benchCellFunc(stdout, stderr, hash, f)
	if err != nil {
		return operational(err)
	}
	return runBenchCells(ctx, stdout, basketPath, basket, cells, hash, f, cellFn)
}

func benchCellFunc(stdout, stderr io.Writer, hash string, f benchFlags) (cellRunner, error) {
	if f.projectsDir == "" || f.runsDir == "" {
		return nil, errBenchOfflineDirs
	}
	o := offlineOpts{
		projectsDir: f.projectsDir,
		runsDir:     f.runsDir,
		pricer:      newPricer(),
		workspace:   workspaceOpts{baseDir: f.workspacesDir, keep: f.keepWorkspaces},
	}
	return func(ctx context.Context, cell bench.Cell, ambient map[string]string) (bench.ManifestEntry, bool, bool) {
		return runBenchCellOffline(ctx, stdout, stderr, cell, hash, ambient, o)
	}, nil
}

func runBenchCells(ctx context.Context, stdout io.Writer, basketPath string, basket bench.Basket, cells []bench.Cell, hash string, f benchFlags, cellFn cellRunner) error {
	manifestPath := f.manifest
	if manifestPath == "" {
		manifestPath = basketPath + ".manifest.jsonl"
	}
	manifest := bench.Manifest{Path: manifestPath}
	completed, err := manifest.Completed()
	if err != nil {
		return operational(err)
	}
	if f.resume {
		if err := verifyResumeHash(completed, hash); err != nil {
			return operational(err)
		}
	} else if len(completed) > 0 {
		return operational(errBenchRerun)
	}
	ambient := model.ParseLabels(os.Getenv("CATACOMB_LABELS"))
	stats := newCheckpointStats()
	vstats := newVerifyStats()
	executed, marked := 0, 0
	for _, cell := range cells {
		if _, done := completed[cell.RunID]; f.resume && done {
			fmt.Fprintf(stdout, "skip %s (already completed)\n", cell.RunID)
			continue
		}
		entry, failed, verified := cellFn(ctx, cell, ambient)
		if err := manifest.Append(entry); err != nil {
			return operational(fmt.Errorf("bench: manifest: %w", err))
		}
		executed++
		if entry.Marked {
			marked++
		}
		if verified {
			stats.record(cell.Task, entry.MissingCheckpoints)
		}
		if entry.Verified {
			vstats.record(cell.Task, verifierPassed(entry.EvidenceDir, cell.RunID))
		}
		if failed && f.failFast {
			return errBenchFailFast
		}
	}
	if executed > 0 {
		fmt.Fprintf(stdout, "marked %d/%d cells\n", marked, executed)
		printCheckpointSummary(stdout, basket, stats)
		printVerifySummary(stdout, basket, vstats)
	}
	printOfflineEpilogue(stdout, basket, f.runsDir)
	return nil
}

type offlineOpts struct {
	projectsDir string
	runsDir     string
	pricer      reduce.Pricer
	workspace   workspaceOpts
}

func runBenchCellOffline(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, hash string, ambient map[string]string, o offlineOpts) (bench.ManifestEntry, bool, bool) {
	entry := bench.ManifestEntry{
		RunID:      cell.RunID,
		Task:       cell.Task.ID,
		Variant:    cell.Variant.ID,
		Rep:        cell.Rep,
		BasketHash: hash,
	}
	if d, _ := cell.Task.TimeoutDuration(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	workdir := cell.Task.Dir
	wsDir := ""
	if ws := cell.EffectiveWorkspace(); ws != nil {
		dir, code, ok := setupWorkspace(ctx, stdout, stderr, cell, o.workspace)
		wsDir = dir
		if !ok {
			entry.ExitCode = code
			entry.Note = "workspace failed"
			if ctxErr := ctx.Err(); ctxErr != nil {
				entry.Note = appendNote(entry.Note, ctxNote(ctxErr))
			}
			entry.FinishedAt = nowFn()
			mergeCleanupNotes(&entry, cleanupWorkspace(stderr, cell, wsDir, o.workspace.keep))
			return entry, true, false
		}
		workdir = dir
	}
	failed, verified := runBenchCellInWorkdir(ctx, stdout, stderr, cell, ambient, o, workdir, &entry)
	mergeCleanupNotes(&entry, cleanupWorkspace(stderr, cell, wsDir, o.workspace.keep))
	return entry, failed, verified
}

func mergeCleanupNotes(entry *bench.ManifestEntry, notes []string) {
	for _, n := range notes {
		entry.Note = appendNote(entry.Note, n)
	}
}

func runBenchCellInWorkdir(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, ambient map[string]string, o offlineOpts, workdir string, entry *bench.ManifestEntry) (bool, bool) {
	if code, ok := runSetup(ctx, stdout, stderr, cell, workdir); !ok {
		entry.ExitCode = code
		entry.Note = "setup failed"
		if ctxErr := ctx.Err(); ctxErr != nil {
			entry.Note = appendNote(entry.Note, ctxNote(ctxErr))
		}
		entry.FinishedAt = nowFn()
		return true, false
	}
	merged := model.MergeLabels(cloneLabels(ambient), cell.Labels)
	peek := &streamPeek{}
	start := nowFn()
	err := runChildLocal(ctx, stdout, stderr, cell.Task.Cmd, workdir, offlineEnv(cell, merged), peek.onLine)
	end := nowFn()
	code, ok := exitInfo(err)
	entry.ExitCode = code
	entry.SessionID = peek.sessionID
	entry.CostUSD = peek.costUSD
	if ctxErr := ctx.Err(); ctxErr != nil {
		entry.Note = appendNote(entry.Note, ctxNote(ctxErr))
	}
	if offlineChildFailed(stderr, cell, err, entry) {
		return !ok, false
	}
	verified := recordOfflineEvidence(ctx, stderr, cell, o, merged, start, end, workdir, entry)
	return !ok, verified
}

func ctxNote(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "cancelled"
}

func offlineChildFailed(stderr io.Writer, cell bench.Cell, err error, entry *bench.ManifestEntry) bool {
	if note := spawnFailure(err); note != "" {
		entry.Note = appendNote(entry.Note, note)
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
		entry.FinishedAt = nowFn()
		return true
	}
	if entry.SessionID == "" {
		entry.Note = appendNote(entry.Note, "no session id observed")
		entry.FinishedAt = nowFn()
		return true
	}
	return false
}

func recordOfflineEvidence(ctx context.Context, stderr io.Writer, cell bench.Cell, o offlineOpts, labels map[string]string, start, end time.Time, workdir string, entry *bench.ManifestEntry) bool {
	ts, err := resolveTranscriptsRetry(o.projectsDir, entry.SessionID, 6, 500*time.Millisecond)
	if err != nil {
		entry.Note = appendNote(entry.Note, "transcripts not found: "+err.Error())
		entry.FinishedAt = nowFn()
		return false
	}
	boundary := boundaryObservations(entry.SessionID, "task:"+cell.Task.ID, start, end)
	g, err := loadGraphOffline(ts.Main, ts.Subagents, newExecutionID(), o.pricer, boundary)
	if err != nil {
		entry.Note = appendNote(entry.Note, "graph: "+err.Error())
		entry.FinishedAt = nowFn()
		return false
	}
	marks := graphMarkerNames(g)
	_, entry.Marked = marks["task:"+cell.Task.ID]
	verified := verifyCheckpointsOffline(stderr, cell, marks, entry)
	finishedAt := nowFn()
	entry.FinishedAt = finishedAt
	dir := filepath.Join(o.runsDir, cell.RunID)
	env := benchEnvStamps(g.RunsSnapshot(), entry.SessionID, cell.EffectiveWorkspace())
	writeOfflineEvidence(dir, offlineMeta(*entry, labels, start, end, finishedAt, env), ts, entry)
	if entry.EvidenceDir != "" {
		captureArtifactsOffline(stderr, cell, dir, workdir, entry)
		verifyCellOffline(ctx, stderr, cell, dir, workdir, entry)
	}
	return verified
}

func captureArtifactsOffline(stderr io.Writer, cell bench.Cell, dir, workdir string, entry *bench.ManifestEntry) {
	if len(cell.Task.Artifacts) == 0 {
		return
	}
	arts, note, err := evidence.CaptureArtifacts(dir, workdir, cell.Task.Artifacts)
	if err != nil {
		entry.Note = appendNote(entry.Note, "artifacts: "+err.Error())
		fmt.Fprintf(stderr, "bench %s: artifacts: %s\n", cell.RunID, err.Error())
		return
	}
	if serr := evidence.StampArtifacts(dir, arts, note); serr != nil {
		entry.Note = appendNote(entry.Note, "artifacts stamp: "+serr.Error())
		fmt.Fprintf(stderr, "bench %s: artifacts stamp: %s\n", cell.RunID, serr.Error())
	}
}

func verifyCellOffline(ctx context.Context, stderr io.Writer, cell bench.Cell, dir, workdir string, entry *bench.ManifestEntry) {
	if cell.Task.Verify == nil {
		return
	}
	rec := runVerifyCell(ctx, stderr, *cell.Task.Verify, verifySpec{
		EvidenceDir: dir,
		Workdir:     workdir,
		RunID:       cell.RunID,
		Basket:      cell.Labels["basket"],
		Task:        cell.Task.ID,
		Variant:     cell.Variant.ID,
		Rep:         cell.Rep,
		AgentExit:   entry.ExitCode,
		Mode:        "bench",
		ExtraEnv:    cellEnv(cell),
	})
	if rec.Error != "" {
		entry.VerifyError = rec.Error
		fmt.Fprintf(stderr, "bench %s: verify failed: %s\n", cell.RunID, rec.Error)
		return
	}
	entry.Verified = true
}

func verifierPassed(dir, runID string) bool {
	entries, err := loadEvidenceScores(dir, runID)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Key == "verifier.pass" && e.Value == 1 {
			return true
		}
	}
	return false
}

func verifyCheckpointsOffline(stderr io.Writer, cell bench.Cell, marks map[string]struct{}, entry *bench.ManifestEntry) bool {
	if len(cell.Task.Checkpoints) == 0 {
		return false
	}
	var missing []string
	for _, name := range cell.Task.Checkpoints {
		if _, ok := marks[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		entry.MissingCheckpoints = missing
		fmt.Fprintf(stderr, "cell %s: missing checkpoints: %s\n", cell.RunID, strings.Join(missing, ", "))
	}
	return true
}

func writeOfflineEvidence(dir string, meta evidence.Meta, ts transcriptSet, entry *bench.ManifestEntry) {
	if err := evidence.Write(dir, meta, offlineFiles(ts)); err != nil {
		entry.Note = appendNote(entry.Note, "evidence write: "+err.Error())
		return
	}
	entry.EvidenceDir = dir
}

func offlineMeta(entry bench.ManifestEntry, labels map[string]string, start, end, finishedAt time.Time, env *evidence.EnvStamps) evidence.Meta {
	return evidence.Meta{
		RunID:       entry.RunID,
		Task:        entry.Task,
		Variant:     entry.Variant,
		Rep:         entry.Rep,
		SessionID:   entry.SessionID,
		Labels:      labels,
		ExitCode:    entry.ExitCode,
		CostUSD:     entry.CostUSD,
		BasketHash:  entry.BasketHash,
		MarkerName:  "task:" + entry.Task,
		MarkerStart: start.UTC(),
		MarkerEnd:   end.UTC(),
		FinishedAt:  finishedAt.UTC(),
		Env:         env,
	}
}

func benchEnvStamps(runs []model.Run, sessionID string, ws *bench.Workspace) *evidence.EnvStamps {
	env := &evidence.EnvStamps{
		CatacombVersion: Version,
		Resources:       evidence.Resources{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()},
	}
	if ws != nil {
		env.Workspace = &evidence.WorkspaceStamp{Rev: ws.Rev, PatchSHA256: ws.PatchSHA256}
	}
	for _, r := range runs {
		if r.ID != sessionID {
			continue
		}
		env.ModelID = r.ModelID
		if r.Repro != nil {
			env.ClaudeCodeVersion = r.Repro.ClaudeCodeVersion
		}
	}
	return env
}

func offlineFiles(ts transcriptSet) []evidence.SourceFile {
	files := []evidence.SourceFile{{Src: ts.Main, Rel: "session.jsonl"}}
	for _, sub := range ts.Subagents {
		files = append(files, evidence.SourceFile{Src: sub, Rel: filepath.Join("subagents", filepath.Base(sub))})
	}
	return files
}

func offlineEnv(cell bench.Cell, labels map[string]string) []string {
	return append(cellEnv(cell), "CATACOMB_LABELS="+model.FormatLabels(labels), "CATACOMB_RUN_ID="+cell.RunID)
}

func printOfflineEpilogue(out io.Writer, b bench.Basket, runsDir string) {
	fmt.Fprintln(out, "Next steps:")
	if len(b.Variants) >= 2 {
		first, second := b.Variants[0].ID, b.Variants[1].ID
		fmt.Fprintf(out, "  catacomb regress --runs-dir %s --baseline label:basket=%s,variant=%s --candidate label:basket=%s,variant=%s\n", runsDir, b.Name, first, b.Name, second)
	}
	if b.Reps < 5 {
		fmt.Fprintf(out, "  note: reps=%d limits rate-gate sensitivity; prefer reps: 5 or more\n", b.Reps)
	}
}

func appendNote(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}

type checkpointStats struct {
	verified map[string]int
	hits     map[string]map[string]int
}

func newCheckpointStats() checkpointStats {
	return checkpointStats{
		verified: map[string]int{},
		hits:     map[string]map[string]int{},
	}
}

func (s checkpointStats) record(t bench.Task, missing []string) {
	s.verified[t.ID]++
	if s.hits[t.ID] == nil {
		s.hits[t.ID] = map[string]int{}
	}
	absent := make(map[string]struct{}, len(missing))
	for _, m := range missing {
		absent[m] = struct{}{}
	}
	for _, name := range t.Checkpoints {
		if _, gone := absent[name]; !gone {
			s.hits[t.ID][name]++
		}
	}
}

func printCheckpointSummary(out io.Writer, b bench.Basket, s checkpointStats) {
	declared := false
	for _, t := range b.Tasks {
		if len(t.Checkpoints) > 0 {
			declared = true
			break
		}
	}
	if !declared {
		return
	}
	for _, t := range b.Tasks {
		if len(t.Checkpoints) == 0 {
			continue
		}
		for _, name := range t.Checkpoints {
			fmt.Fprintf(out, "checkpoints[%s]: %s %d/%d\n", t.ID, name, s.hits[t.ID][name], s.verified[t.ID])
		}
	}
}

type verifyStats struct {
	verified map[string]int
	passed   map[string]int
}

func newVerifyStats() verifyStats {
	return verifyStats{
		verified: map[string]int{},
		passed:   map[string]int{},
	}
}

func (s verifyStats) record(t bench.Task, pass bool) {
	s.verified[t.ID]++
	if pass {
		s.passed[t.ID]++
	}
}

func printVerifySummary(out io.Writer, b bench.Basket, s verifyStats) {
	declared := false
	for _, t := range b.Tasks {
		if t.Verify != nil {
			declared = true
			break
		}
	}
	if !declared {
		return
	}
	for _, t := range b.Tasks {
		if t.Verify == nil {
			continue
		}
		fmt.Fprintf(out, "verify[%s]: pass %d/%d\n", t.ID, s.passed[t.ID], s.verified[t.ID])
	}
}

func spawnFailure(err error) string {
	if err == nil {
		return ""
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ""
	}
	return "spawn failed: " + err.Error()
}

func runSetup(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, dir string) (int, bool) {
	for _, raw := range cell.Variant.Setup {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		c := execCommandContext(ctx, fields[0], fields[1:]...)
		c.Dir = dir
		c.Stdout = stdout
		c.Stderr = stderr
		c.WaitDelay = 10 * time.Second
		if code, ok := exitInfo(c.Run()); !ok {
			return code, false
		}
	}
	return 0, true
}

func exitInfo(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), false
	}
	return -1, false
}

func cellEnv(cell bench.Cell) []string {
	merged := make(map[string]string, len(cell.Task.Env)+len(cell.Variant.Env))
	for k, v := range cell.Task.Env {
		merged[k] = v
	}
	for k, v := range cell.Variant.Env {
		merged[k] = v
	}
	env := make([]string, 0, len(merged))
	for k, v := range merged {
		env = append(env, k+"="+v)
	}
	return env
}

func cloneLabels(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func printDryRun(out io.Writer, cells []bench.Cell) {
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN_ID\tTASK\tVARIANT\tREP")
	for _, c := range cells {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", c.RunID, c.Task.ID, c.Variant.ID, c.Rep)
	}
	_ = tw.Flush()
}

func verifyResumeHash(completed map[string]bench.ManifestEntry, hash string) error {
	ids := make([]string, 0, len(completed))
	for id := range completed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if e := completed[id]; e.BasketHash != hash {
			return fmt.Errorf("bench: manifest basket hash %s does not match current basket %s; delete the manifest or revert the basket", e.BasketHash, hash)
		}
	}
	return nil
}
