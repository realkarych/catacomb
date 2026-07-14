# M4a — Shared Exporter Interface + Multi-Exporter Daemon Wiring + Postgres Exporter

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract a shared `export.Exporter` interface, generalise the daemon to fan-out deltas
to an ordered list of exporters (OTLP + any new ones), implement a postgres materialized-graph
exporter behind a `pgxpool` seam, and wire a `--postgres-export-dsn` CLI flag end-to-end.

**Architecture:** A new zero-statement `export` package holds the shared interface; `export/otlp`
asserts conformance. The daemon replaces its single `*cdc.Consumer` field with a slice and loops
over an in-memory exporter config list built from set flags; snapshot-then-attach discipline is
per-exporter under `d.mu`. `export/postgres` mirrors the `export/otlp` seam pattern: a narrow
`execer` interface, `New→newFull(factory)` injection, `ExporterWithExecer` test constructor, and
a `recordExecer` fake for unit tests; the real `pgxpool.Pool` is lazy (no connect on `New`) so a
"construct with valid DSN" test achieves 100% coverage of the factory without a live DB.

**Tech Stack:** Go 1.26, `github.com/jackc/pgx/v5` (pure-Go, add via `go get`), `testify`.

## Global Constraints

Go 1.26 pure-Go (no cgo; `pgx/v5` + `neo4j-go-driver/v5` are pure-Go); **NO
comments except `//go:build|//go:embed|//go:generate`** (`internal/codepolicy`);
**100% line coverage under `-race`** (`make cover`) achieved via the seam recipe
— **no DB in tests, no new coverage exclusions**; golangci-lint v2 clean (gofumpt,
goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, **forbidigo
bans `time.Sleep`**, unparam, errcheck, rowserrcheck, bodyclose); **never `go mod tidy`**
— add `pgx`/`neo4j` via `go get`; single-mutex daemon (`d.mu`) — exporter
construction + snapshot-then-attach hold it, per-consumer drain loops do NOT;
cross-platform (`GOOS=windows go build ./...` clean — both drivers are
cross-platform pure-Go); loopback + bearer unaffected (exporters are outbound);
commit per task; never commit to master mid-plan.

---

## File Map

| File | Status | Responsibility |
|---|---|---|
| `export/export.go` | Create | Shared `Exporter` interface (zero statements) |
| `export/otlp/export.go` | Modify | Add `var _ export.Exporter = (*Exporter)(nil)` |
| `daemon/daemon.go` | Modify | `exporterConsumers []*cdc.Consumer`, `postgresDSN string`, `SetPostgresDSN` |
| `daemon/server.go` | Modify | `startExporter` generalised, `newPostgresFn` seam var |
| `daemon/testsupport.go` | Modify | Expose `ExporterConsumersForTest` |
| `cmd/catacomb/daemon.go` | Modify | `--postgres-export-dsn` flag + `runDaemonWith` signature + `d.SetPostgresDSN` |
| `export/postgres/export.go` | Create | Postgres exporter (schema, upsert, seam) |
| `export/postgres/export_test.go` | Create | Unit tests (recordExecer, construct tests) |

---

### Task 1: Shared `export.Exporter` Interface

**Files:**

- Create: `export/export.go`
- Modify: `export/otlp/export.go` (add conformance assertion, add `export` import)

**Interfaces:**

- Consumes: `cdc.GraphDelta`, `model.Node`, `model.Edge` (already imported in `export/otlp`)
- Produces:
  - `export.Exporter` interface with five methods (see step 3 below)

- [ ] **Step 1: Write the failing conformance test**

Create `export/otlp/conformance_test.go` (package `otlp`) to verify the assertion
compiles. Actually the assertion is a `var` declaration that fails at compile time,
so the "test" is: add the `var _` line, verify `go build ./export/otlp/...` fails
before the interface exists.

Before any file changes run:

```bash
cd /Users/karych/src/catacomb && go build ./export/otlp/...
```

Expected: succeeds (baseline).

- [ ] **Step 2: Create `export/export.go`**

```go
package export

import (
	"context"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type Exporter interface {
	Name() string
	ApplyDelta(ctx context.Context, d cdc.GraphDelta) error
	SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error
	FlushRun(ctx context.Context, runID string) error
	Shutdown(ctx context.Context) error
}
```

- [ ] **Step 3: Add the conformance assertion to `export/otlp/export.go`**

Add after the import block, before the first type declaration:

```go
var _ exportiface.Exporter = (*Exporter)(nil)
```

And add `exportiface "github.com/realkarych/catacomb/export"` to the import block of
`export/otlp/export.go`. The existing file already satisfies the interface — all five
methods (`Name`, `ApplyDelta`, `SnapshotState`, `FlushRun`, `Shutdown`) are present.

Full import block after the edit:

```go
import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)
```

Place the assertion on the first line after the `import (...)` block (before `type spanExporter`):

```go
var _ exportiface.Exporter = (*Exporter)(nil)
```

- [ ] **Step 4: Build to verify conformance**

```bash
cd /Users/karych/src/catacomb && go build ./export/...
```

Expected: clean exit. If you see `*Exporter does not implement exportiface.Exporter`, a
method signature differs — re-check each method against the interface.

- [ ] **Step 5: Run all tests to confirm nothing broke**

```bash
cd /Users/karych/src/catacomb && go test -race ./export/...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/karych/src/catacomb && git add export/export.go export/otlp/export.go
git commit -m "feat(export): extract shared Exporter interface; assert otlp conformance"
```

---

### Task 2: Daemon Multi-Exporter Wiring

**Files:**

- Modify: `daemon/daemon.go` — replace `exporterConsumer *cdc.Consumer` with
  `exporterConsumers []*cdc.Consumer`; add `postgresDSN string`; add `SetPostgresDSN`
