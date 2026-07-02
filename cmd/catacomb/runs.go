package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func newRunsCmd() *cobra.Command {
	var dbPath string
	var asJSON bool
	var labels []string
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List all runs in the stored catacomb database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateLabelTerms(labels); err != nil {
				return err
			}
			return runRuns(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, newPricer, dbPath, asJSON, labels)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "k=v label selector; keep only runs matching all terms (repeatable, AND)")
	return cmd
}

func validateLabelTerms(terms []string) error {
	for _, term := range terms {
		for _, seg := range strings.Split(term, ",") {
			if len(model.ParseLabels(seg)) != 1 {
				return fmt.Errorf("invalid --label %q: expected k=v (key [a-z0-9_.-]{1,64}, value ≤256 bytes)", term)
			}
		}
	}
	return nil
}

func runRuns(out io.Writer, open storeOpener, mkPricer func() reduce.Pricer, dbPath string, asJSON bool, labels []string) error {
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	graphs, err := storeGraphs(s, mkPricer())
	if err != nil {
		return err
	}
	selector := model.ParseLabels(strings.Join(labels, ","))
	runs := collectRuns(graphs)
	summaries := make([]daemon.SessionSummary, 0, len(runs))
	for _, r := range runs {
		if !model.MatchLabels(r.Labels, selector) {
			continue
		}
		summaries = append(summaries, daemon.SummarizeRun(r.ID, graphs))
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RUN\tSTATUS\tSTARTED\tTOOLS\tTOKENS\tCOST")
	for _, sum := range summaries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
			sum.Session, sum.Status, sum.StartedAt,
			sum.ToolCount, sum.TokensIn+sum.TokensOut,
			formatCost(sum.CostUSD))
	}
	return w.Flush()
}
