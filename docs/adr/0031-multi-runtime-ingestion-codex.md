# ADR-0031: Multi-runtime transcript ingestion, Codex CLI first

- **Status:** Accepted
- **Date:** 2026-07-16
- **Deciders:** @realkarych
- **Related:** [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md) (amended: "one CC format coupling" becomes per-runtime transcript adapters behind one seam); [ADR-0025](0025-capture-format-drift-detection.md) (amended: the tested-version watchlist becomes per-runtime); [ADR-0030](0030-interactive-session-import.md) (the import entry point this lands behind); [ADR-0016](0016-cross-run-step-identity-and-annotations.md) (step-key salience gains per-runtime projections); basket and evidence compatibility surfaces ([VERSIONING.md](../VERSIONING.md) #2, #4)

## Context

Post-pivot, catacomb's only ingestion source is Claude Code's transcript JSONL — a
format the repo itself calls internal and undocumented. That coupling is the
project's largest standing strategic risk: the vendor owes the format nothing, and
a gate that speaks one vendor's dialect cannot answer the first objection a
platform team raises ("what happens when we also run other agent CLIs?").

The architecture already paid for the answer. Parsing is isolated in
`ingest/jsonl` (imports only `ingest/drift` and `model`; sole production consumer
is `cmd/catacomb/offline.go`), and everything downstream — reduce, step/phase
keys, aggregate, regress, evidence, baselines — operates on runtime-neutral
`model.Observation`/`model.Node`. The one Claude-specific model field is
`ReproMeta.ClaudeCodeVersion`.

OpenAI's Codex CLI is the highest-volume non-Claude agent CLI (bundled with every
ChatGPT tier; 5M+ reported users mid-2026). It persists sessions as "rollout"
JSONL under `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-<ts>-<thread-uuid>.jsonl`,
with cold files compressed to `.jsonl.zst` since June 2026. The format is
unversioned and fast-moving (near-daily releases), exactly like Claude Code's —
but structurally richer in places: subagents are separate rollout files with an
explicit `parent_thread_id`, every turn carries a `turn_context` with the model
id, and MCP tool calls are recorded distinctly from builtin/shell tools. Non-interactive
runs (`codex exec --json`) announce the session as a first
`{"type":"thread.started","thread_id":...}` stdout event and report token usage
but no dollar cost.

## Decision

Catacomb becomes a per-runtime agent-CLI regression gate: one gate vocabulary
(baskets, evidence, verify, baselines, regress) over pluggable transcript
adapters. Codex CLI is the second runtime. Staged delivery:

