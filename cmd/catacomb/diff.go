package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	catdiff "github.com/realkarych/catacomb/diff"
)

type diffArgs struct {
	a    string
	b    string
	json bool
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
	return cmd
}

func runDiff(args diffArgs) (catdiff.DiffResult, error) {
	ag, _, err := loadGraph(args.a, newExecutionID())
	if err != nil {
		return catdiff.DiffResult{}, fmt.Errorf("diff: %s: %w (%w)", args.a, err, ErrDiffInput)
	}
	bg, _, err := loadGraph(args.b, newExecutionID())
	if err != nil {
		return catdiff.DiffResult{}, fmt.Errorf("diff: %s: %w (%w)", args.b, err, ErrDiffInput)
	}
	an, ae := ag.Snapshot()
	bn, be := bg.Snapshot()
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
