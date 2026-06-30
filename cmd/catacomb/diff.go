package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	catdiff "github.com/realkarych/catacomb/diff"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/subgraph"
)

type diffArgs struct {
	a      string
	b      string
	json   bool
	phase  string
	aPhase string
	bPhase string
}

func (a diffArgs) aSel() string {
	if a.aPhase != "" {
		return a.aPhase
	}
	return a.phase
}

func (a diffArgs) bSel() string {
	if a.bPhase != "" {
		return a.bPhase
	}
	return a.phase
}

func scopeCLISide(nodes []*model.Node, edges []*model.Edge, execID, sel string) ([]*model.Node, []*model.Edge, error) {
	if sel == "" {
		return nodes, edges, nil
	}
	name, occ, err := subgraph.ParseSelector(sel)
	if err != nil {
		return nil, nil, err
	}
	sn, se, ok := subgraph.ScopeExecution(nodes, edges, execID, name, occ)
	if !ok {
		return nil, nil, fmt.Errorf("diff: phase %q not found", sel)
	}
	return sn, se, nil
}

func newDiffCmd() *cobra.Command {
	args := diffArgs{}
	cmd := &cobra.Command{
		Use:   "diff <A.jsonl> <B.jsonl>",
		Short: "Diff two session transcripts by step_key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, positional []string) error {
			args.a = positional[0]
			args.b = positional[1]
			result, err := runDiff(args)
			if err != nil {
				return err
			}
			if args.json {
				return writeDiffJSON(cmd, result)
			}
			renderDiff(cmd, result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&args.json, "json", false, "output as JSON")
	cmd.Flags().StringVar(&args.phase, "phase", "", "scope both sides to phase name[,occurrence]")
	cmd.Flags().StringVar(&args.aPhase, "a-phase", "", "scope side A to phase name[,occurrence]")
	cmd.Flags().StringVar(&args.bPhase, "b-phase", "", "scope side B to phase name[,occurrence]")
	return cmd
}

func runDiff(args diffArgs) (catdiff.DiffResult, error) {
	aExec := newExecutionID()
	ag, _, err := loadGraph(args.a, aExec)
	if err != nil {
		return catdiff.DiffResult{}, fmt.Errorf("diff: %s: %w (%w)", args.a, err, ErrDiffInput)
	}
	bExec := newExecutionID()
	bg, _, err := loadGraph(args.b, bExec)
	if err != nil {
		return catdiff.DiffResult{}, fmt.Errorf("diff: %s: %w (%w)", args.b, err, ErrDiffInput)
	}
	an, ae := ag.Snapshot()
	bn, be := bg.Snapshot()
	an, ae, err = scopeCLISide(an, ae, aExec, args.aSel())
	if err != nil {
		return catdiff.DiffResult{}, err
	}
	bn, be, err = scopeCLISide(bn, be, bExec, args.bSel())
	if err != nil {
		return catdiff.DiffResult{}, err
	}
	return catdiff.DiffGraphs(an, ae, bn, be), nil
}

func writeDiffJSON(cmd *cobra.Command, result catdiff.DiffResult) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func renderDiff(cmd *cobra.Command, result catdiff.DiffResult) {
	fmt.Fprintf(cmd.OutOrStdout(), "unchanged: %d  changed: %d  added: %d  removed: %d\n",
		len(result.Unchanged), len(result.Changed), len(result.Added), len(result.Removed))
	for _, s := range result.Added {
		fmt.Fprintf(cmd.OutOrStdout(), "+ %s %s\n", s.Type, s.Tool)
	}
	for _, s := range result.Removed {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s %s\n", s.Type, s.Tool)
	}
	for _, c := range result.Changed {
		fmt.Fprintf(cmd.OutOrStdout(), "~ %s %s %s\n", c.Type, c.Tool, summarizeDeltas(c.Deltas))
	}
}

func summarizeDeltas(d catdiff.Deltas) string {
	var parts []string
	if d.Args != nil {
		parts = append(parts, "args")
	}
	if d.Status != nil {
		parts = append(parts, "status")
	}
	if d.CostUSD != nil {
		parts = append(parts, "cost")
	}
	if d.DurationMS != nil {
		parts = append(parts, "duration")
	}
	if d.TokensIn != nil {
		parts = append(parts, "tokens_in")
	}
	if d.TokensOut != nil {
		parts = append(parts, "tokens_out")
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "," + p
	}
	return result
}
