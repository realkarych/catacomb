package otlp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type recordExporter struct {
	batches  [][]sdktrace.ReadOnlySpan
	shutdown bool
}

func (r *recordExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.batches = append(r.batches, spans)
	return nil
}

func (r *recordExporter) Shutdown(_ context.Context) error {
	r.shutdown = true
	return nil
}

type errExporter struct{}

func (e *errExporter) ExportSpans(_ context.Context, _ []sdktrace.ReadOnlySpan) error {
	return errors.New("export error")
}

func (e *errExporter) Shutdown(_ context.Context) error {
	return errors.New("shutdown error")
}

func TestNewRejectsSelfLoopGRPCAddr(t *testing.T) {
	_, err := New(context.Background(), "grpc://127.0.0.1:4317", "localhost:4317", "127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewRejectsSelfLoopHTTPAddr(t *testing.T) {
	_, err := New(context.Background(), "http://localhost:8080", "127.0.0.1:4317", "127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewGRPCSchemeConstructsClient(t *testing.T) {
	e, err := New(context.Background(), "grpc://collector.example:4317", "127.0.0.1:4317", "127.0.0.1:8080")
	require.NoError(t, err)
	assert.Equal(t, "otlp", e.Name())
	assert.NotNil(t, e.client)
}

func TestNewBareHostPortUsesGRPC(t *testing.T) {
	e, err := New(context.Background(), "collector.example:4317", "127.0.0.1:4318", "127.0.0.1:8081")
	require.NoError(t, err)
	assert.NotNil(t, e.client)
}

func TestNewHTTPSchemeConstructsClient(t *testing.T) {
	e, err := New(context.Background(), "https://collector.example:443", "127.0.0.1:4319", "127.0.0.1:8082")
	require.NoError(t, err)
	assert.NotNil(t, e.client)
}

func TestNewWithExporterUsesInjectedSeam(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	assert.Equal(t, "otlp", e.Name())
	assert.Same(t, spanExporter(rec), e.client)
}

func TestNormalizeAddrCanonicalisesLoopback(t *testing.T) {
	assert.Equal(t, normalizeAddr("grpc://localhost:4317"), normalizeAddr("127.0.0.1:4317"))
	assert.Equal(t, normalizeAddr("http://[::1]:8080"), normalizeAddr("localhost:8080"))
	assert.NotEqual(t, normalizeAddr("collector:4317"), normalizeAddr("127.0.0.1:4317"))
}

func TestNormalizeAddrNonLoopback(t *testing.T) {
	assert.Equal(t, "collector.example:4317", normalizeAddr("grpc://collector.example:4317"))
}

func TestNormalizeAddrBadHostPort(t *testing.T) {
	assert.Equal(t, "noport", normalizeAddr("noport"))
}

func TestErrExporterExists(t *testing.T) {
	e := &errExporter{}
	err := e.ExportSpans(context.Background(), nil)
	require.Error(t, err)
	err = e.Shutdown(context.Background())
	require.Error(t, err)
}

func TestRecordExporterRecordsShutdown(t *testing.T) {
	r := &recordExporter{}
	err := r.Shutdown(context.Background())
	require.NoError(t, err)
	assert.True(t, r.shutdown)
}

func TestRecordExporterRecordsBatches(t *testing.T) {
	r := &recordExporter{}
	err := r.ExportSpans(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, r.batches, 1)
}

func TestNormalizeAddrStripsPathAfterSlash(t *testing.T) {
	assert.Equal(t, normalizeAddr("collector.example:4317"), normalizeAddr("collector.example:4317/v1/traces"))
}

func TestNewLoopbackAliasMatchOnHTTP(t *testing.T) {
	_, err := New(context.Background(), "http://[::1]:8080", "127.0.0.1:4317", "localhost:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewFullPropagatesFactoryError(t *testing.T) {
	factoryErr := errors.New("factory failure")
	_, err := newFull(context.Background(), "grpc://collector.example:4317", "localhost:4317", "localhost:8080",
		func(_ context.Context, _ string) (spanExporter, error) {
			return nil, factoryErr
		},
	)
	require.ErrorIs(t, err, factoryErr)
}
