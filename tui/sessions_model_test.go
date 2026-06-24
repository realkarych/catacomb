package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sess(hash string) SessionSummary {
	return SessionSummary{Session: hash, Status: "ok", NodeCount: 3, TokensIn: 100, TokensOut: 50}
}

func TestSessionsCursorClampAndMove(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1"), sess("s2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 1, st.cursor)
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 1, st.cursor)
}

func TestSessionsEnterSelects(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1"), sess("s2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, chosen := st.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "s2", chosen)
}

func TestSessionsFilterNarrows(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "bet" {
		st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	v := st.view(NewStyles(true), 80)
	assert.Contains(t, v, "beta")
	assert.NotContains(t, v, "alpha")
}

func TestSessionsViewRendersColumnsAndDash(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1")})
	v := st.view(NewStyles(true), 80)
	assert.Contains(t, v, "s1")
	assert.Contains(t, v, "—")
	v2 := st.view(NewStyles(true), 120)
	assert.Contains(t, v2, "s1")
}

func TestSessionsEmpty(t *testing.T) {
	v := newSessionsState().view(NewStyles(true), 80)
	assert.NotEmpty(t, v)
	_, chosen := newSessionsState().update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "", chosen)
}

func TestSessionsMoveDown(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1"), sess("s2"), sess("s3")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, st.cursor)
}

func TestSessionsMoveUp(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1"), sess("s2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyDown})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, st.cursor)
}

func TestSessionsMoveUpClamp(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1"), sess("s2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, st.cursor)
}

func TestSessionsFilterBackspace(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "a", st.query)
}

func TestSessionsFilterEscExits(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	assert.True(t, st.filtering)
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, st.filtering)
}

func TestSessionsFilterEnterExits(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, st.filtering)
	v := st.view(NewStyles(true), 80)
	assert.Contains(t, v, "alpha")
}

func TestSessionsFilterNoMatchClampsCursor(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, st.cursor)
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, 0, st.cursor)
}

func TestSessionsRowRendersDash(t *testing.T) {
	noVal := SessionSummary{Session: "s1", Status: "ok"}
	st := newSessionsState().withSessions([]SessionSummary{noVal})
	v := st.view(NewStyles(true), 80)
	assert.Contains(t, v, "—")
}

func TestSessionsUnknownMsgNoOp(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1")})
	st2, chosen := st.update("random-msg")
	assert.Equal(t, "", chosen)
	assert.Equal(t, st.cursor, st2.cursor)
}

func TestSessionsWithSessionsSetsFiltered(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1"), sess("s2")})
	require.Len(t, st.filtered, 2)
}

func TestSessionsEnterWhileFiltering(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	st2, chosen := st.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, st2.filtering)
	assert.Equal(t, "", chosen)
}

func TestSessionsFilteringBackspaceClampsCursor(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("alpha"), sess("beta"), sess("alp2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyDown})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, 0, st.cursor)
}

func TestSessionsFilteringEscClampsCursorWhenFilteredEmpty(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, 0, st.cursor)
}

func TestSessionsViewQueryNoMatch(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("s1")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "zzz" {
		st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyEsc})
	v := st.view(NewStyles(true), 80)
	assert.Contains(t, v, "zzz")
}

func TestSessionsFilteringEnterClampsCursorWhenNarrows(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("hash1"), sess("hash2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, st.cursor)
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, 0, st.cursor)
}

func TestSessionsFilteringRunesClampsCursor(t *testing.T) {
	st := newSessionsState().withSessions([]SessionSummary{sess("hash1"), sess("hash2")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, st.cursor)
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	st, _ = st.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	assert.Equal(t, 0, st.cursor)
}
