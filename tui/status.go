package tui

import "time"

const liveWindow = 5 * time.Minute

func isOutcomeStatus(s string) bool {
	switch s {
	case "ok", "error", "blocked":
		return true
	default:
		return false
	}
}

func sessionIsLive(s SessionSummary, now time.Time) bool {
	if s.Status != "running" || s.LastActivity == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, s.LastActivity)
	if err != nil {
		return false
	}
	return now.Sub(t) < liveWindow
}

func SessionDisplayStatus(s SessionSummary, now time.Time) string {
	if sessionIsLive(s, now) {
		return "live"
	}
	if isOutcomeStatus(s.Status) {
		return s.Status
	}
	return ""
}
