package main

import "github.com/spf13/cobra"

const (
	groupObserve  = "observe"
	groupSetup    = "setup"
	groupAdvanced = "advanced"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "catacomb",
		Short:         "Execution-graph observability for Claude Code agentic pipelines",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddGroup(
		&cobra.Group{ID: groupObserve, Title: "Observe:"},
		&cobra.Group{ID: groupSetup, Title: "Setup:"},
		&cobra.Group{ID: groupAdvanced, Title: "Advanced:"},
	)

	observe := func(cmd *cobra.Command) *cobra.Command {
		cmd.GroupID = groupObserve
		return cmd
	}
	setup := func(cmd *cobra.Command) *cobra.Command {
		cmd.GroupID = groupSetup
		return cmd
	}
	advanced := func(cmd *cobra.Command) *cobra.Command {
		cmd.GroupID = groupAdvanced
		return cmd
	}

	root.AddCommand(observe(newUpCmd()))
	root.AddCommand(observe(newUICmd()))
	root.AddCommand(observe(newWatchCmd()))
	root.AddCommand(observe(newStatusCmd()))
	root.AddCommand(setup(newDaemonCmd()))
	root.AddCommand(setup(newInstallHooksCmd()))
	root.AddCommand(setup(newEnvCmd()))
	root.AddCommand(advanced(newHookCmd()))
	root.AddCommand(advanced(newIngestCmd()))
	root.AddCommand(advanced(newRunCmd()))
	root.AddCommand(advanced(newReplayCmd()))
	root.AddCommand(advanced(newDemoCmd()))
	root.AddCommand(advanced(newVersionCmd()))
	return root
}
