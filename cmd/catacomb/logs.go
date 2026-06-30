package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

type logsDeps struct {
	logPath string
	openLog func(string) (io.ReadCloser, error)
	follow  bool
	tick    <-chan time.Time
}

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print the daemon log (use -f to follow)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var tick <-chan time.Time
			if follow {
				t := time.NewTicker(200 * time.Millisecond)
				defer t.Stop()
				tick = t.C
			}
			deps := logsDeps{
				logPath: daemon.DiscoveryPath() + ".log",
				openLog: func(p string) (io.ReadCloser, error) { return os.Open(p) },
				follow:  follow,
				tick:    tick,
			}
			return runLogs(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the daemon log")
	return cmd
}

func runLogs(ctx context.Context, out io.Writer, deps logsDeps) error {
	r, err := deps.openLog(deps.logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(out, "(no log file yet)")
			return nil
		}
		return fmt.Errorf("logs: open: %w", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("logs: read: %w", err)
	}
	if !deps.follow {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-deps.tick:
			if !ok {
				return nil
			}
			if _, err := io.Copy(out, r); err != nil {
				return fmt.Errorf("logs: follow read: %w", err)
			}
		}
	}
}