- Modify: `daemon/server.go` — add `newPostgresFn` var; generalise `startExporter`
- Modify: `daemon/testsupport.go` — expose `ExporterConsumersForTest`
- Modify: `daemon/server_test.go` — update tests that reference `d.exporterConsumer`
  (singular); add multi-exporter test

**Interfaces:**

- Consumes: `export.Exporter` interface from Task 1
- Produces:
  - `d.SetPostgresDSN(s string)` method on `*Daemon`
  - `newPostgresFn` package-var seam: `var newPostgresFn func(ctx context.Context, dsn string) (export.Exporter, error)`

- [ ] **Step 1: Write a failing test asserting two consumers exist when both OTLP and postgres DSN are set**

Add to `daemon/server_test.go`:

```go
func TestStartExporterAttachesTwoConsumersWhenBothConfigured(t *testing.T) {
	fake := &fakeSpanExporter{}
	origOTLP := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = origOTLP })

	fakeExp := &fakeExporter{}
	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d := New(tempStore(t))
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	d.mu.Lock()
	n := len(d.exporterConsumers)
	d.mu.Unlock()
	assert.Equal(t, 2, n)
}
```

Also add this helper type to `daemon/server_test.go` (it will be reused in Task 4):

```go
type fakeExporter struct {
	mu          sync.Mutex
	snapshots   [][]*model.Node
	deltas      []cdc.GraphDelta
	flushes     []string
	shutdowns   int
}

func (f *fakeExporter) Name() string { return "fake" }

func (f *fakeExporter) SnapshotState(_ context.Context, nodes []*model.Node, _ []*model.Edge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = append(f.snapshots, nodes)
	return nil
}

func (f *fakeExporter) ApplyDelta(_ context.Context, d cdc.GraphDelta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deltas = append(f.deltas, d)
	return nil
}

func (f *fakeExporter) FlushRun(_ context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes = append(f.flushes, runID)
	return nil
}

func (f *fakeExporter) Shutdown(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdowns++
	return nil
}

func (f *fakeExporter) snapshotCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.snapshots)
}

func (f *fakeExporter) deltaCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deltas)
}
```

Also add `exportiface "github.com/realkarych/catacomb/export"` to `daemon/server_test.go`
imports. Run:

```bash
cd /Users/karych/src/catacomb && go test -race ./daemon/ -run TestStartExporterAttachesTwoConsumersWhenBothConfigured
```

Expected: FAIL — `newPostgresFn undefined`, `exporterConsumers undefined`, `SetPostgresDSN undefined`.

- [ ] **Step 2: Update `daemon/daemon.go`**

Replace `exporterConsumer *cdc.Consumer` field (line 70) with `exporterConsumers []*cdc.Consumer`.
Add `postgresDSN string` field immediately after in the struct. Add `SetPostgresDSN` method:

The `Daemon` struct field block changes from:

```go
	otlpEndpoint      string
	exporterConsumer  *cdc.Consumer
	dbPath            string
```

to:

```go
	otlpEndpoint       string
	exporterConsumers  []*cdc.Consumer
	postgresDSN        string
	dbPath             string
```

Add `SetPostgresDSN` after `SetOTLPEndpoint`:

```go
func (d *Daemon) SetPostgresDSN(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.postgresDSN = s
}
```

Update `metricsSnapshot` — the lag computation changes from:

```go
	var lag int64
	if d.exporterConsumer != nil {
		lag = d.exporterConsumer.Dropped()
	}
```

to:

```go
	var lag int64
	for _, c := range d.exporterConsumers {
		lag += c.Dropped()
	}
```

- [ ] **Step 3: Update `daemon/server.go`**

Add the `newPostgresFn` package-var seam after `newExporterFn`. Add import for
`exportiface "github.com/realkarych/catacomb/export"`. Rewrite `startExporter`.

The new import block for `daemon/server.go`:

```go
import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/export/otlp"
	tailingest "github.com/realkarych/catacomb/ingest/tail"
)
```

Add after `var newExporterFn = otlp.New`:

```go
var newPostgresFn func(ctx context.Context, dsn string) (exportiface.Exporter, error)
```

Replace `startExporter` entirely:

```go
func (d *Daemon) startExporter(ctx context.Context, httpAddr, grpcAddr string) {
	d.mu.Lock()
	otlpEndpoint := d.otlpEndpoint
	postgresDSN := d.postgresDSN
	d.mu.Unlock()

	type exporterEntry struct {
		exp  exportiface.Exporter
		name string
	}
	var entries []exporterEntry

	if otlpEndpoint != "" {
		exp, err := newExporterFn(ctx, otlpEndpoint, grpcAddr, httpAddr)
		if err != nil {
			log.Printf("catacomb: otlp exporter disabled: %v", err)
		} else {
			entries = append(entries, exporterEntry{exp: exp, name: "otlp"})
		}
	}

	if postgresDSN != "" && newPostgresFn != nil {
		exp, err := newPostgresFn(ctx, postgresDSN)
		if err != nil {
			log.Printf("catacomb: postgres exporter disabled: %v", err)
		} else {
			entries = append(entries, exporterEntry{exp: exp, name: "postgres"})
		}
	}

	if len(entries) == 0 {
		return
	}

	d.mu.Lock()
	for _, e := range entries {
		for _, g := range d.graphs {
			nodes, edges := g.Snapshot()
			_ = e.exp.SnapshotState(ctx, nodes, edges)
		}
		for _, g := range d.graphs {
			for _, r := range g.RunsSnapshot() {
				if r.EndedAt != nil {
					_ = e.exp.FlushRun(ctx, r.ID)
				}
			}
		}
		consumer := d.bus.Subscribe(exporterBufSize)
		d.exporterConsumers = append(d.exporterConsumers, consumer)
		exp := e.exp
		go func(c *cdc.Consumer, ex exportiface.Exporter) {
			for {
				select {
				case <-ctx.Done():
					d.bus.Unsubscribe(c)
					_ = ex.Shutdown(ctx)
					return
				case delta, ok := <-c.C:
					if !ok {
						if consumerLoopExitHook != nil {
							consumerLoopExitHook()
						}
						return
					}
					_ = ex.ApplyDelta(ctx, delta)
				}
			}
		}(consumer, exp)
	}
	d.mu.Unlock()
}
```

