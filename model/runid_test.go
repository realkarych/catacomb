package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidRunID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"bench-dogfood-fizzbuzz-normal-r1", true},
		{"0193f8a1-7c2e-7f3a-bb11-9d2e4c5a6b7f", true},
		{"sprint_42.rev-3", true},
		{"has space", false},
		{"has/slash", false},
		{"has\ttab", false},
		{"has\nnewline", false},
		{"unicøde", false},
		{strings.Repeat("a", 256), true},
		{strings.Repeat("a", 257), false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, ValidRunID(tc.in), "%q", tc.in)
	}
}
