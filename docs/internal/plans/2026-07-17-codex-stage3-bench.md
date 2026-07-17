# Codex ingestion stage 3 (bench spawn + live E2E) — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `catacomb bench` runs Codex baskets end to end — spawn `codex exec --json` cells, peek the thread id, resolve the rollout, record evidence — with a hermetic bench scenario and an optional live E2E leg. This completes ADR-0031: Codex support goes from import-only to full.

**Architecture:** the bench cell lifecycle is already runtime-agnostic except three seams: the stdout peek (claude stream-json vs codex `thread.started`), transcript resolution (`~/.claude/projects` vs `~/.codex/sessions`), and the reduce dispatch (which stage 1 already generalized as `parseTranscriptsFor`). Spec of record: [ADR-0031](../../adr/0031-multi-runtime-ingestion-codex.md) + design spec §4.

**Tech stack:** Go stdlib; existing e2e bash harnesses. No new dependencies.

## Global constraints

- **No comments in Go code**; TDD; 100% coverage; gofumpt; `make lint` clean; testify; sentinel errors.
- Claude bench behavior byte-unchanged (peek, resolution, cost, markers).
- Codex cells record `CostUSD: nil` in manifest and meta (the exec stream carries no dollar cost — ADR-0030 import parity); the token-derived `cost_usd` metric prices via stage 2.
- Zero API spend in hermetic paths; the live leg must SKIP cleanly (not fail) when `codex` is absent or unauthenticated.
- Commit after every green task; branch `feat/codex-stage3` (stacked on `feat/codex-stage2`).

---

### Task 1: bench spawn for codex baskets (`cmd/catacomb`)

**Files:**

- Modify: `cmd/catacomb/childlocal.go` (codex peek), `cmd/catacomb/bench.go` (runtime dispatch: peek choice, resolution, reduce, rejection removal, `--sessions-dir` flag), `cmd/catacomb/codextranscripts.go` (retry wrapper)
- Test: `cmd/catacomb/childlocal_test.go`, `cmd/catacomb/bench_offline_test.go` (the codex-rejection test flips to a success-path test), `cmd/catacomb/codextranscripts_test.go`

**Interfaces:**

- New `codexPeek` struct mirroring `streamPeek` (childlocal.go:17-37): decodes `{"type":"thread.started","thread_id":"..."}`; first non-empty `thread_id` wins; it never sets a cost (no cost event exists). Both peeks satisfy a tiny consumer-side seam — either keep two structs and branch at the call site (bench.go:225 `peek := &streamPeek{}`), or extract `type peeker interface{ onLine([]byte) }` plus accessors; prefer whichever keeps the diff smallest.
- `resolveCodexTranscriptsRetry(root, threadID string, attempts int, delay time.Duration)` mirroring `resolveTranscriptsRetry` (transcripts.go:42-58) — the rollout file exists during the session but children may land late; same 6×500ms defaults at the call site.
- bench gains `--sessions-dir` (default `~/.codex/sessions` via `benchDefaultDir(home, ".codex", "sessions")`), used only for codex baskets — flag help mirrors import's.
- Cell record path: where bench currently calls `resolveTranscriptsRetry(o.projectsDir, entry.SessionID, …)` (bench.go:266) and the claude-only parse, dispatch on `basket.EffectiveRuntime()`: codex → `resolveCodexTranscriptsRetry(o.sessionsDir, …)` + `parseTranscriptsFor(drift.RuntimeCodex, …)` with mainRunID = the peeked thread id. Marker window synthesis (boundaryObservations from transcript time bounds) and meta assembly (bench.go:381-385) are already runtime-neutral — verify, don't fork.
- The bench codex rejection (bench.go:31 area) is REMOVED; `runBench` proceeds for codex baskets. The exact-string rejection test becomes the spawn test below.

