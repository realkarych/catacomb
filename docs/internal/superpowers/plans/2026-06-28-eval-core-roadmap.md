# Eval-Core Roadmap — Implementation Plan

> **For agentic workers:** This is the PROGRAM-level plan. Each PR below gets its own detailed, bite-sized task plan (authored just-in-time before implementation via superpowers:writing-plans) and is executed with superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking at the per-PR plan level.

**Goal:** Turn catacomb from a faithful single-session observability tool into a genuine **eval-benchmark substrate** for Claude Code agentic pipelines — by completing the OpenInference export, computing the cross-run `step_key`/`phase_key` identity, persisting annotations, adding deterministic session diff, on-demand export/diff CLIs, interop serializers (agentevals/EvalView/DeepEval), reproducibility metadata, and keeping the first-party UI as an opt-in, interop-compatible battery.

**Architecture:** catacomb **owns the substrate** (deterministic 4-source reconciliation → canonical graph → `step_key` cross-run identity → faithful cost/time → lossless + OpenInference export) and **delegates** visualization to Phoenix, scoring to DeepEval/Agent GPA, and part of diffing to agentevals/EvalView — while shipping its own deterministic diff and UI as batteries. Deployment model is **Scenario B: batch eval-harness** (`replay → reduce → finalize → export/diff`, offline, reproducible over many benchmark runs) — NOT a multi-tenant service, so the storage layer is NOT rewritten.

**Tech Stack:** Go 1.26 (pure Go, no cgo), `modernc.org/sqlite`, OTLP/OTel (`go.opentelemetry.io/otel`), gRPC/protobuf, cobra CLI, bubbletea/lipgloss TUI, Vite + Svelte 5 web UI (vitest + Playwright). DeepEval adapter is Python (isolated subdir).

## Global Constraints

- **Always work in a git worktree** under `.claude/worktrees/` (native `EnterWorktree`); never edit the shared checkout.
- **Pure Go, no cgo**; single static cross-platform binary; stdlib-first, minimal deps; SQLite only via `modernc.org/sqlite`.
- **No comments in Go code** — none, not even doc comments; only `//go:build`/`//go:embed`/`//go:generate` directives. Enforced by `internal/codepolicy`.
- **100% test coverage, TDD-first.** Failing test → minimal impl → refactor under green. The threshold never goes down. Frontend: 100% on pure logic (vitest) + Playwright e2e; Svelte components not line-gated.
- **Deterministic core:** same observations in any order → same graph. `step_key` is a pure function of the final post-cascade graph; rebuild-from-log preserves annotations.
- **Errors:** sentinels checked with `errors.Is`/`errors.As`; wrap with `fmt.Errorf("pkg.Op: %w", err)`; never parse error strings. **Logging:** `log/slog` JSON; never log/serialize secrets; payloads only leave through the redaction policy (ADR-0008/0020).
- **Workflow:** one PR = one logical change; feature branch from `master`; squash-merge; **CI green before merge**; no `--no-verify`; no force-push to `master`. `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`).
- **Live-verify gate (non-negotiable — the recurring lesson):** green `make cover`/lint/CI MISS production-path wiring gaps and real-data bugs. Before merging any PR, verify against a real daemon / real export sink / real session (Playwright for web, PTY for TUI, OTLP capture or local Phoenix for the exporter), not only unit tests.

---

## Strategic framing (why these PRs)

From the 2026-06-28 head-to-head (see memory `catacomb-eval-core-gap`): catacomb built the commoditized 80% (capture + reconcile + visualize one session) and deferred the eval-differentiating 20%. The defensible wedge is the **deterministic reconciled action graph as an eval/diff substrate**. The highest-leverage gaps, in order:

1. The OpenInference exporter is **hollow (~55%)** — emits empty-named spans without I/O, model, or tool names → Phoenix renders nothing useful. Small fix, unlocks the whole interop ecosystem.
2. `step_key` (the cross-run join key) field exists but the reducer **never computes it** → no diff, no alignment, no cross-run eval is possible.
3. Annotations (the eval-scoring bridge) **aren't persisted** → any downstream scorer's work is silently lost on merge/rebuild.
4. No `export`/`diff`/`snapshot`/`runs` CLI → no batch-harness entrypoint (only live daemon + `replay --export-jsonl`).

