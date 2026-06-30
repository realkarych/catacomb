package main

import (
	"context"
	"errors"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/tui"
)

var teaRun = func(m tui.Model) error {
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

var stdoutIsTTY = func() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func shouldDisableColor(flag bool, noColorEnv string, isTTY bool) bool {
	return flag || noColorEnv != "" || !isTTY
}

type observeDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	newClient     func(daemon.Discovery) tui.Client
	newModel      func(context.Context, tui.Client, string, bool) tui.Model
	runProgram    func(tui.Model) error
	noColor       bool
}

func newObserveCmd() *cobra.Command {
	var noColor bool
	cmd := &cobra.Command{
		Use:   "observe [hash]",
		Short: "Interactive terminal observer for a Claude session",
		Long: `Interactive terminal observer over the live daemon feed: sessions, the node
tree, and per-node detail. Pass a session hash to open straight into it.`,
		Example: `  catacomb observe`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hash := ""
			if len(args) == 1 {
				hash = args[0]
			}
			deps := observeDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: clientDiscoveryPath(),
				newClient:     func(d daemon.Discovery) tui.Client { return tui.NewHTTPClient(d) },
				newModel:      tui.NewModel,
				runProgram:    teaRun,
				noColor:       shouldDisableColor(noColor, os.Getenv("NO_COLOR"), stdoutIsTTY()),
			}
			err := runObserveHash(cmd.Context(), cmd.OutOrStdout(), deps, hash)
			if err != nil {
				cmd.PrintErrln(renderErr(err))
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI colors")
	return cmd
}

func runObserve(ctx context.Context, out io.Writer, deps observeDeps) error {
	return runObserveHash(ctx, out, deps, "")
}

func runObserveHash(ctx context.Context, _ io.Writer, deps observeDeps, hash string) error {
	disc, err := deps.readDiscovery(deps.discoveryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoDaemon
		}
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := deps.newClient(disc)
	m := deps.newModel(ctx, client, hash, deps.noColor)
	return deps.runProgram(m)
}
