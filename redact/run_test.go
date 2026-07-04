package redact_test

import (
	"strings"
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
		Meta: map[string]any{"lossy": true},
		Repro: &model.ReproMeta{
			Cwd:         "/deploy/AKIAIOSFODNN7EXAMPLE/build",
			PromptsHash: strings.Repeat("ab", 32),
			SkillsHash:  strings.Repeat("cd", 32),
		},
	}
}

func TestRunRedactsLabelValuesAndReproCwdSpanLevel(t *testing.T) {
	r := redact.Run(secretRun())
	assert.Equal(t, "‹redacted:connection-string›", r.Labels["note"])
	assert.Equal(t, "core", r.Labels["team"])
	assert.Equal(t, "/deploy/‹redacted:aws-key›/build", r.Repro.Cwd)
}

func TestRunPreservesReproHashesAndIdentity(t *testing.T) {
	r := redact.Run(secretRun())
	assert.Equal(t, strings.Repeat("ab", 32), r.Repro.PromptsHash)
	assert.Equal(t, strings.Repeat("cd", 32), r.Repro.SkillsHash)
	assert.Equal(t, "r1", r.ID)
	assert.Equal(t, model.StatusRunning, r.Status)
	assert.Equal(t, map[string]any{"lossy": true}, r.Meta)
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
