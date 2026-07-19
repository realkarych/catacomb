package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptKind(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"<command-name>foo", "system"},
		{"<command-message>bar", "system"},
		{"<command-args>baz", "system"},
		{"<local-command-stdout>out", "system"},
		{"<local-command-stderr>err", "system"},
		{"<local-command-caveat>caveat", "system"},
		{"<task-notification>task", "system"},
		{"<system-reminder>reminder", "system"},
		{"<user-prompt-submit-hook>hook", "system"},
		{"Hello there", "human"},
		{"", "human"},
		{"  <system-reminder>with leading space", "system"},
		{"please read <system-reminder> in the middle", "human"},
		{"<system-reminder", "human"},
		{"<unknown-marker>foo", "human"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, PromptKind(c.text), "input: %q", c.text)
	}
}
