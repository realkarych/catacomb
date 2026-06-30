package build

import (
	"context"
	"fmt"
	"log"

	"github.com/realkarych/catacomb/config"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/export/jsonl"
	neo4jexport "github.com/realkarych/catacomb/export/neo4j"
	"github.com/realkarych/catacomb/export/otlp"
	pgexport "github.com/realkarych/catacomb/export/postgres"
)

type Builders struct {
	NewOTLP     func(ctx context.Context, endpoint, grpcAddr, httpAddr string) (exportiface.Exporter, error)
	NewPostgres func(ctx context.Context, dsn string) (exportiface.Exporter, error)
	NewNeo4j    func(ctx context.Context, uri, user, password string) (exportiface.Exporter, error)
	NewJSONL    func(path string) (exportiface.Exporter, error)
}

var defaultBuilders = Builders{
	NewOTLP: func(ctx context.Context, endpoint, grpcAddr, httpAddr string) (exportiface.Exporter, error) {
		return otlp.New(ctx, endpoint, grpcAddr, httpAddr)
	},
	NewPostgres: func(ctx context.Context, dsn string) (exportiface.Exporter, error) {
		return pgexport.New(ctx, dsn)
	},
	NewNeo4j: func(ctx context.Context, uri, user, password string) (exportiface.Exporter, error) {
		return neo4jexport.New(ctx, uri, user, password)
	},
	NewJSONL: func(path string) (exportiface.Exporter, error) {
		return jsonl.NewStreamer(path)
	},
}

func Build(ctx context.Context, sinks []config.Sink, daemonGRPCAddr, daemonHTTPAddr string) ([]exportiface.Exporter, error) {
	return BuildWith(ctx, sinks, daemonGRPCAddr, daemonHTTPAddr, defaultBuilders)
}

func BuildWith(ctx context.Context, sinks []config.Sink, grpcAddr, httpAddr string, b Builders) ([]exportiface.Exporter, error) {
	var out []exportiface.Exporter
	for _, sk := range sinks {
		switch sk.Type {
		case config.SinkOTLP:
			if b.NewOTLP == nil {
				continue
			}
			exp, err := b.NewOTLP(ctx, sk.Endpoint, grpcAddr, httpAddr)
			if err != nil {
				log.Printf("catacomb: otlp sink disabled: %v", err)
				continue
			}
			if p, ok := exp.(interface{ SetProject(string) }); ok && sk.Project != "" {
				p.SetProject(sk.Project)
			}
			out = append(out, exp)
		case config.SinkPostgres:
			if b.NewPostgres == nil {
				continue
			}
			exp, err := b.NewPostgres(ctx, sk.DSN)
			if err != nil {
				log.Printf("catacomb: postgres sink disabled: %v", err)
				continue
			}
			out = append(out, exp)
		case config.SinkNeo4j:
			if b.NewNeo4j == nil {
				continue
			}
			exp, err := b.NewNeo4j(ctx, sk.URI, sk.User, sk.Password)
			if err != nil {
				log.Printf("catacomb: neo4j sink disabled: %v", err)
				continue
			}
			out = append(out, exp)
		case config.SinkJSONL:
			if b.NewJSONL == nil {
				continue
			}
			exp, err := b.NewJSONL(sk.Path)
			if err != nil {
				log.Printf("catacomb: jsonl sink disabled: %v", err)
				continue
			}
			out = append(out, exp)
		default:
			return nil, fmt.Errorf("export/build.Build: %w: %q", config.ErrUnknownSink, sk.Type)
		}
	}
	return out, nil
}
