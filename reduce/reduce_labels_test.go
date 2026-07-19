package reduce

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestEnsureRunMergesLabels(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	tests := []struct {
		name  string
		attrs []map[string]any
		want  map[string]string
	}{
		{
			name:  "single label attr populates run labels",
			attrs: []map[string]any{{"catacomb.labels": "basket=checkout,rep=1"}},
			want:  map[string]string{"basket": "checkout", "rep": "1"},
		},
		{
			name: "highest seq observation wins per key keeping others",
			attrs: []map[string]any{
				{"catacomb.labels": "basket=checkout,rep=1"},
				{"catacomb.labels": "rep=2"},
			},
			want: map[string]string{"basket": "checkout", "rep": "2"},
		},
		{
			name: "observation without attr leaves labels untouched",
			attrs: []map[string]any{
				{"catacomb.labels": "basket=checkout,rep=1"},
				{"model": "claude-opus-4-8"},
			},
			want: map[string]string{"basket": "checkout", "rep": "1"},
		},
		{
			name:  "non-string attr value ignored",
			attrs: []map[string]any{{"catacomb.labels": 42}},
			want:  nil,
		},
		{
			name:  "empty string attr ignored",
			attrs: []map[string]any{{"catacomb.labels": ""}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := make([]model.Observation, 0, len(tt.attrs))
			for i, a := range tt.attrs {
				o := ob("assistant_turn", "", t0.Add(time.Duration(i)*time.Second))
				o.Correlation.MessageID = "lbl" + strconv.Itoa(i)
				o.Attrs = a
				o.Seq = uint64(i) + 1
				obs = append(obs, o)
			}

			g := NewGraph()
			g.ApplyAll(obs)
			r := g.Runs[runID]
			require.NotNil(t, r)
			assert.Equal(t, tt.want, r.Labels)

			for i, p := range permute(obs) {
				pg := NewGraph()
				pg.ApplyAll(p)
				assert.Equal(t, tt.want, pg.Runs[runID].Labels,
					"label resolution must not depend on arrival order (permutation %d)", i)
			}
		})
	}
}
