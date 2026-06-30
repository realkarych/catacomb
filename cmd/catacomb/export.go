package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	exportiface "github.com/realkarych/catacomb/export"
	exportagentevals "github.com/realkarych/catacomb/export/agentevals"
	exportevalview "github.com/realkarych/catacomb/export/evalview"
	xjsonl "github.com/realkarych/catacomb/export/jsonl"
	exportneo4j "github.com/realkarych/catacomb/export/neo4j"
	exportotlp "github.com/realkarych/catacomb/export/otlp"
	exportpostgres "github.com/realkarych/catacomb/export/postgres"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

type exportArgs struct {
	dbPath        string
	to            string
	runID         string
	mode          string
	out           string
	otlpEndpoint  string
	postgresDSN   string
	neo4jURI      string
	neo4jUser     string
	neo4jPassword string
}

type exportDeps struct {
	open        storeOpener
	newPricer   func() reduce.Pricer
	newOTLP     func(ctx context.Context, endpoint string) (exportiface.Exporter, error)
	newNeo4j    func(ctx context.Context, uri, user, pass string) (exportiface.Exporter, error)
	newPostgres func(ctx context.Context, dsn string) (exportiface.Exporter, error)
}

func realNewOTLP(ctx context.Context, endpoint string) (exportiface.Exporter, error) {
	return exportotlp.New(ctx, endpoint, "", "")
}

func realNewNeo4j(ctx context.Context, uri, user, pass string) (exportiface.Exporter, error) {
	return exportneo4j.New(ctx, uri, user, pass)
}

func realNewPostgres(ctx context.Context, dsn string) (exportiface.Exporter, error) {
	return exportpostgres.New(ctx, dsn)
}

func newExportCmd() *cobra.Command {
	var a exportArgs
	deps := exportDeps{
		open:        store.OpenSQLiteReadOnly,
		newPricer:   newPricer,
		newOTLP:     realNewOTLP,
		newNeo4j:    realNewNeo4j,
		newPostgres: realNewPostgres,
	}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export graph data to an external sink (jsonl, otlp, neo4j, postgres, agentevals, evalview)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd.Context(), cmd.OutOrStdout(), deps, a)
		},
	}
	cmd.Flags().StringVar(&a.dbPath, "db", defaultDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&a.to, "to", "", "export sink: jsonl|otlp|neo4j|postgres|agentevals|evalview")
	cmd.Flags().StringVar(&a.runID, "run", "", "filter to a specific run ID")
	cmd.Flags().StringVar(&a.mode, "mode", "", "export mode: materialized (default) or events")
	cmd.Flags().StringVar(&a.out, "out", "", "write to file instead of stdout (jsonl, agentevals, evalview)")
	cmd.Flags().StringVar(&a.otlpEndpoint, "otlp-export-endpoint", "", "OTLP endpoint (grpc://host:port or http(s)://...)")
	cmd.Flags().StringVar(&a.postgresDSN, "postgres-export-dsn", "", "PostgreSQL DSN")
	cmd.Flags().StringVar(&a.neo4jURI, "neo4j-export-uri", "", "Neo4j bolt URI")
	cmd.Flags().StringVar(&a.neo4jUser, "neo4j-export-user", "", "Neo4j user")
	cmd.Flags().StringVar(&a.neo4jPassword, "neo4j-export-password", "", "Neo4j password")
	return cmd
}

