# SP-W — Per-cell workspace isolation: design

- **Date:** 2026-07-13
- **Status:** approved design ([ADR-0028](../adr/0028-per-cell-workspace-isolation.md); schema placement, patch boundary, lifecycle, and stamp shape adjudicated with the owner 2026-07-13)
- **Related:** [ADR-0027](../adr/0027-verification-layer-and-reliability-metrics.md) §2.3 deferral, SP1 spec [2026-07-12-sp1-verifier-contract-design.md](2026-07-12-sp1-verifier-contract-design.md) (exec boundary, verifier contract), SP3 spec [2026-07-12-sp3-env-stamps-pareto-design.md](2026-07-12-sp3-env-stamps-pareto-design.md) (descriptive stamps)

SP-W makes `bench` materialize a **fresh working directory for every cell** from a user-supplied command, with an optional patch file handed over by absolute path, an optional teardown hook, deterministic cleanup, and descriptive identity stamps. It closes the per-trial contamination defect (Anthropic canon: agents reading prior trials' git history) and expresses the Arcadia benchmarking scenario — pinned revision plus optional patch overlay — without a line of VCS semantics in core.

## 1. Basket schema (`bench/basket.go`)

```go
type Workspace struct {
	Cmd      []string `yaml:"cmd" json:"cmd"`
	Patch    string   `yaml:"patch,omitempty" json:"patch,omitempty"`
	Rev      string   `yaml:"rev,omitempty" json:"rev,omitempty"`
	Teardown []string `yaml:"teardown,omitempty" json:"teardown,omitempty"`
}
```

- `Task.Workspace *Workspace` and `Variant.Workspace *Workspace`. The **effective workspace** of a cell is the variant's when present, else the task's (wholesale override — no field merging, mirroring nothing: env merges, workspace replaces; an override that only changes the patch restates `cmd`).
- `workspace.cmd` is plain argv, no shell, like `verify.cmd` (not the `strings.Fields` splitting of `variant.setup`).
- `patch` is a path resolved **relative to the basket file's directory**; at `Load` time the file must exist and be readable — its sha256 is computed once and carried on the parsed basket (fail-fast; a missing patch is a load error, not a mid-run surprise). `rev` is an opaque string; the core never interprets it.
- Validation (new sentinels): `ErrWorkspaceCmd` (declared workspace with empty `cmd`, task or variant), `ErrWorkspaceDir` (task declares both `dir` and `workspace`; also rejects a variant workspace on a task that declares `dir` — the two roots would compete), `ErrWorkspacePatch` (declared patch unreadable at load).
- YAML example:

```yaml
tasks:
  - id: etl-fix
    workspace:
      cmd: ["arc", "mount", "-r", "r123456", "."]
      rev: "r123456"
      teardown: ["arc", "unmount", "."]
    cmd: ["claude", "-p", "...", "--output-format", "stream-json"]
    verify: { cmd: ["python3", "verify_tables.py"] }

variants:
  - id: trunk
  - id: patched
    workspace:
      cmd: ["sh", "apply.sh"]
      patch: fix.patch
      rev: "r123456+fix"
      teardown: ["arc", "unmount", "."]
```

## 2. Cell lifecycle (`cmd/catacomb/bench.go`)

For a cell whose effective workspace is non-nil:

```text
MkdirTemp(workspacesDir, runID+"-*")
  → workspace.cmd   cwd=workdir, cell ctx, env: inherited + CATACOMB_PATCH=<abs> (when declared)
  → variant.setup   cwd=workdir (unchanged semantics otherwise)
  → agent cmd       cwd=workdir (in place of task.dir)
  → artifact capture  root=workdir
  → verifier        verifySpec.Workdir=workdir
  → teardown        cwd=workdir, FRESH context, 1-minute timeout
  → RemoveAll(workdir)   unless --keep-workspaces
```

- **New `bench` flags:** `--workspaces-dir` (base for the temp dirs; default: the OS temp dir) and `--keep-workspaces` (teardown still runs; the kept path is printed to stderr per cell).
- **Contexts.** `workspace.cmd` shares the cell context — the task timeout budgets materialization (documented; `arc mount` is the fast path). `teardown` and `RemoveAll` run **unconditionally** — after success, failure, or timeout — and teardown gets a fresh 1-minute context because the cell context may already be expired; a leaked FUSE mount must not depend on the cell surviving.
- **Env.** Only `workspace.cmd` sees `CATACOMB_PATCH`; the agent and the verifier do not (the verifier reads `rev`/`patch_sha256` from `meta.json` if it cares). `workspace.cmd` inherits the parent process environment (same inheritance as `variant.setup` today) — task/variant `env` maps stay agent-scoped.
- **Dormancy.** Cells with no effective workspace run byte-identically to today: same cwd (`task.dir`), no new env, no stamps block, no temp dirs.

## 3. Stamps (`evidence/evidence.go`)

```go
type WorkspaceStamp struct {
	Rev         string `json:"rev,omitempty"`
	PatchSHA256 string `json:"patch_sha256,omitempty"`
}
```

`EnvStamps` gains `Workspace *WorkspaceStamp` with `omitzero`; the block exists only for workspace cells. Descriptive, never gating, never a join key — the SP3 posture verbatim. The declared `rev` and the patch bytes' hash are the workspace identity; the declaration itself is already pinned by the basket hash, so `--resume` integrity is inherited.

## 4. Failure semantics

| Failure | Effect |
|---|---|
| `MkdirTemp` fails | cell operational failure, note `workspace failed: <err>`; no evidence |
| `workspace.cmd` non-zero / spawn error / ctx expiry | cell operational failure, note `workspace failed` (exit code recorded, as `setup failed` today); no evidence; teardown + RemoveAll still run |
| `teardown` non-zero / spawn error | note appended + stderr line; never flips any verdict; RemoveAll still attempted |
| `RemoveAll` fails (live mount) | note appended + stderr line; bench continues |
| patch unreadable | load error (`ErrWorkspacePatch`), exit 2 before any cell runs |

`--fail-fast` treats a workspace failure like any failing cell.

## 5. Offline `catacomb verify`: no change

The offline verifier contract already runs with cwd = evidence dir and `CATACOMB_WORKDIR=""`. Workspace cells are ephemeral by design, so offline re-verification relies on captured artifacts — the SDK's preferred path since SP1 (`cell.artifact()` reads the redacted evidence copy). No new seam; the spec only documents that workspace tasks must capture what their verifier needs via `artifacts:`.

## 6. Out of scope (decided now)

- No VCS or diff-format semantics in core; no `git apply`/`patch(1)` invocation ever.
- No parallel cell execution in this SP — isolation is its prerequisite, not its payload.
- No workspace reuse/caching across cells; no copy-on-write snapshots.
- No `actual_rev` ground-truth reporting contract (`CATACOMB_WORKSPACE_META`); additive later if declared-vs-materialized drift proves real.
- No workspace support in daemonless commands other than `bench` (`verify` is covered by §5).

## 7. Acceptance

**Unit (TDD, 100% coverage):** schema validation table (empty cmd, dir+workspace conflict, unreadable patch, teardown-without-cmd impossible by construction); effective-workspace resolution (task-only, variant-only, override, neither); patch sha256 computed at load; lifecycle ordering and unconditional teardown/cleanup via injected exec seams; `--keep-workspaces` and `--workspaces-dir`; stamps presence/absence; failure-note taxonomy of §4.

**Hermetic E2E (`e2e/hermetic/run.sh`, ~6 new assertions):**

1. **Isolation proof (the point of the SP):** a workspace task whose scripted agent exits non-zero if a marker file already exists in the workdir, then writes it. The E2E asserts every rep passed — rep 2 must not see rep 1's droppings. Run against today's shared-`dir` behavior, this assertion fails from rep 2 on.
2. `meta.json` env block carries `workspace.rev` and `workspace.patch_sha256` matching `sha256sum` of the patch file; non-workspace cells carry no block.
3. The "patch" is applied by the user command (`sh -c 'cat "$CATACOMB_PATCH" >> seed.sql'`) and its effect is visible to the verifier — proving the env handover without core patch semantics.
4. Teardown ran per cell (teardown appends to a log outside the workdir).
5. Workdirs are gone after the run; with `--keep-workspaces` they survive and teardown still ran.
6. A seeded `workspace.cmd` failure yields the operational note and, with `--fail-fast`, stops the run.

**Live weekly (`e2e/run.sh`):** one existing SQL basket task gains a workspace block (checkout = copy of a seed dir) to exercise the path against real `claude -p`; A-vs-A stays clean.

## 8. Affected surfaces

`bench/basket.go` (+tests) — schema, validation, sha256-at-load, effective-workspace resolution. `cmd/catacomb/bench.go` (+tests) — lifecycle, flags, env handover, notes. `evidence/evidence.go` (+tests) — `WorkspaceStamp`. `docs/guide/cli.md` — `bench` flags and workspace section. `e2e/hermetic/` — basket + assertions. `e2e/` live basket touch-up. No store, schema, regress, aggregate, or reduce changes.
