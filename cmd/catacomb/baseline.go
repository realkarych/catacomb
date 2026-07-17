package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

var (
	nowFn = time.Now
	absFn = filepath.Abs
)

var (
	errBaselineNoRunsDir       = errors.New("baseline set: --runs-dir is required (home directory could not be resolved; set it explicitly)")
	errBaselineExportNoRunsDir = errors.New("baseline export: --runs-dir is required (home directory could not be resolved; set it explicitly)")
	errBaselineImportNoRunsDir = errors.New("baseline import: --runs-dir is required (home directory could not be resolved; set it explicitly)")
)

func newBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Manage named baselines for regression comparison",
	}
	cmd.AddCommand(newBaselineSetCmd(), newBaselineListCmd(), newBaselineRmCmd(), newBaselineExportCmd(), newBaselineImportCmd())
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

func newBaselineImportCmd() *cobra.Command {
	var dbPath string
	var runsDir string
	cmd := &cobra.Command{
		Use:   "import <bundle>",
		Short: "Import a baseline bundle: verify hashes, land its runs, upsert the baseline row",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBaselineImport(cmd.OutOrStdout(), store.OpenSQLite, dbPath, args[0], runsDir)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path for the baselines table (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&runsDir, "runs-dir", benchDefaultDir(home, ".catacomb", "runs"), "evidence dir to land the bundle's runs into")
	return cmd
}

func runBaselineImport(out io.Writer, open storeOpener, dbPath, bundlePath, runsDir string) error {
	if runsDir == "" {
		return operational(errBaselineImportNoRunsDir)
	}
	absRunsDir, err := absFn(runsDir)
	if err != nil {
		return operational(fmt.Errorf("baseline import: resolve --runs-dir %q: %w", runsDir, err))
	}
	manifest, err := importBundleRuns(bundlePath, absRunsDir)
	if err != nil {
		return operational(fmt.Errorf("baseline import: %w", err))
	}
	b := manifest.Baseline
	b.RunsDir = absRunsDir
	if uerr := upsertImportedBaseline(open, dbPath, b); uerr != nil {
		return operational(fmt.Errorf("baseline import: %w", uerr))
	}
	fmt.Fprintf(out, "imported baseline %s: %d runs into %s\n", b.Name, len(b.RunIDs), absRunsDir)
	return nil
}

func upsertImportedBaseline(open storeOpener, dbPath string, b model.Baseline) error {
	s, err := openWriteStore(open, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.UpsertBaseline(b)
}

func importBundleRuns(bundlePath, runsDir string) (bundleManifest, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return bundleManifest{}, err
	}
	defer func() { _ = f.Close() }()
	if merr := os.MkdirAll(runsDir, 0o700); merr != nil {
		return bundleManifest{}, merr
	}
	tmpRoot, err := os.MkdirTemp(runsDir, ".import-*")
	if err != nil {
		return bundleManifest{}, err
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()
	imp := &bundleImporter{runsDir: runsDir, tmpRoot: tmpRoot, seen: map[string]bool{}, existing: map[string]bool{}}
	manifest, err := readBundleWith(f, imp.bindManifest, imp.file)
	if err != nil {
		return bundleManifest{}, err
	}
	if ferr := imp.finalize(); ferr != nil {
		return bundleManifest{}, ferr
	}
	return manifest, nil
}

func (imp *bundleImporter) finalize() error {
	if err := imp.verifyClosure(); err != nil {
		return err
	}
	if err := imp.verifyExistingRuns(); err != nil {
		return err
	}
	return imp.commit()
}

type bundleImporter struct {
	runsDir  string
	tmpRoot  string
	manifest bundleManifest
	seen     map[string]bool
	existing map[string]bool
}

func (imp *bundleImporter) bindManifest(m bundleManifest) error {
	if err := validateBaselineName(m.Baseline.Name); err != nil {
		return err
	}
	imp.manifest = m
	return nil
}

func (imp *bundleImporter) file(p string, r io.Reader) error {
	if imp.seen[p] {
		return fmt.Errorf("duplicate archive entry: %w", errBundleHash)
	}
	imp.seen[p] = true
	want, ok := imp.manifest.Files[p]
	if !ok {
		return fmt.Errorf("not in the manifest: %w", errBundleHash)
	}
	parts := strings.SplitN(p, "/", 3)
	runID, rel := parts[1], filepath.FromSlash(parts[2])
	existing := imp.runOnDisk(runID)
	got, err := imp.consumePayload(existing, runID, rel, r)
	if err != nil {
		return err
	}
	if got != want {
		return errBundleHash
	}
	if existing {
		return imp.matchDisk(runID, rel, want)
	}
	return nil
}

func (imp *bundleImporter) runOnDisk(runID string) bool {
	if v, ok := imp.existing[runID]; ok {
		return v
	}
	_, err := os.Lstat(filepath.Join(imp.runsDir, runID))
	imp.existing[runID] = err == nil
	return imp.existing[runID]
}

func (imp *bundleImporter) consumePayload(existing bool, runID, rel string, r io.Reader) (string, error) {
	if existing {
		return hashStream(io.Discard, r)
	}
	dst := filepath.Join(imp.tmpRoot, runID, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	got, herr := hashStream(out, r)
	return got, errors.Join(herr, out.Close())
}

func (imp *bundleImporter) matchDisk(runID, rel, want string) error {
	disk, err := hashFile(filepath.Join(imp.runsDir, runID, rel))
	if err != nil || disk != want {
		return fmt.Errorf("existing run %q file %q differs from the bundle: %w", runID, rel, errBundleCollision)
	}
	return nil
}

func (imp *bundleImporter) verifyClosure() error {
	for _, p := range slices.Sorted(maps.Keys(imp.manifest.Files)) {
		if !imp.seen[p] {
			return fmt.Errorf("baseline bundle: manifest file %q missing from the archive: %w", p, errBundleHash)
		}
	}
	return nil
}

func (imp *bundleImporter) verifyExistingRuns() error {
	for _, runID := range slices.Sorted(maps.Keys(imp.existing)) {
		if !imp.existing[runID] {
			continue
		}
		if err := imp.verifyNoExtraEntries(runID); err != nil {
			return err
		}
	}
	return nil
}

func (imp *bundleImporter) verifyNoExtraEntries(runID string) error {
	return fs.WalkDir(os.DirFS(filepath.Join(imp.runsDir, runID)), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		_, bundled := imp.manifest.Files[path.Join(bundleRunsPrefix, runID, p)]
		if !bundled || !d.Type().IsRegular() {
			return fmt.Errorf("existing run %q entry %q not in the bundle: %w", runID, p, errBundleCollision)
		}
		return nil
	})
}

func (imp *bundleImporter) commit() error {
	var err error
	for _, runID := range slices.Sorted(maps.Keys(imp.existing)) {
		if imp.existing[runID] {
			continue
		}
		err = errors.Join(err, os.Rename(filepath.Join(imp.tmpRoot, runID), filepath.Join(imp.runsDir, runID)))
	}
	return err
}

func hashStream(w io.Writer, r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return hashStream(io.Discard, f)
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
