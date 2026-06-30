package jsonl_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/export/jsonl"
	"github.com/realkarych/catacomb/model"
)

func TestNewStreamerCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.Shutdown(context.Background()))
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestNewStreamerBadPath(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	_, err := jsonl.NewStreamer(filepath.Join(blocker, "out.jsonl"))
	require.Error(t, err)
}

func TestStreamerName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	defer func() { _ = s.Shutdown(context.Background()) }()
	assert.Equal(t, "jsonl", s.Name())
}

func TestStreamerApplyDeltaWritesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	d := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSession}}
	require.NoError(t, s.ApplyDelta(context.Background(), d))
	require.NoError(t, s.Shutdown(context.Background()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got cdc.GraphDelta
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, cdc.DeltaNodeUpsert, got.Kind)
	assert.Equal(t, "n1", got.Node.ID)
}

func TestStreamerSnapshotStateWritesNodesEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	nodes := []*model.Node{{ID: "n1", RunID: "r1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Src: "n1", Dst: "n2"}}
	require.NoError(t, s.SnapshotState(context.Background(), nodes, edges))
	require.NoError(t, s.Shutdown(context.Background()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "n1")
	assert.Contains(t, string(data), "e1")
}

func TestStreamerFlushRunIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	defer func() { _ = s.Shutdown(context.Background()) }()
	require.NoError(t, s.FlushRun(context.Background(), "run1"))
}

func TestStreamerShutdownIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.Shutdown(context.Background()))
	require.NoError(t, s.Shutdown(context.Background()))
}
