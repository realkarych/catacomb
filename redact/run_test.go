package redact_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func secretRun() model.Run {
	started := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	ended := started.Add(2 * time.Minute)
	return model.Run{
		ID:         "r1",
		SessionIDs: []string{"sess-a", "sess-b"},
		ModelID:    "claude-opus-4-8",
		Status:     model.StatusRunning,
		LastSeq:    77,
		StartedAt:  &started,
		EndedAt:    &ended,
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

func TestRunCarriesEveryNonSecretFieldThroughUnchanged(t *testing.T) {
	in := secretRun()
	r := redact.Run(in)

	assert.Equal(t, in.ID, r.ID)
	assert.Equal(t, in.SessionIDs, r.SessionIDs)
	assert.Equal(t, in.ModelID, r.ModelID)
	assert.Equal(t, in.Status, r.Status)
	assert.Equal(t, in.LastSeq, r.LastSeq)
	assert.Equal(t, in.StartedAt, r.StartedAt)
	assert.Equal(t, in.EndedAt, r.EndedAt)
	assert.Equal(t, in.Repro.ClaudeCodeVersion, r.Repro.ClaudeCodeVersion)
	assert.Equal(t, in.Labels["team"], r.Labels["team"])
	assert.Len(t, r.Labels, len(in.Labels), "label count must be invariant across redaction")
}

func TestRunRedactsNothingButLabelValuesAndCwd(t *testing.T) {
	in := secretRun()
	want := in
	want.Labels = map[string]string{"team": "core", "note": "‹redacted:connection-string›"}
	want.Repro = &model.ReproMeta{Cwd: "/deploy/‹redacted:aws-key›/build", ClaudeCodeVersion: "2.1.199"}

	assert.Equal(t, want, redact.Run(in),
		"redact.Run must be the identity on every field except label values and Repro.Cwd")
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