Note: the goroutine captures `consumer` and `exp` via the closure parameters — no loop-variable
capture issue. The `d.mu.Unlock()` is deferred-free here because the goroutines must launch
**after** the mutex section that appends consumers.

- [ ] **Step 4: Update `daemon/testsupport.go`**

Add after the existing helpers:

```go
func (d *Daemon) ExporterConsumersForTest() []*cdc.Consumer {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*cdc.Consumer, len(d.exporterConsumers))
	copy(out, d.exporterConsumers)
	return out
}
```

Add `"github.com/realkarych/catacomb/cdc"` to the imports of `testsupport.go`.

- [ ] **Step 5: Fix existing tests in `daemon/server_test.go` that reference `d.exporterConsumer` (singular)**

Search for `d.exporterConsumer` in `daemon/server_test.go`. There are three occurrences:

1. `TestServeStartsExporterConsumer` (line 84): `return d.exporterConsumer != nil`
   → change to `return len(d.exporterConsumers) > 0`

2. `TestServeExporterSnapshotsExistingGraphs` (line 114): `return d.exporterConsumer != nil`
   → change to `return len(d.exporterConsumers) > 0`

3. `TestServeSelfLoopEndpointSkipsExporter` (line 138): `consumerNil := d.exporterConsumer == nil`
   → change to `consumerNil := len(d.exporterConsumers) == 0`

4. `TestExporterConsumerLoopExitsOnChannelClose` (line 166): `consumer := d.exporterConsumer`
   → change to `consumer := d.exporterConsumers[0]` (the test sets only OTLP so index 0 is valid)

Also add `exportiface "github.com/realkarych/catacomb/export"` to the imports of `server_test.go`.

- [ ] **Step 6: Run the full daemon test suite**

```bash
cd /Users/karych/src/catacomb && go test -race ./daemon/...
```

Expected: all pass. If you see `newPostgresFn declared but not used`, verify it is referenced
in `startExporter`'s `if postgresDSN != "" && newPostgresFn != nil` guard.

- [ ] **Step 7: Verify coverage gate**

```bash
cd /Users/karych/src/catacomb && make cover 2>&1 | tail -30
```

Expected: 100% for `daemon` package.

- [ ] **Step 8: Commit**

```bash
cd /Users/karych/src/catacomb && git add daemon/daemon.go daemon/server.go daemon/testsupport.go daemon/server_test.go
git commit -m "feat(daemon): multi-exporter wiring; exporterConsumers slice; newPostgresFn seam"
```

---

### Task 3: `export/postgres` Exporter

**Files:**

- Create: `export/postgres/export.go`
- Create: `export/postgres/export_test.go`

**Interfaces:**

- Consumes: `export.Exporter` from Task 1; `cdc.GraphDelta`, `model.Node`, `model.Edge`
- Produces:
  - `export/postgres.New(ctx context.Context, dsn string) (*Exporter, error)`
  - `export/postgres.ExporterWithExecer(e execer) *Exporter`
  - `(*Exporter).Name() string` → `"postgres"`
  - `(*Exporter).ApplyDelta(ctx, cdc.GraphDelta) error`
  - `(*Exporter).SnapshotState(ctx, []*model.Node, []*model.Edge) error`
  - `(*Exporter).FlushRun(ctx, runID string) error` → returns nil (no-op)
  - `(*Exporter).Shutdown(ctx) error`

- [ ] **Step 1: Add pgx dependency**

```bash
cd /Users/karych/src/catacomb && go get github.com/jackc/pgx/v5@latest
```

Expected: `go.mod` and `go.sum` updated with `github.com/jackc/pgx/v5 v5.x.x`.
Never run `go mod tidy`.

- [ ] **Step 2: Write failing tests in `export/postgres/export_test.go`**

