package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/redact"
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
	sinks              []config.Sink
	sources            config.SourcesConfig
	discoveryPath      string
	configPath         string
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
	payloads           config.PayloadsConfig
}

func defaultDaemonDeps() daemonDeps {
	return daemonDeps{
		openStore:  store.Open,
		listen:     daemon.ListenLoopback,
		listenGRPC: daemon.ListenLoopback,
		newToken:   daemon.NewToken,
	}
}

func firstOTLPSink(sinks []config.Sink) (endpoint, project string) {
	for _, s := range sinks {
		if s.Type == config.SinkOTLP {
			return s.Endpoint, s.Project
		}
	}
	return "", ""
}

func dedupSinks(sinks []config.Sink) []config.Sink {
	seen := make(map[string]struct{}, len(sinks))
	out := make([]config.Sink, 0, len(sinks))
	for _, s := range sinks {
		k := config.SinkKey(s)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

func sinkTypeStrings(sinks []config.Sink) []string {
	if len(sinks) == 0 {
		return nil
	}
	out := make([]string, len(sinks))
	for i, s := range sinks {
		out[i] = s.Type
	}
	return out
}

func enabledSourceNames(s config.SourcesConfig) []string {
	enabled := func(b *bool) bool { return b == nil || *b }
	var names []string
	if enabled(s.Hooks.Enabled) {
		names = append(names, "hooks")
	}
	if enabled(s.Otel.Enabled) {
		names = append(names, "otel")
	}
	if enabled(s.StreamJSON.Enabled) {
		names = append(names, "stream_json")
	}
	if enabled(s.JSONL.Enabled) {
		names = append(names, "jsonl")
	}
	return names
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
			resolvedConfigPath := configFilePath(flags, os.LookupEnv, home)
			cfg, err := resolveConfig(flags, os.ReadFile, os.LookupEnv, home)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("transcript-dir") {
				enabled := true
				cfg.Sources.JSONL.Enabled = &enabled
				cfg.Sources.JSONL.TranscriptDir = transcriptDir
			}
			if cmd.Flags().Changed("transcript-exclude") {
				cfg.Sources.JSONL.Exclude = transcriptExclude
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			params := daemonParams{
				store:              cfg.Store,
				sinks:              cfg.Sinks,
				sources:            cfg.Sources,
				discoveryPath:      resolveDiscovery(cfg.Daemon.Discovery),
				configPath:         resolvedConfigPath,
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
				payloads:           cfg.Payloads,
			}
			return runDaemonWith(ctx, defaultDaemonDeps(), params)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file path (default: ~/.catacomb/config.yaml; or $CATACOMB_CONFIG)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default: ~/.catacomb/catacomb.db; maps to store.sqlite.path)")
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	cmd.Flags().DurationVar(&reaperWindow, "reaper-window", 30*time.Minute, "idle window before a run is marked abandoned")
	cmd.Flags().IntVar(&maxShards, "max-shards", 4096, "soft cap on in-memory execution shards")
	cmd.Flags().StringVar(&otlpEndpoint, "otlp-export-endpoint", "", "downstream OTLP endpoint to export the reconstructed trace tree (empty = disabled) [deprecated: prefer sinks in config.yaml]")
	cmd.Flags().StringVar(&otlpProject, "otlp-export-project", "catacomb", "OpenInference project name (resource attribute openinference.project.name)")
	cmd.Flags().StringVar(&postgresDSN, "postgres-export-dsn", "", "PostgreSQL DSN to export the materialized graph (empty = disabled) [deprecated: prefer sinks in config.yaml]")
	cmd.Flags().StringVar(&neo4jURI, "neo4j-export-uri", "", "Neo4j Bolt URI to export the materialized graph (empty = disabled) [deprecated: prefer sinks in config.yaml]")
	cmd.Flags().StringVar(&neo4jUser, "neo4j-export-user", "", "Neo4j username for materialized graph export")
	cmd.Flags().StringVar(&neo4jPassword, "neo4j-export-password", "", "Neo4j password for materialized graph export")
	cmd.Flags().StringVar(&transcriptDir, "transcript-dir", "", "Claude Code transcript dir to tail (empty = disabled; recommended: ~/.claude/projects) [deprecated: prefer sources.jsonl in config.yaml]")
	cmd.Flags().StringArrayVar(&transcriptExclude, "transcript-exclude", nil, "glob(s) of transcript paths to never tail (repeatable; the daemon db + cwd are always excluded)")
	cmd.Flags().BoolVar(&allowPayloadAccess, "allow-payload-access", false, "enable the node payload content endpoint (default off)")
	cmd.Flags().BoolVar(&allowAnnotations, "allow-annotations", false, "enable the node annotation write endpoint (default off)")
	return cmd
}

func runDaemonWith(ctx context.Context, deps daemonDeps, p daemonParams) error {
	if existing, rerr := daemon.ReadDiscovery(p.discoveryPath); rerr == nil && daemonOwned(existing) {
		return fmt.Errorf("daemon: %w", ErrDaemonAlreadyRunning)
	}
	s, err := deps.openStore(p.store)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	dbPath := storeDBPath(p.store)
	d := daemon.New(s)
	d.SetReaperWindow(p.reaperWindow)
	d.SetMaxShards(p.maxShards)
	d.SetDBPath(dbPath)
	d.SetAllowPayloadAccess(p.allowPayloadAccess)
	d.SetAllowAnnotations(p.allowAnnotations)
	d.SetPayloadPolicy(redact.Policy{Mode: redact.Mode(p.payloads.Mode), MaxBytes: p.payloads.MaxBytes})

	sinks := append([]config.Sink(nil), p.sinks...)
	if p.otlpEndpoint != "" {
		sinks = append(sinks, config.Sink{Type: config.SinkOTLP, Endpoint: p.otlpEndpoint, Project: p.otlpProject})
	}
	if p.postgresDSN != "" {
		sinks = append(sinks, config.Sink{Type: config.SinkPostgres, DSN: p.postgresDSN})
	}
	if p.neo4jURI != "" {
		sinks = append(sinks, config.Sink{Type: config.SinkNeo4j, URI: p.neo4jURI, User: p.neo4jUser, Password: p.neo4jPassword})
	}
	sinks = dedupSinks(sinks)

	sources := p.sources
	if p.transcriptDir != "" {
		enabled := true
		sources.JSONL.Enabled = &enabled
		sources.JSONL.TranscriptDir = p.transcriptDir
		if p.transcriptExclude != nil {
			sources.JSONL.Exclude = p.transcriptExclude
		}
	}

	otlpEndpoint, otlpProject := firstOTLPSink(sinks)
	d.SetReproConfig(repro.Config{
		OTLPEndpoint:  otlpEndpoint,
		OTLPProject:   otlpProject,
		TranscriptDir: sources.JSONL.TranscriptDir,
	})
	d.SetSinks(sinks)
	d.SetSources(sources)

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
		TranscriptDir:      sources.JSONL.TranscriptDir,
		DBPath:             dbPath,
		ConfigPath:         p.configPath,
		AllowPayloadAccess: p.allowPayloadAccess,
		AllowAnnotations:   p.allowAnnotations,
		StoreBackend:       p.store.Backend,
		SinkTypes:          sinkTypeStrings(sinks),
		SourcesEnabled:     enabledSourceNames(sources),
		ReaperWindow:       p.reaperWindow.String(),
		MaxShards:          p.maxShards,
	}
	disc.Pid = os.Getpid()
	disc.StartedAt = time.Now().UTC().Format(time.RFC3339)
	if tok, tokErr := processStartTime(disc.Pid); tokErr == nil {
		disc.StartToken = tok
	}
	disc.BootID = bootID()
	if err = daemon.WriteDiscovery(p.discoveryPath, disc); err != nil {
		return err
	}
	log.Printf("catacomb daemon started addr=%s store=%s sinks=%v sources=%v", disc.Addr, p.store.Backend, disc.SinkTypes, disc.SourcesEnabled)
	err = d.Serve(ctx, ln, grpcLn, token)
	_ = os.Remove(p.discoveryPath)
	return err
}
