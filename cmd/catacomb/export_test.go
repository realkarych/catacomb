package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	exportneo4j "github.com/realkarych/catacomb/export/neo4j"
	exportotlp "github.com/realkarych/catacomb/export/otlp"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

type recSpanExporter struct {
	batches [][]sdktrace.ReadOnlySpan
}

func (r *recSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.batches = append(r.batches, spans)
	return nil
}

func (r *recSpanExporter) Shutdown(_ context.Context) error { return nil }

type recNeo4jRunner struct {
	calls []string
}

func (r *recNeo4jRunner) Run(_ context.Context, cypher string, _ map[string]any) error {
	r.calls = append(r.calls, cypher)
	return nil
}

func (r *recNeo4jRunner) Close(_ context.Context) error { return nil }

type fakeExporter struct {
	snapshotCalled bool
	flushCalls     []string
	shutdownCalled bool
	snapshotErr    error
	flushErr       error
	shutdownErr    error
}

func (f *fakeExporter) Name() string { return "fake" }

func (f *fakeExporter) ApplyDelta(_ context.Context, _ cdc.GraphDelta) error { return nil }

func (f *fakeExporter) SnapshotState(_ context.Context, _ []*model.Node, _ []*model.Edge) error {
	f.snapshotCalled = true
	return f.snapshotErr
}

func (f *fakeExporter) FlushRun(_ context.Context, runID string) error {
	f.flushCalls = append(f.flushCalls, runID)
	return f.flushErr
}

func (f *fakeExporter) Shutdown(_ context.Context) error {
	f.shutdownCalled = true
	return f.shutdownErr
}

func TestExportJSONLMaterialized(t *testing.T) {
	dbPath := seedDB(t)
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
	})
	require.NoError(t, err)

	type kindHolder struct {
		Kind string `json:"kind"`
	}
	var nodeCount, edgeCount int
	scanner := bufio.NewScanner(strings.NewReader(buf.String()))
	for scanner.Scan() {
		var kh kindHolder
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &kh))
		switch kh.Kind {
		case "node":
			nodeCount++
		case "edge":
			edgeCount++
		}
	}
	assert.Positive(t, nodeCount)
	assert.Positive(t, edgeCount)
}

func TestExportJSONLEventsDumpsObservations(t *testing.T) {
	dbPath := seedDB(t)
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
		mode:   "events",
	})
	require.NoError(t, err)

	var obs []model.Observation
	scanner := bufio.NewScanner(strings.NewReader(buf.String()))
	for scanner.Scan() {
		var o model.Observation
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &o))
		obs = append(obs, o)
	}
	require.NotEmpty(t, obs)
	for _, o := range obs {
		assert.NotEmpty(t, o.ObsID)
	}
}

func TestExportOTLPViaFakeSpanExporter(t *testing.T) {
	dbPath := seedDB(t)
	rec := &recSpanExporter{}
	exp := exportotlp.ExporterWithSpanExporter(rec)
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newOTLP: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return exp, nil
		},
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath:       dbPath,
		to:           "otlp",
		otlpEndpoint: "fake:4317",
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "exported")
	assert.NotEmpty(t, rec.batches)
}

func TestExportNeo4jViaFakeRunner(t *testing.T) {
	dbPath := seedDB(t)
	rec := &recNeo4jRunner{}
	exp := exportneo4j.ExporterWithRunner(rec)
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newNeo4j: func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
			return exp, nil
		},
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath:   dbPath,
		to:       "neo4j",
		neo4jURI: "bolt://fake",
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "exported")
	assert.NotEmpty(t, rec.calls)
}

func TestExportPostgresViaFakeExporter(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{}
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath:      dbPath,
		to:          "postgres",
		postgresDSN: "postgres://fake",
	})
	require.NoError(t, err)
	assert.True(t, fe.snapshotCalled)
	assert.True(t, fe.shutdownCalled)
	assert.Contains(t, buf.String(), "exported")
}

func TestExportUnknownSink(t *testing.T) {
	dbPath := seedDB(t)
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath: dbPath,
		to:     "badformat",
	})
	assert.True(t, errors.Is(err, ErrUnknownSink))
}

func TestExportEventsModeNonJSONL(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{}
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newOTLP: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath:       dbPath,
		to:           "otlp",
		mode:         "events",
		otlpEndpoint: "x:4317",
	})
	assert.True(t, errors.Is(err, ErrModeUnsupported))
}

func TestExportSinkNotConfigured(t *testing.T) {
	dbPath := seedDB(t)
	tests := []struct {
		name string
		args exportArgs
	}{
		{"otlp", exportArgs{dbPath: dbPath, to: "otlp"}},
		{"neo4j", exportArgs{dbPath: dbPath, to: "neo4j"}},
		{"postgres", exportArgs{dbPath: dbPath, to: "postgres"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := exportDeps{
				open:      store.OpenSQLiteReadOnly,
				newPricer: newPricer,
			}
			err := runExport(context.Background(), io.Discard, deps, tc.args)
			assert.True(t, errors.Is(err, ErrSinkNotConfigured))
		})
	}
}

func TestExportSnapshotStateError(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{snapshotErr: errors.New("snap fail")}
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath:      dbPath,
		to:          "postgres",
		postgresDSN: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export snapshot")
	assert.True(t, fe.shutdownCalled)
}

func TestExportFlushError(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{flushErr: errors.New("flush fail")}
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath:      dbPath,
		to:          "postgres",
		postgresDSN: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export flush")
	assert.True(t, fe.shutdownCalled)
}