```go
package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

var _ exportiface.Exporter = (*Exporter)(nil)

type sqlCall struct {
	sql  string
	args []any
}

type recordExecer struct {
	calls   []sqlCall
	txCalls [][]sqlCall
	closed  bool
}

func (r *recordExecer) Exec(_ context.Context, sql string, args ...any) error {
	r.calls = append(r.calls, sqlCall{sql: sql, args: args})
	return nil
}

func (r *recordExecer) BeginTx(ctx context.Context) (txExecer, error) {
	tx := &recordTx{parent: r}
	return tx, nil
}

func (r *recordExecer) Close() {}

type recordTx struct {
	parent *recordExecer
	calls  []sqlCall
}

func (t *recordTx) Exec(_ context.Context, sql string, args ...any) error {
	t.calls = append(t.calls, sqlCall{sql: sql, args: args})
	return nil
}

func (t *recordTx) Commit(_ context.Context) error {
	t.parent.txCalls = append(t.parent.txCalls, t.calls)
	return nil
}

func (t *recordTx) Rollback(_ context.Context) error { return nil }

func TestNameIsPostgres(t *testing.T) {
	e := ExporterWithExecer(&recordExecer{})
	assert.Equal(t, "postgres", e.Name())
}

func TestShutdownClosesExecer(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.Shutdown(context.Background()))
	assert.True(t, r.closed)
}

func TestFlushRunIsNoop(t *testing.T) {
	e := ExporterWithExecer(&recordExecer{})
	require.NoError(t, e.FlushRun(context.Background(), "any-run"))
}

func TestApplyDeltaNodeUpsertEmitsSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusRunning, Rev: 3}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 3, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].sql, "ON CONFLICT")
	assert.Contains(t, r.calls[0].sql, "WHERE excluded.rev > nodes.rev")
	assert.NotContains(t, strings.ToLower(r.calls[0].sql), "payload")
}

func TestApplyDeltaNodeStatusEmitsSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, Rev: 5}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeStatus, Rev: 5, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].sql, "ON CONFLICT")
}

func TestApplyDeltaEdgeUpsertEmitsSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "c", Rev: 2}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeUpsert, Rev: 2, RunID: "r1", Edge: edge,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, r.calls[0].sql, "edges")
	assert.Contains(t, r.calls[0].sql, "ON CONFLICT")
	assert.Contains(t, r.calls[0].sql, "WHERE excluded.rev > edges.rev")
}

func TestApplyDeltaNodeMergeDeletesThenUpserts(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	n := &model.Node{ID: "n-new", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 7}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeMerge, Rev: 7, RunID: "r1", OldID: "n-old", NewID: "n-new", Node: n,
	}))
	require.Len(t, r.calls, 2)
	assert.Contains(t, strings.ToLower(r.calls[0].sql), "delete")
	assert.Contains(t, r.calls[0].sql, "$1")
	assert.Contains(t, r.calls[1].sql, "ON CONFLICT")
}

func TestApplyDeltaEdgeDeleteEmitsDelete(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	edge := &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaEdgeDelete, Rev: 1, RunID: "r1", Edge: edge,
	}))
	require.Len(t, r.calls, 1)
	assert.Contains(t, strings.ToLower(r.calls[0].sql), "delete")
	assert.Contains(t, r.calls[0].sql, "edges")
}

func TestApplyDeltaLifecycleKindsAreNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	for _, k := range []cdc.GraphDeltaKind{cdc.DeltaRunStarted, cdc.DeltaSessionEnded, cdc.DeltaRunEnded} {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: k, Rev: 1, RunID: "r1"}))
	}
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNilNodeIsNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1"}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNilEdgeIsNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1"}))
	assert.Empty(t, r.calls)
}

func TestApplyDeltaNodeUpsertAttrsJSONEncoded(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	n := &model.Node{
		ID: "n3", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1,
		Attrs: map[string]any{"key": "val"},
	}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	require.Len(t, r.calls, 1)
	var foundJSON bool
	for _, arg := range r.calls[0].args {
		if s, ok := arg.(string); ok {
			var m map[string]any
			if json.Unmarshal([]byte(s), &m) == nil {
				if m["key"] == "val" {
					foundJSON = true
				}
			}
		}
	}
	assert.True(t, foundJSON, "attrs must be JSON-encoded in args")
}

func TestApplyDeltaNodeUpsertPayloadNeverInSQL(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	n := &model.Node{
		ID: "n4", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 1,
		Payload: &model.Payload{Hash: "abc123"},
	}
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{
		Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: n,
	}))
	for _, call := range r.calls {
		assert.NotContains(t, strings.ToLower(call.sql), "payload", "payload column must never appear")
		for _, arg := range call.args {
			assert.NotContains(t, strings.ToLower(fmt.Sprintf("%v", arg)), "payload")
		}
	}
}

func TestSnapshotStateUpsertsBatched(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	nodes := []*model.Node{
		{ID: "a", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK, Rev: 1},
		{ID: "b", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, Rev: 2},
	}
	edges := []*model.Edge{
		{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "a", Dst: "b", Rev: 1},
	}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edges))
	require.Equal(t, 1, len(r.txCalls), "snapshot must use one transaction")
	totalCalls := len(r.txCalls[0])
	assert.Equal(t, 3, totalCalls, "two node upserts + one edge upsert in the tx")
}

func TestSnapshotStateEmptyIsNoop(t *testing.T) {
	r := &recordExecer{}
	e := ExporterWithExecer(r)
	require.NoError(t, e.SnapshotState(context.Background(), nil, nil))
	assert.Empty(t, r.txCalls)
}

func TestNewWithValidDSNConstructsPool(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, "postgres://localhost:5432/catacomb_test")
	require.NoError(t, err)
	assert.Equal(t, "postgres", e.Name())
	_ = e.Shutdown(ctx)
}

func TestNewWithMalformedDSNReturnsError(t *testing.T) {
	_, err := New(context.Background(), "not-a-dsn://\x00invalid")
	require.Error(t, err)
}
```

Run:

```bash
cd /Users/karych/src/catacomb && go test -race ./export/postgres/... 2>&1 | head -20
```

Expected: FAIL — package `postgres` not found.

- [ ] **Step 3: Implement `export/postgres/export.go`**

