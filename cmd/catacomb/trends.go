package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
)

var totalMetrics = []string{"duration_ms", "cost_usd", "tokens_in", "tokens_out", "nodes", "error_rate"}

type seqRecord struct {
	seq int
	rec regress.Record
}

type trendsJSONEntry struct {
	Seq    int            `json:"seq"`
	Record regress.Record `json:"record"`
}

func newTrendsCmd() *cobra.Command {
	var dbPath, metric string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "trends <baseline>",
		Short: "Show the recorded regression history for a baseline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrends(cmd.OutOrStdout(), store.OpenSQLiteReadOnly, dbPath, args[0], metric, asJSON)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", defaultBatchDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")
	cmd.Flags().StringVar(&metric, "metric", "", "restrict to a total-scope metric: "+strings.Join(totalMetrics, "|"))
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func runTrends(out io.Writer, open storeOpener, dbPath, name, metric string, asJSON bool) error {
	if metric != "" && !isTotalMetric(metric) {
		return operational(fmt.Errorf("trends: unknown --metric %q (want one of %s)", metric, strings.Join(totalMetrics, ", ")))
	}
	s, err := openReadStore(open, dbPath)
	if err != nil {
		return operational(err)
	}
	defer func() { _ = s.Close() }()

	_, ok, err := s.GetBaseline(name)
	if err != nil {
		if errors.Is(err, store.ErrSchemaOutdated) {
			return operational(store.ErrSchemaOutdated)
		}
		return operational(fmt.Errorf("trends get baseline %q: %w", name, err))
	}
	if !ok {
		return operational(fmt.Errorf("%w: %q", ErrBaselineNotFound, name))
	}

	results, err := s.RegressResultsFor(name)
	if err != nil {
		return operational(fmt.Errorf("trends: %w", err))
	}
	if len(results) == 0 {
		return operational(fmt.Errorf("trends: baseline %q has no recorded regress runs (record one: catacomb regress --record --baseline name:%s --candidate ...)", name, name))
	}
	records, err := decodeRecords(results)
	if err != nil {
		return operational(err)
	}

	if asJSON {
		return renderTrendsJSON(out, records)
	}
	if metric != "" {
		return renderTrendsMetric(out, records, metric)
	}
	return renderTrendsDefault(out, records)
}

func isTotalMetric(metric string) bool {
	for _, m := range totalMetrics {
		if m == metric {
			return true
		}
	}
	return false
}

func decodeRecords(results []model.RegressResult) ([]seqRecord, error) {
	out := make([]seqRecord, 0, len(results))
	for _, r := range results {
		var rec regress.Record
		if err := json.Unmarshal(r.Body, &rec); err != nil {
			return nil, fmt.Errorf("trends: malformed record body at seq %d: %w", r.Seq, err)
		}
		out = append(out, seqRecord{seq: r.Seq, rec: rec})
	}
	return out, nil
}

func renderTrendsJSON(out io.Writer, records []seqRecord) error {
	entries := make([]trendsJSONEntry, 0, len(records))
	for _, sr := range records {
		entries = append(entries, trendsJSONEntry{Seq: sr.seq, Record: sr.rec})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func renderTrendsDefault(out io.Writer, records []seqRecord) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tCREATED\tCANDIDATE\tVERDICT\tREGRESSIONS\tINSUFFICIENT\tDURATION_MS\tCOST_USD\tERROR_RATE")
	for _, sr := range records {
		rep := sr.rec.Report
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\n",
			sr.seq, sr.rec.CreatedAt.UTC().Format(time.RFC3339), sr.rec.CandidateSelector,
			rep.OverallVerdict, rep.Regressions, rep.Insufficient,
			totalMetricCandidate(rep, "duration_ms"),
			totalMetricCandidate(rep, "cost_usd"),
			totalMetricCandidate(rep, "error_rate"))
	}
	return w.Flush()
}

func renderTrendsMetric(out io.Writer, records []seqRecord, metric string) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tCREATED\tCANDIDATE\tVERDICT\tBASELINE-VALUE\tCANDIDATE-VALUE\tBAND")
	for _, sr := range records {
		baseVal, candVal, band := "-", "-", "-"
		if f, ok := totalFinding(sr.rec.Report, metric); ok {
			baseVal = formatTrendValue(f.Baseline)
			candVal = formatTrendValue(f.Candidate)
			band = formatTrendBand(f)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			sr.seq, sr.rec.CreatedAt.UTC().Format(time.RFC3339), sr.rec.CandidateSelector,
			sr.rec.Report.OverallVerdict, baseVal, candVal, band)
	}
	return w.Flush()
}

func totalFinding(rep regress.Report, metric string) (regress.Finding, bool) {
	for _, f := range rep.Findings {
		if f.Scope == "total" && f.Metric == metric {
			return f, true
		}
	}
	return regress.Finding{}, false
}

func totalMetricCandidate(rep regress.Report, metric string) string {
	if f, ok := totalFinding(rep, metric); ok {
		return formatTrendValue(f.Candidate)
	}
	return "-"
}

func formatTrendBand(f regress.Finding) string {
	if f.BandLo == 0 && f.BandHi == 0 {
		return "-"
	}
	return fmt.Sprintf("[%s, %s]", formatTrendValue(f.BandLo), formatTrendValue(f.BandHi))
}

func formatTrendValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
