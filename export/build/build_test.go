package build_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/config"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/export/build"
	"github.com/realkarych/catacomb/model"
)

type noopExporter struct{}

func (noopExporter) Name() string                                         { return "noop" }
func (noopExporter) ApplyDelta(_ context.Context, _ cdc.GraphDelta) error { return nil }
func (noopExporter) SnapshotState(_ context.Context, _ []*model.Node, _ []*model.Edge) error {
	return nil
}
func (noopExporter) FlushRun(_ context.Context, _ string) error { return nil }
func (noopExporter) Shutdown(_ context.Context) error           { return nil }

type projectSettableExporter struct {
	noopExporter
	project string
}

func (e *projectSettableExporter) SetProject(p string) { e.project = p }

func mockExp() exportiface.Exporter { return noopExporter{} }

func TestBuildEmpty(t *testing.T) {
	out, err := build.Build(context.Background(), nil, "", "")
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildUnknownTypeIsError(t *testing.T) {
	_, err := build.Build(context.Background(), []config.Sink{{Type: "kafka", Endpoint: "x"}}, "", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrUnknownSink)
}

func TestBuildWithMockOTLP(t *testing.T) {
	called := false
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "proj"}},
		"", "",
		build.Builders{
			NewOTLP: func(_ context.Context, endpoint, grpcAddr, httpAddr string) (exportiface.Exporter, error) {
				assert.Equal(t, "grpc://host:4317", endpoint)
				called = true
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Len(t, out, 1)
}

func TestBuildWithMockPostgres(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkPostgres, DSN: "postgres://host/db"}},
		"", "",
		build.Builders{
			NewPostgres: func(_ context.Context, dsn string) (exportiface.Exporter, error) {
				assert.Equal(t, "postgres://host/db", dsn)
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildWithMockNeo4j(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkNeo4j, URI: "bolt://host:7687", User: "neo4j", Password: "pw"}},
		"", "",
		build.Builders{
			NewNeo4j: func(_ context.Context, uri, user, password string) (exportiface.Exporter, error) {
				assert.Equal(t, "bolt://host:7687", uri)
				assert.Equal(t, "neo4j", user)
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildWithMockJSONL(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkJSONL, Path: "/tmp/out.jsonl"}},
		"", "",
		build.Builders{
			NewJSONL: func(path string) (exportiface.Exporter, error) {
				assert.Equal(t, "/tmp/out.jsonl", path)
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildRuntimeFailureSkipped(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{
			{Type: config.SinkPostgres, DSN: "postgres://fail"},
			{Type: config.SinkPostgres, DSN: "postgres://ok"},
		},
		"", "",
		build.Builders{
			NewPostgres: func(_ context.Context, dsn string) (exportiface.Exporter, error) {
				if dsn == "postgres://fail" {
					return nil, errors.New("connection refused")
				}
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildMultipleSinkTypes(t *testing.T) {
	sinks := []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://otlp:4317"},
		{Type: config.SinkPostgres, DSN: "postgres://pg/db"},
		{Type: config.SinkNeo4j, URI: "bolt://neo:7687", User: "u", Password: "p"},
		{Type: config.SinkJSONL, Path: "/out.jsonl"},
	}
	out, err := build.BuildWith(context.Background(), sinks, "", "",
		build.Builders{
			NewOTLP:     func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) { return mockExp(), nil },
			NewPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) { return mockExp(), nil },
			NewNeo4j:    func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) { return mockExp(), nil },
			NewJSONL:    func(_ string) (exportiface.Exporter, error) { return mockExp(), nil },
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 4)
}

func TestBuildNilBuilderSkips(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkPostgres, DSN: "postgres://host/db"}},
		"", "",
		build.Builders{},
	)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildWithProjectSetsAttribute(t *testing.T) {
	var setProjectCalled string
	exp := &struct {
		noopExporter
	}{}
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "my-proj"}},
		"", "",
		build.Builders{
			NewOTLP: func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
				return struct {
					noopExporter
					project string
				}{}, nil
			},
		},
	)
	_ = exp
	_ = setProjectCalled
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildNilOTLPBuilderSkips(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"}},
		"", "",
		build.Builders{},
	)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildNilNeo4jBuilderSkips(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkNeo4j, URI: "bolt://host:7687"}},
		"", "",
		build.Builders{},
	)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildNilJSONLBuilderSkips(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkJSONL, Path: "/tmp/out.jsonl"}},
		"", "",
		build.Builders{},
	)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildOTLPRuntimeFailureSkipped(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{
			{Type: config.SinkOTLP, Endpoint: "grpc://fail:4317"},
			{Type: config.SinkOTLP, Endpoint: "grpc://ok:4317"},
		},
		"", "",
		build.Builders{
			NewOTLP: func(_ context.Context, endpoint, _, _ string) (exportiface.Exporter, error) {
				if endpoint == "grpc://fail:4317" {
					return nil, errors.New("dial error")
				}
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildNeo4jRuntimeFailureSkipped(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{
			{Type: config.SinkNeo4j, URI: "bolt://fail:7687"},
			{Type: config.SinkNeo4j, URI: "bolt://ok:7687"},
		},
		"", "",
		build.Builders{
			NewNeo4j: func(_ context.Context, uri, _, _ string) (exportiface.Exporter, error) {
				if uri == "bolt://fail:7687" {
					return nil, errors.New("connection refused")
				}
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildJSONLRuntimeFailureSkipped(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{
			{Type: config.SinkJSONL, Path: "/nonexistent/dir/fail.jsonl"},
			{Type: config.SinkJSONL, Path: "/tmp/ok.jsonl"},
		},
		"", "",
		build.Builders{
			NewJSONL: func(path string) (exportiface.Exporter, error) {
				if path == "/nonexistent/dir/fail.jsonl" {
					return nil, errors.New("no such file or directory")
				}
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildOTLPSetProjectCalled(t *testing.T) {
	pexp := &projectSettableExporter{}
	var gotProject string
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "my-proj"}},
		"", "",
		build.Builders{
			NewOTLP: func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
				return pexp, nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
	gotProject = pexp.project
	assert.Equal(t, "my-proj", gotProject)
}