```go
package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

var _ exportiface.Exporter = (*Exporter)(nil)

type txExecer interface {
	Exec(ctx context.Context, sql string, args ...any) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) error
	BeginTx(ctx context.Context) (txExecer, error)
	Close()
}

type poolAdapter struct {
	pool *pgxpool.Pool
}

func (p *poolAdapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := p.pool.Exec(ctx, sql, args...)
	return err
}

func (p *poolAdapter) BeginTx(ctx context.Context) (txExecer, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	return &txAdapter{tx: tx}, nil
}

func (p *poolAdapter) Close() { p.pool.Close() }

type txAdapter struct {
	tx pgx.Tx
}

func (t *txAdapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.tx.Exec(ctx, sql, args...)
	return err
}

func (t *txAdapter) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *txAdapter) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

type Exporter struct {
	db execer
}

func New(ctx context.Context, dsn string) (*Exporter, error) {
	return newFull(ctx, dsn, newPool)
}

func newFull(ctx context.Context, dsn string, factory func(context.Context, string) (execer, error)) (*Exporter, error) {
	db, err := factory(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres exporter: %w", err)
	}
	return newWithExecer(db), nil
}

func newPool(ctx context.Context, dsn string) (execer, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, &poolAdapter{pool: pool}); err != nil {
		pool.Close()
		return nil, err
	}
	return &poolAdapter{pool: pool}, nil
}

func newWithExecer(db execer) *Exporter {
	return &Exporter{db: db}
}

func ExporterWithExecer(db execer) *Exporter {
	return newWithExecer(db)
}

func ensureSchema(ctx context.Context, db execer) error {
	const nodesTable = `CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY,
		run_id TEXT,
		type TEXT,
		name TEXT,
		status TEXT,
		tier TEXT,
		parent_id TEXT,
		agent_id TEXT,
		t_start TIMESTAMPTZ,
		t_end TIMESTAMPTZ,
		duration_ms BIGINT,
		tokens_in BIGINT,
		tokens_out BIGINT,
		cost_usd DOUBLE PRECISION,
		payload_hash TEXT,
		attrs JSONB,
		annotations JSONB,
		rev BIGINT
	)`
	const edgesTable = `CREATE TABLE IF NOT EXISTS edges (
		id TEXT PRIMARY KEY,
		run_id TEXT,
		type TEXT,
		src TEXT,
		dst TEXT,
		attrs JSONB,
		rev BIGINT
	)`
	if err := db.Exec(ctx, nodesTable); err != nil {
		return fmt.Errorf("postgres exporter: create nodes table: %w", err)
	}
	if err := db.Exec(ctx, edgesTable); err != nil {
		return fmt.Errorf("postgres exporter: create edges table: %w", err)
	}
	return nil
}

func (e *Exporter) Name() string { return "postgres" }

func (e *Exporter) Shutdown(_ context.Context) error {
	e.db.Close()
	return nil
}

func (e *Exporter) FlushRun(_ context.Context, _ string) error { return nil }

func (e *Exporter) ApplyDelta(ctx context.Context, d cdc.GraphDelta) error {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		if d.Node == nil {
			return nil
		}
		return e.upsertNode(ctx, d.Node)
	case cdc.DeltaEdgeUpsert:
		if d.Edge == nil {
			return nil
		}
		return e.upsertEdge(ctx, d.Edge)
	case cdc.DeltaNodeMerge:
		if d.Node == nil {
			return nil
		}
		if err := e.db.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, d.OldID); err != nil {
			return fmt.Errorf("postgres exporter node_merge delete: %w", err)
		}
		return e.upsertNode(ctx, d.Node)
	case cdc.DeltaEdgeDelete:
		if d.Edge == nil {
			return nil
		}
		if err := e.db.Exec(ctx, `DELETE FROM edges WHERE id = $1`, d.Edge.ID); err != nil {
			return fmt.Errorf("postgres exporter edge_delete: %w", err)
		}
		return nil
	default:
		return nil
	}
}

func (e *Exporter) SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error {
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}
	tx, err := e.db.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("postgres exporter snapshot begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, n := range nodes {
		if err := upsertNodeTx(ctx, tx, n); err != nil {
			return err
		}
	}
	for _, edge := range edges {
		if err := upsertEdgeTx(ctx, tx, edge); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const nodeUpsertSQL = `INSERT INTO nodes (id, run_id, type, name, status, tier, parent_id, agent_id,
	t_start, t_end, duration_ms, tokens_in, tokens_out, cost_usd, payload_hash, attrs, annotations, rev)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (id) DO UPDATE SET
	run_id=$2, type=$3, name=$4, status=$5, tier=$6, parent_id=$7, agent_id=$8,
	t_start=$9, t_end=$10, duration_ms=$11, tokens_in=$12, tokens_out=$13, cost_usd=$14,
	payload_hash=$15, attrs=$16, annotations=$17, rev=$18
WHERE excluded.rev > nodes.rev`

const edgeUpsertSQL = `INSERT INTO edges (id, run_id, type, src, dst, attrs, rev)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (id) DO UPDATE SET
	run_id=$2, type=$3, src=$4, dst=$5, attrs=$6, rev=$7
WHERE excluded.rev > edges.rev`

func jsonMarshal(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func nodeArgs(n *model.Node) []any {
	return []any{
		n.ID, n.RunID, string(n.Type), n.Name, string(n.Status), n.Tier,
		n.ParentID, n.AgentID,
		n.TStart, n.TEnd, n.DurationMS, n.TokensIn, n.TokensOut, n.CostUSD,
		n.PayloadHash,
		jsonMarshal(n.Attrs),
		jsonMarshal(n.Annotations),
		n.Rev,
	}
}

func edgeArgs(edge *model.Edge) []any {
	return []any{
		edge.ID, edge.RunID, string(edge.Type), edge.Src, edge.Dst,
		jsonMarshal(edge.Attrs),
		edge.Rev,
	}
}

func (e *Exporter) upsertNode(ctx context.Context, n *model.Node) error {
	if err := e.db.Exec(ctx, nodeUpsertSQL, nodeArgs(n)...); err != nil {
		return fmt.Errorf("postgres exporter upsert node: %w", err)
	}
	return nil
}

func (e *Exporter) upsertEdge(ctx context.Context, edge *model.Edge) error {
	if err := e.db.Exec(ctx, edgeUpsertSQL, edgeArgs(edge)...); err != nil {
		return fmt.Errorf("postgres exporter upsert edge: %w", err)
	}
	return nil
}

func upsertNodeTx(ctx context.Context, tx txExecer, n *model.Node) error {
	if err := tx.Exec(ctx, nodeUpsertSQL, nodeArgs(n)...); err != nil {
		return fmt.Errorf("postgres exporter snapshot upsert node: %w", err)
	}
	return nil
}

func upsertEdgeTx(ctx context.Context, tx txExecer, edge *model.Edge) error {
	if err := tx.Exec(ctx, edgeUpsertSQL, edgeArgs(edge)...); err != nil {
		return fmt.Errorf("postgres exporter snapshot upsert edge: %w", err)
	}
	return nil
}
```

