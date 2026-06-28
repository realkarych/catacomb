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
	var dbPath, discoveryPath, otlpEndpoint, otlpProject, postgresDSN string
	var neo4jURI, neo4jUser, neo4jPassword string
	var reaperWindow time.Duration
	var maxShards int
	var transcriptDir string
	var transcriptExclude []string
	var allowPayloadAccess bool
	var allowAnnotations bool
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the catacomb daemon (receives hook events, builds the live graph)",
		Long: `Run the catacomb daemon: it receives hook events, builds the live graph,
persists it to SQLite, and serves the web UI and gRPC feed.

Pass --transcript-dir ~/.claude/projects to also tail recorded transcripts,
which backfills past sessions and follows live ones. Pass --allow-payload-access
to enable the token-gated content endpoint (off by default).`,
		Example: `  # live only
  catacomb daemon

  # backfill and tail every past + live session
  catacomb daemon --transcript-dir ~/.claude/projects --allow-payload-access`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if discoveryPath == "" {
				discoveryPath = daemon.DiscoveryPath()
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken, dbPath, discoveryPath, reaperWindow, maxShards, otlpEndpoint, otlpProject, postgresDSN, neo4jURI, neo4jUser, neo4jPassword, transcriptDir, transcriptExclude, allowPayloadAccess, allowAnnotations)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "catacomb.db", "SQLite database path")
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	cmd.Flags().DurationVar(&reaperWindow, "reaper-window", 30*time.Minute, "idle window before a run is marked abandoned")
	cmd.Flags().IntVar(&maxShards, "max-shards", 4096, "soft cap on in-memory execution shards")
	cmd.Flags().StringVar(&otlpEndpoint, "otlp-export-endpoint", "", "downstream OTLP endpoint to export the reconstructed trace tree (empty = disabled)")
	cmd.Flags().StringVar(&otlpProject, "otlp-export-project", "catacomb", "OpenInference project name (resource attribute openinference.project.name)")
	cmd.Flags().StringVar(&postgresDSN, "postgres-export-dsn", "", "PostgreSQL DSN to export the materialized graph (empty = disabled)")
	cmd.Flags().StringVar(&neo4jURI, "neo4j-export-uri", "", "Neo4j Bolt URI to export the materialized graph (empty = disabled)")
	cmd.Flags().StringVar(&neo4jUser, "neo4j-export-user", "", "Neo4j username for materialized graph export")
	cmd.Flags().StringVar(&neo4jPassword, "neo4j-export-password", "", "Neo4j password for materialized graph export")
	cmd.Flags().StringVar(&transcriptDir, "transcript-dir", "", "Claude Code transcript dir to tail (empty = disabled; recommended: ~/.claude/projects)")
	cmd.Flags().StringArrayVar(&transcriptExclude, "transcript-exclude", nil, "glob(s) of transcript paths to never tail (repeatable; the daemon db + cwd are always excluded)")
	cmd.Flags().BoolVar(&allowPayloadAccess, "allow-payload-access", false, "enable the node payload content endpoint (default off)")
	cmd.Flags().BoolVar(&allowAnnotations, "allow-annotations", false, "enable the node annotation write endpoint (default off)")
	return cmd
}

func runDaemonWith(
	ctx context.Context,
	open func(string) (store.Store, error),
	listen func() (net.Listener, error),
	listenGRPC func() (net.Listener, error),
	newToken func() (string, error),
	dbPath, discoveryPath string,
	reaperWindow time.Duration,
	maxShards int,
	otlpEndpoint string,
	otlpProject string,
	postgresDSN string,
	neo4jURI string,
	neo4jUser string,
	neo4jPassword string,
	transcriptDir string,
	transcriptExclude []string,
	allowPayloadAccess bool,
	allowAnnotations bool,
) error {
	s, err := open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	d := daemon.New(s)
	d.SetReaperWindow(reaperWindow)
	d.SetMaxShards(maxShards)
	d.SetOTLPEndpoint(otlpEndpoint)
	d.SetOTLPProject(otlpProject)
	d.SetPostgresDSN(postgresDSN)
	d.SetNeo4j(neo4jURI, neo4jUser, neo4jPassword)
	d.SetDBPath(dbPath)
	d.SetTranscriptDir(transcriptDir)
	d.SetTranscriptExclude(transcriptExclude)
	d.SetAllowPayloadAccess(allowPayloadAccess)
	d.SetAllowAnnotations(allowAnnotations)
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

	grpcLn, err := listenGRPC()
	if err != nil {
		return err
	}
	defer func() { _ = grpcLn.Close() }()

	disc := daemon.Discovery{
		Addr:               ln.Addr().String(),
		Token:              token,
		GRPCAddr:           grpcLn.Addr().String(),
		TranscriptDir:      transcriptDir,
		DBPath:             dbPath,
		AllowPayloadAccess: allowPayloadAccess,
		AllowAnnotations:   allowAnnotations,
	}
	disc.Pid = os.Getpid()
	disc.StartedAt = time.Now().UTC().Format(time.RFC3339)
	if err := daemon.WriteDiscovery(discoveryPath, disc); err != nil {
		return err
	}
	return d.Serve(ctx, ln, grpcLn, token)
}