func runExport(ctx context.Context, out io.Writer, deps exportDeps, a exportArgs) error {
	s, err := openReadStore(deps.open, a.dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	switch a.to {
	case "jsonl":
		return exportJSONL(ctx, out, s, deps, a)
	case "agentevals":
		return exportSerialized(out, s, deps, a, exportagentevals.WriteAll)
	case "evalview":
		return exportSerialized(out, s, deps, a, exportevalview.WriteAll)
	case "otlp", "neo4j", "postgres":
		if a.mode == "events" {
			return ErrModeUnsupported
		}
		return exportSink(ctx, out, s, deps, a)
	default:
		return ErrUnknownSink
	}
}

func exportJSONL(_ context.Context, out io.Writer, s store.Store, deps exportDeps, a exportArgs) error {
	w := out
	if a.out != "" {
		f, err := os.Create(a.out)
		if err != nil {
			return fmt.Errorf("export create: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	if a.mode == "events" {
		return exportObservations(w, s, a.runID)
	}
	graphs, err := storeGraphs(s, deps.newPricer())
	if err != nil {
		return err
	}
	nodes, edges := collectSnapshot(graphs, a.runID)
	runs := collectRunsFor(graphs, a.runID)
	return xjsonl.Snapshot(w, nodes, edges, runs)
}

func exportObservations(w io.Writer, s store.Store, runID string) error {
	obs, err := s.ObservationsSince(0)
	if err != nil {
		return fmt.Errorf("store read: %w", err)
	}
	enc := json.NewEncoder(w)
	for _, o := range obs {
		if runID != "" && o.RunID != runID {
			continue
		}
		if err := enc.Encode(o); err != nil {
			return fmt.Errorf("export encode: %w", err)
		}
	}
	return nil
}

func buildExporter(ctx context.Context, deps exportDeps, a exportArgs) (exportiface.Exporter, error) {
	switch a.to {
	case "otlp":
		if a.otlpEndpoint == "" {
			return nil, ErrSinkNotConfigured
		}
		return deps.newOTLP(ctx, a.otlpEndpoint)
	case "neo4j":
		if a.neo4jURI == "" {
			return nil, ErrSinkNotConfigured
		}
		return deps.newNeo4j(ctx, a.neo4jURI, a.neo4jUser, a.neo4jPassword)
	default:
		if a.postgresDSN == "" {
			return nil, ErrSinkNotConfigured
		}
		return deps.newPostgres(ctx, a.postgresDSN)
	}
}

func exportSerialized(out io.Writer, s store.Store, deps exportDeps, a exportArgs, write func(io.Writer, []*model.Node, []*model.Edge) error) error {
	if a.mode == "events" {
		return ErrModeUnsupported
	}
	w := out
	if a.out != "" {
		f, err := os.Create(a.out)
		if err != nil {
			return fmt.Errorf("export create: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	graphs, err := storeGraphs(s, deps.newPricer())
	if err != nil {
		return err
	}
	nodes, edges := collectSnapshot(graphs, a.runID)
	return write(w, nodes, edges)
}

func exportSink(ctx context.Context, out io.Writer, s store.Store, deps exportDeps, a exportArgs) error {
	exp, err := buildExporter(ctx, deps, a)
	if err != nil {
		return err
	}
	graphs, err := storeGraphs(s, deps.newPricer())
	if err != nil {
		_ = exp.Shutdown(ctx)
		return err
	}
	nodes, edges := collectSnapshot(graphs, a.runID)
	if err := exp.SnapshotState(ctx, nodes, edges); err != nil {
		_ = exp.Shutdown(ctx)
		return fmt.Errorf("export snapshot: %w", err)
	}
	runs := collectRuns(graphs)
	if re, ok := exp.(exportiface.RunExporter); ok {
		runsToExport := runs
		if a.runID != "" {
			var filtered []model.Run
			for _, r := range runs {
				if r.ID == a.runID {
					filtered = append(filtered, r)
				}
			}
			runsToExport = filtered
		}
		if err := re.SnapshotRuns(ctx, runsToExport); err != nil {
			_ = exp.Shutdown(ctx)
			return fmt.Errorf("export snapshot runs: %w", err)
		}
	}
	for _, r := range runs {
		if a.runID != "" && r.ID != a.runID {
			continue
		}
		if err := exp.FlushRun(ctx, r.ID); err != nil {
			_ = exp.Shutdown(ctx)
			return fmt.Errorf("export flush: %w", err)
		}
	}
	if err := exp.Shutdown(ctx); err != nil {
		return fmt.Errorf("export shutdown: %w", err)
	}
	fmt.Fprintf(out, "exported %d nodes, %d edges to %s\n", len(nodes), len(edges), a.to)
	return nil
}