Note: `pgxpool.New` validates the DSN config but does **not** dial the server (lazy pool),
so `TestNewWithValidDSNConstructsPool` succeeds without a live DB. A malformed DSN
(with a null byte) causes `pgxpool.New` to return an error, covering the error branch.

The `recordExecer` in the test file must match this `execer` interface. Verify the methods:
`Exec(ctx, sql, args...any) error`, `BeginTx(ctx) (txExecer, error)`, `Close()`.
The `recordTx` must match `txExecer`: `Exec`, `Commit`, `Rollback`.

Also add the missing `"fmt"` import to `export_test.go` (used in `TestApplyDeltaNodeUpsertPayloadNeverInSQL`).

- [ ] **Step 4: Run the postgres tests**

```bash
cd /Users/karych/src/catacomb && go test -race ./export/postgres/...
```

Expected: all pass.

- [ ] **Step 5: Verify no new coverage exclusions needed**

```bash
cd /Users/karych/src/catacomb && make cover 2>&1 | grep -E "postgres|FAIL|ok"
```

Expected: `ok github.com/realkarych/catacomb/export/postgres` with 100% coverage.

- [ ] **Step 6: Cross-platform build check**

```bash
cd /Users/karych/src/catacomb && GOOS=windows go build ./...
```

Expected: clean exit.

- [ ] **Step 7: Lint**

```bash
cd /Users/karych/src/catacomb && make lint 2>&1 | grep -v "^$" | head -40
```

Expected: no errors. If `unused` flags `poolAdapter`/`txAdapter` as unused, verify they are
used in `newPool`. If `errcheck` flags `tx.Rollback` in the `defer`, the `_ =` prefix suppresses
it cleanly.

- [ ] **Step 8: Commit**

```bash
cd /Users/karych/src/catacomb && git add export/postgres/export.go export/postgres/export_test.go go.mod go.sum
git commit -m "feat(export/postgres): materialized graph exporter with execer seam + 100% coverage"
```

---

### Task 4: Wire `--postgres-export-dsn` Flag

**Files:**

- Modify: `cmd/catacomb/daemon.go` — add `postgresDSN` var; add flag; extend `runDaemonWith`
  signature; call `d.SetPostgresDSN`; assign `newPostgresFn` via import of `export/postgres`
- Modify: `daemon/server.go` — initialise `newPostgresFn` to the real factory

**Interfaces:**

- Consumes: `daemon.SetPostgresDSN(string)` from Task 2; `newPostgresFn` seam from Task 2
- Produces:
  - `--postgres-export-dsn` CLI flag (string, default empty)
  - `runDaemonWith` extended with `postgresDSN string` parameter (after `otlpEndpoint`)
  - `newPostgresFn` wired to `postgres.New` at process start in `daemon/server.go`

- [ ] **Step 1: Write a failing e2e wiring test**

Add to `daemon/server_test.go`:

```go
func TestWiringPostgresDSNAttachesExporterAndReceivesDelta(t *testing.T) {
	fakeExp := &fakeExporter{}
	orig := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = orig })

	d := New(tempStore(t))
	fixedExecID(d)
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	require.Eventually(t, func() bool {
		return len(d.ExporterConsumersForTest()) > 0
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.Eventually(t, func() bool {
		return fakeExp.deltaCount() > 0
	}, 3*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
}

func TestWiringEmptyPostgresDSNAttachesNothing(t *testing.T) {
	called := false
	orig := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		called = true
		return &fakeExporter{}, nil
	}
	t.Cleanup(func() { newPostgresFn = orig })

	d := New(tempStore(t))
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	assert.False(t, called)
	assert.Empty(t, d.ExporterConsumersForTest())
}

func TestWiringOTLPAndPostgresRunTogether(t *testing.T) {
	fake := &fakeSpanExporter{}
	origOTLP := newExporterFn
	newExporterFn = func(_ context.Context, _, _, _ string) (*otlp.Exporter, error) {
		return otlp.ExporterWithSpanExporter(fake), nil
	}
	t.Cleanup(func() { newExporterFn = origOTLP })

	fakeExp := &fakeExporter{}
	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d := New(tempStore(t))
	fixedExecID(d)
	d.SetOTLPEndpoint("grpc://collector.example:4317")
	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	require.Eventually(t, func() bool {
		return len(d.ExporterConsumersForTest()) == 2
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))

	require.Eventually(t, func() bool { return fake.spanCount() > 0 }, 3*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return fakeExp.deltaCount() > 0 }, 3*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
}

func TestSnapshotReceivedByPostgresExporterOnAttach(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	fakeExp := &fakeExporter{}
	origPG := newPostgresFn
	newPostgresFn = func(_ context.Context, _ string) (exportiface.Exporter, error) {
		return fakeExp, nil
	}
	t.Cleanup(func() { newPostgresFn = origPG })

	d.SetPostgresDSN("postgres://localhost/test")
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx := context.Background()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())

	assert.Positive(t, fakeExp.snapshotCount(), "snapshot must be called at attach")
}
```

Run:

