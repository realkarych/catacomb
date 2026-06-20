# ADR-0009: Conversation threading, interruption, and transcript meta-records

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §3, §5.2, §5.5, §5.8, §7, §17; ADR-0002, ADR-0003, ADR-0004, ADR-0005, ADR-0012, ADR-0014
- **Amended by:** ADR-0012/ADR-0014 — `cancelled`/`superseded` are **provisional** (a genuine terminal supersedes them) and **cascade** transitively over the `parent_child` closure (across subagent files); the never-resolved-orphan case adds `unknown`/`abandoned`. The "does not override any of them" line in §Relationship below is superseded by that status lattice + cascade.

## Context

The earlier ADRs decide *which* sources we capture (0002), *how* overlapping observations of one action merge (0003), the *shape* of the canonical model (0004), and *run grouping* (0005). None of them decide how to interpret the **structure of a single conversation** — and that structure is richer than "session → prompt → turn → tool" implies. A hard question (multiple prompts; the user interrupting and steering mid-flight) exposed that the design under-specified threading, interruption, regeneration branches, and the many non-conversational record types in a transcript.

This ADR is grounded in **empirical inspection of real transcripts on the development machine** plus an adversarially-verified research pass over Anthropic docs, the Agent SDK types, and reverse-engineering of `~/.claude/projects`. The load-bearing facts:

- **On-disk threading is a genuine TREE**, keyed by `uuid` / `parentUuid` (camelCase). Regeneration / edit / retry produce **multiple children of one `parentUuid`** (observed: up to 6). It is not a linear chain.
- **The on-disk transcript ≠ the SDK message surface.** The SDK `getSessionMessages()` exposes only `{type, uuid, session_id, message, parent_tool_use_id}` (snake_case) and does **not** surface `parentUuid` / `isSidechain` / `leafUuid`. Threading is only reconstructable by reading the **files**.
- **Interruption is NOT a hook event.** The `Stop` hook explicitly does not fire on user interrupt (API errors fire `StopFailure`; a "User Interrupt Hook" is still an open feature request). On disk, an interruption appears as a **`user` record carrying `interruptedMessageId`** (the cut assistant message id) plus the `[Request interrupted by user]` text marker. Detection must come from the transcript, not hooks.
- **Subagents live in separate files** `…/<sessionId>/subagents/agent-<agentId>.jsonl`, each paired with `agent-<agentId>.meta.json` carrying `{agentType, description, toolUseId}`; that `toolUseId` matches the parent's `Agent` (formerly `Task`) tool_use id. Older versions stored sidechains **inline** in the main file with `isSidechain:true`. Live attribution also flows through hook fields `agent_id` / `agent_type` / `agent_transcript_path` and the SDK `parent_tool_use_id`.
- A transcript is full of **non-conversational meta-records** (`system`/`compact_boundary`, `isCompactSummary`, `file-history-snapshot`, `attachment`, `permission-mode`, `mode`, `last-prompt`, `ai-title`, `queue-operation`, `pr-link`, `isMeta`) that must not be mistaken for prompts/turns.
- **Version fragility is the dominant risk:** `Task`→`Agent` rename (v2.1.63), nested subagents (v2.1.172), inline→separate sidechain layout, and hook subagent-attribution fields are recent (SDK 0.2.69); older transcripts lack them.

## Decision

Adopt an explicit **conversation-threading interpretation** for transcript/SDK sources, feeding the structural edges that ADR-0003's precedence then reconciles. Concretely:

