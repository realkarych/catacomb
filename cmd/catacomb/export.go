package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/evidence"
	xjsonl "github.com/realkarych/catacomb/export/jsonl"
	"github.com/realkarych/catacomb/model"
)

type exportArgs struct {
	input string
	to    string
	out   string
}

func newExportCmd() *cobra.Command {
	var a exportArgs
	cmd := &cobra.Command{
		Use:   "export <transcript.jsonl | evidence-dir>",
		Short: "Export a transcript or evidence dir as a JSONL graph snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			a.input = positional[0]
			return runExport(cmd.OutOrStdout(), a)
		},
	}
	cmd.Flags().StringVar(&a.to, "to", "jsonl", "export format: jsonl")
	cmd.Flags().StringVar(&a.out, "out", "", "write to file instead of stdout")
	return cmd
}

func runExport(out io.Writer, a exportArgs) error {
	if a.to != "jsonl" {
		return ErrUnknownSink
	}
	nodes, edges, runs, err := loadExportInput(a.input)
	if err != nil {
		return err
	}
	w := out
	if a.out != "" {
		f, cerr := os.Create(a.out)
		if cerr != nil {
			return fmt.Errorf("export create: %w", cerr)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	return xjsonl.Snapshot(w, nodes, edges, runs)
}

func loadExportInput(input string) ([]*model.Node, []*model.Edge, []model.Run, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("export input: %w", err)
	}
	if info.IsDir() {
		return loadExportDir(input)
	}
	g, err := loadGraphOffline(input, nil, newExecutionID(), newPricer(), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	nodes, edges := sortedGraphSnapshot(g)
	runs := g.RunsSnapshot()
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return nodes, edges, runs, nil
}

func loadExportDir(dir string) ([]*model.Node, []*model.Edge, []model.Run, error) {
	m, err := evidence.ReadMeta(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	rg, err := evidenceRunGraph(dir, m, newPricer())
	if err != nil {
		return nil, nil, nil, err
	}
	return rg.Nodes, rg.Edges, []model.Run{rg.Run}, nil
}
