package reduce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func markerResultNoName(toolUseID string, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID:       toolUseID + "_r",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceJSONL,
		Kind:        "tool_result",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUseID},
		Attrs:       map[string]any{"status": string(model.StatusOK)},
		EventTime:   ts,
		ObservedAt:  ts,
		Seq:         seq,
	}
}

func TestMarkerSuppressionOrderIndependent(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	use := markerToolUse("tm", "phase1", "start", "", nil, t0.Add(time.Second), 2)
	result := markerResultNoName("tm", t0.Add(2*time.Second), 3)

	fwd := NewGraph()
	fwd.Apply(sessionStart(t0))
	fwd.ApplyAll([]model.Observation{use, result})

	rev := NewGraph()
	rev.Apply(sessionStart(t0))
	rev.ApplyAll([]model.Observation{result, use})

	toolID := model.ToolCallID(execID, "tm")
	assert.Nil(t, fwd.Nodes[toolID])
	assert.Nil(t, rev.Nodes[toolID])
	assert.Equal(t, canonGraph(fwd), canonGraph(rev))
}
