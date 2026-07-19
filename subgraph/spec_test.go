package subgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestSpecEmpty(t *testing.T) {
	assert.True(t, Spec{}.Empty())
	assert.False(t, Spec{Phase: "p"}.Empty())
	assert.False(t, Spec{From: "a", To: "b"}.Empty())
}

func TestParseSpecValidation(t *testing.T) {
	_, err := ParseSpec(Spec{From: "a"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{To: "b"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{Phase: "p", From: "a", To: "b"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{Phase: "p,x"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{From: "a,x", To: "b"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{From: "a", To: "b,x"})
	assert.ErrorIs(t, err, ErrInvalidSelector)
}

func TestRangeWindow(t *testing.T) {
	exec := "E"
	fromID := model.PhaseMarkerID(exec, "plan", 0)
	toID := model.PhaseMarkerID(exec, "impl", 0)
	nodes := []*model.Node{
		{ID: fromID, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(150)},
		{ID: toID, Type: model.NodeMarker, TStart: ts(300), TEnd: ts(400)},
	}

	w, ok := RangeWindow(nodes, exec, "plan", 0, "impl", 0)
	require.True(t, ok)
	assert.Equal(t, *ts(100), w.Start)
	require.NotNil(t, w.End)
	assert.Equal(t, *ts(300), *w.End,
		"a range ends where the to-phase begins, never where the from-phase ends")

	_, ok = RangeWindow(nodes, exec, "plan", 0, "missing", 0)
	assert.False(t, ok)
	_, ok = RangeWindow(nodes, exec, "missing", 0, "impl", 0)
	assert.False(t, ok)
}

func TestScopeExecutionParsedPhaseAndRange(t *testing.T) {
	exec := "E"
	plan := model.PhaseMarkerID(exec, "plan", 0)
	impl := model.PhaseMarkerID(exec, "impl", 0)
	nodes := []*model.Node{
		{ID: plan, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: impl, Type: model.NodeMarker, TStart: ts(300), TEnd: ts(400)},
		{ID: "t-in", Type: model.NodeToolCall, TStart: ts(150)},
		{ID: "t-mid", Type: model.NodeToolCall, TStart: ts(250)},
		{ID: "t-out", Type: model.NodeToolCall, TStart: ts(900)},
	}
	edges := []*model.Edge{}

	pPhase, err := ParseSpec(Spec{Phase: "plan"})
	require.NoError(t, err)
	sn, _, ok := ScopeExecutionParsed(nodes, edges, exec, pPhase)
	require.True(t, ok)
	assert.Equal(t, []string{"t-in"}, ids(sn))

	pRange, err := ParseSpec(Spec{From: "plan", To: "impl"})
	require.NoError(t, err)
	sn, _, ok = ScopeExecutionParsed(nodes, edges, exec, pRange)
	require.True(t, ok)
	assert.Equal(t, []string{"t-in", "t-mid"}, ids(sn))

	pMissing, err := ParseSpec(Spec{Phase: "ghost"})
	require.NoError(t, err)
	_, _, ok = ScopeExecutionParsed(nodes, edges, exec, pMissing)
	assert.False(t, ok)

	pBadRange, err := ParseSpec(Spec{From: "plan", To: "ghost"})
	require.NoError(t, err)
	_, _, ok = ScopeExecutionParsed(nodes, edges, exec, pBadRange)
	assert.False(t, ok)
}