"Detect wasteful/unjustified steps" via LLM-judge is **research-unsolved** (RedundancyBench ~25%); we lean on deterministic diff (solved) + feed external scorers, and do NOT build a homegrown judge.

## Dependency DAG

```
PR-1 (OpenInference exporter) ───────────────▶ PR-6 (export/snapshot CLI)
PR-2 (step_key/phase_key) ──┬──▶ PR-5 (diff + catacomb diff CLI) ──▶ PR-10 (UI interop)
                            ├──▶ PR-7 (agentevals/EvalView serializers) ──▶ PR-6
                            └──▶ PR-8 (DeepEval adapter)
PR-3 (annotations persist)  ── independent (store/cdc/reduce)
PR-4 (markers + phase_key)  ── independent (ingest/reduce); enriches PR-2's phase_key
PR-9 (repro metadata)       ── independent (run model)
```

**Execution order (sequential to keep `master` green; shared-file PRs serialized):** PR-1 → PR-2 → PR-9 → PR-4 → PR-3 → PR-5 → PR-6 → PR-7 → PR-8 → PR-10. PR-1/PR-2/PR-9 touch different areas and could parallelize, but `model.go`/`reduce.go` overlap argues for serial landing with rebase.

---

## PR-1 — Complete the OpenInference OTLP exporter

**Goal:** Make `export/otlp` emit spans a real OpenInference consumer (Phoenix) renders fully.

**Files:** Modify `export/otlp/export.go` (+ its test). Read-only refs: `model/model.go` (Node/Payload), `ingest/otel/otel.go` (attr keys).

**Contract (exact OpenInference keys to add, alongside the existing `openinference.span.kind` + dual `gen_ai.*`/`llm.token_count.*` + `llm.cost.total`):**

- `llm.model_name` and `gen_ai.request.model` ← `node.Attrs["model"]` (LLM spans).
- `tool.name` ← `node.Attrs["name"]` (TOOL spans); plus `tool_call.function.name` + `tool_call.function.arguments` (arguments = JSON string).
- `input.value` / `output.value` (+ `*.mime_type`) ← `node.Payload.Input`/`Output`, **redacted per ADR-0020**, size-capped.
- `graph.node.id` ← `node.id`; `graph.node.parent_id` ← `node.parent_id` (empty ⇒ root); `graph.node.name` ← `node.name`.
- `llm.cost.prompt`/`llm.cost.completion` where derivable; keep `llm.cost.total`.
- Resource attr `openinference.project.name` (configurable; default e.g. `catacomb`).
- `session.id` ← run/session hash (Phoenix groups by it).

**Acceptance:** unit round-trip asserts every key above for each node type; **live-verify**: emit a real session to a local Phoenix (or OTLP capture) and confirm spans show tool names, I/O, model, and the agent graph. Span still flushed only on genuine terminal / lifecycle close (batch-friendly). 100% cover, lint clean.

**Risks:** OSS Phoenix may render the `graph.node.*` agent-graph only in Arize AX — emit regardless (standard attrs); verify the span-tree trajectory view in the target Phoenix version. Cap base64/large payloads client-side.

## PR-2 — Compute `step_key` + `phase_key` in the reducer/finalizer

**Goal:** Populate the cross-run join key so diff/alignment/eval become possible.

**Files:** Modify `model/model.go` (ensure `step_key`, add `step_key_method`/confidence to attrs or field), `reduce/` (new pure `stepkey.go` + finalize hook), `redact/` (reuse normalization). Tests alongside.

**Contract (ADR-0016 + amendments):** `step_key` = pure function of the **final post-cascade** graph, computed over the **rich/core-tier structural path pre-contraction**, walking **only live (non-`superseded`/`abandoned`) ancestors** (superseded/abandoned siblings excluded from ordinal/sibling-index counting): structural path (prompt-subtree ordinal, depth, sibling index) + tool `name` + subagent `agentType` + **post-redaction** normalized salient-input hash (file path + normalized args for `Edit`; command for `Bash`; redaction placeholders → stable typed token e.g. `‹redacted:uri›`). Excludes volatile ids, timestamps, `seq`. `phase_key` = enclosing-step structural path + marker name + occurrence-ordinal within scope. Tag `step_key_method` like `identity=heuristic`.

