package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/realkarych/catacomb/regress"
)

const (
	paretoSourceBaseline = "baseline"
	paretoSourceRecord   = "record"
	paretoAccuracyMetric = "ann:" + regress.VerifierOutcomeKey
	paretoCostMetric     = "cost_usd"
)

type paretoPoint struct {
	Source    string    `json:"source"`
	Seq       int       `json:"seq,omitempty"`
	Candidate string    `json:"candidate,omitempty"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	Accuracy  *float64  `json:"accuracy,omitempty"`
	CostUSD   *float64  `json:"cost_usd,omitempty"`
	Dominated *bool     `json:"dominated,omitempty"`
	Spliced   *bool     `json:"spliced,omitempty"`
}

type paretoReport struct {
	Baseline string        `json:"baseline"`
	Points   []paretoPoint `json:"points"`
}

func (p paretoPoint) comparable() bool {
	return p.Accuracy != nil && p.CostUSD != nil
}

func buildParetoReport(name string, records []seqRecord, current time.Time) paretoReport {
	points := make([]paretoPoint, 0, len(records)+1)
	if len(records) > 0 {
		newest := records[len(records)-1].rec.Report
		points = append(points, paretoPoint{
			Source:   paretoSourceBaseline,
			Accuracy: baselineAxis(newest, paretoAccuracyMetric),
			CostUSD:  baselineAxis(newest, paretoCostMetric),
		})
	}
	for _, sr := range records {
		spliced := !sr.rec.BaselineCreatedAt.Equal(current)
		points = append(points, paretoPoint{
			Source:    paretoSourceRecord,
			Seq:       sr.seq,
			Candidate: sr.rec.CandidateSelector,
			CreatedAt: sr.rec.CreatedAt.UTC(),
			Accuracy:  candidateAxis(sr.rec.Report, paretoAccuracyMetric),
			CostUSD:   candidateAxis(sr.rec.Report, paretoCostMetric),
			Spliced:   &spliced,
		})
	}
	markParetoDominated(points)
	sortParetoPoints(points)
	return paretoReport{Baseline: name, Points: points}
}

func baselineAxis(rep regress.Report, metric string) *float64 {
	if f, ok := totalFinding(rep, metric); ok {
		return &f.Baseline
	}
	return nil
}

func candidateAxis(rep regress.Report, metric string) *float64 {
	if f, ok := totalFinding(rep, metric); ok {
		return &f.Candidate
	}
	return nil
}

func markParetoDominated(points []paretoPoint) {
	for i := range points {
		if !points[i].comparable() {
			continue
		}
		dominated := false
		for j := range points {
			if j != i && paretoDominates(points[j], points[i]) {
				dominated = true
				break
			}
		}
		points[i].Dominated = &dominated
	}
}

func paretoDominates(b, a paretoPoint) bool {
	if !b.comparable() {
		return false
	}
	return *b.Accuracy >= *a.Accuracy && *b.CostUSD <= *a.CostUSD &&
		(*b.Accuracy > *a.Accuracy || *b.CostUSD < *a.CostUSD)
}

func sortParetoPoints(points []paretoPoint) {
	sort.Slice(points, func(i, j int) bool { return paretoLess(points[i], points[j]) })
}

func paretoLess(a, b paretoPoint) bool {
	ac, bc := a.comparable(), b.comparable()
	if ac != bc {
		return ac
	}
	if !ac {
		return a.Seq < b.Seq
	}
	if *a.CostUSD != *b.CostUSD {
		return *a.CostUSD < *b.CostUSD
	}
	if *a.Accuracy != *b.Accuracy {
		return *a.Accuracy > *b.Accuracy
	}
	return a.Seq < b.Seq
}

func renderParetoTable(out io.Writer, rep paretoReport) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tCREATED\tCANDIDATE\tACCURACY\tCOST_USD\tDOMINATED")
	spliced := false
	uncompared := 0
	for _, p := range rep.Points {
		if p.Dominated == nil {
			uncompared++
		}
		spliced = spliced || (p.Spliced != nil && *p.Spliced)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			paretoSeqCell(p), paretoCreatedCell(p), paretoCandidateCell(p),
			paretoAccuracyCell(p), paretoCostCell(p), paretoDominatedCell(p))
	}
	if spliced {
		fmt.Fprintln(w, spliceFootnote)
	}
	if uncompared > 0 {
		fmt.Fprintf(w, "pareto: %d row(s) lack an accuracy axis (no %s finding) and are not compared\n", uncompared, paretoAccuracyMetric)
	}
	return w.Flush()
}

func paretoSeqCell(p paretoPoint) string {
	if p.Source == paretoSourceBaseline {
		return "-"
	}
	if *p.Spliced {
		return fmt.Sprintf("%d *", p.Seq)
	}
	return fmt.Sprintf("%d", p.Seq)
}

func paretoCreatedCell(p paretoPoint) string {
	if p.Source == paretoSourceBaseline {
		return "-"
	}
	return p.CreatedAt.Format(time.RFC3339)
}

func paretoCandidateCell(p paretoPoint) string {
	if p.Source == paretoSourceBaseline {
		return paretoSourceBaseline
	}
	return p.Candidate
}

func paretoAccuracyCell(p paretoPoint) string {
	if p.Accuracy == nil {
		return "-"
	}
	return formatTrendValue(*p.Accuracy)
}

func paretoCostCell(p paretoPoint) string {
	if p.CostUSD == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", *p.CostUSD)
}

func paretoDominatedCell(p paretoPoint) string {
	if p.Dominated == nil {
		return "-"
	}
	if *p.Dominated {
		return "yes"
	}
	return "no"
}

func renderParetoJSON(out io.Writer, rep paretoReport) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}
