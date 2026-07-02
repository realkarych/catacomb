package reduce

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestApplyAnnotationsAttachesBySourceKey(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.ApplyAll([]model.Observation{ob("session_start", "", t0)})

	sessID := model.SessionNodeID(execID)
	sk := model.NodeSourceKey(sessID)
	g.ApplyAnnotations([]model.Annotation{
		{ExecutionID: execID, SourceKey: sk, Owner: "eval", Key: "score", Value: json.RawMessage(`5`)},
		{ExecutionID: execID, SourceKey: sk, Owner: "eval", Key: "score", Value: json.RawMessage(`9`)},
		{ExecutionID: execID, SourceKey: sk, Owner: "other", Key: "flag", Value: json.RawMessage(`true`)},
	})

	n := g.Nodes[sessID]
	require.NotNil(t, n)
	assert.Equal(t, json.RawMessage(`9`), n.Annotations["eval.score"])
	assert.Equal(t, json.RawMessage(`true`), n.Annotations["other.flag"])
}

func TestApplyAnnotationsUnknownSourceKeyNoop(t *testing.T) {
	g := NewGraph()
	g.ApplyAnnotations([]model.Annotation{
		{SourceKey: "noexist", Owner: "eval", Key: "score", Value: json.RawMessage(`9`)},
	})
	assert.Empty(t, g.Nodes)
}

func TestNodeBySourceKey(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	g := NewGraph()
	g.ApplyAll([]model.Observation{ob("session_start", "", t0)})

	sessID := model.SessionNodeID(execID)
	n := g.NodeBySourceKey(model.NodeSourceKey(sessID))
	require.NotNil(t, n)
	assert.Equal(t, sessID, n.ID)
	assert.Nil(t, g.NodeBySourceKey("missing"))
}