- [ ] **Step 1: failing peek tests** — table: `thread.started` line → thread id captured; later `thread.started` ignored (first wins); `turn.completed` with usage → no cost recorded; non-JSON lines ignored; claude `streamPeek` cases untouched.
- [ ] **Step 2–3:** implement `codexPeek` → green; commit `feat(cmd): codex exec --json stream peek`.
- [ ] **Step 4: failing bench integration test** — pattern-match the existing offline bench tests (`bench_offline_test.go`): a codex basket whose task cmd is a helper script that (a) prints a `thread.started` line with a fixture thread id and (b) writes a minimal fixture rollout into a temp sessions tree (`<tmp>/2026/07/17/rollout-…-<threadid>.jsonl`); run `bench --runs-dir … --sessions-dir <tmp>`; assert: cell marked, evidence dir written with `session.jsonl`, meta `agent_runtime == "codex"`, `CostUSD == nil`, manifest cost nil, marker window == transcript bounds. Also: a codex basket with a cmd that emits NO thread.started → cell fails with the same "no session id observed" note the claude path uses.
- [ ] **Step 5–6:** implement dispatch (+ flag, + retry wrapper, remove rejection) → green. Remove/replace the rejection unit test. `make fmt && make cover && make lint`.
- [ ] **Step 7:** commit `feat(bench): spawn codex cells — thread peek, rollout resolution, evidence parity`.

### Task 2: hermetic production scenario `56-codex-bench`

**Files:**

- Create: `e2e/hermetic/prod/scenarios/56-codex-bench.sh` + `e2e/hermetic/prod/fixtures/56-codex-*.tmpl` (fake-codex script template + rollout templates — reuse the 55 fixture family shapes)

**Requirements:**

- Fake codex: a `fake-codex.sh` fixture the basket's task cmd invokes; it reads env (`FAKE_THREAD_ID`, `FAKE_SESSIONS_DIR`, `FAKE_ROLLOUT_TMPL`), prints `{"type":"thread.started","thread_id":"<id>"}` plus a couple of `item.completed`/`turn.completed` lines to stdout, and copies the templated rollout into `$FAKE_SESSIONS_DIR/2026/07/17/rollout-2026-07-17T12-00-00-<id>.jsonl`. Zero network, zero real binaries beyond bash/sed.
- Basket: `runtime: codex`, reps 3, variants baseline/degraded (degraded rollout = error tool result + 3x tokens_out, as in 55), `checkpoints: [plan]` with the mark MCP pair in the rollouts; per-cell distinct thread ids via env templating in `variants[].env` + `$CATACOMB_*`? — bench sets no per-rep env var, so derive uniqueness inside fake-codex from `$RANDOM`/`uuidgen`-free deterministic counter file in `$FAKE_SESSIONS_DIR` (a `seq` file incremented per invocation) and template the rollout id accordingly; the scenario asserts 6/6 cells marked.
- Drive: `catacomb bench <basket> --runs-dir … --sessions-dir $FAKE_SESSIONS_DIR --manifest …` (exit 0, "marked 6/6 cells"); then `regress` baseline-vs-degraded (exit 1, tokens_out + error_rate + phase plan rows) and A-vs-A on a re-bench of baseline into a second variant label — simpler and equally honest: assert the degraded gate only, plus meta/manifest cost-nil assertions and the checkpoint marker node; A-vs-A coverage already exists in 55 over the same reduce path.
- Register: nothing needed (flat `scenarios/*.sh` glob).
- Full hermetic suite must pass locally (`make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh`; prepend `$PWD/bin` to PATH if invoking prod/run.sh standalone — a stale system catacomb exists).

- [ ] Scenario + fixtures; full-suite run green; commit `test(e2e): hermetic codex bench production scenario`.

### Task 3: live E2E leg (optional, skip-clean)

**Files:**

- Create: `e2e/basket-codex.yaml`, `e2e/codex-live.sh`
- Modify: `e2e/run.sh` (new optional section), `.github/workflows/e2e-live.yml` (optional env + step wiring)

**Requirements:**

