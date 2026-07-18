package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestSnapshotIncludesAnnotations(t *testing.T) {
	raw := json.RawMessage(`"high"`)
	nodes := []*model.Node{
		{
			ID:          "session:e1",
			RunID:       "s1",
			Type:        model.NodeSession,
			Annotations: map[string]any{"eval.score": raw},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, nil, nil))
	assert.Contains(t, buf.String(), `"annotations"`)
	assert.Contains(t, buf.String(), `"eval.score"`)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestSnapshotOrderAndKinds(t *testing.T) {
	nodes := []*model.Node{{ID: "session:s1", RunID: "s1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "session:s1", Dst: "n2"}}

	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, edges, nil))

	var kinds []string
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var rec struct {
			Kind string `json:"kind"`
		}
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		kinds = append(kinds, rec.Kind)
	}
	assert.Equal(t, []string{"node", "edge"}, kinds)
}

func TestSnapshotWriterErrorNode(t *testing.T) {
	err := Snapshot(failWriter{}, []*model.Node{{ID: "n"}}, nil, nil)
	require.Error(t, err)
}

func TestSnapshotWriterErrorEdge(t *testing.T) {
	err := Snapshot(failWriter{}, nil, []*model.Edge{{ID: "e"}}, nil)
	require.Error(t, err)
}

func TestSnapshotEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil, nil))
	assert.Empty(t, buf.String())
}

func TestSnapshotRunLine(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	runs := []model.Run{{ID: "s1", Status: model.StatusOK, StartedAt: &now}}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil, runs))
	assert.Contains(t, buf.String(), `"run"`)
	assert.Contains(t, buf.String(), `"s1"`)
}

func TestSnapshotRunWriterError(t *testing.T) {
	runs := []model.Run{{ID: "r1"}}
	err := Snapshot(failWriter{}, nil, nil, runs)
	require.Error(t, err)
}

func TestSnapshotRedactsPayloadInput(t *testing.T) {
	secret := "Authorization: Bearer sk-live_ABC123DEF456GHI789JKL"
	original := json.RawMessage(`{"cmd":"` + secret + `"}`)
	nodes := []*model.Node{
		{
			ID:      "n1",
			RunID:   "s1",
			Type:    model.NodeToolCall,
			Payload: &model.Payload{Input: append(json.RawMessage(nil), original...)},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, nil, nil))

	assert.NotContains(t, buf.String(), secret, "raw secret must not appear in jsonl output")
	assert.Contains(t, buf.String(), "‹redacted:", "redaction marker must appear in jsonl output")
	assert.Equal(t, string(original), string(nodes[0].Payload.Input), "original node payload must be unchanged after Snapshot")
}

func TestSnapshotRedactsPayloadOutput(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	nodes := []*model.Node{
		{
			ID:      "n1",
			RunID:   "s1",
			Type:    model.NodeToolCall,
			Payload: &model.Payload{Output: json.RawMessage(`{"result":"` + secret + `"}`)},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, nil, nil))

	assert.NotContains(t, buf.String(), secret, "raw secret must not appear in jsonl output")
	assert.Contains(t, buf.String(), "‹redacted:", "redaction marker must appear in jsonl output")
}

func TestSnapshotRedactsRunLabelsAndCwd(t *testing.T) {
	labelSecret := "sk-live_ABC123DEF456GHI789JKL"
	cwdSecret := "AKIAIOSFODNN7EXAMPLE"
	runs := []model.Run{
		{
			ID:     "s1",
			Status: model.StatusOK,
			Labels: map[string]string{"token": labelSecret},
			Repro:  &model.ReproMeta{Cwd: "/deploy/" + cwdSecret + "/build"},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil, runs))

	assert.NotContains(t, buf.String(), labelSecret, "raw label secret must not appear in jsonl output")
	assert.NotContains(t, buf.String(), cwdSecret, "raw cwd secret must not appear in jsonl output")
	assert.Contains(t, buf.String(), "‹redacted:", "redaction marker must appear in jsonl output")
	assert.Equal(t, labelSecret, runs[0].Labels["token"], "original run labels must be unchanged after Snapshot")
	assert.Equal(t, "/deploy/"+cwdSecret+"/build", runs[0].Repro.Cwd, "original run cwd must be unchanged after Snapshot")
}

func TestSnapshotAllKindsOrdered(t *testing.T) {
	nodes := []*model.Node{{ID: "n1", RunID: "s1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"}}
	runs := []model.Run{{ID: "s1", Status: model.StatusOK}}

	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, edges, runs))

	var kinds []string
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var rec struct {
			Kind string `json:"kind"`
		}
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		kinds = append(kinds, rec.Kind)
	}
	assert.Equal(t, []string{"node", "edge", "run"}, kinds)
}
