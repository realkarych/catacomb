package main

import "github.com/spf13/cobra"

const (
	groupObserve  = "observe"
	groupSetup    = "setup"
	groupAdvanced = "advanced"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "catacomb",
		Short: "Execution-graph observability for Claude Code agentic pipelines",
		Long: `Catacomb builds a real-time execution graph of your Claude Code sessions —
prompts, turns, tool calls, MCP calls, and subagents — and serves it in a
web UI and a terminal observer.

Common recipes:
  Observe every session (all projects):
      catacomb up --global

  Load past sessions into the UI:
      catacomb up --history

  Read conversation content in the UI (off by default):
      catacomb daemon --allow-payload-access

Run 'catacomb <command> --help' for details on any command.`,
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
	root.AddCommand(observe(newDownCmd()))
	root.AddCommand(observe(newRestartCmd()))
	root.AddCommand(observe(newUICmd()))
	root.AddCommand(observe(newWatchCmd()))
	root.AddCommand(observe(newStatusCmd()))
	root.AddCommand(observe(newObserveCmd()))
	root.AddCommand(observe(newLogsCmd()))
	root.AddCommand(setup(newDaemonCmd()))
	root.AddCommand(setup(newInstallHooksCmd()))
	root.AddCommand(setup(newEnvCmd()))
	root.AddCommand(advanced(newHookCmd()))
	root.AddCommand(advanced(newMarkCmd()))
	root.AddCommand(advanced(newIngestCmd()))
	root.AddCommand(advanced(newRunCmd()))
	root.AddCommand(advanced(newReplayCmd()))
	root.AddCommand(advanced(newDiffCmd()))
	root.AddCommand(advanced(newDemoCmd()))
	root.AddCommand(advanced(newRunsCmd()))
	root.AddCommand(advanced(newSnapshotCmd()))
	root.AddCommand(advanced(newInspectCmd()))
	root.AddCommand(advanced(newVersionCmd()))
	root.AddCommand(advanced(newExportCmd()))
	return root
}
