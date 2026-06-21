package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "catacomb",
		Short:         "Execution-graph observability for Claude Code agentic pipelines",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newReplayCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newInstallHooksCmd())
	return root
}
