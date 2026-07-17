# Codex ingestion stage 2 (gate-quality metrics) — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Codex baskets gate with the same power Claude baskets have: correct token semantics, a priced `cost_usd` metric (OpenAI tiers), and per-runtime step-key salience so Codex step keys stop churning per rep.

**Architecture:** three seams, no new packages: `ingest/codex` (token semantics), `pricing` (OpenAI table + families), `stepkey` (additive salience cases). Spec of record: [ADR-0031](../../adr/0031-multi-runtime-ingestion-codex.md) + design spec §3. Prices verified against <https://developers.openai.com/api/docs/pricing> (raw payload, 2026-07-17); single primary source — openai.com/api/pricing returned 403.

**Tech stack:** Go stdlib only; no new dependencies.

## Global constraints

- **No comments in Go code** — none, not even doc comments (`internal/codepolicy`).
- **TDD**; 100% file/package/total coverage (`make cover`); threshold never goes down.
- `gofumpt` + `goimports`; `make lint` clean; testify; table-driven tests.
- **Claude behavior must not change**: existing Claude pricing tiers, Claude salience projections, and Claude token semantics are frozen (stepkey/v1 compatibility surface — a salience change re-keys every existing baseline). Characterization tests pin this.
- `cost_usd` stays `Source: "estimated"`; unpriced models stay unpriced (no guessing).
- Commit after every green task; branch `feat/codex-stage2` (stacked on `feat/codex-import`).

---

### Task 1: Codex token semantics — uncached `tokens_in` + `cache_write` attr

**Files:**

- Modify: `ingest/codex/codex.go` (turn emission), `ingest/codex/codex_test.go`
- Modify: `docs/internal/specs/2026-07-16-codex-ingestion-design.md` (§2.1 assistant_turn row — one-line amendment)
- Check (likely no change): `e2e/hermetic/prod/scenarios/55-codex-import.sh` asserts tokens_out only

**Why:** OpenAI's `input_tokens` INCLUDES `cached_input_tokens`; Anthropic's excludes cache reads. Stage 1 mapped `tokens_in = input_tokens`, which (a) breaks parity with Claude's uncached-input semantics and (b) would double-count cached tokens once pricing lands (Engine.Cost charges TokensIn at full rate PLUS CacheReadIn at cache rate). Stage 1 is unmerged — amend cleanly now.

**Interfaces:**

