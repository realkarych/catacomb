package regress

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
)

func sampleReport() Report {
	totals := func(cost float64) aggregate.RunTotals {
		return aggregate.RunTotals{
			DurationMS: metric(5, 1000, 900, 1100),
			CostUSD:    metric(5, cost, cost*0.9, cost*1.1),
			TokensIn:   metric(5, 2000, 1900, 2100),
			TokensOut:  metric(5, 800, 750, 850),
			Nodes:      metric(5, 12, 11, 13),
		}
	}

	paBase := presentRow("pa", "alpha", 5)
	paCand := presentRow("pa", "alpha", 5)
	paCand.StatusRates = map[model.Status]float64{model.StatusError: 0.6}
	paCand.DurationMS = metric(5, 600, 500, 700)

	s1Base := presentRow("s1", "step-one", 5)
	s1Cand := presentRow("s1", "step-one", 5)
	s1Cand.DurationMS = metric(5, 1600, 1500, 1700)

	in := Input{
		Baseline: aggregate.Report{
			Runs:   5,
			Totals: totals(0.10),
			Phases: []aggregate.Row{paBase, presentRow("pb", "beta", 5)},
			Steps:  []aggregate.Row{s1Base, presentRow("s2", "step-two", 5)},
		},
		Candidate: aggregate.Report{
			Runs:   5,
			Totals: totals(0.20),
			Phases: []aggregate.Row{paCand, presentRow("pd", "delta", 5)},
			Steps:  []aggregate.Row{s1Cand},
		},
	}
	return Compare(in, DefaultThresholds())
}

func TestRenderHumanGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderHuman(sampleReport(), &buf)
	golden, err := os.ReadFile("testdata/golden_report.txt")
	require.NoError(t, err)
	assert.Equal(t, string(golden), buf.String())
}

func TestRenderJSONGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(sampleReport(), &buf))
	golden, err := os.ReadFile("testdata/golden_report.json")
	require.NoError(t, err)
	assert.Equal(t, string(golden), buf.String())
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRenderJSONError(t *testing.T) {
	t.Parallel()
	err := RenderJSON(sampleReport(), errWriter{})
	require.Error(t, err)
}
