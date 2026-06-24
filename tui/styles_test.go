package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewStylesNoColorIsPlain(t *testing.T) {
	s := NewStyles(true)
	out := s.Selected.Render("x")
	assert.Equal(t, "x", out)
	assert.NotContains(t, out, "\x1b[")
	assert.Equal(t, "ab", s.Pane.Render("ab"))
	assert.Equal(t, "ab", s.Faint.Render("ab"))
	assert.Equal(t, "ab", s.StatusOK.Render("ab"))
	assert.Equal(t, "ab", s.StatusRunning.Render("ab"))
	assert.Equal(t, "ab", s.StatusError.Render("ab"))
	assert.Equal(t, "ab", s.Header.Render("ab"))
	assert.Equal(t, "ab", s.Bold.Render("ab"))
}

func TestNewStylesColored(t *testing.T) {
	s := NewStyles(false)
	out := s.Selected.Render("x")
	assert.Contains(t, out, "x")
	_ = strings.Contains(out, "\x1b[")
}

func TestStylesAllFieldsAccessible(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		s := NewStyles(noColor)
		assert.NotNil(t, s.Pane)
		assert.NotNil(t, s.Selected)
		assert.NotNil(t, s.Faint)
		assert.NotNil(t, s.StatusOK)
		assert.NotNil(t, s.StatusRunning)
		assert.NotNil(t, s.StatusError)
		assert.NotNil(t, s.Header)
		assert.NotNil(t, s.Bold)
	}
}
