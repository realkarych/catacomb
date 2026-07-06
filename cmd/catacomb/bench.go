package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

const baselineNameMaxBytes = 128

var errBenchRerun = errors.New("bench: manifest already has entries: pass --resume to continue or --manifest for a fresh run")

var errBenchFailFast = errors.New("bench: stopped after a failing cell (--fail-fast)")

var errBenchOfflineDirs = errors.New("bench: --offline requires --projects-dir and --runs-dir (home directory could not be resolved; set them explicitly)")

type benchFlags struct {
	manifest    string
	resume      bool
	failFast    bool
	dryRun      bool
	offline     bool
	projectsDir string
	runsDir     string
}

func newBenchCmd() *cobra.Command {
	var f benchFlags
	cmd := &cobra.Command{
		Use:   "bench <basket.yaml>",
		Short: "Run a benchmark basket: expand cells, execute, mark phases, record a manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBench(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), clientDiscoveryPath(), args[0], f)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&f.manifest, "manifest", "", "manifest path (default: <basket>.manifest.jsonl)")
	cmd.Flags().BoolVar(&f.resume, "resume", false, "skip cells already recorded in the manifest")
	cmd.Flags().BoolVar(&f.failFast, "fail-fast", false, "stop at the first failing cell")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print the cell expansion and exit without executing")
	cmd.Flags().BoolVar(&f.offline, "offline", false, "run daemonless: read Claude transcripts and write evidence dirs")
	cmd.Flags().StringVar(&f.projectsDir, "projects-dir", benchDefaultDir(home, ".claude", "projects"), "Claude projects dir holding session transcripts (--offline)")
	cmd.Flags().StringVar(&f.runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence output dir for --offline runs")
	return cmd
}

func benchDefaultDir(home string, parts ...string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

type cellRunner func(cell bench.Cell, ambient map[string]string) (bench.ManifestEntry, bool, bool)

func runBench(ctx context.Context, stdout, stderr io.Writer, discoveryPath, basketPath string, f benchFlags) error {
	basket, hash, err := bench.Load(basketPath)
	if err != nil {
		return operational(err)
	}
	cells := basket.Cells()
	if f.dryRun {
		printDryRun(stdout, cells)
		return nil
	}
	cellFn, err := benchCellFunc(ctx, stdout, stderr, discoveryPath, hash, f)
	if err != nil {
		return operational(err)
	}
	return runBenchCells(stdout, basketPath, basket, cells, hash, f, cellFn)
}

func benchCellFunc(ctx context.Context, stdout, stderr io.Writer, discoveryPath, hash string, f benchFlags) (cellRunner, error) {
	if f.offline {
		if f.projectsDir == "" || f.runsDir == "" {
			return nil, errBenchOfflineDirs
		}
		o := offlineOpts{projectsDir: f.projectsDir, runsDir: f.runsDir, pricer: newPricer()}
		return func(cell bench.Cell, ambient map[string]string) (bench.ManifestEntry, bool, bool) {
			return runBenchCellOffline(stdout, stderr, cell, hash, ambient, o)
		}, nil
	}
	disc, err := benchPreflight(ctx, discoveryPath)
	if err != nil {
		return nil, err
	}
	return func(cell bench.Cell, ambient map[string]string) (bench.ManifestEntry, bool, bool) {
		return runBenchCell(ctx, stdout, stderr, discoveryPath, disc, cell, hash, ambient)
	}, nil
}

func runBenchCells(stdout io.Writer, basketPath string, basket bench.Basket, cells []bench.Cell, hash string, f benchFlags, cellFn cellRunner) error {
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
	executed, marked := 0, 0
	for _, cell := range cells {
		if _, done := completed[cell.RunID]; f.resume && done {
			fmt.Fprintf(stdout, "skip %s (already completed)\n", cell.RunID)
			continue
		}
		entry, failed, verified := cellFn(cell, ambient)
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
		if failed && f.failFast {
			return errBenchFailFast
		}
	}
	if executed > 0 {
		fmt.Fprintf(stdout, "marked %d/%d cells\n", marked, executed)
		printCheckpointSummary(stdout, basket, stats)
	}
	printBenchEpilogue(stdout, basket, f)
	return nil
}

func printBenchEpilogue(out io.Writer, b bench.Basket, f benchFlags) {
	if f.offline {
		printOfflineEpilogue(out, b, f.runsDir)
		return
	}
	printEpilogue(out, b)
}

func benchPreflight(ctx context.Context, discoveryPath string) (daemon.Discovery, error) {
	disc, err := daemon.ReadDiscovery(discoveryPath)
	if err != nil {
		return daemon.Discovery{}, fmt.Errorf("bench: daemon not running (start it: catacomb up): %w", err)
	}
	if err := upPollHealthz(ctx, disc.Addr); err != nil {
		return daemon.Discovery{}, fmt.Errorf("bench: daemon unreachable at %s (restart it: catacomb up): %w", disc.Addr, err)
	}
	return disc, nil
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

func runBenchCell(ctx context.Context, stdout, stderr io.Writer, discoveryPath string, disc daemon.Discovery, cell bench.Cell, hash string, ambient map[string]string) (bench.ManifestEntry, bool, bool) {
	entry := bench.ManifestEntry{
		RunID:      cell.RunID,
		Task:       cell.Task.ID,
		Variant:    cell.Variant.ID,
		Rep:        cell.Rep,
		BasketHash: hash,
	}
	if code, ok := runSetup(stdout, stderr, cell); !ok {
		entry.ExitCode = code
		entry.Note = "setup failed"
		entry.FinishedAt = nowFn()
		return entry, true, false
	}
	labels := model.FormatLabels(model.MergeLabels(cloneLabels(ambient), cell.Labels))
	marks := &markState{discoveryPath: discoveryPath, name: "task:" + cell.Task.ID}
	err := runChildObserved(stdout, stderr, discoveryPath, cell.RunID, cell.Task.Cmd, labels, cell.Task.Dir, cellEnv(cell), marks.onLine)
	marks.finish()
	code, ok := exitInfo(err)
	entry.ExitCode = code
	entry.SessionID = marks.sessionID
	entry.Marked = marks.marked()
	if note := spawnFailure(err); note != "" {
		entry.Note = note
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
	} else {
		entry.Note = cellNote(marks)
	}
	verified := verifyCheckpoints(ctx, stderr, disc, cell, &entry)
	entry.FinishedAt = nowFn()
	return entry, !ok, verified
}

func verifyCheckpoints(ctx context.Context, stderr io.Writer, disc daemon.Discovery, cell bench.Cell, entry *bench.ManifestEntry) bool {
	if len(cell.Task.Checkpoints) == 0 || entry.SessionID == "" {
		return false
	}
	markers, err := fetchSessionMarkers(ctx, disc, entry.SessionID)
	if err != nil {
		entry.Note = appendNote(entry.Note, "checkpoint verification skipped: "+err.Error())
		return false
	}
	var missing []string
	for _, name := range cell.Task.Checkpoints {
		if _, ok := markers[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		entry.MissingCheckpoints = missing
		fmt.Fprintf(stderr, "cell %s: missing checkpoints: %s\n", cell.RunID, strings.Join(missing, ", "))
	}
	return true
}

type offlineOpts struct {
	projectsDir string
	runsDir     string
	pricer      reduce.Pricer
}

func runBenchCellOffline(stdout, stderr io.Writer, cell bench.Cell, hash string, ambient map[string]string, o offlineOpts) (bench.ManifestEntry, bool, bool) {
	entry := bench.ManifestEntry{
		RunID:      cell.RunID,
		Task:       cell.Task.ID,
		Variant:    cell.Variant.ID,
		Rep:        cell.Rep,
		BasketHash: hash,
	}
	if code, ok := runSetup(stdout, stderr, cell); !ok {
		entry.ExitCode = code
		entry.Note = "setup failed"
		entry.FinishedAt = nowFn()
		return entry, true, false
	}
	merged := model.MergeLabels(cloneLabels(ambient), cell.Labels)
	peek := &streamPeek{}
	start := nowFn()
	err := runChildLocal(stdout, stderr, cell.Task.Cmd, cell.Task.Dir, offlineEnv(cell, merged), peek.onLine)
	end := nowFn()
	code, ok := exitInfo(err)
	entry.ExitCode = code
	entry.SessionID = peek.sessionID
	entry.CostUSD = peek.costUSD
	if offlineChildFailed(stderr, cell, err, &entry) {
		return entry, !ok, false
	}
	verified := recordOfflineEvidence(stderr, cell, o, merged, start, end, &entry)
	return entry, !ok, verified
}

func offlineChildFailed(stderr io.Writer, cell bench.Cell, err error, entry *bench.ManifestEntry) bool {
	if note := spawnFailure(err); note != "" {
		entry.Note = note
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
		entry.FinishedAt = nowFn()
		return true
	}
	if entry.SessionID == "" {
		entry.Note = "no session id observed"
		entry.FinishedAt = nowFn()
		return true
	}
	return false
}

func recordOfflineEvidence(stderr io.Writer, cell bench.Cell, o offlineOpts, labels map[string]string, start, end time.Time, entry *bench.ManifestEntry) bool {
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
	writeOfflineEvidence(filepath.Join(o.runsDir, cell.RunID), offlineMeta(*entry, labels, start, end, finishedAt), ts, entry)
	return verified
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

func offlineMeta(entry bench.ManifestEntry, labels map[string]string, start, end, finishedAt time.Time) evidence.Meta {
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
	}
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

type graphEvent struct {
	Kind string `json:"kind"`
	Node *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"node"`
}

func fetchSessionMarkers(ctx context.Context, disc daemon.Discovery, sessionID string) (map[string]struct{}, error) {
	endpoint := "http://" + disc.Addr + "/v1/sessions/" + url.PathEscape(sessionID) + "/graph"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+disc.Token)
	resp, err := statusHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graph status %d", resp.StatusCode)
	}
	var evs []graphEvent
	if err := json.NewDecoder(resp.Body).Decode(&evs); err != nil {
		return nil, fmt.Errorf("graph decode: %w", err)
	}
	markers := make(map[string]struct{}, len(evs))
	for _, ev := range evs {
		if ev.Kind == "node_upsert" && ev.Node != nil && ev.Node.Type == "marker" {
			markers[ev.Node.Name] = struct{}{}
		}
	}
	return markers, nil
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

func runSetup(stdout, stderr io.Writer, cell bench.Cell) (int, bool) {
	for _, raw := range cell.Variant.Setup {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		c := execCommand(fields[0], fields[1:]...)
		c.Dir = cell.Task.Dir
		c.Stdout = stdout
		c.Stderr = stderr
		if code, ok := exitInfo(c.Run()); !ok {
			return code, false
		}
	}
	return 0, true
}

type markState struct {
	discoveryPath string
	name          string
	sessionID     string
	startErr      error
	endErr        error
}

func (m *markState) onLine(line []byte) {
	if m.sessionID != "" {
		return
	}
	id := peekSessionID(line)
	if id == "" {
		return
	}
	m.sessionID = id
	m.startErr = runMark(markArgs{discoveryPath: m.discoveryPath, sessionID: id, name: m.name, boundary: "start"})
}

func (m *markState) finish() {
	if m.sessionID == "" {
		return
	}
	m.endErr = runMark(markArgs{discoveryPath: m.discoveryPath, sessionID: m.sessionID, name: m.name, boundary: "end"})
}

func (m *markState) marked() bool {
	return m.sessionID != "" && m.startErr == nil && m.endErr == nil
}

func cellNote(m *markState) string {
	if m.sessionID == "" {
		return "no session id observed"
	}
	if m.startErr != nil || m.endErr != nil {
		return "marker failed"
	}
	return ""
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

func peekSessionID(line []byte) string {
	var e struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &e) != nil {
		return ""
	}
	return e.SessionID
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

func printEpilogue(out io.Writer, b bench.Basket) {
	first := b.Variants[0].ID
	baselineName := truncateBaselineName(b.Name + "-" + first)
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintf(out, "  catacomb baseline set %s --label basket=%s,variant=%s\n", baselineName, b.Name, first)
	if len(b.Variants) >= 2 {
		second := b.Variants[1].ID
		fmt.Fprintf(out, "  catacomb regress --baseline label:basket=%s,variant=%s --candidate label:basket=%s,variant=%s\n", b.Name, first, b.Name, second)
	}
	if b.Reps < 5 {
		fmt.Fprintf(out, "  note: reps=%d limits rate-gate sensitivity; prefer reps: 5 or more\n", b.Reps)
	}
}

func truncateBaselineName(name string) string {
	if len(name) > baselineNameMaxBytes {
		return name[:baselineNameMaxBytes]
	}
	return name
}
