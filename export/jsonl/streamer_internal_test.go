package jsonl

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

func TestStreamerApplyDeltaEncodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.f.Close())
	err = s.ApplyDelta(context.Background(), cdc.GraphDelta{})
	require.Error(t, err)
	s.f = nil
}

func TestStreamerSnapshotStateNodeEncodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.f.Close())
	err = s.SnapshotState(context.Background(), []*model.Node{{ID: "n1"}}, nil)
	require.Error(t, err)
	s.f = nil
}

func TestStreamerSnapshotStateEdgeEncodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.f.Close())
	err = s.SnapshotState(context.Background(), nil, []*model.Edge{{ID: "e1"}})
	require.Error(t, err)
	s.f = nil
}

func TestStreamerShutdownCloseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.f.Close())
	err = s.Shutdown(context.Background())
	require.Error(t, err)
}