- `codex-live.sh`: `exec codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "$PROMPT" < /dev/null`.
- `basket-codex.yaml`: `basket: e2e-codex`, `runtime: codex`, reps 3, one task (`id: answer`, cmd `["./codex-live.sh"]`), two variants (short prompt vs verbose chain-of-thought prompt), mirroring basket-presence's shape.
- `e2e/run.sh`: a new section AFTER the existing baskets, gated: if `! command -v codex` OR `codex exec` auth-probe fails (`codex exec -m gpt-5.4-mini --skip-git-repo-check --json "ping" < /dev/null` with short timeout — or the cheapest reliable auth check found in `codex --help`) → `skip "codex live leg (codex unavailable/unauthenticated)"` and continue with exit 0 semantics. When available: bench (6 cells), then regress baseline-vs-candidate asserting the report RENDERS (exit 0 or 1 both acceptable — live tokens_out may genuinely gate) and `--json` parses; assert every meta has `agent_runtime: codex`. Do NOT hard-assert A-vs-A cleanliness here (n=3, new runtime — gather data first; log the verdict).
- `e2e-live.yml`: pass `CODEX_API_KEY` from secrets into the run step env IF the secret exists (workflows can't branch on secret presence directly — export unconditionally; run.sh's auth probe handles absence by skipping); add one line to the workflow header comment documenting the optional leg + expected extra spend (~$0.05 on gpt-5.4-mini). Keep actions SHA-pinned; zizmor/actionlint must stay green (`npx --yes` equivalents not needed — reviewer checks lint in CI).
- The costs note: rollouts have no dollar cost; run.sh's spend accounting for this leg reports token counts (or notes "codex: token-billed, no reported cost").

- [ ] Implement; validate the gating logic locally BOTH ways (with codex present it runs — but to avoid spend in this task, validate the skip path by `PATH`-masking codex, and validate the run path with a single manual `bash -n` + dry-run of the section guarded behind an env var if needed; the REAL live run happens in Task 5); commit `test(e2e): optional live codex leg`.

### Task 4: documentation — codex goes full-support

**Files:**

- Modify: `README.md` (feature bullet + blockquote drop "import-only" qualifiers; requirements note codex optional), `docs/guide/cli.md` (bench: codex contract — cmd must emit `codex exec --json`, `< /dev/null` stdin note, `--sessions-dir` flag row, cost-nil parity; REMOVE the rejection paragraph; exit-code list unchanged), `docs/guide/basket.md` (runtime row: bench now supported; update the "What happens if" bullet), `docs/guide/ingestion.md` (Runtimes section: bench entry point added; "What stays Claude-only" shrinks to replay/subgraph/raw-transcript export), `docs/guide/troubleshooting.md` (REPLACE the bench-rejection row with a "codex cell fails: no thread id observed" row → cmd must pass `--json`; keep the no-transcript row), `docs/guide/workflows.md` ONLY if it hardcodes claude-only bench claims (grep `claude -p` context), `skills/catacomb/references/setup.md`/`ci-gate.md` ONLY if they assert claude-only bench (grep; minimal touch).
- Every claim cross-checked against the built binary (`bin/catacomb bench --help`). markdownlint + anchors clean.

- [ ] Implement; commit `docs: codex full bench support`.

### Task 5: live validation + final review + PR

- [ ] Real live run on the maintainer machine: `bench e2e/basket-codex.yaml`-equivalent scratch basket (reps 2, 4 cells, gpt-5.4-mini — a few cents) → regress renders with cost_usd/tokens rows; capture VALIDATION.md.
- [ ] Final whole-branch review (subagent, most capable model) incl. accumulated-Minors triage; fix wave if needed; re-review.
- [ ] PR `feat: codex bench spawn + live E2E — full runtime support (ADR-0031 stage 3)` stacked on `feat/codex-stage2`.

## Deliberately out of scope

- `codex exec resume` sessions, `--ephemeral` runs (no rollout — nothing to gate), desktop-app sqlite stores.
- Parallel bench, interleaving (separate roadmap item).
- A CI-blocking codex live gate (the leg is advisory/skip-clean until auth + budget are provisioned).
