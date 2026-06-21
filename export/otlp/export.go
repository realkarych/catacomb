package otlp

import (
	"context"
	"fmt"
	"net"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type spanExporter interface {
	ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
	Shutdown(ctx context.Context) error
}

type Exporter struct {
	client spanExporter
}

func New(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string) (*Exporter, error) {
	return newFull(ctx, endpoint, daemonGRPCAddr, daemonHTTPAddr, newClient)
}

func newFull(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string, factory func(context.Context, string) (spanExporter, error)) (*Exporter, error) {
	norm := normalizeAddr(endpoint)
	if norm == normalizeAddr(daemonGRPCAddr) || norm == normalizeAddr(daemonHTTPAddr) {
		return nil, fmt.Errorf("otlp exporter: endpoint %q targets the daemon's own receiver — self-loop refused", endpoint)
	}
	client, err := factory(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return newWithExporter(client), nil
}

func newWithExporter(exp spanExporter) *Exporter {
	return &Exporter{client: exp}
}

func (e *Exporter) Name() string { return "otlp" }

func newClient(ctx context.Context, endpoint string) (spanExporter, error) {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	}
	host := strings.TrimPrefix(endpoint, "grpc://")
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(host), otlptracegrpc.WithInsecure())
}

func normalizeAddr(addr string) string {
	a := addr
	for _, p := range []string{"grpc://", "http://", "https://"} {
		a = strings.TrimPrefix(a, p)
	}
	if i := strings.IndexByte(a, '/'); i >= 0 {
		a = a[:i]
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil {
		return a
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]", "":
		host = "loopback"
	}
	return host + ":" + port
}
