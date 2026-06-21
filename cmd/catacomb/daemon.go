package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

func newDaemonCmd() *cobra.Command {
	var dbPath, discoveryPath string
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the catacomb daemon (receives hook events, builds the live graph)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if discoveryPath == "" {
				discoveryPath = daemon.DiscoveryPath()
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.NewToken, dbPath, discoveryPath)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "catacomb.db", "SQLite database path")
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	return cmd
}

func runDaemonWith(ctx context.Context, open func(string) (store.Store, error), listen func() (net.Listener, error), newToken func() (string, error), dbPath, discoveryPath string) error {
	s, err := open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	d := daemon.New(s)
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