```bash
cd /Users/karych/src/catacomb && go test -race ./daemon/ -run "TestWiring" 2>&1 | head -30
```

Expected: FAIL — `ExporterConsumersForTest` already exists from Task 2. The new tests
should compile but fail because `TestWiringPostgresDSNAttachesExporterAndReceivesDelta`
expects `d.Serve` to work with the postgres DSN set. The `startExporter` from Task 2
already handles `postgresDSN != "" && newPostgresFn != nil`, so these tests should
**pass** if Task 2 is complete. Confirm they pass:

```bash
cd /Users/karych/src/catacomb && go test -race ./daemon/ -run "TestWiring|TestSnapshotReceivedByPostgres"
```

If they do not pass, re-examine the `startExporter` implementation from Task 2.

- [ ] **Step 2: Wire `newPostgresFn` to the real factory in `daemon/server.go`**

In `daemon/server.go`, initialise `newPostgresFn` at declaration time. Add import for
`pgexport "github.com/realkarych/catacomb/export/postgres"`. Change the var declaration:

```go
var newPostgresFn func(ctx context.Context, dsn string) (exportiface.Exporter, error) = pgexport.New
```

This means in production the real `postgres.New` is used; tests override it before calling
`startExporter`.

- [ ] **Step 3: Extend `cmd/catacomb/daemon.go`**

Add `postgresDSN string` flag var. Add it to `runDaemonWith` signature (after `otlpEndpoint`).
Call `d.SetPostgresDSN(postgresDSN)` in `runDaemonWith`.

Full updated `cmd/catacomb/daemon.go`:

```go
package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/store"
)

func newDaemonCmd() *cobra.Command {
	var dbPath, discoveryPath, otlpEndpoint, postgresDSN string
	var reaperWindow time.Duration
	var maxShards int
	var transcriptDir string
	var transcriptExclude []string
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the catacomb daemon (receives hook events, builds the live graph)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if discoveryPath == "" {
				discoveryPath = daemon.DiscoveryPath()
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken, dbPath, discoveryPath, reaperWindow, maxShards, otlpEndpoint, postgresDSN, transcriptDir, transcriptExclude)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "catacomb.db", "SQLite database path")
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	cmd.Flags().DurationVar(&reaperWindow, "reaper-window", 30*time.Minute, "idle window before a run is marked abandoned")
	cmd.Flags().IntVar(&maxShards, "max-shards", 4096, "soft cap on in-memory execution shards")
	cmd.Flags().StringVar(&otlpEndpoint, "otlp-export-endpoint", "", "downstream OTLP endpoint to export the reconstructed trace tree (empty = disabled)")
	cmd.Flags().StringVar(&postgresDSN, "postgres-export-dsn", "", "PostgreSQL DSN to export the materialized graph (empty = disabled)")
	cmd.Flags().StringVar(&transcriptDir, "transcript-dir", "", "Claude Code transcript dir to tail (empty = disabled; recommended: ~/.claude/projects)")
	cmd.Flags().StringArrayVar(&transcriptExclude, "transcript-exclude", nil, "glob(s) of transcript paths to never tail (repeatable; the daemon db + cwd are always excluded)")
	return cmd
}

func runDaemonWith(
	ctx context.Context,
	open func(string) (store.Store, error),
	listen func() (net.Listener, error),
	listenGRPC func() (net.Listener, error),
	newToken func() (string, error),
	dbPath, discoveryPath string,
	reaperWindow time.Duration,
	maxShards int,
	otlpEndpoint string,
	postgresDSN string,
	transcriptDir string,
	transcriptExclude []string,
) error {
	s, err := open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	d := daemon.New(s)
	d.SetReaperWindow(reaperWindow)
	d.SetMaxShards(maxShards)
	d.SetOTLPEndpoint(otlpEndpoint)
	d.SetPostgresDSN(postgresDSN)
	d.SetDBPath(dbPath)
	d.SetTranscriptDir(transcriptDir)
	d.SetTranscriptExclude(transcriptExclude)
	err = d.Recover()
	if err != nil {
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

- [ ] **Step 4: Check that `cmd/catacomb/daemon_test.go` (if it exists) calls `runDaemonWith` with the new signature**

```bash
ls /Users/karych/src/catacomb/cmd/catacomb/
```

If a `daemon_test.go` exists, open it and add the `postgresDSN` argument (`""`) at the correct
position in every call to `runDaemonWith`. The new parameter is the 11th positional argument
(after `otlpEndpoint`, before `transcriptDir`).

- [ ] **Step 5: Full build**

```bash
cd /Users/karych/src/catacomb && go build ./...
```

Expected: clean.

- [ ] **Step 6: Full test suite**

```bash
cd /Users/karych/src/catacomb && go test -race ./...
```

Expected: all pass.

- [ ] **Step 7: Coverage gate**

```bash
cd /Users/karych/src/catacomb && make cover 2>&1 | tail -20
```

Expected: 100% across all packages, no new exclusions.

- [ ] **Step 8: Lint**

```bash
cd /Users/karych/src/catacomb && make lint
```

Expected: clean.

- [ ] **Step 9: Windows cross-compile**

```bash
cd /Users/karych/src/catacomb && GOOS=windows go build ./...
```

Expected: clean.

- [ ] **Step 10: Commit**

```bash
cd /Users/karych/src/catacomb && git add cmd/catacomb/daemon.go daemon/server.go
git commit -m "feat(cmd): wire --postgres-export-dsn flag through runDaemonWith to daemon"
```

---

## Self-Review

### Spec Coverage Table

| Spec section | Addressed by |
|---|---|
| §2 LIVE upsert semantics (no buffering) | Task 3: `ApplyDelta` emits SQL immediately for every live delta kind |
| §3 Shared `Exporter` interface | Task 1: `export/export.go` |
| §3 `var _ export.Exporter` for otlp | Task 1: conformance assertion in `export/otlp/export.go` |
| §4 Multi-exporter daemon wiring | Task 2: `exporterConsumers` slice + loop in `startExporter` |
| §4 Snapshot-then-attach per exporter | Task 2: each entry snapshots all graphs under `d.mu` before subscribing |
| §4 `exporter_lag` sums across consumers | Task 2: `metricsSnapshot` loops `d.exporterConsumers` |
| §4 OTLP path preserved byte-for-byte | Task 2: OTLP is first entry when `otlpEndpoint != ""`, same snapshot/flush/drain logic |
| §5 Seam recipe (no DB in tests) | Task 3: `execer` interface + `recordExecer` fake |
| §5 Lazy-pool construct test covers factory | Task 3: `TestNewWithValidDSNConstructsPool` — `pgxpool.New` lazy, no dial |
| §5 Malformed-DSN error path | Task 3: `TestNewWithMalformedDSNReturnsError` |
| §6 Schema `CREATE TABLE IF NOT EXISTS` | Task 3: `ensureSchema` in `newPool` |
| §6 `nodes` table columns (no payload column) | Task 3: `nodeUpsertSQL` has `payload_hash` not `payload` |
| §6 `edges` table columns | Task 3: `edgeUpsertSQL` |
| §6 Rev-guard `WHERE excluded.rev > nodes.rev` | Task 3: SQL constant + test asserts substring |
| §6 `node_merge` = delete-old + upsert-new | Task 3: `DeltaNodeMerge` branch; test asserts 2 calls |
| §6 `edge_delete` = DELETE | Task 3: `DeltaEdgeDelete` branch; test asserts DELETE |
| §6 Lifecycle kinds no-op | Task 3: default branch; `TestApplyDeltaLifecycleKindsAreNoop` |
| §6 Snapshot = one tx batched | Task 3: `SnapshotState` opens one tx; test asserts `len(txCalls)==1` |
| §6 Attrs JSONB / JSON-encoded | Task 3: `jsonMarshal(n.Attrs)` + test asserts JSON in args |
| §6 Payload NEVER in SQL | Task 3: test asserts `"payload"` not in SQL or args |
| §8.1 Task 4 `--postgres-export-dsn` flag | Task 4: `cmd/catacomb/daemon.go` |
| §8.1 Task 4 `newPostgresFn` seam | Task 2+4: var in `server.go`, wired to `postgres.New` |
| §8.1 e2e: fake observes snapshot + live delta | Task 4: `TestWiringPostgresDSNAttachesExporterAndReceivesDelta` |
| §8.1 e2e: empty DSN attaches nothing | Task 4: `TestWiringEmptyPostgresDSNAttachesNothing` |
| §8.1 e2e: OTLP + postgres run together | Task 4: `TestWiringOTLPAndPostgresRunTogether` |
| §9 No comments except directives | All tasks: no `//` non-directive comments in any `.go` file |
| §9 100% coverage no new exclusions | Tasks 1-4: seam recipe + construct test |
| §9 `go get pgx/v5` not `go mod tidy` | Task 3 step 1: explicit `go get` command shown |
| §9 Cross-platform `GOOS=windows` | Task 3 step 6 + Task 4 step 9 |
| §9 Commit per task | Each task ends with a `git commit` step |
| §10 Test: each delta kind | Task 3: one test per delta kind |
| §10 Test: rev-guard | Task 3: `WHERE excluded.rev > nodes.rev` asserted in SQL string |
| §10 Test: snapshot batching | Task 3: `TestSnapshotStateUpsertsBatched` |
| §10 Test: attrs encoding | Task 3: `TestApplyDeltaNodeUpsertAttrsJSONEncoded` |
| §10 Test: payload never emitted | Task 3: `TestApplyDeltaNodeUpsertPayloadNeverInSQL` |
| §10 Daemon wiring: lag aggregates | Task 2: `metricsSnapshot` sums; existing `TestMetricsEndpoint` still passes |

