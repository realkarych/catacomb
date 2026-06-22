package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"time"

	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/realkarych/catacomb/cdc"
	catacombv1 "github.com/realkarych/catacomb/gen/catacomb/v1"
	"github.com/realkarych/catacomb/model"
)

var defaultWaitFn = func(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

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

func streamBearerInterceptor(token string) grpc.StreamServerInterceptor {
	want := []byte("Bearer " + token)
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		vals := md.Get("authorization")
		if len(vals) == 0 || subtle.ConstantTimeCompare([]byte(vals[0]), want) != 1 {
			return status.Error(codes.Unauthenticated, "invalid token")
		}
		return handler(srv, ss)
	}
}

type graphServer struct {
	catacombv1.UnimplementedGraphServiceServer
	d *Daemon
}

func (g *graphServer) Subscribe(req *catacombv1.SubscribeRequest, stream catacombv1.GraphService_SubscribeServer) error {
	f := SubFilter{
		RunID:     req.GetRunId(),
		NodeTypes: req.GetNodeTypes(),
		Tiers:     req.GetTiers(),
	}
	sub := g.d.SubscribeFiltered(f, subBufSize)
	defer g.d.Unsubscribe(sub)
	for _, snap := range sub.Snapshot {
		if err := stream.Send(toProtoDelta(snap)); err != nil {
			return err
		}
	}
	return g.streamLoop(sub, f, stream)
}

func (g *graphServer) streamLoop(sub *Subscription, f SubFilter, stream catacombv1.GraphService_SubscribeServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case delta, ok := <-sub.Consumer.C:
			if !ok {
				return nil
			}
			if !matchDelta(f, delta) {
				continue
			}
			if err := stream.Send(toProtoDelta(delta)); err != nil {
				return err
			}
		}
	}
}

func toProtoDelta(d cdc.GraphDelta) *catacombv1.GraphDelta {
	pd := &catacombv1.GraphDelta{
		Kind:        string(d.Kind),
		Rev:         d.Rev,
		OldId:       d.OldID,
		NewId:       d.NewID,
		RunId:       d.RunID,
		ExecutionId: d.ExecutionID,
	}
	if d.Node != nil {
		pd.Node = toProtoNode(d.Node)
	}
	if d.Edge != nil {
		pd.Edge = toProtoEdge(d.Edge)
	}
	return pd
}

func toProtoNode(n *model.Node) *catacombv1.Node {
	pn := &catacombv1.Node{
		Id:            n.ID,
		RunId:         n.RunID,
		Type:          string(n.Type),
		ParentId:      n.ParentID,
		AgentId:       n.AgentID,
		ParentAgentId: n.ParentAgentID,
		SubagentType:  n.SubagentType,
		Name:          n.Name,
		Status:        string(n.Status),
		PayloadHash:   n.PayloadHash,
		Tier:          n.Tier,
		Rev:           n.Rev,
	}
	if n.TStart != nil {
		pn.TStartMs = n.TStart.UnixMilli()
	}
	if n.TEnd != nil {
		pn.TEndMs = n.TEnd.UnixMilli()
	}
	if n.DurationMS != nil {
		pn.DurationMs = *n.DurationMS
	}
	if n.TokensIn != nil {
		pn.TokensIn = *n.TokensIn
	}
	if n.TokensOut != nil {
		pn.TokensOut = *n.TokensOut
	}
	if n.CostUSD != nil {
		pn.CostUsd = *n.CostUSD
	}
	if len(n.Attrs) > 0 {
		pn.Attrs = encodeAttrs(n.Attrs)
	}
	return pn
}

func toProtoEdge(e *model.Edge) *catacombv1.Edge {
	pe := &catacombv1.Edge{
		Id:    e.ID,
		RunId: e.RunID,
		Type:  string(e.Type),
		Src:   e.Src,
		Dst:   e.Dst,
		Rev:   e.Rev,
	}
	if len(e.Attrs) > 0 {
		pe.Attrs = encodeAttrs(e.Attrs)
	}
	return pe
}

func encodeAttrs(attrs map[string]any) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		b, err := json.Marshal(v)
		if err != nil {
			out[k] = ""
			continue
		}
		out[k] = string(b)
	}
	return out
}

func (d *Daemon) newGRPCServer(token string) *grpc.Server {
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(bearerInterceptor(token)),
		grpc.StreamInterceptor(streamBearerInterceptor(token)),
	)
	collectorv1.RegisterTraceServiceServer(srv, &traceServer{d: d})
	catacombv1.RegisterGraphServiceServer(srv, &graphServer{d: d})
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
