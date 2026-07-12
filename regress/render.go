package regress

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func RenderHuman(r Report, w io.Writer) {
	_, _ = fmt.Fprintf(w, "baseline runs %d  candidate runs %d\n", r.BaselineRuns, r.CandidateRuns)
	_, _ = fmt.Fprintf(w, "coverage steps %.2f  phases %.2f  steps_trusted %t  overall %s\n",
		r.Coverage.Steps, r.Coverage.Phases, r.StepsTrusted, r.OverallVerdict)
	if r.Sensitivity != nil {
		axes := []string{
			formatSensitivity("presence", r.Sensitivity.Presence),
			formatSensitivity("error_rate", r.Sensitivity.ErrorRate),
		}
		if r.Sensitivity.Annotation != nil {
			axes = append(axes, formatSensitivity("annotation", *r.Sensitivity.Annotation))
		}
		_, _ = fmt.Fprintf(w, "sensitivity: rate gate cannot fire at this support (%s)\n", strings.Join(axes, ", "))
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "VERDICT\tSCOPE\tKEY\tNAME\tMETRIC\tBASELINE\tCANDIDATE\tBAND\tDETAIL")
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Verdict, f.Scope, keyOrDash(f.Key), keyOrDash(f.Name), f.Metric,
			renderValue(f, f.Baseline), renderValue(f, f.Candidate), formatBand(f), keyOrDash(f.Detail))
	}
	_ = tw.Flush()
	if r.Reliability != nil {
		_, _ = fmt.Fprintln(w, formatReliability("baseline", r.Reliability.Baseline))
		_, _ = fmt.Fprintln(w, formatReliability("candidate", r.Reliability.Candidate))
	}
}

func formatReliability(group string, gr GroupReliability) string {
	head := fmt.Sprintf("reliability (%s): pass^1 %.2f", group, gr.Mean[0])
	if gr.KMax == 1 {
		return fmt.Sprintf("%s (%d tasks)", head, len(gr.Tasks))
	}
	return fmt.Sprintf("%s -> pass^%d %.2f (%d tasks)", head, gr.KMax, gr.Mean[gr.KMax-1], len(gr.Tasks))
}

func RenderJSON(r Report, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func formatSensitivity(name string, rs RateSensitivity) string {
	if rs.MinFullFlipRuns == 0 {
		return fmt.Sprintf("full flip unreachable %s", name)
	}
	return fmt.Sprintf("full flip needs k>=%d %s", rs.MinFullFlipRuns, name)
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

func renderValue(f Finding, v float64) string {
	if f.Metric == "presence" {
		return formatNum(1 - v)
	}
	return formatNum(v)
}

func formatBand(f Finding) string {
	if f.BandLo == 0 && f.BandHi == 0 {
		return "-"
	}
	lo, hi := f.BandLo, f.BandHi
	if f.Metric == "presence" {
		lo, hi = 1-f.BandHi, 1-f.BandLo
	}
	return fmt.Sprintf("[%s, %s]", formatNum(lo), formatNum(hi))
}
