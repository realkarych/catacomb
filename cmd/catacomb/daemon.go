package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/repro"
	"github.com/realkarych/catacomb/store"
)

type daemonDeps struct {
	openStore  func(config.StoreConfig) (store.Store, error)
	listen     func() (net.Listener, error)
	listenGRPC func() (net.Listener, error)
	newToken   func() (string, error)
}

type daemonParams struct {
	store              config.StoreConfig
	discoveryPath      string
	reaperWindow       time.Duration
	maxShards          int
	otlpEndpoint       string
	otlpProject        string
	postgresDSN        string
	neo4jURI           string
	neo4jUser          string
	neo4jPassword      string
	transcriptDir      string
	transcriptExclude  []string
	allowPayloadAccess bool
	allowAnnotations   bool
}

func defaultDaemonDeps() daemonDeps {
	return daemonDeps{
		openStore:  store.Open,
		listen:     daemon.ListenLoopback,
		listenGRPC: daemon.ListenLoopback,
		newToken:   daemon.NewToken,
	}
}

func storeDBPath(c config.StoreConfig) string {
	if c.Backend == config.BackendSQLite {
		return c.SQLite.Path
	}
	return ""
}

func resolveDiscovery(s string) string {
	if s != "" {
		return s
	}
	return daemon.DiscoveryPath()
}

func newDaemonCmd() *cobra.Command {
	var configPath, dbPath, discoveryPath, otlpEndpoint, otlpProject, postgresDSN string
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
persists it to the configured primary store, and serves the web UI and gRPC feed.

Configuration is loaded from ~/.catacomb/config.yaml (override with --config or
$CATACOMB_CONFIG). Set store.backend: memory for a live-only daemon that persists
nothing. The default SQLite database is ~/.catacomb/catacomb.db. Existing flags
remain the highest-precedence override.`,
		Example: `  # live only
  catacomb daemon

  # backfill and tail every past + live session
  catacomb daemon --transcript-dir ~/.claude/projects --allow-payload-access`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := osUserHomeDir()
			if err != nil {
				return fmt.Errorf("daemon: resolve home: %w", err)
			}
			flags := daemonFlags{
				configPath: configPath, configPathSet: cmd.Flags().Changed("config"),
				dbPath: dbPath, dbPathSet: cmd.Flags().Changed("db"),
				discoveryPath: discoveryPath, discoveryPathSet: cmd.Flags().Changed("discovery"),
				reaperWindow: reaperWindow, reaperWindowSet: cmd.Flags().Changed("reaper-window"),
				maxShards: maxShards, maxShardsSet: cmd.Flags().Changed("max-shards"),
				allowPayloadAccess: allowPayloadAccess, allowPayloadAccessSet: cmd.Flags().Changed("allow-payload-access"),
				allowAnnotations: allowAnnotations, allowAnnotationsSet: cmd.Flags().Changed("allow-annotations"),
			}
			cfg, err := resolveConfig(flags, os.ReadFile, os.LookupEnv, home)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			params := daemonParams{
				store:              cfg.Store,
				discoveryPath:      resolveDiscovery(cfg.Daemon.Discovery),
				reaperWindow:       time.Duration(cfg.Daemon.ReaperWindow),
				maxShards:          cfg.Daemon.MaxShards,
				otlpEndpoint:       otlpEndpoint,
				otlpProject:        otlpProject,
				postgresDSN:        postgresDSN,
				neo4jURI:           neo4jURI,
				neo4jUser:          neo4jUser,
				neo4jPassword:      neo4jPassword,
				transcriptDir:      transcriptDir,
				transcriptExclude:  transcriptExclude,
				allowPayloadAccess: cfg.Daemon.AllowPayloadAccess,
				allowAnnotations:   cfg.Daemon.AllowAnnotations,
			}
			return runDaemonWith(ctx, defaultDaemonDeps(), params)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file path (default: ~/.catacomb/config.yaml; or $CATACOMB_CONFIG)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default: ~/.catacomb/catacomb.db; maps to store.sqlite.path)")
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

func runDaemonWith(ctx context.Context, deps daemonDeps, p daemonParams) error {
	s, err := deps.openStore(p.store)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	dbPath := storeDBPath(p.store)
	d := daemon.New(s)
	d.SetReaperWindow(p.reaperWindow)
	d.SetMaxShards(p.maxShards)
	d.SetOTLPEndpoint(p.otlpEndpoint)
	d.SetOTLPProject(p.otlpProject)
	d.SetPostgresDSN(p.postgresDSN)
	d.SetNeo4j(p.neo4jURI, p.neo4jUser, p.neo4jPassword)
	d.SetDBPath(dbPath)
	d.SetTranscriptDir(p.transcriptDir)
	d.SetTranscriptExclude(p.transcriptExclude)
	d.SetAllowPayloadAccess(p.allowPayloadAccess)
	d.SetAllowAnnotations(p.allowAnnotations)
	d.SetReproConfig(repro.Config{
		OTLPEndpoint:  p.otlpEndpoint,
		OTLPProject:   p.otlpProject,
		TranscriptDir: p.transcriptDir,
	})
	err = d.Recover()
	if err != nil {
		return err
	}
	token, err := deps.newToken()
	if err != nil {
		return err
	}
	ln, err := deps.listen()
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	grpcLn, err := deps.listenGRPC()
	if err != nil {
		return err
	}
	defer func() { _ = grpcLn.Close() }()

	disc := daemon.Discovery{
		Addr:               ln.Addr().String(),
		Token:              token,
		GRPCAddr:           grpcLn.Addr().String(),
		TranscriptDir:      p.transcriptDir,
		DBPath:             dbPath,
		AllowPayloadAccess: p.allowPayloadAccess,
		AllowAnnotations:   p.allowAnnotations,
	}
	disc.Pid = os.Getpid()
	disc.StartedAt = time.Now().UTC().Format(time.RFC3339)
	if err = daemon.WriteDiscovery(p.discoveryPath, disc); err != nil {
		return err
	}
	err = d.Serve(ctx, ln, grpcLn, token)
	_ = os.Remove(p.discoveryPath)
	return err
}
