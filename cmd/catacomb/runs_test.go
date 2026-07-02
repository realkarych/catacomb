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
	err := runRuns(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, false, nil)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "RUN")
	assert.Contains(t, out, "STATUS")
}

func TestRunsJSON(t *testing.T) {
	dbPath := seedDB(t)
	var buf strings.Builder
	err := runRuns(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, true, nil)
	require.NoError(t, err)
	var summaries []daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &summaries))
	require.NotEmpty(t, summaries)
	assert.NotEmpty(t, summaries[0].RunIDs)
}

func TestRunsStoreMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nope.db")
	err := runRuns(nil, store.OpenSQLiteReadOnly, newPricer, dbPath, false, nil)
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

func TestRunsCmdExecuteViaRoot(t *testing.T) {
	dbPath := seedDB(t)
	root := newRootCmd()
	root.SetArgs([]string{"runs", "--db", dbPath})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "RUN")
}

func seedRunsWithLabels(t *testing.T, runs map[string]string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	d := daemon.New(s)
	for id, labels := range runs {
		require.NoError(t, d.IngestWithLabels("SessionStart", []byte(`{"session_id":"`+id+`"}`), labels))
	}
	require.NoError(t, s.Close())
	return dbPath
}

func TestRunsLabelSelectorFiltersJSON(t *testing.T) {
	dbPath := seedRunsWithLabels(t, map[string]string{"r1": "basket=b1", "r2": "basket=b2"})

	var buf strings.Builder
	err := runRuns(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, true, []string{"basket=b1"})
	require.NoError(t, err)

	var summaries []daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &summaries))
	require.Len(t, summaries, 1)
	assert.Equal(t, "r1", summaries[0].Session)
	assert.Equal(t, map[string]string{"basket": "b1"}, summaries[0].Labels)
}

func TestRunsLabelSelectorANDsTerms(t *testing.T) {
	dbPath := seedRunsWithLabels(t, map[string]string{
		"r1": "basket=b1,rep=1",
		"r2": "basket=b1,rep=2",
		"r3": "rep=1",
	})

	var buf strings.Builder
	err := runRuns(&buf, store.OpenSQLiteReadOnly, newPricer, dbPath, true, []string{"basket=b1", "rep=1"})
	require.NoError(t, err)

	var summaries []daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &summaries))
	require.Len(t, summaries, 1)
	assert.Equal(t, "r1", summaries[0].Session)
	assert.Equal(t, map[string]string{"basket": "b1", "rep": "1"}, summaries[0].Labels)
}

func TestRunsLabelFlagInvalidErrors(t *testing.T) {
	dbPath := seedRunsWithLabels(t, map[string]string{"r1": "basket=b1"})

	root := newRootCmd()
	root.SetArgs([]string{"runs", "--db", dbPath, "--json", "--label", "Bad=x"})
	var buf strings.Builder
	root.SetOut(&buf)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --label")
	assert.NotContains(t, buf.String(), "r1")
}

func TestRunsLabelFlagMultiPairTermViaRoot(t *testing.T) {
	dbPath := seedRunsWithLabels(t, map[string]string{"r1": "a=1,b=2", "r2": "a=1"})

	root := newRootCmd()
	root.SetArgs([]string{"runs", "--db", dbPath, "--json", "--label", "a=1,b=2"})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())

	var summaries []daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &summaries))
	require.Len(t, summaries, 1)
	assert.Equal(t, "r1", summaries[0].Session)
}

func TestRunsLabelFlagViaRoot(t *testing.T) {
	dbPath := seedRunsWithLabels(t, map[string]string{"r1": "basket=b1", "r2": "basket=b2"})

	root := newRootCmd()
	root.SetArgs([]string{"runs", "--db", dbPath, "--json", "--label", "basket=b2"})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())

	var summaries []daemon.SessionSummary
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &summaries))
	require.Len(t, summaries, 1)
	assert.Equal(t, "r2", summaries[0].Session)
	assert.Equal(t, map[string]string{"basket": "b2"}, summaries[0].Labels)
}

func TestRunsStoreReadError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "g.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runRuns(io.Discard, func(string) (store.Store, error) {
		return &obsErrStore{}, nil
	}, newPricer, f.Name(), false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}