- assistant_turn attrs become: `tokens_in` = max(0, last_token_usage.input_tokens − cached_input_tokens); `cache_read_in` = cached_input_tokens (unchanged); NEW `cache_write` = cache_write_input_tokens (0 omitted, mirroring the Claude adapter's attr name); `tokens_out` unchanged (includes reasoning tokens — that is what the output rate bills).
- Known limitation to record in the spec amendment: whether `input_tokens` also includes `cache_write_input_tokens` is unverified upstream (new in gpt-5.6); no subtraction for it until observed.

- [ ] **Step 1: failing tests** — update the basic.jsonl expectations: fixture has input 11663 / cached 5504 → expect `tokens_in == 6159`; add a case with `cache_write_input_tokens: 500` → attr `cache_write == 500`; add a degenerate case cached > input → `tokens_in == 0`.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5:** amend spec §2.1 row (tokens_in = uncached input; cache_write mapped; rationale one clause: pricing correctness + Claude parity).
- [ ] **Step 6:** `make fmt && make cover && make lint`; run hermetic scenario 55 (`e2e/hermetic/run.sh` full or the prod runner) — its assertions use tokens_out (fixtures carry no cached tokens), must stay green.
- [ ] **Step 7:** commit `fix(ingest/codex): uncached tokens_in + cache_write mapping (pricing-correct, Claude-parity)`.

### Task 2: OpenAI pricing tiers

**Files:**

- Modify: `pricing/pricing.go` (`defaultTable()`, `defaultFamilies()`), `pricing/pricing_test.go`

**Interfaces:** none new — data only. `familyTier` already picks the LONGEST matching prefix, so family order is irrelevant; exact-table lookup precedes families.

**Exact table additions (USD per 1M tokens: Input / CacheRead / Output / CacheWrite), verified 2026-07-17:**

| id | In | CacheRead | Out | CacheWrite |
|---|---|---|---|---|
| gpt-5.6-sol | 5.00 | 0.50 | 30.00 | 6.25 |
| gpt-5.6-terra | 2.50 | 0.25 | 15.00 | 3.125 |
| gpt-5.6-luna | 1.00 | 0.10 | 6.00 | 1.25 |
| gpt-5.5 | 5.00 | 0.50 | 30.00 | 0 |
| gpt-5.5-pro | 30.00 | 0 | 180.00 | 0 |
| gpt-5.5-cyber | 12.50 | 1.25 | 75.00 | 0 |
| gpt-5.4 | 2.50 | 0.25 | 15.00 | 0 |
| gpt-5.4-mini | 0.75 | 0.075 | 4.50 | 0 |
| gpt-5.4-nano | 0.20 | 0.02 | 1.25 | 0 |
| gpt-5.4-pro | 30.00 | 0 | 180.00 | 0 |
| gpt-5.2 | 1.75 | 0.175 | 14.00 | 0 |
| gpt-5.2-pro | 21.00 | 0 | 168.00 | 0 |
| gpt-5.1 | 1.25 | 0.125 | 10.00 | 0 |
| gpt-5 | 1.25 | 0.125 | 10.00 | 0 |
| gpt-5-mini | 0.25 | 0.025 | 2.00 | 0 |
| gpt-5-nano | 0.05 | 0.005 | 0.40 | 0 |
| gpt-5-pro | 15.00 | 0 | 120.00 | 0 |
| gpt-5.3-codex | 1.75 | 0.175 | 14.00 | 0 |
| gpt-5.2-codex | 1.75 | 0.175 | 14.00 | 0 |
| gpt-5.1-codex-max | 1.25 | 0.125 | 10.00 | 0 |
| gpt-5.1-codex | 1.25 | 0.125 | 10.00 | 0 |
| gpt-5-codex | 1.25 | 0.125 | 10.00 | 0 |
| gpt-5.1-codex-mini | 0.25 | 0.025 | 2.00 | 0 |
| codex-mini-latest | 1.50 | 0.375 | 6.00 | 0 |

**Family additions** (cover dated snapshots `<base>-YYYY-MM-DD`, which the existing `[@-]\d{8}$` normalizer does NOT strip): `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, `gpt-5.5`, `gpt-5.4-mini`, `gpt-5.4-nano`, `gpt-5.4`, `gpt-5.3-codex`, `gpt-5.2-codex`, `gpt-5.2`, `gpt-5.1-codex-max`, `gpt-5.1-codex-mini`, `gpt-5.1-codex`, `gpt-5.1`, `gpt-5-codex`, `gpt-5-mini`, `gpt-5-nano`, `gpt-5` (fallback) — each with its exact-table tier.

**Deliberately unpriced** (test-pinned): `codex-auto-review` (no API price exists), `gpt-5.3-codex-spark` (research preview, no API price), `gpt-5.4-cyber` (listed unpriced upstream — accept that the `gpt-5.4` family would claim a hypothetical dated snapshot of it; the bare id must stay unpriced, so it must NOT be added to the exact table and the test asserts `Cost` returns ok=false for `codex-auto-review` and `gpt-5.3-codex-spark`; `gpt-5.4-cyber` bare id falls to the `gpt-5.4` family by prefix — assert the current behavior explicitly so the tradeoff is visible and pinned).

- [ ] **Step 1: failing tests** — hand-checkable vectors: `gpt-5.4-mini` {in 6159, cached 5504, out 16} → 0.00510405; `gpt-5.6-sol` {in 1000, out 100, cacheRead 2000, cacheWrite 500} → 0.012125; snapshot `gpt-5.5-2026-04-23` → gpt-5.5 tier via family; `gpt-5.1-codex-mini-x` longest-prefix beats `gpt-5.1-codex`; `codex-auto-review` → ok=false; Claude vectors unchanged (existing tests must not be touched).
- [ ] **Step 2:** FAIL → **Step 3:** data-only implementation → **Step 4:** PASS.
- [ ] **Step 5:** full hermetic suite (`make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh`) — scenario 55's fixtures carry model `gpt-5.4-mini`, so `cost_usd` becomes a live metric there; verify A-vs-A stays clean (identical tokens → identical cost) and the degraded gate still exits 1. If cost rows change scenario output, extend the scenario's assertion ONLY if it fails; report what happened either way.
- [ ] **Step 6:** `make fmt && make cover && make lint`; commit `feat(pricing): OpenAI GPT-5-family tiers for the codex cost_usd metric`.

### Task 3: Codex step-key salience

**Files:**

- Modify: `stepkey/stepkey.go` (`salient`, new small helpers), `stepkey/stepkey_test.go`

**Interfaces:** additive `switch normTool(name)` cases; Claude cases byte-identical.

- `exec_command` → `project(red, "cmd")` (same shape as bash → command).
- `apply_patch` → first file directive from the patch envelope, both shapes: red may be a JSON string (custom_tool_call form: the patch text) or an object with key `input` (function_call form) — extract the string, then regex `` `\*\*\* (?:Add|Update|Delete) File: (.+)` `` first match; result rendered as `{"file":"<path>"}` for hash stability; no match → fall through to `canon(red)`.
- `write_stdin` → `project(red, "session_id")`.
- Everything else (incl. `spawn_agent`, `update_plan`) stays `canon` — no case added (YAGNI).

- [ ] **Step 1: failing tests** — table: exec_command with cmd+noise keys projects only cmd; apply_patch string-form → `{"file":"probe.txt"}` (use the committed fixture's envelope); apply_patch object-form with `input`; apply_patch garbage → canon; write_stdin projects session_id; **characterization block pinning Claude projections**: exact salient() outputs for bash/edit/multiedit/write/read with fixed inputs (copy current behavior into expected values — these lines are the compatibility pin).
- [ ] **Step 2:** FAIL → **Step 3:** implement → **Step 4:** PASS.
- [ ] **Step 5:** cross-check: run the full repo test suite — stepkey changes ripple into cmd/catacomb golden tests if any embed step keys for codex fixtures; update ONLY codex-derived expectations, never Claude ones (any Claude golden churn = STOP, report BLOCKED).
- [ ] **Step 6:** `make fmt && make cover && make lint`; commit `feat(stepkey): per-runtime salience for codex tools (exec_command, apply_patch, write_stdin)`.

### Task 4: docs + release checklist

**Files:**

- Modify: `docs/guide/cli.md` (import/codex cost note: cost_usd now estimated via OpenAI tiers; long-context >272K surcharge not modeled — flat estimate undercounts those requests), `docs/guide/ingestion.md` (same cost paragraph), `docs/RELEASING.md` (**new checklist line**: bump `TestedClaudeCodeVersion`/`TestedCodexVersion` ceilings after a green live run against a newer CLI — closes the ADR-0025 promise), `docs/guide/basket.md` only if it mentions codex cost semantics.
- markdownlint + relative-link check; commit `docs: codex cost estimation + tested-ceiling release checklist`.

### Task 5: local validation + PR

- [ ] Re-import yesterday's probe rollouts (or run one fresh cheap probe) with the stage-2 binary; assert regress/export now shows a plausible nonzero `cost_usd` for gpt-5.4-mini evidence (~$0.005 per the Task 2 vector); capture for the PR body.
- [ ] Final whole-branch review (subagent), fix wave if needed, then PR `feat: codex gate-quality metrics — token semantics, OpenAI pricing, step salience (ADR-0031 stage 2)` stacked on `feat/codex-import`.

## Deliberately out of scope

- Spec §3's "power-test characterization for Codex-shaped groups" — scenario 55 already gates regress power paths over codex evidence; a bespoke power harness adds no new signal at stage 2.
- Cache-write subtraction from `input_tokens` (unverified upstream semantics; revisit with a real gpt-5.6 rollout).
- Credit→USD conversion for ChatGPT-plan billing (no published rate; dollars stay API-semantics).
