package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

func inspectRunID(t *testing.T, dbPath string) string {
	t.Helper()
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)
	runs := collectRuns(graphs)
	require.NotEmpty(t, runs)
	return runs[0].ID
}

func TestInspectJSON(t *testing.T) {
	dbPath := seedDB(t)
	runID := inspectRunID(t, dbPath)

	var buf strings.Builder
	err := runInspect(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, runID, true)
	require.NoError(t, err)

	var sum daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &sum))
	assert.Equal(t, runID, sum.Session)
}

func TestInspectHumanShowsBreakdown(t *testing.T) {
	dbPath := seedDB(t)
	runID := inspectRunID(t, dbPath)

	var buf strings.Builder
	err := runInspect(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, runID, false)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Run:")
	assert.Contains(t, out, runID)
	assert.Contains(t, out, "Status:")
}

func TestInspectUnknownRun(t *testing.T) {
	dbPath := seedDB(t)
	err := runInspect(io.Discard, store.OpenSQLiteReadOnly, newPricer, dbPath, "no-such-run", false)
	assert.True(t, errors.Is(err, ErrRunNotFound))
}

func TestInspectStoreMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nope.db")
	err := runInspect(io.Discard, store.OpenSQLiteReadOnly, newPricer, dbPath, "any", false)
	assert.True(t, errors.Is(err, ErrStoreNotFound))
}

func TestInspectStoreReadError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "g.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runInspect(io.Discard, func(string) (store.Store, error) {
		return &obsErrStore{}, nil
	}, newPricer, f.Name(), "any", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

func TestInspectCmdExecuteViaRoot(t *testing.T) {
	dbPath := seedDB(t)
	runID := inspectRunID(t, dbPath)

	root := newRootCmd()
	root.SetArgs([]string{"inspect", runID, "--db", dbPath})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "Run:")
}
