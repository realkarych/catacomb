package regress

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

func RenderHuman(r Report, w io.Writer) {
	_, _ = fmt.Fprintf(w, "baseline runs %d  candidate runs %d\n", r.BaselineRuns, r.CandidateRuns)
	_, _ = fmt.Fprintf(w, "coverage steps %.2f  phases %.2f  steps_trusted %t  overall %s\n",
		r.Coverage.Steps, r.Coverage.Phases, r.StepsTrusted, r.OverallVerdict)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "VERDICT\tSCOPE\tKEY\tMETRIC\tBASELINE\tCANDIDATE\tBAND")
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Verdict, f.Scope, keyOrDash(f.Key), f.Metric,
			formatNum(f.Baseline), formatNum(f.Candidate), formatBand(f))
	}
	_ = tw.Flush()
}

func RenderJSON(r Report, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func keyOrDash(key string) string {
	if key == "" {
		return "-"
	}
	return key
}

func formatNum(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func formatBand(f Finding) string {
	if f.BandLo == 0 && f.BandHi == 0 {
		return "-"
	}
	return fmt.Sprintf("[%s, %s]", formatNum(f.BandLo), formatNum(f.BandHi))
}
