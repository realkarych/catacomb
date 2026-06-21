# M1a-grpc — OTLP/gRPC receiver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Complete the M1a spec scope by adding the OTLP/gRPC transport. M1a delivered the HTTP receiver (`POST /v1/traces`), `ingest/otel.Parse`, `Daemon.IngestOTLP`, `catacomb env`, and `model.Edge.Rev`. M1a-grpc wires a second loopback gRPC listener whose `TraceServiceServer.Export` routes to the existing `Daemon.IngestOTLP` — no reducer change, no `ingest/otel` change. It also adds `otlp_grpc_addr` to the discovery file and a `--protocol grpc` flag to `catacomb env`.

**Architecture:** A new `daemon/grpc.go` houses `traceServer` (implementing `collectorv1.TraceServiceServer`), the bearer unary interceptor, and the `serveGRPC` supervisor. `Daemon.Serve` is extended to accept a second `net.Listener` for gRPC and launch the supervisor goroutine. `runDaemonWith` in `cmd/catacomb/daemon.go` calls the `listen` seam twice, writes both addresses to an extended `Discovery` struct, and passes the gRPC listener into `Serve`. `catacomb env` gains `--protocol http|grpc` (default `http`, backward-compatible). The `Export` handler mirrors `handleOTLP`'s fail-open contract exactly: `IngestOTLP` recovers/quarantines internally and always returns `nil`, so `Export` always returns `(&collectorv1.ExportTraceServiceResponse{}, nil)`.

**Tech Stack:** Go 1.26, `google.golang.org/grpc v1.81.1` (already in `go.mod` indirect), `go.opentelemetry.io/proto/otlp v1.10.0` (already indirect), `google.golang.org/protobuf v1.36.11` (already indirect), pure-Go, `testify`.

## Global Constraints

- Go 1.26 pure-Go (modernc.org/sqlite, no cgo); **NO comments** in Go except `//go:build|//go:embed|//go:generate` (`internal/codepolicy`).
- **100% line coverage** under `-race` (`make cover`); `golangci-lint v2` clean (gofumpt, goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, **forbidigo bans `time.Sleep`**, unparam, errcheck, rowserrcheck, bodyclose).
- **NEVER `go mod tidy`** (grpc/proto deps already present as indirect; promote to direct with `go get` if the build demands it).
- Single-mutex daemon discipline (`d.mu`); time via `nowFn` seam, no `time.Sleep` (use timers / `require.Eventually`).
- Cross-platform: loopback TCP only, no unix sockets, no `0600`-mode asserts in tests; close listeners/store before `t.TempDir` cleanup (Windows).
- All test files white-box (same package as the code under test).
- Observation log is the system of record. Step 7 (live field-name verification) is deferred operator testing — NOT a task here.

## Interfaces consumed (existing, read from source before coding)

- `Daemon.IngestOTLP(req *collectorv1.ExportTraceServiceRequest) error` — declared at `daemon/daemon.go:156`. Signature: always returns `nil` (fail-open: recover/quarantine is internal). The `Export` handler calls this and ignores the error return, matching `handleOTLP` at `daemon/server.go:43` which does `_ = d.IngestOTLP(&req)`.
- `daemon.Serve(ctx context.Context, ln net.Listener, token string) error` — `daemon/server.go:89`. To be extended to `Serve(ctx, httpLn, grpcLn, token)`.
- `daemon.Discovery{Addr string, Token string}` — `daemon/discovery.go:13`. To gain `GRPCAddr string`.
- `daemon.WriteDiscovery(path string, d Discovery) error`, `daemon.ReadDiscovery(path string) (Discovery, error)` — `daemon/discovery.go:55,69`.
- `runDaemonWith(ctx, open, listen, newToken, dbPath, discoveryPath, reaperWindow, maxShards)` — `cmd/catacomb/daemon.go:40`. To be extended with a second `listenGRPC func() (net.Listener, error)` seam.
- `collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"` — `ExportTraceServiceRequest`, `ExportTraceServiceResponse`, `TraceServiceServer`, `RegisterTraceServiceServer`, `UnimplementedTraceServiceServer`.
- `google.golang.org/grpc` — `grpc.NewServer`, `grpc.UnaryServerInterceptor`, `grpc.UnaryInterceptor`, `grpc.Server.Serve`, `grpc.Server.GracefulStop`.
- `google.golang.org/grpc/codes`, `google.golang.org/grpc/status`, `google.golang.org/grpc/metadata`.

