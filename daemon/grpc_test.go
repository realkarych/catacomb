package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/realkarych/catacomb/cdc"
	catacombv1 "github.com/realkarych/catacomb/gen/catacomb/v1"
	"github.com/realkarych/catacomb/model"
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

func TestServeStartsGRPC(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	token := "grpctoken"

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, token) }()

	conn, err := grpc.NewClient(grpcLn.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := collectorv1.NewTraceServiceClient(conn)
	rpcCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))

	require.Eventually(t, func() bool {
		_, e := client.Export(rpcCtx, &collectorv1.ExportTraceServiceRequest{})
		return e == nil
	}, 3*time.Second, 20*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
}

func TestDefaultWaitFn(t *testing.T) {
	require.True(t, defaultWaitFn(context.Background(), time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, defaultWaitFn(ctx, time.Hour))
}

func TestStreamBearerInterceptorValidToken(t *testing.T) {
	interceptor := streamBearerInterceptor("stream-tok")
	md := metadata.Pairs("authorization", "Bearer stream-tok")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &fakeServerStream{ctx: ctx}
	called := false
	err := interceptor(nil, ss, nil, func(_ any, _ grpc.ServerStream) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestStreamBearerInterceptorMissingMetadata(t *testing.T) {
	interceptor := streamBearerInterceptor("stream-tok")
	ss := &fakeServerStream{ctx: context.Background()}
	err := interceptor(nil, ss, nil, func(_ any, _ grpc.ServerStream) error {
		return nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestStreamBearerInterceptorWrongToken(t *testing.T) {
	interceptor := streamBearerInterceptor("stream-tok")
	md := metadata.Pairs("authorization", "Bearer wrong")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &fakeServerStream{ctx: ctx}
	err := interceptor(nil, ss, nil, func(_ any, _ grpc.ServerStream) error {
		return nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestStreamBearerInterceptorMissingAuthorizationKey(t *testing.T) {
	interceptor := streamBearerInterceptor("stream-tok")
	md := metadata.Pairs("other-key", "Bearer stream-tok")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &fakeServerStream{ctx: ctx}
	err := interceptor(nil, ss, nil, func(_ any, _ grpc.ServerStream) error {
		return nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func TestToProtoDeltaMapping(t *testing.T) {
	tStart := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	tEnd := time.Date(2025, 1, 2, 3, 4, 6, 0, time.UTC)
	durMS := int64(1000)
	tokIn := int64(10)
	tokOut := int64(20)
	cost := float64(0.001)

	n := &model.Node{
		ID:            "n1",
		RunID:         "run1",
		Type:          model.NodeToolCall,
		ParentID:      "p1",
		AgentID:       "a1",
		ParentAgentID: "pa1",
		SubagentType:  "code",
		Name:          "bash",
		Status:        model.StatusRunning,
		TStart:        &tStart,
		TEnd:          &tEnd,
		DurationMS:    &durMS,
		TokensIn:      &tokIn,
		TokensOut:     &tokOut,
		CostUSD:       &cost,
		PayloadHash:   "abc123",
		Tier:          "core",
		Rev:           7,
		Attrs:         map[string]any{"exit_code": float64(0)},
		Payload:       &model.Payload{Hash: "abc123"},
	}
	e := &model.Edge{
		ID:    "e1",
		RunID: "run1",
		Type:  model.EdgeParentChild,
		Src:   "n1",
		Dst:   "n2",
		Rev:   3,
		Attrs: map[string]any{"weight": "heavy"},
	}

	t.Run("node_upsert", func(t *testing.T) {
		d := cdc.GraphDelta{
			Kind:        cdc.DeltaNodeUpsert,
			Rev:         7,
			Node:        n,
			RunID:       "run1",
			ExecutionID: "exec1",
		}
		pd := toProtoDelta(d)
		require.Equal(t, "node_upsert", pd.Kind)
		require.Equal(t, uint64(7), pd.Rev)
		require.Equal(t, "run1", pd.RunId)
		require.Equal(t, "exec1", pd.ExecutionId)
		require.NotNil(t, pd.Node)
		require.Equal(t, "n1", pd.Node.Id)
		require.Equal(t, "run1", pd.Node.RunId)
		require.Equal(t, "tool_call", pd.Node.Type)
		require.Equal(t, "p1", pd.Node.ParentId)
		require.Equal(t, "a1", pd.Node.AgentId)
		require.Equal(t, "pa1", pd.Node.ParentAgentId)
		require.Equal(t, "code", pd.Node.SubagentType)
		require.Equal(t, "bash", pd.Node.Name)
		require.Equal(t, "running", pd.Node.Status)
		require.Equal(t, tStart.UnixMilli(), pd.Node.TStartMs)
		require.Equal(t, tEnd.UnixMilli(), pd.Node.TEndMs)
		require.Equal(t, int64(1000), pd.Node.DurationMs)
		require.Equal(t, int64(10), pd.Node.TokensIn)
		require.Equal(t, int64(20), pd.Node.TokensOut)
		require.InDelta(t, 0.001, pd.Node.CostUsd, 1e-9)
		require.Equal(t, "abc123", pd.Node.PayloadHash)
		require.Equal(t, "core", pd.Node.Tier)
		require.Equal(t, uint64(7), pd.Node.Rev)
		attrVal, ok := pd.Node.Attrs["exit_code"]
		require.True(t, ok)
		var decoded any
		require.NoError(t, json.Unmarshal([]byte(attrVal), &decoded))
		require.InDelta(t, float64(0), decoded, 1e-9)
		require.Nil(t, pd.Edge)
	})

	t.Run("payload_omitted", func(t *testing.T) {
		d := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: n}
		pd := toProtoDelta(d)
		require.NotNil(t, pd.Node)
		require.Equal(t, "abc123", pd.Node.PayloadHash)
	})

	t.Run("nil_node_timing", func(t *testing.T) {
		bare := &model.Node{ID: "n2", Rev: 1}
		d := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: bare}
		pd := toProtoDelta(d)
		require.Equal(t, int64(0), pd.Node.TStartMs)
		require.Equal(t, int64(0), pd.Node.TEndMs)
		require.Equal(t, int64(0), pd.Node.DurationMs)
		require.Equal(t, int64(0), pd.Node.TokensIn)
		require.Equal(t, int64(0), pd.Node.TokensOut)
		require.InDelta(t, 0.0, pd.Node.CostUsd, 1e-9)
	})

	t.Run("edge_upsert", func(t *testing.T) {
		d := cdc.GraphDelta{
			Kind:        cdc.DeltaEdgeUpsert,
			Rev:         3,
			Edge:        e,
			RunID:       "run1",
			ExecutionID: "exec1",
		}
		pd := toProtoDelta(d)
		require.Equal(t, "edge_upsert", pd.Kind)
		require.NotNil(t, pd.Edge)
		require.Equal(t, "e1", pd.Edge.Id)
		require.Equal(t, "run1", pd.Edge.RunId)
		require.Equal(t, "parent_child", pd.Edge.Type)
		require.Equal(t, "n1", pd.Edge.Src)
		require.Equal(t, "n2", pd.Edge.Dst)
		require.Equal(t, uint64(3), pd.Edge.Rev)
		attrVal, ok := pd.Edge.Attrs["weight"]
		require.True(t, ok)
		var decoded any
		require.NoError(t, json.Unmarshal([]byte(attrVal), &decoded))
		require.Equal(t, "heavy", decoded)
		require.Nil(t, pd.Node)
	})

	t.Run("node_merge_old_new_id", func(t *testing.T) {
		d := cdc.GraphDelta{
			Kind:  cdc.DeltaNodeMerge,
			OldID: "old1",
			NewID: "new1",
			RunID: "run1",
		}
		pd := toProtoDelta(d)
		require.Equal(t, "node_merge", pd.Kind)
		require.Equal(t, "old1", pd.OldId)
		require.Equal(t, "new1", pd.NewId)
	})

	t.Run("nil_attrs_produces_nil_map", func(t *testing.T) {
		bare := &model.Node{ID: "n3"}
		d := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: bare}
		pd := toProtoDelta(d)
		require.Nil(t, pd.Node.Attrs)
	})
}

const bufconnBufSize = 1 << 20

func newBufconnServer(t *testing.T, token string) (catacombv1.GraphServiceClient, *Daemon) {
	t.Helper()
	s := openTestStore(t)
	d := New(s)
	srv := d.newGRPCServer(token)
	lis := bufconn.Listen(bufconnBufSize)
	t.Cleanup(func() { _ = lis.Close() })
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.GracefulStop)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return catacombv1.NewGraphServiceClient(conn), d
}

func authCtx(token string) context.Context {
	return metadata.NewOutgoingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+token),
	)
}

func TestSubscribeUnauthenticated(t *testing.T) {
	client, _ := newBufconnServer(t, "secret")
	stream, err := client.Subscribe(context.Background(), &catacombv1.SubscribeRequest{})
	if err == nil {
		_, err = stream.Recv()
	}
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestSubscribeSnapshotAndLiveDelta(t *testing.T) {
	client, d := newBufconnServer(t, "tok")
	fixedExecID(d)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	stream, err := client.Subscribe(authCtx("tok"), &catacombv1.SubscribeRequest{})
	require.NoError(t, err)

	first, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "node_upsert", first.Kind)

	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	second, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "node_upsert", second.Kind)
}

func TestSubscribeContextCancelUnsubscribes(t *testing.T) {
	client, d := newBufconnServer(t, "tok")
	baseline := d.busConsumerCountForTest()

	ctx, cancel := context.WithCancel(authCtx("tok"))
	stream, err := client.Subscribe(ctx, &catacombv1.SubscribeRequest{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return d.busConsumerCountForTest() == baseline+1
	}, 3*time.Second, 10*time.Millisecond)

	cancel()

	require.Eventually(t, func() bool {
		return d.busConsumerCountForTest() == baseline
	}, 3*time.Second, 10*time.Millisecond)

	_, err = stream.Recv()
	require.Error(t, err)
}

func TestSubscribeFilterDropsNonMatchingRun(t *testing.T) {
	client, d := newBufconnServer(t, "filter-tok")
	fixedExecID(d)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s-A"}`)))

	stream, err := client.Subscribe(authCtx("filter-tok"), &catacombv1.SubscribeRequest{RunId: d.execBySession["s-A"]})
	require.NoError(t, err)

	first, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "node_upsert", first.Kind)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s-B"}`)))

	recvCh := make(chan *catacombv1.GraphDelta, 4)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			recvCh <- msg
		}
	}()

	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case ev := <-recvCh:
		t.Fatalf("expected no events for filtered run, got: %v", ev)
	case <-timer.C:
	}
}

func TestSubscribeSnapshotSendError(t *testing.T) {
	_, d := newBufconnServer(t, "tok")
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	gs := &graphServer{d: d}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &fakeSubscribeStream{
		ctx:     ctx,
		sendErr: io.ErrClosedPipe,
	}

	err := gs.Subscribe(&catacombv1.SubscribeRequest{}, stream)
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestEncodeAttrsJSONMarshalError(t *testing.T) {
	attrs := map[string]any{
		"ok":  "value",
		"bad": make(chan int),
	}
	result := encodeAttrs(attrs)
	require.Equal(t, "\"value\"", result["ok"])
	require.Equal(t, "", result["bad"])
}

func TestStreamLoopConsumerChannelClosed(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	gs := &graphServer{d: d}

	sub := d.SubscribeFiltered(SubFilter{}, 4)
	d.Unsubscribe(sub)

	stream := &fakeSubscribeStream{ctx: context.Background()}
	err := gs.streamLoop(sub, SubFilter{}, stream)
	require.NoError(t, err)
}

func TestStreamLoopSendError(t *testing.T) {
	s := openTestStore(t)
	d := New(s)
	gs := &graphServer{d: d}

	sub := d.SubscribeFiltered(SubFilter{}, 4)
	defer d.Unsubscribe(sub)

	sentErr := errors.New("send failed")
	stream := &fakeSubscribeStream{
		ctx: context.Background(),
		sendFn: func(_ *catacombv1.GraphDelta) error {
			return sentErr
		},
	}

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	err := gs.streamLoop(sub, SubFilter{}, stream)
	require.ErrorIs(t, err, sentErr)
}

type fakeSubscribeStream struct {
	catacombv1.GraphService_SubscribeServer
	ctx     context.Context
	sendErr error
	sendFn  func(*catacombv1.GraphDelta) error
}

func (f *fakeSubscribeStream) Context() context.Context { return f.ctx }

func (f *fakeSubscribeStream) Send(delta *catacombv1.GraphDelta) error {
	if f.sendFn != nil {
		return f.sendFn(delta)
	}
	return f.sendErr
}