1. **Stage 1 — import-only** (this ADR's implementation plan): a new
   `ingest/codex` adapter parses rollout JSONL (plain and `.jsonl.zst`) into the
   existing observation stream; `catacomb import` learns to resolve and ingest
   Codex sessions (main thread plus subagent threads). Evidence, verify, and
   regress consume the result unchanged.
2. **Stage 2 — gate-quality metrics**: per-runtime step-key salience projections
   (`exec_command`, `apply_patch`), OpenAI pricing families for the derived
   `cost_usd` metric, and prompt-kind parity, so Codex baskets gate with the same
   power Claude baskets have.
3. **Stage 3 — bench spawn**: `bench` learns to peek `codex exec --json` streams
   (`thread.started` → thread id; no reported cost, like import), plus a live E2E
   leg. Until stage 3 lands, Codex support is documented as import-only.

The load-bearing choices:

- **Runtime is declared in the basket**: a top-level `runtime: claude-code |
  codex` field (default `claude-code`), additive under the existing
  known-fields validation. The basket stays the single source of truth for what
  a cell is; no per-command runtime flags. Mixed-runtime baskets are rejected —
  comparing runtimes is model-confounded and step identity is per-runtime by
  construction, so cross-runtime step-level A/B is a non-goal, permanently.
- **`model.Source` is unchanged**: adapter output uses `SourceJSONL`. Source
  describes the provenance channel (session transcript vs in-run hook), not the
  vendor; reusing it preserves the reducer's structural-authority ranking with
  zero reduce changes and keeps the §16 commutativity invariant untouched. The
  runtime rides observation attrs (`agent_runtime`) and evidence env stamps.
- **MCP calls map to the `mcp__<server>__<tool>` name convention** that the
  reducer already recognizes, so `mcp_call` nodes, the catacomb `mark`
  checkpoint tool, and phase keys work for Codex sessions without touching the
  marker matcher.
- **Subagent linkage uses Codex's explicit parent ids**: child rollout files are
  discovered by scanning the sessions tree for `session_meta` records whose
  `parent_thread_id` equals the imported thread (transitively, for depth > 1),
  and their observations join the same run keyed by the main session id — the
  same merge shape bench uses for Claude sub-transcripts.
- **Tolerant reader, per-runtime watchlist**: unknown rollout record types and
  payload types bump the existing drift counts; `ingest/drift` gains a
  `TestedCodexVersion` ceiling read from `session_meta.cli_version`, warned
  exactly like the Claude watchlist. ADR-0025's single-ceiling design is amended
  accordingly.
- **One new dependency**: `github.com/klauspost/compress/zstd` (pure Go, no cgo)
  to read `.jsonl.zst` rollouts. Cold-session compression is on by default
  upstream, so refusing compressed files would break import for exactly the
  sessions users go back to gate.
- **Cost semantics follow ADR-0030**: rollouts carry token usage but no dollar
  cost, so `meta.CostUSD` stays unset for Codex evidence while the token-derived
  `cost_usd` metric prices through the same pricing engine (stage 2 adds OpenAI
  tiers). Tokens and duration are first-class from stage 1.

## Alternatives considered

- **A new `model.Source` constant per runtime** (`SourceCodexJSONL`). Rejected:
  every reducer authority table (`payloadRank`, `structureRank`) would need a new
  case per runtime forever, for zero semantic gain — the observations are still
  "session transcript JSONL". Vendor identity is data (attrs/stamps), not a
  provenance channel.
- **Normalizing Codex tool names onto Claude's vocabulary** (`exec_command` →
  `Bash`). Rejected: it fabricates cross-runtime step-identity that the gate
  must not promise (terminals embed tool names by design), and it hides the real
  runtime from evidence readers. Adapters preserve native names; salience
  handles per-runtime projection.
- **Shelling out to `zstd`/skipping compressed rollouts.** Rejected: a runtime
  dependency on a system binary breaks the single-static-binary constitution,
  and skipping `.zst` silently strands every cold session; a pure-Go decoder is
  the smallest honest fix.
- **A generic "bring your own adapter" plugin interface.** Rejected (for now):
  two in-tree adapters are the proof the seam needs; a plugin ABI is surface
  area with no second consumer, and the tolerant-reader + watchlist discipline
  is per-format work a plugin boundary cannot absorb.

## Consequences

- The pitch-level single-vendor risk gets its structural answer: baskets,
  evidence layout, verifiers, baselines, and the statistical gate are now
  demonstrably runtime-neutral, with Codex as the existence proof.
- The drift-watch maintenance surface deliberately doubles — two undocumented
  vendor formats, two watchlist ceilings, two hermetic fixture families. This
  reverses part of ADR-0026's "one format coupling" simplification, accepted
  knowingly: the coupling was the risk being paid down.
- `ingest/jsonl` is implicitly the Claude Code adapter from now on; a future
  rename (`ingest/claudecode`) is cosmetic and deferred.
- The basket schema gains `runtime` (compatibility surface #2, additive);
  evidence `meta.json` env stamps gain `agent_runtime`/`agent_version`
  (surface #4, additive). Neither breaks existing evidence or baskets.
- Until stage 3, `bench` rejects `runtime: codex` baskets with a clear
  operational error pointing at `import` — a documented, temporary asymmetry.