func TestExportStoreMissing(t *testing.T) {
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath: "/no/such/path/nope.db",
		to:     "jsonl",
	})
	assert.True(t, errors.Is(err, ErrStoreNotFound))
}

func TestExportCmdGrouped(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "advanced", groups["export"])
}

func TestExportCmdExecuteViaRoot(t *testing.T) {
	dbPath := seedDB(t)
	root := newRootCmd()
	root.SetArgs([]string{"export", "--db", dbPath, "--to", "jsonl"})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())
	assert.NotEmpty(t, buf.String())
}

func TestExportSinkStoreGraphsError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "g.db")
	require.NoError(t, err)
	_ = f.Close()
	fe := &fakeExporter{}
	deps := exportDeps{
		open: func(string) (store.Store, error) {
			return &obsErrStore{}, nil
		},
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err = runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath:      f.Name(),
		to:          "postgres",
		postgresDSN: "x",
	})
	require.Error(t, err)
	assert.True(t, fe.shutdownCalled)
}

func TestExportJSONLToFile(t *testing.T) {
	dbPath := seedDB(t)
	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
		out:    outPath,
	})
	require.NoError(t, err)
	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Positive(t, info.Size())
}

func TestExportJSONLBadOutPath(t *testing.T) {
	dbPath := seedDB(t)
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
		out:    "/no/such/dir/x.jsonl",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export create")
}

func TestExportShutdownError(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{shutdownErr: errors.New("shutdown fail")}
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err := runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath:      dbPath,
		to:          "postgres",
		postgresDSN: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export shutdown")
}

func TestExportRunFilter(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{}
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)
	_ = s.Close()
	runs := collectRuns(graphs)
	require.NotEmpty(t, runs)
	runID := runs[0].ID

	var buf strings.Builder
	err = runExport(context.Background(), &buf, deps, exportArgs{
		dbPath:      dbPath,
		to:          "postgres",
		postgresDSN: "x",
		runID:       runID,
	})
	require.NoError(t, err)
	assert.True(t, fe.snapshotCalled)
	for _, id := range fe.flushCalls {
		assert.Equal(t, runID, id)
	}
}

func TestExportJSONLEventsRunFilter(t *testing.T) {
	dbPath := seedDB(t)
	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	graphs, err := storeGraphs(s, newPricer())
	require.NoError(t, err)
	_ = s.Close()
	runs := collectRuns(graphs)
	require.NotEmpty(t, runs)
	runID := runs[0].ID

	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err = runExport(context.Background(), &buf, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
		mode:   "events",
		runID:  runID,
	})
	require.NoError(t, err)
	scanner := bufio.NewScanner(strings.NewReader(buf.String()))
	count := 0
	for scanner.Scan() {
		var o model.Observation
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &o))
		assert.Equal(t, runID, o.RunID)
		count++
	}
	assert.Positive(t, count)
}

type errWriter struct{}

func (e *errWriter) Write(_ []byte) (int, error) { return 0, errors.New("write fail") }

func TestExportObservationsEncodeError(t *testing.T) {
	dbPath := seedDB(t)
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), &errWriter{}, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
		mode:   "events",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export encode")
}

func TestExportObservationsStoreReadError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "g.db")
	require.NoError(t, err)
	_ = f.Close()
	deps := exportDeps{
		open: func(string) (store.Store, error) {
			return &obsErrStore{}, nil
		},
		newPricer: newPricer,
	}
	err = runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath: f.Name(),
		to:     "jsonl",
		mode:   "events",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

func TestExportObservationsRunIDNoMatch(t *testing.T) {
	dbPath := seedDB(t)
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath: dbPath,
		to:     "jsonl",
		mode:   "events",
		runID:  "nonexistent-run-id",
	})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestExportSinkRunIDSkipsOtherRuns(t *testing.T) {
	dbPath := seedDB(t)
	fe := &fakeExporter{}
	var buf strings.Builder
	deps := exportDeps{
		open:      store.OpenSQLiteReadOnly,
		newPricer: newPricer,
		newPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) {
			return fe, nil
		},
	}
	err := runExport(context.Background(), &buf, deps, exportArgs{
		dbPath:      dbPath,
		to:          "postgres",
		postgresDSN: "x",
		runID:       "nonexistent-run-id",
	})
	require.NoError(t, err)
	assert.True(t, fe.snapshotCalled)
	assert.Empty(t, fe.flushCalls)
	assert.True(t, fe.shutdownCalled)
}

func TestExportJSONLStoreGraphsError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "g.db")
	require.NoError(t, err)
	_ = f.Close()
	deps := exportDeps{
		open: func(string) (store.Store, error) {
			return &obsErrStore{}, nil
		},
		newPricer: newPricer,
	}
	err = runExport(context.Background(), io.Discard, deps, exportArgs{
		dbPath: f.Name(),
		to:     "jsonl",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

func TestRealNewOTLPBuildsExporter(t *testing.T) {
	exp, err := realNewOTLP(context.Background(), "grpc://localhost:1")
	require.NoError(t, err)
	require.NotNil(t, exp)
	_ = exp.Shutdown(context.Background())
}

func TestRealNewNeo4jBuildsExporter(t *testing.T) {
	exp, err := realNewNeo4j(context.Background(), "bolt://localhost:1", "", "")
	require.NoError(t, err)
	require.NotNil(t, exp)
	_ = exp.Shutdown(context.Background())
}

func TestRealNewPostgresBadDSN(t *testing.T) {
	_, err := realNewPostgres(context.Background(), "not-a-valid-dsn")
	require.Error(t, err)
}
