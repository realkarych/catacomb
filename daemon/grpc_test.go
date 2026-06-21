package daemon

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/realkarych/catacomb/store"
)

func openTestStore(t *testing.T) store.Store {
	t.Helper()
	return tempStore(t)
}

func TestExportRoutesToDaemon(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	ts := &traceServer{d: d}
	req := &collectorv1.ExportTraceServiceRequest{}
	resp, err := ts.Export(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestExportNilReqDoesNotPanic(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	ts := &traceServer{d: d}
	resp, err := ts.Export(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBearerInterceptorValidToken(t *testing.T) {
	interceptor := bearerInterceptor("tok-A")
	md := metadata.Pairs("authorization", "Bearer tok-A")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	called := false
	_, err := interceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestBearerInterceptorMissingMetadata(t *testing.T) {
	interceptor := bearerInterceptor("secret")
	_, err := interceptor(context.Background(), nil, nil, func(ctx context.Context, req any) (any, error) {
		return nil, nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestBearerInterceptorEmptyToken(t *testing.T) {
	interceptor := bearerInterceptor("secret")
	md := metadata.Pairs("authorization", "")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := interceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		return nil, nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestBearerInterceptorWrongToken(t *testing.T) {
	interceptor := bearerInterceptor("tok-B")
	md := metadata.Pairs("authorization", "Bearer nope")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := interceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		return nil, nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestBearerInterceptorMissingAuthorizationKey(t *testing.T) {
	interceptor := bearerInterceptor("secret")
	md := metadata.Pairs("other-key", "Bearer secret")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := interceptor(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		return nil, nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}
