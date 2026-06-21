package daemon

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

func TestServeGRPCCtxAlreadyCancelledOnEntry(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer("tok-A")
	ln := loopbackListener(t)
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	serveFn := func(_ *grpc.Server, _ net.Listener) error {
		calls++
		return nil
	}
	waits := 0
	waitFn := func(_ context.Context, _ time.Duration) bool {
		waits++
		return true
	}
	d.serveGRPC(ctx, srv, ln, serveFn, waitFn)
	require.Equal(t, 0, calls)
	require.Equal(t, 0, waits)
}

func TestServeGRPCErrorBackoffAndStop(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer("tok-B")
	ln := loopbackListener(t)
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	serveFn := func(_ *grpc.Server, _ net.Listener) error {
		calls++
		if calls >= 3 {
			cancel()
			return nil
		}
		return errors.New("serve error")
	}
	var waited []time.Duration
	waitFn := func(wctx context.Context, dur time.Duration) bool {
		waited = append(waited, dur)
		select {
		case <-wctx.Done():
			return false
		default:
			return true
		}
	}
	d.serveGRPC(ctx, srv, ln, serveFn, waitFn)
	require.Equal(t, 3, calls)
	require.Len(t, waited, 2)
	require.Equal(t, 100*time.Millisecond, waited[0])
	require.Equal(t, 200*time.Millisecond, waited[1])
}

func TestServeGRPCPanicRecoveredAndRestarts(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer("tok-A")
	ln := loopbackListener(t)
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	serveFn := func(_ *grpc.Server, _ net.Listener) error {
		calls++
		if calls == 1 {
			panic("boom")
		}
		cancel()
		return errors.New("done")
	}
	waitFn := func(wctx context.Context, _ time.Duration) bool {
		select {
		case <-wctx.Done():
			return false
		default:
			return true
		}
	}
	require.NotPanics(t, func() {
		d.serveGRPC(ctx, srv, ln, serveFn, waitFn)
	})
	require.Equal(t, 2, calls)
}

func TestServeGRPCWaitCancelledAborts(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer("tok-B")
	ln := loopbackListener(t)
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	serveFn := func(_ *grpc.Server, _ net.Listener) error {
		calls++
		return errors.New("err")
	}
	waitFn := func(_ context.Context, _ time.Duration) bool {
		cancel()
		return false
	}
	d.serveGRPC(ctx, srv, ln, serveFn, waitFn)
	require.Equal(t, 1, calls)
}

func TestServeGRPCGracefulStop(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer("tok-A")
	ln := loopbackListener(t)

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	var readyOnce sync.Once
	serveFn := func(_ *grpc.Server, _ net.Listener) error {
		readyOnce.Do(func() { close(ready) })
		<-ctx.Done()
		return errors.New("stopped")
	}
	waitFn := func(_ context.Context, _ time.Duration) bool { return true }
	done := make(chan struct{})
	go func() {
		d.serveGRPC(ctx, srv, ln, serveFn, waitFn)
		close(done)
	}()
	<-ready
	cancel()
	<-done
}

func TestServeGRPCBackoffCaps(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer("tok-B")
	ln := loopbackListener(t)
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	serveFn := func(_ *grpc.Server, _ net.Listener) error {
		calls++
		if calls == 12 {
			cancel()
			return nil
		}
		return errors.New("err")
	}
	var waited []time.Duration
	waitFn := func(wctx context.Context, dur time.Duration) bool {
		waited = append(waited, dur)
		select {
		case <-wctx.Done():
			return false
		default:
			return true
		}
	}
	d.serveGRPC(ctx, srv, ln, serveFn, waitFn)
	require.Equal(t, 12, calls)
	require.Len(t, waited, 11)
	require.Equal(t, 25600*time.Millisecond, waited[8])
	require.Equal(t, 30*time.Second, waited[9])
	require.Equal(t, 30*time.Second, waited[10])
}

func loopbackListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}
