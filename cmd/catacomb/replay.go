package main

import (
	"fmt"
	"os"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	xjsonl "github.com/realkarych/catacomb/export/jsonl"
	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/reduce"
)

type replayArgs struct {
	input      string
	exportPath string
}

func newReplayCmd() *cobra.Command {
	args := replayArgs{}
	cmd := &cobra.Command{
		Use:   "replay <transcript.jsonl>",
		Short: "Build a graph from a recorded Claude Code transcript",
		Long: `Build an in-memory graph from a single recorded Claude Code transcript and
print a node/edge summary. Use --export-jsonl to also write the graph as a
JSONL snapshot.`,
		Example: `  catacomb replay ~/.claude/projects/<project>/<session>.jsonl`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			args.input = positional[0]
			g, err := runReplay(args)
			if err != nil {
				return operational(err)
			}
			cmd.Printf("replayed %s -> %d nodes, %d edges\n", args.input, len(g.Nodes), len(g.Edges))
			return nil
		},
	}
	cmd.Flags().StringVar(&args.exportPath, "export-jsonl", "", "also write a JSONL graph snapshot")
	return cmd
}

func newExecutionID() string { return ulid.Make().String() }

func runReplay(args replayArgs) (*reduce.Graph, error) {
	g, err := loadGraph(args.input, newExecutionID())
	if err != nil {
		return nil, err
	}
	if args.exportPath != "" {
		if err := export(args.exportPath, g); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func loadGraph(path, executionID string) (*reduce.Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	obs, err := ijsonl.ParseReader(f, executionID)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	policy := redact.DefaultPolicy()
	for i := range obs {
		obs[i] = policy.Observation(obs[i])
	}

	g := reduce.NewGraph()
	g.ApplyAll(obs)
	return g, nil
}

func export(path string, g *reduce.Graph) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("replay export: %w", err)
	}
	defer func() { _ = out.Close() }()

	nodes, edges := g.Snapshot()
	runs := g.RunsSnapshot()
	return xjsonl.Snapshot(out, nodes, edges, runs)
}
