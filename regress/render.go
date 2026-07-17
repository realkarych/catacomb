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
	if axes := unreachableAxes(r.Sensitivity); len(axes) > 0 {
		_, _ = fmt.Fprintf(w, "sensitivity: gate cannot fire at this support (%s)\n", strings.Join(axes, ", "))
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "VERDICT\tSCOPE\tKEY\tNAME\tMETRIC\tBASELINE\tCANDIDATE\tBAND\tDETAIL")
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.Verdict, f.Scope, keyOrDash(f.Key), keyOrDash(f.Name), f.Metric,
			renderValue(f, f.Baseline), renderValue(f, f.Candidate), formatBand(f), keyOrDash(f.Detail))
	}
	_ = tw.Flush()
	for _, line := range reliabilityAuditLines(r) {
		_, _ = fmt.Fprintln(w, line)
	}
}

func RenderMarkdown(r Report, w io.Writer) {
	_, _ = fmt.Fprintf(w, "**Verdict: %s %s**\n\n", verdictEmoji(r.OverallVerdict), r.OverallVerdict)
	_, _ = fmt.Fprintf(w, "baseline %d runs · candidate %d runs · coverage steps %s phases %s\n\n",
		r.BaselineRuns, r.CandidateRuns, formatNum(r.Coverage.Steps), formatNum(r.Coverage.Phases))
	if axes := unreachableAxes(r.Sensitivity); len(axes) > 0 {
		_, _ = fmt.Fprintf(w, "> ⚠️ gate cannot fire at this support: %s\n\n", strings.Join(axes, ", "))
	}
	_, _ = fmt.Fprintln(w, "| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |")
	_, _ = fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			f.Verdict, f.Scope, escapePipe(keyOrDash(f.Key)), escapePipe(keyOrDash(f.Name)), escapePipe(f.Metric),
			renderValue(f, f.Baseline), renderValue(f, f.Candidate), formatBand(f), escapePipe(keyOrDash(f.Detail)))
	}
	if r.Reliability == nil && r.Audit == nil {
		return
	}
	_, _ = fmt.Fprint(w, "\n<details>\n<summary>reliability &amp; audit</summary>\n\n")
	for _, line := range reliabilityAuditLines(r) {
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprint(w, "\n</details>\n")
}

func verdictEmoji(v Verdict) string {
	switch v {
	case VerdictRegression:
		return "❌"
	case VerdictOK, VerdictImprovement:
		return "✅"
	default:
		return "⚠️"
	}
}

func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func unreachableAxes(s *Sensitivity) []string {
	if s == nil {
		return nil
	}
	var axes []string
	if !s.Presence.Reachable {
		axes = append(axes, formatSensitivity("presence", s.Presence))
	}
	if !s.ErrorRate.Reachable {
		axes = append(axes, formatSensitivity("error_rate", s.ErrorRate))
	}
	if s.Annotation != nil && !s.Annotation.Reachable {
		axes = append(axes, formatSensitivity("annotation", *s.Annotation))
	}
	if s.Paired != nil && !s.Paired.Reachable {
		axes = append(axes, formatPairedSensitivity(*s.Paired))
	}
	return axes
}

func reliabilityAuditLines(r Report) []string {
	var lines []string
	if r.Reliability != nil {
		lines = append(lines,
			formatReliability("baseline", r.Reliability.Baseline),
			formatReliability("candidate", r.Reliability.Candidate))
	}
	if r.Audit != nil {
		for _, f := range r.Audit.Baseline {
			lines = append(lines, formatCellFlag("baseline", f))
		}
		for _, f := range r.Audit.Candidate {
			lines = append(lines, formatCellFlag("candidate", f))
		}
	}
	return lines
}

func formatCellFlag(group string, f CellFlag) string {
	task := ""
	if f.Task != "" {
		task = fmt.Sprintf(" (task %s)", f.Task)
	}
	return fmt.Sprintf("audit: %s run %s%s %s %g vs group median %g (band %g)",
		group, f.RunID, task, f.Metric, f.Value, f.Median, f.Band)
}

func formatReliability(group string, gr GroupReliability) string {
	head := fmt.Sprintf("reliability (%s): pass^1 %.2f", group, gr.Mean[0])
	if gr.KMax == 1 {
		return fmt.Sprintf("%s (%d %s)", head, len(gr.Tasks), taskWord(len(gr.Tasks)))
	}
	return fmt.Sprintf("%s -> pass^%d %.2f (%d %s)", head, gr.KMax, gr.Mean[gr.KMax-1], len(gr.Tasks), taskWord(len(gr.Tasks)))
}

func taskWord(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
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

func formatPairedSensitivity(ps PairedSensitivity) string {
	return fmt.Sprintf("paired gate needs k>=%d tasks", ps.MinTasks)
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