1. **Thread from the on-disk `parentUuid` tree.** The JSONL adapter reads the files and captures `parentUuid` (and `uuid`, `promptId`, `leafUuid`, `agentId`, `isSidechain`, `subtype`, `interruptedMessageId`). The SDK `parent_tool_use_id` is a **separate, weaker** signal used only for the subagent boundary — never as the conversation thread.
2. **Prompt attribution.** A `user_prompt` is a real user message (not a meta/synthetic record). An `assistant_turn` (and its `tool_call`s) is attributed to the nearest ancestor `user_prompt` by walking `parentUuid` upward; `promptId` (present on user-side records) is the corroborating tag. This yields `user_prompt → assistant_turn` `parent_child` edges.
3. **Interruption = a new prompt + cancellation, detected from the transcript.** The interrupting message is an ordinary new `user_prompt` sibling under the session. The assistant turn named by its `interruptedMessageId` — and any in-flight `tool_call` of that turn with no matching `tool_result` — transitions to status `cancelled`. Cancellation is **not** a re-parenting and **not** a hook signal.
4. **Regeneration / edit branches.** `parentUuid` may have several children; all are kept as branches in the graph. The **active** branch is the path ending at the current leaf (`leafUuid` / the latest `last-prompt`); nodes off that path are marked status `superseded`. The SDK session-level fork (`fork_session`, new `sessionId`) is a *different* mechanism — handled as a new session grouped by the wrapper run-id (ADR-0005), not an in-file branch.
5. **Subagent parentage, both layouts.** New layout: link `subagents/agent-<agentId>.jsonl` to the parent via the meta `toolUseId` → the parent `Agent`/`Task` `tool_call`; nodes inside carry `agentId`. Old layout: inline `isSidechain:true` records threaded by `parentUuid`. Live runs additionally use hook `agent_id`/`agent_type`/`agent_transcript_path` and SDK `parent_tool_use_id`. All resolve to the same `tool_call → subagent` `parent_child` edge.
6. **Meta-record taxonomy.** Non-conversational records are classified and excluded from the prompt/turn/tool structure (optionally retained as `hook_event`-like annotations or dropped per config), never promoted to `user_prompt`/`assistant_turn`. Compaction boundaries (`compact_boundary`, `isCompactSummary`) are treated as segment markers, not prompts.
7. **Version-duality is first-class.** Tool name (`Agent`|`Task`), sidechain layout (inline|separate), and the presence of recent attribution fields are resolved behind the versioned parser (spec §17); unknown record types are recorded generically rather than dropped or misclassified.

## Relationship to existing ADRs

This is an **interpretation layer over a single source**, upstream of ADR-0003's cross-source merge: it turns one transcript's `parentUuid`/`agentId`/`interruptedMessageId` into candidate structural edges and node statuses, which 0003 then reconciles against OTel/stream-json/hook observations under the existing structure precedence. It extends ADR-0004's node lifecycle with two statuses (`cancelled` via interruption, `superseded` via abandoned branch) and confirms ADR-0002's "JSONL is the subagent-tree source of truth." It does not override any of them.

## Alternatives considered

- **Treat the thread as linear** (parentUuid as a chain) — rejected: real transcripts branch (regeneration/edit/retry), so a linear assumption would mis-attribute or silently merge sibling branches.
- **Detect interruption from the `Stop` hook** — rejected: `Stop` does not fire on user interrupt (documented; open feature request confirms no interrupt hook). The transcript `interruptedMessageId` marker is the only reliable signal.
- **Reconstruct threading from the SDK message surface** (`parent_tool_use_id`) — rejected: that surface omits `parentUuid`/`isSidechain`/`leafUuid`, so it cannot express the conversation tree or branches; it only links subagent messages.
- **Assume one stable on-disk subagent→parent key** — rejected as an assumption: the documented shared key is requested but not yet shipped (issue #32175); we join via the meta `toolUseId` and tolerate its absence on older/edge versions.

## Consequences

- **+** The graph faithfully models multi-prompt conversations, interruptions, regeneration branches, and subagent delegation, with explicit `cancelled`/`superseded` semantics rather than silent gaps.
- **+** Reading the transcript files (not the SDK surface) makes threading robust and works for offline replay.
- **+** Version-duality and meta-record handling are designed in, so the parser degrades gracefully across Claude Code versions.
- **−** More fields to capture and a branch-aware parser (heavier than M0.1's single-pass reducer); threading/interruption/branch handling lands with the JSONL tailer in **M0.2+**, not M0.1.
- **−** Several load-bearing on-disk keys are **undocumented and version-fragile** (`interruptedMessageId`, meta `toolUseId`, `leafUuid` branch semantics); they sit behind the versioned parser and are the most likely future maintenance surface.
- **−** Distinguishing active vs abandoned branches depends on `leafUuid`/`last-prompt`, whose exact in-file semantics are community-observed; a wrong call mislabels a `superseded` node. Mitigated by conservative defaults (keep both branches; mark, don't delete).
