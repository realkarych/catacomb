package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func TestOpenReadStoreMissing(t *testing.T) {
	_, err := openReadStore(store.OpenSQLiteReadOnly, filepath.Join(t.TempDir(), "nope.db"))
	assert.True(t, errors.Is(err, ErrStoreNotFound))
}

func TestOpenReadStoreOpenError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	badOpen := func(string) (store.Store, error) { return nil, errors.New("boom") }
	_, err = openReadStore(badOpen, f.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store open")
}

type obsErrStore struct {
	fakeStore
}

func (o *obsErrStore) ObservationsSince(uint64) ([]model.Observation, error) {
	return nil, errors.New("read fail")
}

func TestStoreGraphsReadError(t *testing.T) {
	s := &obsErrStore{}
	_, err := storeGraphs(s, newPricer())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

func TestStoreGraphsRebuildsRunsAndNodes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.db")
	_, err := runReplayWith(store.OpenSQLite, func() string { return "exec1" }, replayArgs{input: "testdata/session.jsonl", dbPath: dbPath})
	require.NoError(t, err)

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)
	require.NotEmpty(t, graphs)

	runs := collectRuns(graphs)
	assert.NotEmpty(t, runs)

	nodes, edges := collectSnapshot(graphs, "")
	assert.NotEmpty(t, nodes)
	assert.NotEmpty(t, edges)
}

func TestCollectSnapshotFiltersByRunAndIsSorted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.db")
	_, err := runReplayWith(store.OpenSQLite, func() string { return "exec1" }, replayArgs{input: "testdata/session.jsonl", dbPath: dbPath})
	require.NoError(t, err)

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)

	runs := collectRuns(graphs)
	require.NotEmpty(t, runs)
	runID := runs[0].ID

	nodes, _ := collectSnapshot(graphs, runID)
	require.NotEmpty(t, nodes)

	for _, n := range nodes {
		assert.Equal(t, runID, n.RunID)
	}

	for i := 1; i < len(nodes); i++ {
		assert.LessOrEqual(t, nodes[i-1].ID, nodes[i].ID)
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name string
		in   *float64
		want string
	}{
		{"nil", nil, "-"},
		{"zero", floatPtr(0.0), "$0.0000"},
		{"rounded", floatPtr(1.23456), "$1.2346"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatCost(tc.in))
		})
	}
}

func floatPtr(f float64) *float64 { return &f }

func TestOpenReadStoreSuccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.db")
	_, err := runReplayWith(store.OpenSQLite, func() string { return "exec1" }, replayArgs{input: "testdata/session.jsonl", dbPath: dbPath})
	require.NoError(t, err)
	s, err := openReadStore(store.OpenSQLiteReadOnly, dbPath)
	require.NoError(t, err)
	require.NotNil(t, s)
	_ = s.Close()
}

func TestCollectRunsSortsTwoOrMore(t *testing.T) {
	g1 := reduce.NewGraph()
	r1 := model.Run{ID: "bbb"}
	g1.Runs["bbb"] = &r1
	g2 := reduce.NewGraph()
	r2 := model.Run{ID: "aaa"}
	g2.Runs["aaa"] = &r2

	runs := collectRuns([]*reduce.Graph{g1, g2})
	require.Len(t, runs, 2)
	assert.Equal(t, "aaa", runs[0].ID)
	assert.Equal(t, "bbb", runs[1].ID)
}

func TestSortedKeys(t *testing.T) {
	m := map[string]int{"b": 1, "a": 2, "c": 3}
	assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(m))
}