### Placeholder Scan

No "TBD", "TODO", "implement later", or "similar to Task N" phrases. All code blocks
contain compilable Go. All test assertions name exact methods and SQL substrings.

### Type Consistency

- `export.Exporter` interface (Task 1): five methods with exact signatures — verified against
  `export/otlp`'s existing methods.
- `execer` interface (Task 3): three methods — `Exec`, `BeginTx`, `Close`. `recordExecer`
  in test implements all three.
- `txExecer` interface (Task 3): three methods — `Exec`, `Commit`, `Rollback`. `recordTx`
  implements all three.
- `newPostgresFn` var type: `func(ctx context.Context, dsn string) (exportiface.Exporter, error)` —
  matches `postgres.New` signature (`func New(ctx context.Context, dsn string) (*Exporter, error)`)
  because `*Exporter` satisfies `exportiface.Exporter` (conformance assertion in test file).
- `fakeExporter` in `daemon/server_test.go` implements `exportiface.Exporter` (five methods)
  — matches the interface from Task 1.
- `runDaemonWith` parameter `postgresDSN string` added at position 11 (after `otlpEndpoint`,
  before `transcriptDir`) — consistent across declaration and call site.

### Windows / No-Sleep / Deps

- No `time.Sleep` anywhere — all "wait until" patterns use `require.Eventually` with channels.
- `github.com/jackc/pgx/v5` is pure-Go (no cgo); cross-compiles to Windows cleanly.
- `go get` command shown with `@latest`; no `go mod tidy` in any step.
- `ensureSchema` is called inside `newPool` (the production path only); the test seam
  (`ExporterWithExecer`) bypasses it, so schema DDL is never sent to the fake.
