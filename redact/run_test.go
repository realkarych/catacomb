package redact_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func secretRun() model.Run {
	return model.Run{
		ID:     "r1",
		Status: model.StatusRunning,
		Labels: map[string]string{
			"team": "core",
			"note": "postgres://runner:run_label_password@db.internal/labels",
		},
		Repro: &model.ReproMeta{
			Cwd:               "/deploy/AKIAIOSFODNN7EXAMPLE/build",
			ClaudeCodeVersion: "2.1.199",
		},
	}
}

func TestRunRedactsLabelValuesAndReproCwdSpanLevel(t *testing.T) {
	r := redact.Run(secretRun())
	assert.Equal(t, "‹redacted:connection-string›", r.Labels["note"])
	assert.Equal(t, "core", r.Labels["team"])
	assert.Equal(t, "/deploy/‹redacted:aws-key›/build", r.Repro.Cwd)
}

func TestRunPreservesReproVersionAndIdentity(t *testing.T) {
	r := redact.Run(secretRun())
	assert.Equal(t, "2.1.199", r.Repro.ClaudeCodeVersion)
	assert.Equal(t, "r1", r.ID)
	assert.Equal(t, model.StatusRunning, r.Status)
}

func TestRunDoesNotMutateInput(t *testing.T) {
	in := secretRun()
	_ = redact.Run(in)
	assert.Equal(t, "postgres://runner:run_label_password@db.internal/labels", in.Labels["note"])
	assert.Equal(t, "/deploy/AKIAIOSFODNN7EXAMPLE/build", in.Repro.Cwd)
}

func TestRunIdempotent(t *testing.T) {
	once := redact.Run(secretRun())
	twice := redact.Run(once)
	assert.Equal(t, once, twice)
}

func TestRunNilLabelsAndRepro(t *testing.T) {
	r := redact.Run(model.Run{ID: "r2"})
	assert.Nil(t, r.Labels)
	assert.Nil(t, r.Repro)
	require.Equal(t, "r2", r.ID)
}
