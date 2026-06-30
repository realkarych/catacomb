package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

type restartDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	stopFn        func(pid int, force bool) (bool, error)
	waitGoneFn    func(pid int) bool
	removeDisc    func(string) error
	startDaemon   func() error
	pollHealthz   func(ctx context.Context, addr string) error
	after         func(time.Duration) <-chan time.Time
	force         bool
	asJSON        bool
	waitSeconds   int
}

type restartReport struct {
	Stopped bool   `json:"stopped"`
	Started bool   `json:"started"`
	Addr    string `json:"addr,omitempty"`
}

func newRestartCmd() *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Stop the running daemon and start a fresh one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			discPath := daemon.DiscoveryPath()
			deps := restartDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: discPath,
				stopFn:        stopDaemon,
				waitGoneFn:    waitGone,
				removeDisc:    os.Remove,
				startDaemon:   buildStartDaemon(discPath, ""),
				pollHealthz:   prodPollHealthz,
				after:         time.After,
				force:         force,
				asJSON:        asJSON,
				waitSeconds:   5,
			}
			return runRestart(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "escalate a stuck daemon stop to SIGKILL")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	return cmd
}

func runRestart(ctx context.Context, out io.Writer, deps restartDeps) error {
	rep := restartReport{}

	disc, derr := deps.readDiscovery(deps.discoveryPath)
	if derr != nil && !errors.Is(derr, os.ErrNotExist) {
		return fmt.Errorf("restart: read discovery: %w", derr)
	}

	if derr == nil {
		stopped, serr := deps.stopFn(disc.Pid, deps.force)
		if serr != nil {
			return serr
		}
		if !stopped {
			return ErrDaemonStop
		}
		rep.Stopped = true
		if err := deps.removeDisc(deps.discoveryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restart: remove discovery: %w", err)
		}
	}

	if err := deps.startDaemon(); err != nil {
		return fmt.Errorf("restart: start daemon: %w", err)
	}

	ready := false
	var newDisc daemon.Discovery
	for attempt := 0; attempt < deps.waitSeconds; attempt++ {
		d, err := deps.readDiscovery(deps.discoveryPath)
		if err == nil {
			if hzErr := deps.pollHealthz(ctx, d.Addr); hzErr == nil {
				newDisc = d
				ready = true
				break
			}
		}
		<-deps.after(time.Second)
	}
	rep.Started = ready
	if ready {
		rep.Addr = newDisc.Addr
	}

	if deps.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}

	if rep.Stopped {
		_, _ = fmt.Fprintln(out, "daemon stopped")
	}
	if rep.Started {
		_, _ = fmt.Fprintf(out, "daemon restarted (%s)\n", rep.Addr)
	} else {
		_, _ = fmt.Fprintln(out, "restart: daemon did not start in time")
	}
	return nil
}
