package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

type fakeStore struct {
	failPersist bool
}

func (f *fakeStore) Persist([]model.Observation, []*model.Node, []*model.Edge) error {
	if f.failPersist {
		return errors.New("persist")
	}
	return nil
}

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) AppendDeltas(model.Observation, []cdc.GraphDelta) error {
	return nil
}

func (f *fakeStore) MaxSeq() (uint64, error) { return 0, nil }

func (f *fakeStore) ObservationsSince(uint64) ([]model.Observation, error)        { return nil, nil }
func (f *fakeStore) ObservationsForExecution(string) ([]model.Observation, error) { return nil, nil }
func (f *fakeStore) UpsertRun(model.Run) error                                    { return nil }
func (f *fakeStore) ListOpenRuns() ([]model.Run, error)                           { return nil, nil }
func (f *fakeStore) Runs() ([]model.Run, error)                                   { return nil, nil }
func (f *fakeStore) Quarantine(model.QuarantineRecord) error                      { return nil }
func (f *fakeStore) QuarantineCount() (int64, error)                              { return 0, nil }
func (f *fakeStore) UpsertTailCursor(model.TailCursor) error                      { return nil }
func (f *fakeStore) LoadTailCursors() ([]model.TailCursor, error)                 { return nil, nil }
func (f *fakeStore) UpsertAnnotation(model.Annotation) error                      { return nil }
func (f *fakeStore) AnnotationsForExecution(string) ([]model.Annotation, error)   { return nil, nil }
func (f *fakeStore) MoveAnnotations(string, string, string) error                  { return nil }

func openFake(f *fakeStore) storeOpener {
	return func(string) (store.Store, error) { return f, nil }
}

func fixedExecID() string { return "exec-T" }

func TestRunReplayBuildsGraph(t *testing.T) {
	dir := t.TempDir()
	g, err := runReplay(replayArgs{
		input:      "testdata/session.jsonl",
		dbPath:     filepath.Join(dir, "g.db"),
		exportPath: filepath.Join(dir, "g.jsonl"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, g.Nodes)
	assert.NotEmpty(t, g.Edges)
	assert.FileExists(t, filepath.Join(dir, "g.jsonl"))
}

func TestRunReplayNoExport(t *testing.T) {
	g, err := runReplay(replayArgs{
		input:  "testdata/session.jsonl",
		dbPath: filepath.Join(t.TempDir(), "g.db"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, g.Nodes)
}

func TestRunReplayMissingInput(t *testing.T) {
	_, err := runReplay(replayArgs{input: filepath.Join(t.TempDir(), "nope.jsonl"), dbPath: filepath.Join(t.TempDir(), "g.db")})
	require.Error(t, err)
}

func TestRunReplayMalformedInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte("{not json}\n"), 0o644))
	_, err := runReplay(replayArgs{input: bad, dbPath: filepath.Join(dir, "g.db")})
	require.Error(t, err)
}

func TestRunReplayBadDBPath(t *testing.T) {
	_, err := runReplay(replayArgs{
		input:  "testdata/session.jsonl",
		dbPath: filepath.Join(t.TempDir(), "nodir", "g.db"),
	})
	require.Error(t, err)
}

func TestRunReplayBadExportPath(t *testing.T) {
	dir := t.TempDir()
	_, err := runReplay(replayArgs{
		input:      "testdata/session.jsonl",
		dbPath:     filepath.Join(dir, "g.db"),
		exportPath: filepath.Join(dir, "nodir", "g.jsonl"),
	})
	require.Error(t, err)
}

func TestRunReplayPersistError(t *testing.T) {
	_, err := runReplayWith(openFake(&fakeStore{failPersist: true}), fixedExecID, replayArgs{input: "testdata/session.jsonl", dbPath: "x"})
	require.Error(t, err)
}

func TestRunReplayWithHappyNoExport(t *testing.T) {
	g, err := runReplayWith(openFake(&fakeStore{}), fixedExecID, replayArgs{input: "testdata/session.jsonl", dbPath: "x"})
	require.NoError(t, err)
	require.NotNil(t, g.Nodes[model.SessionNodeID("exec-T")])
}

func TestReplayCommandWiring(t *testing.T) {
	dir := t.TempDir()
	root := newRootCmd()
	root.SetArgs([]string{"replay", "testdata/session.jsonl", "--db", filepath.Join(dir, "g.db")})
	require.NoError(t, root.Execute())
}

func TestReplayCommandError(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"replay", filepath.Join(t.TempDir(), "nope.jsonl"), "--db", filepath.Join(t.TempDir(), "g.db")})
	require.Error(t, root.Execute())
}
