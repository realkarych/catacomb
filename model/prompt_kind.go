package model

import "strings"

var syntheticMarkers = []string{
	"<command-name>",
	"<command-message>",
	"<command-args>",
	"<local-command-stdout>",
	"<local-command-stderr>",
	"<local-command-caveat>",
	"<task-notification>",
	"<system-reminder>",
	"<user-prompt-submit-hook>",
}

func PromptKind(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, m := range syntheticMarkers {
		if strings.HasPrefix(trimmed, m) {
			return "system"
		}
	}
	return "human"
}
