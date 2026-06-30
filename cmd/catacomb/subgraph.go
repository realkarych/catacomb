package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/subgraph"
)

type subgraphArgs struct {
	input string
	phase string
	from  string
	to    string
	json  bool
}

func newSubgraphCmd() *cobra.Command {
	a := subgraphArgs{}
	cmd := &cobra.Command{
		Use:   "subgraph <session.jsonl>",
		Short: "Extract the execution subgraph of a checkpoint phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			a.input = positional[0]
			nodes, edges, err := runSubgraph(a)
			if err != nil {
				return err
			}
			if a.json {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"nodes": nodes, "edges": edges})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "nodes: %d  edges: %d\n", len(nodes), len(edges))
			for _, n := range nodes {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", n.Type, n.Name, n.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&a.phase, "phase", "", "phase name[,occurrence]")
	cmd.Flags().StringVar(&a.from, "from", "", "range start checkpoint name[,occurrence]")
	cmd.Flags().StringVar(&a.to, "to", "", "range end checkpoint name[,occurrence]")
	cmd.Flags().BoolVar(&a.json, "json", false, "output as JSON")
	return cmd
}

func runSubgraph(a subgraphArgs) ([]*model.Node, []*model.Edge, error) {
	exec := newExecutionID()
	g, _, err := loadGraph(a.input, exec)
	if err != nil {
		return nil, nil, fmt.Errorf("subgraph: %s: %w (%w)", a.input, err, ErrDiffInput)
	}
	nodes, edges := g.Snapshot()
	spec := subgraph.Spec{Phase: a.phase, From: a.from, To: a.to}
	if spec.Empty() {
		return nil, nil, fmt.Errorf("subgraph: %w: provide --phase or --from/--to", subgraph.ErrInvalidSelector)
	}
	parsed, err := subgraph.ParseSpec(spec)
	if err != nil {
		return nil, nil, err
	}
	sn, se, ok := subgraph.ScopeExecutionParsed(nodes, edges, exec, parsed)
	if !ok {
		return nil, nil, fmt.Errorf("subgraph: phase not found: %w", subgraph.ErrPhaseNotFound)
	}
	return sn, se, nil
}
