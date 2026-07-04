package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
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

func seedRunGroup(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	d := daemon.New(s)
	d.SetAllowAnnotations(true)
	seeds := []struct{ id, labels string }{
		{"r1", "basket=b1"},
		{"r2", "basket=b1"},
		{"r3", "basket=b2"},
	}
	for _, sd := range seeds {
		require.NoError(t, d.IngestWithLabels("SessionStart", []byte(`{"session_id":"`+sd.id+`"}`), sd.labels, ""))
	}
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	execByRun := map[string]string{}
	for _, o := range obs {
		if _, ok := execByRun[o.RunID]; !ok {
			execByRun[o.RunID] = o.ExecutionID
		}
	}
	for _, rid := range []string{"r1", "r2"} {
		execID := execByRun[rid]
		sk := model.NodeSourceKey(model.SessionNodeID(execID))
		require.NoError(t, d.Annotate(execID, sk, "eval", "score", json.RawMessage(`7`)))
	}
	require.NoError(t, s.Close())
	return dbPath
}

func TestLoadRunGroupFiltersBySelectorWithAnnotations(t *testing.T) {
	dbPath := seedRunGroup(t)
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	group, err := loadRunGroup(s, newPricer(), map[string]string{"basket": "b1"})
	require.NoError(t, err)
	require.Len(t, group, 2)
	assert.Equal(t, "r1", group[0].Run.ID)
	assert.Equal(t, "r2", group[1].Run.ID)

	for _, rg := range group {
		require.NotEmpty(t, rg.Nodes)
		var found bool
		for _, n := range rg.Nodes {
			assert.Equal(t, rg.Run.ID, n.RunID)
			if v, ok := n.Annotations["eval.score"]; ok {
				assert.Equal(t, json.RawMessage(`7`), v)
				found = true
			}
		}
		assert.True(t, found, "annotation should be reattached for run %s", rg.Run.ID)
	}
}

func TestLoadRunGroupEmptySelectorReturnsAll(t *testing.T) {
	dbPath := seedRunGroup(t)
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	group, err := loadRunGroup(s, newPricer(), nil)
	require.NoError(t, err)
	require.Len(t, group, 3)
	assert.Equal(t, "r1", group[0].Run.ID)
	assert.Equal(t, "r2", group[1].Run.ID)
	assert.Equal(t, "r3", group[2].Run.ID)
}

func TestLoadRunGroupReadError(t *testing.T) {
	_, err := loadRunGroup(&obsErrStore{}, newPricer(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

type annErrStore struct {
	fakeStore
}

func (a *annErrStore) ObservationsSince(uint64) ([]model.Observation, error) {
	return []model.Observation{{ExecutionID: "e1", RunID: "r1", Kind: "session_start"}}, nil
}

func (a *annErrStore) AnnotationsForExecution(string) ([]model.Annotation, error) {
	return nil, errors.New("ann fail")
}

func TestLoadRunGroupAnnotationsError(t *testing.T) {
	_, err := loadRunGroup(&annErrStore{}, newPricer(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store annotations")
}

func TestLoadRunGroupMultiRunBucketsAndOrders(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	d := daemon.New(s)
	for _, rid := range []string{"r3", "r1", "r2"} {
		require.NoError(t, d.IngestWithLabels("SessionStart", []byte(`{"session_id":"`+rid+`"}`), "basket=b1", ""))
		require.NoError(t, d.IngestWithLabels("PreToolUse", []byte(`{"session_id":"`+rid+`","tool_name":"Bash","tool_use_id":"`+rid+`-t2","tool_input":{}}`), "basket=b1", ""))
		require.NoError(t, d.IngestWithLabels("PreToolUse", []byte(`{"session_id":"`+rid+`","tool_name":"Bash","tool_use_id":"`+rid+`-t1","tool_input":{}}`), "basket=b1", ""))
	}
	require.NoError(t, s.Close())

	ro, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = ro.Close() }()

	group, err := loadRunGroup(ro, newPricer(), map[string]string{"basket": "b1"})
	require.NoError(t, err)
	require.Len(t, group, 3)
	assert.Equal(t, []string{"r1", "r2", "r3"},
		[]string{group[0].Run.ID, group[1].Run.ID, group[2].Run.ID})

	for _, rg := range group {
		require.NotEmpty(t, rg.Nodes)
		for _, n := range rg.Nodes {
			assert.Equal(t, rg.Run.ID, n.RunID)
		}
		for i := 1; i < len(rg.Nodes); i++ {
			assert.LessOrEqual(t, rg.Nodes[i-1].ID, rg.Nodes[i].ID)
		}
		for i := 1; i < len(rg.Edges); i++ {
			assert.LessOrEqual(t, rg.Edges[i-1].ID, rg.Edges[i].ID)
		}
	}
}
