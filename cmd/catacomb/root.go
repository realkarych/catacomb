package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "catacomb",
		Short: "Offline eval gate for Claude Code agentic pipelines",
		Long: `Catacomb is an offline eval gate for Claude Code agentic pipelines. It runs
prompt baskets, reduces the recorded transcripts into a canonical execution
graph, derives step and phase keys, aggregates metrics, and gates regressions
against saved baselines.

Common recipes:
  Run a basket and record a run:
      catacomb bench <basket> --record

  Gate a candidate against a baseline:
      catacomb regress --baseline label:main --candidate label:pr

  Build a graph from a single recorded transcript:
      catacomb replay <session>.jsonl

Run 'catacomb <command> --help' for details on any command.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newBenchCmd())
	root.AddCommand(newRegressCmd())
	root.AddCommand(newBaselineCmd())
	root.AddCommand(newTrendsCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newSubgraphCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newReplayCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newVersionCmd())
	return root
}
