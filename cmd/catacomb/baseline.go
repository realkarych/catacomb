package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

var nowFn = time.Now

var (
	errBaselineNoRunsDir       = errors.New("baseline set: --runs-dir is required (home directory could not be resolved; set it explicitly)")
	errBaselineExportNoRunsDir = errors.New("baseline export: --runs-dir is required (home directory could not be resolved; set it explicitly)")
)

func newBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Manage named baselines for regression comparison",
	}
	cmd.AddCommand(newBaselineSetCmd(), newBaselineListCmd(), newBaselineRmCmd(), newBaselineExportCmd())
	return cmd
}

func newBaselineSetCmd() *cobra.Command {
	var dbPath string
	var labels []string
	var runsDir string
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Create or replace a baseline from a label selector over evidence dirs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBaselineSet(cmd.OutOrStdout(), store.OpenSQLite, dbPath, args[0], labels, runsDir)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path for the baselines table (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "k=v label selector (repeatable, AND)")
	cmd.Flags().StringVar(&runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence dir to resolve the label selector from")
	return cmd
}

func newBaselineListCmd() *cobra.Command {
	var dbPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored baselines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBaselineList(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, dbPath, asJSON)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newBaselineRmCmd() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a stored baseline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBaselineRm(cmd.OutOrStdout(), store.OpenSQLite, dbPath, args[0])
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	return cmd
}

func newBaselineExportCmd() *cobra.Command {
	var dbPath string
	var runsDir string
	var outPath string
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a baseline and its pinned evidence runs as a portable bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBaselineExport(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, dbPath, args[0], runsDir, outPath)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence dir holding the baseline's pinned runs")
	cmd.Flags().StringVar(&outPath, "out", "", "bundle output file (.tar.gz); must not already exist (required)")
	return cmd
}

func runBaselineExport(out io.Writer, open storeOpener, dbPath, name, runsDir, outPath string) error {
	if runsDir == "" {
		return operational(errBaselineExportNoRunsDir)
	}
	if outPath == "" {
		return operational(fmt.Errorf("baseline export %q: --out is required", name))
	}
	if _, err := os.Lstat(outPath); err == nil {
		return operational(fmt.Errorf("baseline export: out file %q already exists; refusing to overwrite it", outPath))
	}
	b, err := exportBaseline(open, dbPath, name)
	if err != nil {
		return err
	}
	for _, id := range b.RunIDs {
		info, statErr := os.Stat(filepath.Join(runsDir, id))
		if statErr != nil || !info.IsDir() {
			return operational(fmt.Errorf("baseline export %q: pinned run %q has no evidence dir under %q", name, id, runsDir))
		}
	}
	fileCount, err := writeBundleFileAtomic(outPath, b, runsDir)
	if err != nil {
		return operational(fmt.Errorf("baseline export %q: %w", name, err))
	}
	fmt.Fprintf(out, "exported baseline %s: %s (%d runs, %d files)\n", name, outPath, len(b.RunIDs), fileCount)
	return nil
}

func exportBaseline(open storeOpener, dbPath, name string) (model.Baseline, error) {
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return model.Baseline{}, operational(err)
	}
	defer func() { _ = s.Close() }()
	b, ok, err := s.GetBaseline(name)
	if err != nil {
		return model.Baseline{}, operational(fmt.Errorf("baseline export: %w", err))
	}
	if !ok {
		return model.Baseline{}, operational(fmt.Errorf("%w: %q", ErrBaselineNotFound, name))
	}
	return b, nil
}

func writeBundleFileAtomic(outPath string, b model.Baseline, runsDir string) (int, error) {
	tmp, err := os.CreateTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".tmp-*")
	if err != nil {
		return 0, err
	}
	fileCount, writeErr := writeBundle(tmp, b, runsDir)
	err = errors.Join(writeErr, tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), outPath)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return 0, err
	}
	return fileCount, nil
}

func runBaselineSet(out io.Writer, open storeOpener, dbPath, name string, labels []string, runsDir string) error {
	if err := validateBaselineName(name); err != nil {
		return operational(err)
	}
	if len(labels) == 0 {
		return operational(fmt.Errorf("baseline set %q: at least one --label is required", name))
	}
	if err := validateLabelTerms(labels); err != nil {
		return operational(err)
	}
	if runsDir == "" {
		return operational(errBaselineNoRunsDir)
	}
	selector := model.ParseLabels(strings.Join(labels, ","))
	ids, err := offlineBaselineRunIDs(runsDir, name, selector)
	if err != nil {
		return err
	}
	s, err := openWriteStore(open, dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()
	b := model.Baseline{Name: name, RunIDs: ids, Selector: selector, CreatedAt: nowFn(), RunsDir: runsDir, Stamps: currentStamps()}
	if err := s.UpsertBaseline(b); err != nil {
		return operational(fmt.Errorf("baseline set: %w", err))
	}
	fmt.Fprintf(out, "baseline %q set: %d runs\n", name, len(ids))
	return nil
}

func offlineBaselineRunIDs(runsDir, name string, selector map[string]string) ([]string, error) {
	runs, err := evidence.ScanRuns(runsDir)
	if err != nil {
		return nil, operational(fmt.Errorf("baseline set --runs-dir: %w", err))
	}
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		if evidence.MatchLabels(r.Meta.Labels, selector) {
			ids = append(ids, r.Meta.RunID)
		}
	}
	if len(ids) == 0 {
		return nil, operational(fmt.Errorf("baseline set %q: selector %q: %w", name, formatSelector(selector), ErrEmptyGroup))
	}
	sort.Strings(ids)
	return ids, nil
}

func validateBaselineName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("invalid baseline name: must not be empty")
	case len(name) > 128:
		return fmt.Errorf("invalid baseline name: must be <= 128 bytes")
	case strings.TrimSpace(name) != name:
		return fmt.Errorf("invalid baseline name %q: no leading or trailing whitespace", name)
	default:
		return nil
	}
}

func runBaselineList(out io.Writer, open storeOpener, dbPath string, asJSON bool) error {
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()
	baselines, err := s.ListBaselines()
	if err != nil {
		return operational(fmt.Errorf("baseline list: %w", err))
	}
	sort.Slice(baselines, func(i, j int) bool { return baselines[i].Name < baselines[j].Name })
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(baselines)
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tRUNS\tSELECTOR\tCREATED")
	for _, b := range baselines {
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", b.Name, len(b.RunIDs), formatSelector(b.Selector), b.CreatedAt.UTC().Format(time.RFC3339))
	}
	return w.Flush()
}

func runBaselineRm(out io.Writer, open storeOpener, dbPath, name string) error {
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.DeleteBaseline(name); err != nil {
		return operational(fmt.Errorf("baseline rm: %w", err))
	}
	fmt.Fprintf(out, "baseline %q removed\n", name)
	return nil
}

func formatSelector(sel map[string]string) string {
	if len(sel) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(sel))
	for k := range sel {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+sel[k])
	}
	return strings.Join(parts, ",")
}
