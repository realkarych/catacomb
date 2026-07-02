package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

var nowFn = time.Now

func newBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Manage named baselines for regression comparison",
	}
	cmd.AddCommand(newBaselineSetCmd(), newBaselineListCmd(), newBaselineRmCmd())
	return cmd
}

func newBaselineSetCmd() *cobra.Command {
	var dbPath string
	var labels []string
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Create or replace a baseline from a label selector",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBaselineSet(cmd.OutOrStdout(), store.OpenSQLite, newPricer, dbPath, args[0], labels)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "k=v label selector (repeatable, AND)")
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
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
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
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	return cmd
}

func runBaselineSet(out io.Writer, open storeOpener, mkPricer func() reduce.Pricer, dbPath, name string, labels []string) error {
	if err := validateBaselineName(name); err != nil {
		return operational(err)
	}
	if len(labels) == 0 {
		return operational(fmt.Errorf("baseline set %q: at least one --label is required", name))
	}
	if err := validateLabelTerms(labels); err != nil {
		return operational(err)
	}
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()

	selector := model.ParseLabels(strings.Join(labels, ","))
	group, err := loadRunGroup(s, mkPricer(), selector)
	if err != nil {
		return operational(err)
	}
	if len(group) == 0 {
		return operational(fmt.Errorf("baseline set %q: %w", name, ErrEmptyGroup))
	}
	ids := make([]string, 0, len(group))
	repro := make(map[string]*model.ReproMeta, len(group))
	for _, rg := range group {
		ids = append(ids, rg.Run.ID)
		if rg.Run.Repro != nil {
			repro[rg.Run.ID] = rg.Run.Repro
		}
	}
	sort.Strings(ids)
	b := model.Baseline{Name: name, RunIDs: ids, Selector: selector, CreatedAt: nowFn(), Repro: repro}
	if err := s.UpsertBaseline(b); err != nil {
		return operational(fmt.Errorf("baseline set: %w", err))
	}
	fmt.Fprintf(out, "baseline %q set: %d runs\n", name, len(ids))
	return nil
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