---

## Task 1: `traceServer.Export` handler + bearer unary interceptor

**Files:**

- Create `daemon/grpc.go`
- Create `daemon/grpc_test.go`

**Interfaces:**

Consumes:

- `Daemon.IngestOTLP(req *collectorv1.ExportTraceServiceRequest) error` (`daemon/daemon.go:156`)

Produces:

- `traceServer` struct embedding `collectorv1.UnimplementedTraceServiceServer` with method:

  ```go
  func (s *traceServer) Export(ctx context.Context, req *collectorv1.ExportTraceServiceRequest) (*collectorv1.ExportTraceServiceResponse, error)
  ```

- `bearerInterceptor(token string) grpc.UnaryServerInterceptor`

**TDD steps:**

- [ ] Write failing tests in `daemon/grpc_test.go`:

```go
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
    interceptor := bearerInterceptor("secret")
    md := metadata.Pairs("authorization", "Bearer secret")
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
    interceptor := bearerInterceptor("secret")
    md := metadata.Pairs("authorization", "Bearer wrong")
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
```

Helper `openTestStore` reuses the pattern from existing `daemon_test.go` — open an in-memory or `t.TempDir()` SQLite store; add it at the top of `grpc_test.go` if it does not already exist as a package-level helper.

- [ ] Run `go test ./daemon/ -run TestExport -run TestBearer` → FAIL (`traceServer`, `bearerInterceptor` undefined).

- [ ] Implement in `daemon/grpc.go`:

```go
package daemon

import (
    "context"
    "crypto/subtle"

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
```

- [ ] Run → PASS; `make cover` 100% for `daemon` package; `make lint` 0.

- [ ] Commit: `feat(daemon): gRPC Export handler + bearer unary interceptor`.

---

## Task 2: `serveGRPC` supervisor with seams, recover, and exponential backoff

**Files:**

- Modify `daemon/grpc.go` (add `serveGRPC` + `waitFn` type)
- Modify `daemon/grpc_test.go` (add supervisor tests)

**Interfaces:**

Produces:

```go
func (d *Daemon) newGRPCServer(token string) *grpc.Server
func (d *Daemon) serveGRPC(
    ctx context.Context,
    srv *grpc.Server,
    ln net.Listener,
    serveFn func(*grpc.Server, net.Listener) error,
    waitFn func(ctx context.Context, d time.Duration) bool,
)
```

`serveFn` default: `func(srv *grpc.Server, ln net.Listener) error { return srv.Serve(ln) }`
`waitFn` default: timer-based — creates `time.NewTimer(d)`, selects on `timer.C` vs `ctx.Done()`; returns `true` if the timer fired (continue), `false` if context was cancelled (abort).

**Design invariants:**

- Backoff: base 100ms, factor 2, cap 30s. The backoff counter resets to base only when `ctx` is cancelled (i.e., it grows until the supervisor exits). This is the simplest correct rule that keeps the counter monotone within a supervisor lifetime.
- On `ctx.Done()`: call `srv.GracefulStop()`, then return.
- On panic inside `serveFn`: `recover()` catches it; count the restart, back off, loop.
- On `serveFn` returning an error (non-nil): treat the same as a panic — back off, loop.
- If `ctx` is already done on entry: return immediately without calling `serveFn`.

**TDD steps:**

- [ ] Write failing tests in `daemon/grpc_test.go`:

```go
func TestServeGRPCCtxAlreadyCancelledOnEntry(t *testing.T) {
    s := openTestStore(t)
    d := New(s)
    srv := d.newGRPCServer("tok")
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
    srv := d.newGRPCServer("tok")
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
    srv := d.newGRPCServer("tok")
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
        return nil
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
    srv := d.newGRPCServer("tok")
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
    srv := d.newGRPCServer("tok")
    ln := loopbackListener(t)

    ctx, cancel := context.WithCancel(context.Background())
    ready := make(chan struct{})
    serveFn := func(_ *grpc.Server, _ net.Listener) error {
        close(ready)
        <-ctx.Done()
        return nil
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
    srv := d.newGRPCServer("tok")
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
```