**Acceptance:** property test — same logical pipeline run twice (two fixtures with different ids/timestamps) yields **equal `step_key`** for corresponding steps; distinct steps get distinct keys; key is **invariant to observation order** and **identical in lean vs rich mode**; recompute-on-rebuild is stable. 100% cover.

**Risks:** heuristic collisions/drift — ship the confidence tag; never use `step_key` for within-run merging (that stays `execution_id`-keyed). Redaction is currently serve-time; compute `step_key` at finalize/export time where the redactor is available.

## PR-9 — Reproducibility metadata

**Goal:** Make runs comparable for benchmarking (spec §15).

**Files:** Modify `model/model.go` (`Run` repro fields), the run-metadata population path (daemon/reduce/ingest), summaries + exports.

**Contract:** each `Run` records pinned `model_id` (already done), plus version hashes of: system/user prompts, skills, subagent definitions, and catacomb config; and the Claude Code/SDK version (for #53954/span-schema gating). Surfaced in `SessionSummary` and every exporter.

**Acceptance:** two runs with identical agent config produce identical repro hashes; a changed prompt/skill/subagent-def/config changes its hash; missing inputs hash to a stable "unknown" sentinel, never panic. 100% cover.

## PR-4 — Markers + `phase_key` emission

**Goal:** User-defined phase boundaries → per-phase cost/time + `phase_key` (spec §5.6).

**Files:** `ingest/` (marker channels), `reduce/` (pair start/end into `marker` node, `marker_span` edges, occurrence ordinal), `model/` if needed, CLI/daemon endpoint for direct POST.

**Contract:** emission channels (most-robust first): no-op MCP `catacomb__mark`, sentinel `UserPromptSubmit`/log convention, direct daemon POST. Pair `start`/`end` by name+occurrence; link phase nodes via `marker_span`; opaque `state_ref` stored uninterpreted. Feeds `phase_key` (PR-2).

**Acceptance:** a start/end pair across out-of-order observations yields one `marker` node + correct `marker_span` membership; occurrence ordinal increments per repeat; deterministic on rebuild. 100% cover.

## PR-3 — Persist annotations (ADR-0016 contract)

**Goal:** Durable, namespaced, lifecycle-preserved annotation slot — the eval-scoring bridge.

**Files:** `store/` (new `annotations` table + recovery path), `reduce/` (carry-over hooks), `cdc/` (rev bump + emit), a write API (library method + gated HTTP `PATCH` handler in `daemon/`).

**Contract:** annotations keyed by the immutable **(`execution_id`, source-native event key)** handle (NOT node id, NOT `step_key`); namespaced `annotations.<owner>.<key>`; **carry-over** on heuristic→canonical merge, supersede/cancel, and rebuild-from-log (re-attached by handle; `step_key` as secondary index); collision policy = union, same key → last-writer-wins; a write bumps `rev` + emits `node_upsert`; `node_merge` moves old→new. Annotations live in their own recovery-aware table, **not** in the observation log.

**Acceptance:** annotation survives a heuristic→canonical id change, a supersede, and a full rebuild-from-log; namespaced writers don't collide; CDC delta emitted on write. 100% cover.

## PR-5 — Session diff by `step_key` + `catacomb diff` CLI

**Goal:** Deterministic, reference-free diff of two runs of the same pipeline — the owner's "diff sessions".

**Files:** new `diff/` pkg (pure align/compare), `cmd/catacomb/diff.go` (+ tests).

**Contract:** align nodes of run A and run B by `step_key`; classify each as added / removed / changed / unchanged; for changed report deltas in tool args (normalized), `cost_usd`, `duration_ms`, `tokens_in/out`, `status`; stable ordering; `--json` machine output + human summary.

**Acceptance:** identical runs → empty diff; an added tool call → one `added`; a changed `Bash` command → one `changed` with the arg delta; cost regression surfaced. Deterministic. 100% cover. **Live-verify** on two real runs of one pipeline.

## PR-6 — `catacomb export` / `snapshot` / `runs` / `inspect` CLI

**Goal:** The batch-harness entrypoint (spec §12) — on-demand export from the store, no live daemon.

**Files:** `cmd/catacomb/export.go`, `snapshot.go`, `runs.go`, `inspect.go` (+ tests); read-only store open.

**Contract:** `catacomb export --to jsonl|otlp|neo4j|postgres [--run <id>] [--mode]`; `catacomb snapshot [--run <id>]`; `catacomb runs` (list the forest); `catacomb inspect <run_id>`. Opens the store read-only (one-shot verbs, ADR-0010). Reuses PR-1 exporter + PR-7 serializers.

**Acceptance:** export a stored run to each sink and round-trip back to graph-equality; `runs`/`inspect` list/query without a running daemon. 100% cover. **Live-verify** against a real store.

## PR-7 — agentevals + EvalView serializers

**Goal:** Feed the two deterministic external diff/trajectory tools.

**Files:** `export/agentevals/` (OpenAI message-array), `export/evalview/` (`trace_spec_version 1.0` JSONL), wired into PR-6 CLI (+ tests).

**Contract:** **agentevals** — list of OpenAI-format messages: `{role, content}`, assistant `tool_calls:[{function:{name, arguments}}]` with `arguments` a **JSON string**, `{role:"tool", content}` results; also a graph form `{results:[...], steps:[[node-name...]]}` for `graph_trajectory_strict_match`. **EvalView** — newline-delimited `trace_start`/`span`/`trace_end`; span fields `span_id`/`parent_span_id`/`trace_id`/`span_type`(agent|llm|tool|mcp|http)/`name`/`start_time`/`end_time`/`latency_ms`/`status`, typed `llm{provider,model,input_tokens,output_tokens,cost_usd}` / `tool{tool_name,tool_args_bytes,tool_result_bytes,tool_success}`.

**Acceptance:** golden-file tests for both schemas; a Python smoke test feeds the agentevals output to `create_trajectory_match_evaluator` and `graph_trajectory_strict_match` and they accept it. 100% Go cover.

## PR-8 — DeepEval Python adapter

**Goal:** Offline process-scoring without a homegrown judge.

**Files:** `integrations/deepeval/` (Python pkg: reader + adapter + tests + README).

**Contract:** read catacomb lossless JSONL → `LLMTestCase(input, actual_output, tools_called=[ToolCall(name, input_parameters=<dict>, output=...)], expected_tools=...)`; run `ToolCorrectnessMetric` (deterministic, no key) + `ArgumentCorrectnessMetric`; **document** the `@observe`-replay path for trace-only `StepEfficiencyMetric`/`TaskCompletionMetric`. Use `input_parameters=` (NOT `input=`).

**Acceptance:** Python unit tests (pytest) on a fixture JSONL; isolated from the Go coverage gate; README shows the offline run. Note in CI that this dir is excluded from Go coverage.

## PR-10 — UI interop compatibility

**Goal:** Keep the first-party UI as an opt-in battery, compatible with the ecosystem; surface the new substrate.

**Files:** `webui/web/src/` (Outline/NodeDrawer surface `step_key`; a minimal diff view over PR-5; optional "export / open in Phoenix" affordance), `lib/` logic + tests.

**Contract:** UI survives the `step_key`/annotations model changes; minimalist / silence-when-healthy (no decorative chrome); diff view reuses PR-5's engine output. UI is never the only path — exports remain first-class.

**Acceptance:** existing Playwright e2e green on a real session; new diff view live-verified on two real runs; vitest 100% on new logic.

---

## Execution strategy

- **Per PR:** fresh worktree off updated `master` → author the detailed bite-sized plan (writing-plans) → subagent-driven-development (fresh subagent per task, TDD, review between) → whole-branch opus review (requesting-code-review) → **live-verify** → push → open PR → wait for green CI → squash-merge → delete worktree → rebase next.
- **Diagnostics caveat (recurring):** stale gopls/compiler diagnostics (undefined helpers, missing-method, WrongArgCount) on catacomb are almost always false — confirm via `go build`/`go test`, never trust the editor diagnostic.
- **Interrupt autonomy only on genuine uncertainty** (a real design fork where the owner's intent is unclear) — otherwise proceed and report.
