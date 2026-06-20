package main

import (
	"fmt"
	"os"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	xjsonl "github.com/realkarych/catacomb/export/jsonl"
	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

type replayArgs struct {
	input      string
	dbPath     string
	exportPath string
}

type storeOpener func(path string) (store.Store, error)

func newReplayCmd() *cobra.Command {
	args := replayArgs{}
	cmd := &cobra.Command{
		Use:   "replay <transcript.jsonl>",
		Short: "Build a graph from a recorded Claude Code transcript",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			args.input = positional[0]
			g, err := runReplay(args)
			if err != nil {
				return err
			}
			cmd.Printf("replayed %s -> %d nodes, %d edges\n", args.input, len(g.Nodes), len(g.Edges))
			return nil
		},
	}
	cmd.Flags().StringVar(&args.dbPath, "db", "catacomb.db", "SQLite database path")
	cmd.Flags().StringVar(&args.exportPath, "export-jsonl", "", "also write a JSONL graph snapshot")
	return cmd
}

func newExecutionID() string { return ulid.Make().String() }

func runReplay(args replayArgs) (*reduce.Graph, error) {
	return runReplayWith(store.OpenSQLite, newExecutionID, args)
}

func runReplayWith(open storeOpener, newExecID func() string, args replayArgs) (*reduce.Graph, error) {
	executionID := newExecID()

	f, err := os.Open(args.input)
	if err != nil {
		return nil, fmt.Errorf("replay open: %w", err)
	}
	defer func() { _ = f.Close() }()

	obs, err := ijsonl.ParseReader(f, executionID)
	if err != nil {
		return nil, fmt.Errorf("replay parse: %w", err)
	}

	g := reduce.NewGraph()
	g.ApplyAll(obs)

	if err := persist(open, args.dbPath, obs, g); err != nil {
		return nil, err
	}
	if args.exportPath != "" {
		if err := export(args.exportPath, g); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func graphSlices(g *reduce.Graph) ([]*model.Node, []*model.Edge) {
	nodes := make([]*model.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, n)
	}
	edges := make([]*model.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, e)
	}
	return nodes, edges
}

func persist(open storeOpener, dbPath string, obs []model.Observation, g *reduce.Graph) error {
	s, err := open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	nodes, edges := graphSlices(g)
	return s.Persist(obs, nodes, edges)
}

func export(path string, g *reduce.Graph) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("replay export: %w", err)
	}
	defer func() { _ = out.Close() }()

	nodes, edges := graphSlices(g)
	return xjsonl.Snapshot(out, nodes, edges)
}
