package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

var errWriterBoom = errors.New("writer boom")

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWriterBoom }

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		out = append(out, rec)
	}
	require.NoError(t, sc.Err())
	return out
}

func kindIDSequence(recs []map[string]any) []string {
	seq := make([]string, len(recs))
	for i, r := range recs {
		seq[i] = r["kind"].(string) + ":" + r["id"].(string)
	}
	return seq
}

func TestSnapshotPreservesAnnotationKeysAndValues(t *testing.T) {
	nodes := []*model.Node{
		{
			ID:    "session:e1",
			RunID: "s1",
			Type:  model.NodeSession,
			Annotations: map[string]any{
				"eval.score":  json.RawMessage(`"high"`),
				"eval.reason": json.RawMessage(`{"rubric":"clarity","points":7}`),
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, nil, nil))

	recs := decodeLines(t, &buf)
	require.Len(t, recs, 1)
	assert.Equal(t, map[string]any{
		"eval.score":  "high",
		"eval.reason": map[string]any{"rubric": "clarity", "points": float64(7)},
	}, recs[0]["annotations"], "annotation keys and values must survive the snapshot verbatim")
}

func TestSnapshotEmitsNodesThenEdgesThenRunsInInputOrder(t *testing.T) {
	nodes := []*model.Node{
		{ID: "n2", RunID: "s1", Type: model.NodeToolCall},
		{ID: "n1", RunID: "s1", Type: model.NodeSession},
	}
	edges := []*model.Edge{
		{ID: "e2", RunID: "s1", Type: model.EdgeMarkerSpan, Src: "n1", Dst: "n2"},
		{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"},
	}
	runs := []model.Run{{ID: "r2", Status: model.StatusOK}, {ID: "r1", Status: model.StatusOK}}

	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, edges, runs))

	assert.Equal(t,
		[]string{"node:n2", "node:n1", "edge:e2", "edge:e1", "run:r2", "run:r1"},
		kindIDSequence(decodeLines(t, &buf)),
		"snapshot must emit every node, then every edge, then every run, each group in caller order",
	)
}

func TestSnapshotWritesExactlyOneJSONObjectPerLine(t *testing.T) {
	nodes := []*model.Node{{ID: "n1", RunID: "s1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"}}
	runs := []model.Run{{ID: "r1", Status: model.StatusOK}}

	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, edges, runs))

	out := buf.String()
	require.True(t, strings.HasSuffix(out, "\n"))
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 3)
	for _, line := range lines {
		assert.NotContains(t, line, "\n")
		assert.True(t, json.Valid([]byte(line)), "each line must be a standalone JSON object: %q", line)
	}
}

func TestSnapshotWriterErrorPropagatesUnwrapped(t *testing.T) {
	tests := []struct {
		name  string
		nodes []*model.Node
		edges []*model.Edge
		runs  []model.Run
	}{
		{name: "node", nodes: []*model.Node{{ID: "n"}}},
		{name: "edge", edges: []*model.Edge{{ID: "e"}}},
		{name: "run", runs: []model.Run{{ID: "r1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Snapshot(failWriter{}, tc.nodes, tc.edges, tc.runs)
			require.ErrorIs(t, err, errWriterBoom)
		})
	}
}

func TestSnapshotEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil, nil))
	assert.Empty(t, buf.String())
}

func TestSnapshotRunLineCarriesEveryRunField(t *testing.T) {
	started := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	ended := started.Add(90 * time.Second)
	runs := []model.Run{{
		ID:         "s1",
		SessionIDs: []string{"sess-a", "sess-b"},
		ModelID:    "claude-opus-4-8",
		Status:     model.StatusOK,
		LastSeq:    42,
		StartedAt:  &started,
		EndedAt:    &ended,
		Labels:     map[string]string{"variant": "base"},
		Repro:      &model.ReproMeta{ClaudeCodeVersion: "2.1.199", Cwd: "/work/repo"},
	}}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil, runs))

	recs := decodeLines(t, &buf)
	require.Len(t, recs, 1)
	assert.Equal(t, map[string]any{
		"kind":        "run",
		"id":          "s1",
		"session_ids": []any{"sess-a", "sess-b"},
		"model_id":    "claude-opus-4-8",
		"status":      "ok",
		"last_seq":    float64(42),
		"started_at":  "2026-06-20T10:00:00Z",
		"ended_at":    "2026-06-20T10:01:30Z",
		"labels":      map[string]any{"variant": "base"},
		"repro":       map[string]any{"claude_code_version": "2.1.199", "cwd": "/work/repo"},
	}, recs[0])
}

func TestSnapshotRedactsPayloadInput(t *testing.T) {
	secret := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef0123"
	original := json.RawMessage(`{"cmd":"git push origin main --token ` + secret + `"}`)
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

	recs := decodeLines(t, &buf)
	require.Len(t, recs, 1)
	payload := recs[0]["payload"].(map[string]any)
	assert.Equal(t, map[string]any{"cmd": "git push origin main --token ‹redacted:github-token›"}, payload["input"],
		"the secret span must be replaced by its typed placeholder, and only that span")
	assert.Equal(t, string(original), string(nodes[0].Payload.Input), "original node payload must be unchanged after Snapshot")
}

func TestSnapshotRedactsPayloadOutput(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	nodes := []*model.Node{
		{
			ID:      "n1",
			RunID:   "s1",
			Type:    model.NodeToolCall,
			Payload: &model.Payload{Output: json.RawMessage(`{"result":"` + secret + `","file":"main.go"}`)},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, nil, nil))

	recs := decodeLines(t, &buf)
	require.Len(t, recs, 1)
	payload := recs[0]["payload"].(map[string]any)
	assert.Equal(t, map[string]any{"result": "‹redacted:aws-key›", "file": "main.go"}, payload["output"],
		"clean sibling fields must survive alongside the redacted one")
}

func TestSnapshotRedactsRunLabelsAndCwd(t *testing.T) {
	labelSecret := "sk-live_ABC123DEF456GHI789JKL"
	cwdSecret := "AKIAIOSFODNN7EXAMPLE"
	runs := []model.Run{
		{
			ID:     "s1",
			Status: model.StatusOK,
			Labels: map[string]string{"token": labelSecret, "variant": "base"},
			Repro:  &model.ReproMeta{Cwd: "/deploy/" + cwdSecret + "/build", ClaudeCodeVersion: "2.1.199"},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil, runs))

	recs := decodeLines(t, &buf)
	require.Len(t, recs, 1)
	assert.Equal(t, map[string]any{"token": "‹redacted:openai-key›", "variant": "base"}, recs[0]["labels"],
		"only the secret label value may change")
	assert.Equal(t, map[string]any{"cwd": "/deploy/‹redacted:aws-key›/build", "claude_code_version": "2.1.199"}, recs[0]["repro"],
		"cwd must be redacted span-level while the version stamp survives")

	assert.Equal(t, labelSecret, runs[0].Labels["token"], "original run labels must be unchanged after Snapshot")
	assert.Equal(t, "/deploy/"+cwdSecret+"/build", runs[0].Repro.Cwd, "original run cwd must be unchanged after Snapshot")
}
