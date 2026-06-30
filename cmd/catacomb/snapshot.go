package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	xjsonl "github.com/realkarych/catacomb/export/jsonl"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func newSnapshotCmd() *cobra.Command {
	var dbPath, runID, outPath string
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Dump current graph state as JSONL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSnapshot(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, newPricer, dbPath, runID, outPath)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&runID, "run", "", "filter to a specific run ID")
	cmd.Flags().StringVar(&outPath, "out", "", "write to file instead of stdout")
	return cmd
}

func runSnapshot(out io.Writer, open storeOpener, mkPricer func() reduce.Pricer, dbPath, runID, outPath string) error {
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	graphs, err := storeGraphs(s, mkPricer())
	if err != nil {
		return err
	}
	nodes, edges := collectSnapshot(graphs, runID)
	runs := collectRunsFor(graphs, runID)
	w := out
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("snapshot create: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	return xjsonl.Snapshot(w, nodes, edges, runs)
}
