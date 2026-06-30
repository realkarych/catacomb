package subgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestParseSelector(t *testing.T) {
	n, occ, err := ParseSelector("plan")
	require.NoError(t, err)
	assert.Equal(t, "plan", n)
	assert.Equal(t, 0, occ)

	n, occ, err = ParseSelector("plan,2")
	require.NoError(t, err)
	assert.Equal(t, "plan", n)
	assert.Equal(t, 2, occ)

	_, _, err = ParseSelector("plan,x")
	assert.ErrorIs(t, err, ErrInvalidSelector)
}

func TestPhaseWindowResolvesMarker(t *testing.T) {
	exec := "E"
	markerID := model.PhaseMarkerID(exec, "plan", 0)
	nodes := []*model.Node{
		{ID: markerID, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: "other", Type: model.NodeToolCall, TStart: ts(150)},
	}

	w, ok := PhaseWindow(nodes, exec, "plan", 0)
	require.True(t, ok)
	assert.Equal(t, ts(100).Unix(), w.Start.Unix())
	require.NotNil(t, w.End)
	assert.Equal(t, ts(200).Unix(), w.End.Unix())

	_, ok = PhaseWindow(nodes, exec, "missing", 0)
	assert.False(t, ok)
}

func TestPhaseWindowOpenPhaseAndNilStart(t *testing.T) {
	exec := "E"
	open := model.PhaseMarkerID(exec, "open", 0)
	bad := model.PhaseMarkerID(exec, "bad", 0)
	nodes := []*model.Node{
		{ID: open, Type: model.NodeMarker, TStart: ts(100), TEnd: nil},
		{ID: bad, Type: model.NodeMarker, TStart: nil},
	}

	w, ok := PhaseWindow(nodes, exec, "open", 0)
	require.True(t, ok)
	assert.Nil(t, w.End)

	_, ok = PhaseWindow(nodes, exec, "bad", 0)
	assert.False(t, ok)
}

