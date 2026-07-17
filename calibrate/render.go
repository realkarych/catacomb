package calibrate

import (
	"encoding/json"
	"fmt"
	"io"
)

func RenderHuman(r CalibrateReport, w io.Writer) {
	_, _ = fmt.Fprintf(w, "self-check: %s · runs %d · min-support %d\n", sufficiencyWord(r.Sufficient), r.Runs, r.MinSupport)
	if !r.Sufficient {
		_, _ = fmt.Fprintln(w, r.Detail)
		return
	}
	_, _ = fmt.Fprintf(w, "A/A %s (first %d vs second %d)\n", r.Split.Verdict, r.Split.FirstN, r.Split.SecondN)
	for _, d := range r.Split.Drift {
		_, _ = fmt.Fprintf(w, "drift: %s %s %s %.2f -> %.2f\n", d.Scope, d.Metric, d.Verdict, d.Baseline, d.Candidate)
	}
	renderInfluence(*r.Influence, w)
}

func renderInfluence(inf InfluenceResult, w io.Writer) {
	if !inf.Evaluated {
		_, _ = fmt.Fprintf(w, "influence: %s\n", inf.Detail)
		return
	}
	if len(inf.FlippingRuns) == 0 {
		_, _ = fmt.Fprintln(w, "influence: no single run flips the verdict")
		return
	}
	for _, flip := range inf.FlippingRuns {
		_, _ = fmt.Fprintf(w, "influence: dropping run #%d flips %s -> %s\n", flip.DroppedIndex, flip.From, flip.To)
	}
}

func RenderJSON(r CalibrateReport, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func sufficiencyWord(sufficient bool) string {
	if sufficient {
		return "sufficient"
	}
	return "insufficient"
}