The `waited` sequence is `100ms, 200, 400, 800, 1600, 3200, 6400, 12800, 25600, 30000, 30000` — index 8 is the last pre-cap value (`25600ms`), index 9 is the first capped value (`25600*2 = 51200 ≥ 30000` → `30s`), index 10 confirms it stays capped. This exercises the `else { backoff = grpcBackoffCap }` branch and the cap-stable case. (`errors` must be in the test imports.)

- [ ] Run → FAIL (`newGRPCServer`, `serveGRPC` undefined).

- [ ] Implement in `daemon/grpc.go` (append after `bearerInterceptor`):

```go
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

// ... traceServer and bearerInterceptor from Task 1 ...

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
```

**Lifecycle ownership (correctness):** `serveGRPC` does NOT call `srv.GracefulStop()` — it only supervises (recover + backoff-restart). The grpc server's lifecycle is owned by `Serve` (Task 3), whose shutdown goroutine calls `grpcSrv.GracefulStop()` on `ctx.Done()`. That is what makes the real `serveFn` (`srv.Serve(ln)`) return so the supervisor loop sees `ctx.Err() != nil` and exits. If `serveGRPC` called `GracefulStop` itself after `serveFn` returned, it would deadlock the real path (Serve never returns until GracefulStop is called from elsewhere). A `serveFn` returning `nil` while ctx is not done loops immediately without backing off (the `serveErr != nil` guard is false) — harmless for the fake-seam tests; the real path only returns `nil` from `srv.Serve` after GracefulStop, which coincides with `ctx.Err() != nil`.

- [ ] Run → PASS; `make cover` 100% for `daemon` package; `make lint` 0.

- [ ] Commit: `feat(daemon): serveGRPC supervisor with seams, recover, exponential backoff`.

---

## Task 3: Second listener, `Discovery.GRPCAddr`, `Serve` extension, `runDaemonWith` wiring

**Files:**

- Modify `daemon/discovery.go` — add `GRPCAddr string \`json:"grpc_addr,omitempty"\`` to `Discovery`
- Modify `daemon/server.go` — extend `Serve` signature to `Serve(ctx, httpLn, grpcLn net.Listener, token string) error`
- Modify `cmd/catacomb/daemon.go` — add `listenGRPC` seam parameter, bind second listener, write `GRPCAddr`, call extended `Serve`
- Modify `daemon/grpc.go` — `defaultServeFn`, `defaultWaitFn` helpers (package-level vars for testability if needed)
- Modify `daemon/grpc_test.go` — add `TestServeStartsGRPC`
- Modify `cmd/catacomb/daemon_test.go` — update all `runDaemonWith` call sites with the new seam; add `TestRunDaemonWithGRPCListenError`; assert `GRPCAddr` in discovery
- Modify existing tests that construct `daemon.Discovery{...}` directly — add `GRPCAddr` only where the test needs it; existing tests that omit it will still compile because `GRPCAddr` has `omitempty` and Go zero-value is `""`

**Interfaces:**

Produces:

- `daemon.Discovery{Addr string \`json:"addr"\`, Token string \`json:"token"\`, GRPCAddr string \`json:"grpc_addr,omitempty"\`}`
- `func (d *Daemon) Serve(ctx context.Context, httpLn net.Listener, grpcLn net.Listener, token string) error`
- Updated `runDaemonWith` signature:

  ```go
  func runDaemonWith(
      ctx context.Context,
      open func(string) (store.Store, error),
      listen func() (net.Listener, error),
      listenGRPC func() (net.Listener, error),
      newToken func() (string, error),
      dbPath, discoveryPath string,
      reaperWindow time.Duration,
      maxShards int,
  ) error
  ```

**TDD steps:**

- [ ] Write failing tests:

In `daemon/grpc_test.go`, add a network-level smoke test:

