package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

func newDaemonCmd() *cobra.Command {
	var dbPath, discoveryPath string
	var reaperWindow time.Duration
	var maxShards int
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the catacomb daemon (receives hook events, builds the live graph)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if discoveryPath == "" {
				discoveryPath = daemon.DiscoveryPath()
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.NewToken, dbPath, discoveryPath, reaperWindow, maxShards)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "catacomb.db", "SQLite database path")
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	cmd.Flags().DurationVar(&reaperWindow, "reaper-window", 30*time.Minute, "idle window before a run is marked abandoned")
	cmd.Flags().IntVar(&maxShards, "max-shards", 4096, "soft cap on in-memory execution shards")
	return cmd
}

func runDaemonWith(ctx context.Context, open func(string) (store.Store, error), listen func() (net.Listener, error), newToken func() (string, error), dbPath, discoveryPath string, reaperWindow time.Duration, maxShards int) error {
	s, err := open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	d := daemon.New(s)
	d.SetReaperWindow(reaperWindow)
	d.SetMaxShards(maxShards)
	err = d.Recover()
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	ln, err := listen()
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	err = daemon.WriteDiscovery(discoveryPath, daemon.Discovery{Addr: ln.Addr().String(), Token: token})
	if err != nil {
		return err
	}
	return d.Serve(ctx, ln, token)
}
