# Baseline bundle (`baseline export` / `baseline import`) — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `catacomb baseline export <name>` writes one byte-deterministic, hash-manifested `.tar.gz` containing the baseline row and its complete evidence dirs; `catacomb baseline import <bundle>` verifies and restores it on an ephemeral runner in one step. Decision record: [ADR-0032](../../adr/0032-baseline-bundle.md).

**Architecture:** one new file pair in `cmd/catacomb` (`baselinebundle.go` + tests) plus subcommand wiring in `baseline.go`; stdlib only (`archive/tar`, `compress/gzip`, `crypto/sha256`). Reuses: `model.Baseline` (name/run_ids/selector/created_at/runs_dir/stamps), the store's open/upsert path (`baseline set` flow), and pack's hardened dir-walk discipline (symlink refusal, `filepath.IsLocal`).

## Global constraints

- **No comments in Go code**; TDD; 100% coverage (`make cover`); gofumpt; `make lint`; testify; sentinel errors + `errors.Is`; operational errors → exit 2.
- **Byte-determinism is a tested contract**: export twice → identical bytes (a test asserts `sha256(bundle1) == sha256(bundle2)`); no `time.Now()` anywhere in the bundle content path (tar mtimes = the baseline row's `CreatedAt`; gzip header has zero ModTime and empty Name/OS byte normalized — construct `gzip.Writer` and explicitly zero `Header.ModTime`, set `Header.OS = 255`).
- **Extraction is untrusted input**: reject entry names that are absolute, contain `..`, or fall outside `runs/<runid>/…` (plus exactly one `bundle.json`); reject non-regular entries (symlink/hardlink/device); enforce `filepath.IsLocal` on every joined path; cap per-file size only by disk (no decompression-bomb cap needed beyond tar's own framing — document why in the spec section of the code review notes, not in code).
- Bundle format: versioned `bundle.json` **v1**; import refuses `version > 1` with a dedicated sentinel (mirror `ErrSchemaTooNew` posture).
- Commit after every green task; branch `feat/baseline-bundle` (based on this plan-doc branch).

---

### Task 1: bundle format — deterministic writer + manifest (`cmd/catacomb/baselinebundle.go`)

**Files:**

- Create: `cmd/catacomb/baselinebundle.go`, `cmd/catacomb/baselinebundle_test.go`

**Interfaces (produced):**

```go
type bundleManifest struct {
	Version  int               `json:"version"`
	Baseline model.Baseline    `json:"baseline"`
	Files    map[string]string `json:"files"`
}

const bundleVersion = 1

var (
	errBundleVersion   = errors.New("baseline bundle: format version newer than this catacomb supports")
	errBundleEntry     = errors.New("baseline bundle: entry escapes bundle root or is not a regular file")
	errBundleHash      = errors.New("baseline bundle: file hash mismatch")
	errBundleCollision = errors.New("baseline bundle: run dir exists with different content")
)

func writeBundle(w io.Writer, b model.Baseline, runsDir string) error
func readBundle(r io.Reader) (bundleManifest, map[string][]byte, error)
```

Notes: `Files` maps bundle-relative paths (`runs/<runid>/<rel>`) to hex sha256.
`writeBundle` walks each pinned run dir (sorted run IDs, sorted file walk —
reuse/mirror pack's symlink-refusing walk), buffers `bundle.json` FIRST as the
first tar entry, then files in sorted order; tar headers normalized: `Mode` 0o644,
`Uid/Gid` 0, `Uname/Gname` empty, `ModTime = b.CreatedAt.UTC()`, `Format =
tar.FormatUSTAR` (falls back to PAX only if a path exceeds USTAR limits — then
normalize PAX records; test with a long-ish path). `readBundle` streams the tar,
enforces the entry rules, returns manifest + file contents keyed by bundle path.
For memory sanity file contents may instead be returned via a callback — pick the
simpler shape that keeps regress-scale bundles (tens of MB) comfortable; the
callback form `readBundle(r io.Reader, onFile func(path string, r io.Reader) error) (bundleManifest, error)` is preferred.

- [ ] **Step 1: failing tests** — round-trip: build a fake runs dir with 2 run dirs × 2 files, a `model.Baseline` fixture with fixed `CreatedAt`; `writeBundle` → `readBundle` returns identical manifest + contents; determinism: two writes byte-identical (sha256 compare); hash coverage: every file appears in `Files` with correct sha256; hostile reads: entry `../evil`, absolute entry, symlink entry, unknown `bundle.json` version 2 → the right sentinels; truncated/garbage gzip → wrapped error.
- [ ] **Step 2:** RED → **Step 3:** implement → **Step 4:** GREEN; `make fmt`.
- [ ] **Step 5:** commit `feat(cmd): deterministic baseline bundle format (writer/reader, v1 manifest)`.

### Task 2: `baseline export` subcommand

**Files:**

- Modify: `cmd/catacomb/baseline.go` (subcommand wiring — read the existing `baseline set/list/rm` structure first and mirror it)
- Create/extend: `cmd/catacomb/baselineexport_test.go` (or extend the existing baseline test file per local convention)

**Interfaces:** `catacomb baseline export <name> --db <path> --runs-dir <dir> --out <bundle.tar.gz>`; flags mirror `baseline set` defaults (`--db` default `~/.catacomb/catacomb.db`, `--runs-dir` default `~/.catacomb/runs`). Behavior: load the named baseline from the store (unknown name → operational error, same shape as `baseline rm`'s); verify every pinned run dir exists under `--runs-dir` (missing → operational error naming the run id — matches the documented hard-error posture for missing pinned evidence); refuse `--out` that already exists (mirror `pack --out` semantics); write the bundle atomically (temp file + rename); print `exported baseline <name>: <out> (<n> runs, <m> files)`.

- [ ] **Step 1: failing CLI tests** — happy path (store seeded via the real `baseline set` flow against fixture evidence — reuse existing test helpers in the baseline tests); unknown baseline → exit-2 error; missing run dir → exit-2 naming the id; existing `--out` → exit-2; output is byte-deterministic across two invocations.
- [ ] **Step 2–4:** RED → implement → GREEN; `make fmt && make cover && make lint`.
- [ ] **Step 5:** commit `feat(cmd): baseline export`.

### Task 3: `baseline import` subcommand

**Files:**

- Modify: `cmd/catacomb/baseline.go`
- Create/extend: `cmd/catacomb/baselineimport_test.go`

**Interfaces:** `catacomb baseline import <bundle> --db <path> --runs-dir <dir>`; verifies every hash while extracting (stream through `sha256` — a mismatch aborts with `errBundleHash` before the baseline row is upserted); writes each run dir under `--runs-dir` with pack-style safe file writes (0700 dirs / 0600 files, mirror evidence perms); **idempotent collision policy**: an existing run dir is accepted iff every bundled file for it hash-matches what is on disk AND the dir contains no extra files under the bundled paths' tree — on match, skip writes for that run; any difference → `errBundleCollision` (exit 2, nothing mutated for that run — extract to a temp dir per run and rename into place so a mid-import failure never leaves a half-written run dir); after all runs land, upsert the baseline row with `RunsDir` set to the ABSOLUTE local `--runs-dir` and all other fields verbatim from the manifest (stamps included, so `--strict` behaves identically post-import); print `imported baseline <name>: <n> runs into <runs-dir>`; refuse version-newer bundles with `errBundleVersion`.

- [ ] **Step 1: failing CLI tests** — export→import round-trip into a fresh `--runs-dir`+`--db`: `baseline list` shows the row, `regress name:<name>` resolves (reuse the existing regress-over-fixture test helpers) with NO runs-dir mismatch warning; idempotent re-import → exit 0, no changes; tampered bundle (flip one byte in a file region) → `errBundleHash`, no row upserted; collision with different content → `errBundleCollision`; version 2 manifest → `errBundleVersion`; hostile entries covered at the format layer (Task 1) get one integration spot-check.
- [ ] **Step 2–4:** RED → implement → GREEN; `make fmt && make cover && make lint`.
- [ ] **Step 5:** commit `feat(cmd): baseline import`.

### Task 4: hermetic E2E + docs

**Files:**

- Modify: `e2e/hermetic/run.sh` — extend the existing baseline step (the one doing `baseline set`/`list`/`rm`) with: export the golden baseline → import into a FRESH runs-dir+db (simulating an ephemeral runner) → `regress name:golden --runs-dir <fresh>` exits with the same verdict as the original; assert byte-determinism (`export` twice, `cmp`); assert tampered-bundle refusal (flip a byte with `dd`, expect exit 2).
- Modify docs: `docs/guide/cli.md` (two subcommand sections with flag tables + exit codes, next to `baseline set/list/rm`), `docs/guide/workflows.md` (the CI recipe section: bundle-first story, commit-db fallback demoted to alternative), `docs/VERSIONING.md` (bundle format joins the surfaces list, v1, additive-only), `skills/catacomb/references/ci-gate.md` (rewrite the restore step around the bundle; keep artifact-restore framing), `docs/adr/README.md` (0032 row).
- Full hermetic suite green; markdownlint + lychee-offline clean.

- [ ] Implement; commit `test(e2e)+docs: baseline bundle round-trip gate and CI recipe`.

### Task 5: final review + PR

- [ ] Final whole-branch review (most capable model; determinism, extraction hardening, and collision atomicity are the named risks); fix wave if needed; re-review.
- [ ] PR `feat: baseline bundles — deterministic export/import for ephemeral CI (ADR-0032)` — base: the ADR docs branch; note the stack.

## Deliberately out of scope

- Bundling `--record`/trends history (persistent-store concern, per ADR-0032).
- pack adopting the hash manifest (follow-up).
- Encryption/signing of bundles (CI artifact stores handle transport; cosign-style signing is a roadmap note, not v1).
- GitHub Action integration (separate roadmap item that consumes this).