```go
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
```

`TestServeStartsGRPC` makes a real authenticated `Export` RPC over the wire: this proves the gRPC server is serving (deterministically covering the inline `s.Serve(l)` closure in `Serve` and the interceptor's success path), unlike a lazy `grpc.NewClient` readiness check which connects lazily and would leave the closure coverage racy. `TestDefaultWaitFn` covers both branches of `defaultWaitFn` directly (timer-fires → `true`; ctx-cancelled → `false`) — `Serve`'s happy path never hits a serve-error, so the supervisor never calls `waitFn` there. Imports needed in `grpc_test.go`: `google.golang.org/grpc/credentials/insecure`, `google.golang.org/grpc/metadata` (already used in Task 1).

In `cmd/catacomb/daemon_test.go`, update all existing `runDaemonWith` call sites to include the new `daemon.ListenLoopback` seam argument (second position after `listen`). Add:

```go
func TestRunDaemonWithGRPCListenError(t *testing.T) {
    listenGRPC := func() (net.Listener, error) { return nil, errors.New("grpc listen") }
    err := runDaemonWith(
        context.Background(),
        store.OpenSQLite,
        daemon.ListenLoopback,
        listenGRPC,
        daemon.NewToken,
        filepath.Join(t.TempDir(), "g.db"),
        filepath.Join(t.TempDir(), "d.json"),
        30*time.Minute, 4096,
    )
    require.Error(t, err)
}

func TestRunDaemonDiscoveryHasGRPCAddr(t *testing.T) {
    dir := t.TempDir()
    discovery := filepath.Join(dir, "d.json")
    ctx, cancel := context.WithCancel(context.Background())
    errc := make(chan error, 1)
    go func() {
        errc <- runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken, filepath.Join(dir, "g.db"), discovery, 30*time.Minute, 4096)
    }()
    var grpcAddr string
    require.Eventually(t, func() bool {
        d, err := daemon.ReadDiscovery(discovery)
        if err != nil || d.GRPCAddr == "" {
            return false
        }
        grpcAddr = d.GRPCAddr
        return true
    }, 2*time.Second, 10*time.Millisecond)
    require.NotEmpty(t, grpcAddr)
    cancel()
    require.NoError(t, <-errc)
}
```

- [ ] Run → FAIL (`Serve` wrong arity, `runDaemonWith` wrong arity, `GRPCAddr` undefined).

- [ ] **Implement:**

`daemon/discovery.go` — add field:

```go
type Discovery struct {
    Addr     string `json:"addr"`
    Token    string `json:"token"`
    GRPCAddr string `json:"grpc_addr,omitempty"`
}
```

`daemon/server.go` — extend `Serve`. **Read the real current `Serve` first** and integrate these additions while preserving its existing structure (the `reapLoop` goroutine, the `http.ErrServerClosed` handling, etc.). The two required additions: (1) create `grpcSrv` and launch the `serveGRPC` supervisor goroutine; (2) the shutdown goroutine that already calls `srv.Close()` on `ctx.Done()` must ALSO call `grpcSrv.GracefulStop()` — this is what makes the real `srv.Serve(grpcLn)` return so the supervisor exits (do NOT put `GracefulStop` inside `serveGRPC`).

```go
func (d *Daemon) Serve(ctx context.Context, httpLn net.Listener, grpcLn net.Listener, token string) error {
    srv := &http.Server{Handler: d.Handler(token)}
    grpcSrv := d.newGRPCServer(token)
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()
    go d.reapLoop(ctx)
    go func() {
        <-ctx.Done()
        _ = srv.Close()
        grpcSrv.GracefulStop()
    }()
    go d.serveGRPC(ctx, grpcSrv, grpcLn, func(s *grpc.Server, l net.Listener) error {
        return s.Serve(l)
    }, defaultWaitFn)
    if err := srv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
        return err
    }
    return nil
}
```

`daemon/grpc.go` — add `defaultWaitFn`:

```go
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
```

`cmd/catacomb/daemon.go` — extend `runDaemonWith`:

```go
func runDaemonWith(
    ctx context.Context,
    open func(string) (store.Store, error),
    listen func() (net.Listener, error),
    listenGRPC func() (net.Listener, error),
    newToken func() (string, error),
    dbPath, discoveryPath string,
    reaperWindow time.Duration,
    maxShards int,
) error {
    s, err := open(dbPath)
    if err != nil {
        return err
    }
    defer func() { _ = s.Close() }()

    d := daemon.New(s)
    d.SetReaperWindow(reaperWindow)
    d.SetMaxShards(maxShards)
    if err := d.Recover(); err != nil {
        return err
    }
    token, err := newToken()
    if err != nil {
        return err
    }
    ln, err := listen()
    if err != nil {
        return err
    }
    defer func() { _ = ln.Close() }()

    grpcLn, err := listenGRPC()
    if err != nil {
        return err
    }
    defer func() { _ = grpcLn.Close() }()

    disc := daemon.Discovery{
        Addr:     ln.Addr().String(),
        Token:    token,
        GRPCAddr: grpcLn.Addr().String(),
    }
    if err := daemon.WriteDiscovery(discoveryPath, disc); err != nil {
        return err
    }
    return d.Serve(ctx, ln, grpcLn, token)
}
```

`cmd/catacomb/daemon.go` — update `newDaemonCmd` to pass `daemon.ListenLoopback` as the second listen seam:

```go
return runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken, dbPath, discoveryPath, reaperWindow, maxShards)
```

- [ ] Update ALL existing `runDaemonWith` call sites in `cmd/catacomb/daemon_test.go` by inserting the `listenGRPC` seam argument in the new position (grep `runDaemonWith(` to find every one; do not rely on a hard-coded count). For an open-error/recover-error/new-token-error case that returns before binding the gRPC listener, pass `daemon.ListenLoopback` (it simply won't be reached). The cobra tests that drive the command via `root.ExecuteContext` do not call `runDaemonWith` directly and require no change.
- [ ] Update ALL existing `Serve(...)` call sites (grep `.Serve(` in `daemon/server_test.go` and anywhere else) to pass the new `grpcLn` argument — the signature change from `Serve(ctx, ln, token)` to `Serve(ctx, httpLn, grpcLn, token)` breaks them at compile time. Give each a real `loopbackListener(t)` (a closed/again-loopback listener is fine for HTTP-only assertions, but the supervisor goroutine will try to serve on it, so use a live loopback listener and let ctx-cancel stop it).

- [ ] Run → PASS; `make cover` 100%; `make lint` 0; `make build`; `GOOS=windows go build ./...`.

- [ ] Commit: `feat(daemon): second gRPC listener + Discovery.GRPCAddr + Serve/runDaemonWith wiring`.

---

## Task 4: `catacomb env --protocol grpc` flag

**Files:**

- Modify `cmd/catacomb/env.go` — add `--protocol` flag; `grpc` branch emits gRPC vars; `http` branch unchanged
- Modify `cmd/catacomb/env_test.go` — add gRPC output tests + missing `GRPCAddr` error test; keep existing HTTP tests passing (they use `Discovery{Addr: ..., Token: ...}` with no `GRPCAddr`, so `--protocol http` is unaffected)

**Before editing, read the current `cmd/catacomb/env.go`** (M1a shipped it). The `http` branch output and the discovery-path resolution must stay byte-identical to what M1a emits (the existing `TestEnvCmd`/`TestEnvCmdDefaultDiscovery` assertions depend on it — notably M1a's HTTP output does NOT include `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA`). The code block below is the shape; reconcile it with the real M1a code (especially the default-discovery resolution helper name) rather than assuming `daemon.DiscoveryPath()` exists. Only ADD the `--protocol` flag (default `http`) and the new `grpc` branch.

**Interfaces:**

Consumes:

- `daemon.Discovery.GRPCAddr` (added in Task 3)

Produces:

- `catacomb env --protocol grpc` stdout:

  ```
  CLAUDE_CODE_ENABLE_TELEMETRY=1
  CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1
  OTEL_TRACES_EXPORTER=otlp
  OTEL_EXPORTER_OTLP_PROTOCOL=grpc
  OTEL_EXPORTER_OTLP_ENDPOINT=http://<grpc_addr>
  OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer <token>
  ```

  Note: gRPC endpoint has NO `/v1/traces` path suffix (the gRPC SDK derives the path from the service descriptor).

**TDD steps:**

- [ ] Write failing tests in `cmd/catacomb/env_test.go`:

```go
func TestEnvCmdGRPC(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "d.json")
    require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{
        Addr:     "127.0.0.1:5000",
        Token:    "tok",
        GRPCAddr: "127.0.0.1:5001",
    }))

    buf := &bytes.Buffer{}
    root := newRootCmd()
    root.SetArgs([]string{"env", "--protocol", "grpc", "--discovery", path})
    root.SetOut(buf)
    require.NoError(t, root.Execute())

    out := buf.String()
    require.True(t, strings.Contains(out, "CLAUDE_CODE_ENABLE_TELEMETRY=1"), "missing CLAUDE_CODE_ENABLE_TELEMETRY=1")
    require.True(t, strings.Contains(out, "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1"), "missing CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1")
    require.True(t, strings.Contains(out, "OTEL_TRACES_EXPORTER=otlp"), "missing OTEL_TRACES_EXPORTER=otlp")
    require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_PROTOCOL=grpc"), "missing OTEL_EXPORTER_OTLP_PROTOCOL=grpc")
    require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:5001"), "missing gRPC endpoint")
    require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer tok"), "missing headers")
    require.False(t, strings.Contains(out, "http/protobuf"), "grpc output must not contain http/protobuf")
}

func TestEnvCmdGRPCMissingGRPCAddr(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "d.json")
    require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{
        Addr:  "127.0.0.1:5000",
        Token: "tok",
    }))

    root := newRootCmd()
    root.SetArgs([]string{"env", "--protocol", "grpc", "--discovery", path})
    require.Error(t, root.Execute())
}

func TestEnvCmdHTTPDefaultUnchanged(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "d.json")
    require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{Addr: "127.0.0.1:5000", Token: "tok"}))

    buf := &bytes.Buffer{}
    root := newRootCmd()
    root.SetArgs([]string{"env", "--protocol", "http", "--discovery", path})
    root.SetOut(buf)
    require.NoError(t, root.Execute())

    out := buf.String()
    require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf"), "http output must contain http/protobuf")
}

func TestEnvCmdInvalidProtocol(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "d.json")
    require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{Addr: "127.0.0.1:5000", Token: "tok"}))

    root := newRootCmd()
    root.SetArgs([]string{"env", "--protocol", "banana", "--discovery", path})
    require.Error(t, root.Execute())
}
```

- [ ] Run → FAIL (`--protocol` flag undefined, new vars missing).

- [ ] Implement in `cmd/catacomb/env.go`:

```go
package main

import (
    "fmt"

    "github.com/spf13/cobra"

    "github.com/realkarych/catacomb/daemon"
)

func newEnvCmd() *cobra.Command {
    var discoveryPath, protocol string
    cmd := &cobra.Command{
        Use:   "env",
        Short: "Print OTLP environment variables for connecting to the running daemon",
        RunE: func(cmd *cobra.Command, _ []string) error {
            if discoveryPath == "" {
                discoveryPath = daemon.DiscoveryPath()
            }
            d, err := daemon.ReadDiscovery(discoveryPath)
            if err != nil {
                return err
            }
            switch protocol {
            case "http":
                fmt.Fprintf(cmd.OutOrStdout(), "CLAUDE_CODE_ENABLE_TELEMETRY=1\n")
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_TRACES_EXPORTER=otlp\n")
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf\n")
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_ENDPOINT=http://%s\n", d.Addr)
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer %s\n", d.Token)
            case "grpc":
                if d.GRPCAddr == "" {
                    return fmt.Errorf("discovery file has no grpc_addr; ensure the daemon was started with M1a-grpc")
                }
                fmt.Fprintf(cmd.OutOrStdout(), "CLAUDE_CODE_ENABLE_TELEMETRY=1\n")
                fmt.Fprintf(cmd.OutOrStdout(), "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1\n")
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_TRACES_EXPORTER=otlp\n")
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_PROTOCOL=grpc\n")
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_ENDPOINT=http://%s\n", d.GRPCAddr)
                fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer %s\n", d.Token)
            default:
                return fmt.Errorf("unknown protocol %q; use http or grpc", protocol)
            }
            return nil
        },
    }
    cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
    cmd.Flags().StringVar(&protocol, "protocol", "http", "OTLP transport protocol: http or grpc")
    return cmd
}
```

- [ ] Update existing `TestEnvCmd` and `TestEnvCmdDefaultDiscovery` in `env_test.go`: these do not pass `--protocol` so they exercise the default `http` path — they pass unchanged. No edit needed.

- [ ] Run → PASS; `make cover` 100%; `make lint` 0; `make build`; `GOOS=windows go build ./...`.

- [ ] Commit: `feat(cmd): catacomb env --protocol grpc (gRPC OTLP vars + CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1)`.

---

## Deferred (documented)

- **OTel enrichment / precedence / #53954 edges / cascade / `Edge.Rev` population** → M1b.
- **CDC delta bus + passthrough exporter** → M1c.
- **Step 7 (live field-name operator verification)** — deferred to the very end per `autonomous-completion-mandate`.

## Self-Review

- **Spec coverage §3.1 (two listeners):** Task 3 adds the second loopback gRPC listener bound by `runDaemonWith` and passed into `Serve`. Both listeners are loopback TCP. ✓
- **Spec coverage §3.2 (discovery file + env):** Task 3 adds `GRPCAddr` (`grpc_addr`) to `Discovery`; Task 4 adds `catacomb env --protocol grpc` reading that field. ✓
- **Spec coverage §3.3 (security/isolation):** Bearer interceptor in Task 1 gates all gRPC calls. `serveGRPC` recover in Task 2 isolates panics. Insecure transport (loopback + bearer = same trust model as HTTP). ✓
- **Fail-open mirror:** `handleOTLP` does `_ = d.IngestOTLP(&req)` and returns 200 even on quarantine. `Export` does `_ = s.d.IngestOTLP(req)` and returns `(&ExportTraceServiceResponse{}, nil)`. `IngestOTLP` always returns `nil` (verified at `daemon/daemon.go:156-209`). Match is exact. ✓
- **No `ingest/otel` or `reduce/` edits:** M1a-grpc is transport-only. ✓
- **`time.Sleep` ban:** supervisor uses `waitFn` seam (timer-based default); no `time.Sleep` anywhere. Tests use injected `waitFn` returning immediately. ✓
- **100% coverage discipline:** Every branch in `serveGRPC` is exercised by injecting `serveFn`/`waitFn`: (a) ctx already done on entry, (b) error N times then ctx cancel, (c) panic recovered + restart, (d) `waitFn` returns false (ctx cancelled during backoff), (e) graceful stop via ctx. `Export` covered by direct call tests. Interceptor covered by all 5 metadata cases. ✓
- **Interface ripple:** `Discovery.GRPCAddr` is `omitempty` — existing tests constructing `Discovery{Addr, Token}` compile and run without change. `Serve` signature change requires updating every call site; the only caller is `runDaemonWith`, and that is updated in Task 3 with tests verifying the new wiring. ✓
- **`go mod tidy` ban:** `google.golang.org/grpc`, `go.opentelemetry.io/proto/otlp`, `google.golang.org/grpc/codes`, `google.golang.org/grpc/status`, `google.golang.org/grpc/metadata`, `google.golang.org/grpc/credentials/insecure` are all transitively available via existing indirect deps. Use `go get` only if the build complains about a missing direct dep. ✓
- **Placeholder scan:** No "TBD", "similar to above", or "..." placeholders in code blocks. ✓
- **Type consistency:** `traceServer.Export` signature matches `collectorv1.TraceServiceServer` interface. `bearerInterceptor` returns `grpc.UnaryServerInterceptor` (= `func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error)`). `serveGRPC` seam types are explicit `func(*grpc.Server, net.Listener) error` and `func(context.Context, time.Duration) bool`. ✓
