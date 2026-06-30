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

func newInspectCmd() *cobra.Command {
	var dbPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "inspect <run_id>",
		Short: "Show detailed summary for a specific run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, newPricer, dbPath, args[0], asJSON)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func runInspect(out io.Writer, open storeOpener, mkPricer func() reduce.Pricer, dbPath, runID string, asJSON bool) error {
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
	found := false
	for _, r := range runs {
		if r.ID == runID {
			found = true
			break
		}
	}
	if !found {
		return ErrRunNotFound
	}
	sum := daemon.SummarizeRun(runID, graphs)
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(sum)
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Run:\t%s\n", sum.Session)
	fmt.Fprintf(w, "Status:\t%s\n", sum.Status)
	fmt.Fprintf(w, "Started:\t%s\n", sum.StartedAt)
	fmt.Fprintf(w, "Nodes:\t%d\n", sum.NodeCount)
	fmt.Fprintf(w, "Tools:\t%d\n", sum.ToolCount)
	fmt.Fprintf(w, "Tokens in:\t%d\n", sum.TokensIn)
	fmt.Fprintf(w, "Tokens out:\t%d\n", sum.TokensOut)
	fmt.Fprintf(w, "Cost:\t%s\n", formatCost(sum.CostUSD))
	for _, k := range sortedKeys(sum.CountsByType) {
		fmt.Fprintf(w, "  %s:\t%d\n", k, sum.CountsByType[k])
	}
	return w.Flush()
}
