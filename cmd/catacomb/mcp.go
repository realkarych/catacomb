package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the catacomb MCP stdio server (exposes the mark checkpoint tool)",
		Long: `Run the catacomb MCP server over stdio (JSON-RPC 2.0, newline-delimited).

Wire it into Claude Code with --mcp-config so the agent can call the
mcp__catacomb__mark checkpoint tool during a run:

  {"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}

The tool is a pure acknowledgement: the phase marker rides the trace stream and
the catacomb reducer synthesizes it from the tool-call input, so the server needs
no daemon and no configuration. It exits when its stdin is closed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return mcp.Serve(ctx, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
