package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

func seedDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "g.db")
	_, err := runReplayWith(store.OpenSQLite, func() string { return "exec1" }, replayArgs{
		input:  "testdata/session.jsonl",
		dbPath: dbPath,
	})
	require.NoError(t, err)
	return dbPath
}

func TestRunsHumanOutput(t *testing.T) {
	dbPath := seedDB(t)
	var buf strings.Builder
	err := runRuns(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, false)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "RUN")
	assert.Contains(t, out, "STATUS")
}

func TestRunsJSON(t *testing.T) {
	dbPath := seedDB(t)
	var buf strings.Builder
	err := runRuns(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, true)
	require.NoError(t, err)
	var summaries []daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &summaries))
	require.NotEmpty(t, summaries)
	assert.NotEmpty(t, summaries[0].RunIDs)
}

func TestRunsStoreMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nope.db")
	err := runRuns(nil, store.OpenSQLiteReadOnly, newPricer, dbPath, false)
	assert.True(t, errors.Is(err, ErrStoreNotFound))
}

func TestRunsCmdWiredAndGrouped(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "advanced", groups["runs"])
}
