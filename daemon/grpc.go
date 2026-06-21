package daemon

import (
	"context"
	"crypto/subtle"
	"net"
	"time"

	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type traceServer struct {
	collectorv1.UnimplementedTraceServiceServer
	d *Daemon
}

func (s *traceServer) Export(_ context.Context, req *collectorv1.ExportTraceServiceRequest) (*collectorv1.ExportTraceServiceResponse, error) {
	_ = s.d.IngestOTLP(req)
	return &collectorv1.ExportTraceServiceResponse{}, nil
}

func bearerInterceptor(token string) grpc.UnaryServerInterceptor {
	want := []byte("Bearer " + token)
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		vals := md.Get("authorization")
		if len(vals) == 0 || subtle.ConstantTimeCompare([]byte(vals[0]), want) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return handler(ctx, req)
	}
}

const (
	grpcBackoffBase   = 100 * time.Millisecond
	grpcBackoffFactor = 2
	grpcBackoffCap    = 30 * time.Second
)

func (d *Daemon) newGRPCServer(token string) *grpc.Server {
	srv := grpc.NewServer(grpc.UnaryInterceptor(bearerInterceptor(token)))
	collectorv1.RegisterTraceServiceServer(srv, &traceServer{d: d})
	return srv
}

func (d *Daemon) serveGRPC(
	ctx context.Context,
	srv *grpc.Server,
	ln net.Listener,
	serveFn func(*grpc.Server, net.Listener) error,
	waitFn func(ctx context.Context, d time.Duration) bool,
) {
	backoff := grpcBackoffBase
	for {
		if ctx.Err() != nil {
			return
		}
		var serveErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					serveErr = status.Errorf(codes.Internal, "panic: %v", r)
				}
			}()
			serveErr = serveFn(srv, ln)
		}()
		if ctx.Err() != nil {
			return
		}
		if serveErr != nil {
			if !waitFn(ctx, backoff) {
				return
			}
			if backoff*grpcBackoffFactor < grpcBackoffCap {
				backoff *= grpcBackoffFactor
			} else {
				backoff = grpcBackoffCap
			}
		}
	}
}
