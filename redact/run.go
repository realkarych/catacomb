package redact

import (
	"github.com/realkarych/catacomb/model"
)

func Run(r model.Run) model.Run {
	if r.Labels != nil {
		labels := make(map[string]string, len(r.Labels))
		for k, v := range r.Labels {
			labels[k] = redactString(v)
		}
		r.Labels = labels
	}
	if r.Repro != nil {
		repro := *r.Repro
		repro.Cwd = redactString(repro.Cwd)
		r.Repro = &repro
	}
	return r
}
