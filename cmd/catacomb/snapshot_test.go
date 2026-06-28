package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/store"
)

func TestSnapshotStdoutRoundTrips(t *testing.T) {
	dbPath := seedDB(t)
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)
	nodes, edges := collectSnapshot(graphs, "")

	var buf strings.Builder
	err = runSnapshot(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, "", "")
	require.NoError(t, err)

	type kindHolder struct {
		Kind string `json:"kind"`
	}
	var gotNodes, gotEdges []string
	scanner := bufio.NewScanner(strings.NewReader(buf.String()))
	for scanner.Scan() {
		var kh kindHolder
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &kh))
		switch kh.Kind {
		case "node":
			var n struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(scanner.Bytes(), &n))
			gotNodes = append(gotNodes, n.ID)
		case "edge":
			var e struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(scanner.Bytes(), &e))
			gotEdges = append(gotEdges, e.ID)
		}
	}

	wantNodeIDs := make([]string, len(nodes))
	for i, n := range nodes {
		wantNodeIDs[i] = n.ID
	}
	wantEdgeIDs := make([]string, len(edges))
	for i, e := range edges {
		wantEdgeIDs[i] = e.ID
	}

	assert.ElementsMatch(t, wantNodeIDs, gotNodes)
	assert.ElementsMatch(t, wantEdgeIDs, gotEdges)
}

func TestSnapshotToFileAndRunFilter(t *testing.T) {
	dbPath := seedDB(t)
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)
	runs := collectRuns(graphs)
	require.NotEmpty(t, runs)
	runID := runs[0].ID

	outPath := filepath.Join(t.TempDir(), "snap.jsonl")
	err = runSnapshot(io.Discard, store.OpenSQLiteReadOnly, newPricer, dbPath, runID, outPath)
	require.NoError(t, err)

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Positive(t, info.Size())

	f, err := os.Open(outPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row struct {
			Kind  string `json:"kind"`
			RunID string `json:"run_id"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &row))
		if row.Kind == "node" {
			assert.Equal(t, runID, row.RunID)
		}
	}
}

func TestSnapshotBadOutPath(t *testing.T) {
	dbPath := seedDB(t)
	err := runSnapshot(io.Discard, store.OpenSQLiteReadOnly, newPricer, dbPath, "", "/no/such/dir/x.jsonl")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot create")
}

func TestSnapshotStoreMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nope.db")
	err := runSnapshot(io.Discard, store.OpenSQLiteReadOnly, newPricer, dbPath, "", "")
	assert.True(t, errors.Is(err, ErrStoreNotFound))
}

func TestSnapshotStoreReadError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "g.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runSnapshot(io.Discard, func(string) (store.Store, error) {
		return &obsErrStore{}, nil
	}, newPricer, f.Name(), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

func TestSnapshotCmdExecuteViaRoot(t *testing.T) {
	dbPath := seedDB(t)
	root := newRootCmd()
	root.SetArgs([]string{"snapshot", "--db", dbPath})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())
}
