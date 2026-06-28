package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsOutcomeStatus(t *testing.T) {
	assert.True(t, isOutcomeStatus("ok"))
	assert.True(t, isOutcomeStatus("error"))
	assert.True(t, isOutcomeStatus("blocked"))
	assert.False(t, isOutcomeStatus("running"))
	assert.False(t, isOutcomeStatus("pending"))
	assert.False(t, isOutcomeStatus(""))
}

func TestSessionIsLive(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute).Format(time.RFC3339)
	stale := now.Add(-10 * time.Minute).Format(time.RFC3339)

	assert.True(t, sessionIsLive(SessionSummary{Status: "running", LastActivity: fresh}, now))
	assert.False(t, sessionIsLive(SessionSummary{Status: "running", LastActivity: stale}, now))
	assert.False(t, sessionIsLive(SessionSummary{Status: "ok", LastActivity: fresh}, now))
	assert.False(t, sessionIsLive(SessionSummary{Status: "running", LastActivity: ""}, now))
	assert.False(t, sessionIsLive(SessionSummary{Status: "running", LastActivity: "not-a-time"}, now))
}

func TestSessionDisplayStatus(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute).Format(time.RFC3339)
	stale := now.Add(-10 * time.Minute).Format(time.RFC3339)

	assert.Equal(t, "live", SessionDisplayStatus(SessionSummary{Status: "running", LastActivity: fresh}, now))
	assert.Equal(t, "", SessionDisplayStatus(SessionSummary{Status: "running", LastActivity: stale}, now))
	assert.Equal(t, "", SessionDisplayStatus(SessionSummary{Status: "running"}, now))
	assert.Equal(t, "ok", SessionDisplayStatus(SessionSummary{Status: "ok"}, now))
	assert.Equal(t, "error", SessionDisplayStatus(SessionSummary{Status: "error"}, now))
	assert.Equal(t, "blocked", SessionDisplayStatus(SessionSummary{Status: "blocked"}, now))
	assert.Equal(t, "", SessionDisplayStatus(SessionSummary{Status: "pending"}, now))
	assert.Equal(t, "", SessionDisplayStatus(SessionSummary{Status: ""}, now))
}
