package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func newRunsCmd() *cobra.Command {
	var dbPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List all runs in the stored catacomb database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRuns(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, newPricer, dbPath, asJSON)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func runRuns(out io.Writer, open storeOpener, mkPricer func() reduce.Pricer, dbPath string, asJSON bool) error {
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	graphs, err := storeGraphs(s, mkPricer())
	if err != nil {
		return err
	}
	runs := collectRuns(graphs)
	summaries := make([]daemon.SessionSummary, 0, len(runs))
	for _, r := range runs {
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
