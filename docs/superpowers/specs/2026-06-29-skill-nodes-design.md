# Design: First-class skill nodes (`NodeSkill`)

Date: 2026-06-29
Status: approved (design), pending implementation plan

## Problem

Skill invocations (`/foo`, executed via the Claude Code `Skill` tool, and built-in
`SlashCommand` invocations) currently land in catacomb as generic `tool_call`
nodes. They are indistinguishable from `Bash`, `Edit`, `Read`, etc.: the timeline,
outline, TUI, and exports all render them as "tool call", with the only hint being
the `name` field in the node detail.

MCP tools already get first-class treatment (`NodeMCPCall` / `mcp_call`), detected
by the `mcp__` name prefix in `reduce`. Skills deserve the same: a dedicated node
type, a meaningful name (the actual skill/command, not the literal "Skill"), and
parity everywhere `mcp_call` is handled (exports, step keys, stats, UI, TUI).

## Goal

Add a first-class `NodeSkill` node type. Recording already works (skills flow
through ingestion as `assistant_tool_use`); this change makes them recognizable
and distinctly displayed, without touching the ingestion layer.

## Detection signal (decided)

A node is a skill iff its tool name is `Skill` **or** `SlashCommand`. There is no
`SlashCommand` tool in the current Claude Code environment, but it is included
defensively for built-in slash commands that may appear in traces.

`Skill` and `SlashCommand` are exact-match names; MCP tools use the `mcp__` prefix.
The two predicates are mutually exclusive, so there is no ordering conflict.

## Display name (decided)

The node `Name` is the real skill/command, parsed from the tool-call input payload:

- `Skill` tool: `input.skill` (e.g. `superpowers:brainstorming`).
- `SlashCommand` tool: `input.command`, cleaned — strip a leading `/` and take the
  first whitespace-delimited token (e.g. `/code-review high` -> `code-review`).

Fallback to the literal tool name (`"Skill"` / `"SlashCommand"`) when the input is
absent or unparseable (e.g. an OTEL span that carries only the tool name). The full,
unmodified payload is always preserved on the node, so args are never lost.

## Architecture

Ingestion is **unchanged**. Hooks / JSONL / OTEL already emit an
`assistant_tool_use` observation with `attrs["name"] in {"Skill","SlashCommand"}`
and the tool input in `payload.Input`. All new logic lives in `reduce` plus the
downstream parity sites.

### 1. Data model — `model/model.go`

Add `NodeSkill NodeType = "skill"` to the `NodeType` const block (after
`NodeMCPCall`).

### 2. Reduce — `reduce/reduce.go` + new `reduce/skill.go`

- New predicate `isSkill(name string) bool` -> `name == "Skill" || name == "SlashCommand"`.
- New file `reduce/skill.go` with `extractSkillName(o model.Observation) string`,
  mirroring `reduce/marker.go`'s `extractMarkerFromPayload`. It unmarshals
  `{ "skill": ..., "command": ... }` from `o.Payload.Input` and returns the cleaned
  display name (`skill` wins; else cleaned `command`; else `""`).
- In `applyTool`, replace the MCP `if` with a `switch` selecting the node type:
  `isMCP -> NodeMCPCall`, `isSkill -> NodeSkill`, else `NodeToolCall`.
- Generalize the existing "type upgrade" guard so it never downgrades:
  `if n.Type == model.NodeToolCall && nodeType != model.NodeToolCall { n.Type = nodeType }`.
  (Preserves the current MCP behavior: a node first created as `NodeToolCall` by an
  obs lacking the name is later upgraded when the named obs arrives; an obs lacking
  the name never downgrades an already-typed node.)
- Name resolution: when `isSkill(name)`, compute `extractSkillName(o)`; if non-empty
  use it as the display name passed to `setName`, else fall back to the literal
  `name`. Non-skill tools keep current behavior. Because the input-carrying
  `assistant_tool_use` obs supplies both the literal name and the payload at the same
  `Seq`, `setName`'s earliest-Seq-wins logic deterministically selects the extracted
  name.

### 3. Step keys — `stepkey/stepkey.go`

Add `model.NodeSkill` to `eligible()` alongside `NodeToolCall`, `NodeMCPCall`,
`NodeSubagent`. A skill invocation is a real step and must participate in cross-run
step matching (diff / eval). Without this, skills would be invisible to diffing.

### 4. Exports — parity with `mcp_call`

- `export/neo4j/export.go` `nodeLabel()`: `NodeSkill -> "Skill"`.
- `export/otlp/export.go` `openInferenceKind()`: `skill -> "TOOL"` (a skill is a
  tool-like operation in the OpenInference vocabulary).
- `export/evalview/export.go` `spanType()`: `skill -> "skill"`; ensure
  `nodeToSpan` role handling covers skill the same way it covers `mcp_call`.
- `export/agentevals/export.go`: treat `skill` as a tool message wherever `mcp_call`
  is treated as a tool (the payload handling at the tool sites).

### 5. Stats — `daemon/sessions.go`

Include `NodeSkill` in `ToolCount` wherever `mcp_call` is counted. No separate
`SkillCount` (YAGNI).

### 6. Web UI

- `webui/web/src/lib/node-legend.ts`: add
  `skill: { token: '--node-skill', label: 'skill' }` to `NODE_TYPE_MAP`.
- `webui/web/style.css`: define `--node-skill` in both the dark block (~line 34)
  and the light block (~line 101). Hue 25 (warm coral), matching the chroma/lightness
  of the surrounding tokens: dark `oklch(0.74 0.12 25)`, light `oklch(0.40 0.12 25)`.
  Hue 25 is unused by the existing palette (session 250, user_prompt 300,
  assistant_turn 150, tool_call 75, subagent 195, mcp_call 340, hook_event 110,
  marker low-chroma).
- `webui/web/src/lib/graph/outline.ts` `outlineLabel()`: add
  `case 'skill': return { primary: node.name || 'skill', secondary: '' };`.
- `webui/web/src/lib/conversation.ts` `isToolNode()`: include `'skill'` so skills
  render in the conversation view like other tool nodes.

### 7. TUI — `tui/vocab.go`

`NodeTypeLabel()`: add `case "skill": return "skill"`.

## Testing (TDD, 100% coverage)

Tests are written first. Each existing exhaustive table test that lists node types
gains a `skill` row; new behavior gets new tests.

- `reduce`: Skill tool -> `NodeSkill` + name from `input.skill`; SlashCommand ->
  `NodeSkill` + cleaned `input.command`; type-upgrade ordering (named obs after
  unnamed obs); fallback to literal name when input is absent/unparseable;
  `isSkill`/`isMCP` mutual exclusion.
- `stepkey/stepkey_test.go`: skill is eligible.
- `export/neo4j`, `export/otlp`, `export/evalview`, `export/agentevals`: skill row in
  each mapping/table test.
- `daemon/sessions.go` tests: skill counted in `ToolCount`.
- `tui/vocab_test.go`: skill label.
- Web: `node-legend.test.ts`, `conversation.test.ts`, `graph/outline.test.ts` gain
  skill cases.
- New end-to-end fixture: a JSONL (and/or hook) testdata sample containing a `Skill`
  tool call, asserting it reduces to a `NodeSkill` named after the skill.

## Out of scope (YAGNI)

- Separate `SkillCount` session stat.
- Parsing skill arguments beyond the headline name (full payload is preserved).
- Modeling nested sub-skills / skill call trees as a distinct hierarchy.
- Any change to the ingestion layer (hook / jsonl / otel / stream_json).
- Backfill/migration of historical `tool_call` nodes that were skills (reduce is
  recomputed from observations, so re-reduction picks up the new type naturally).

## Affected files (summary)

| File | Change |
|------|--------|
| `model/model.go` | add `NodeSkill` const |
| `reduce/reduce.go` | `isSkill`, switch node type, generalized upgrade, name resolution |
| `reduce/skill.go` (new) | `extractSkillName` |
| `stepkey/stepkey.go` | skill eligible |
| `export/neo4j/export.go` | skill label |
| `export/otlp/export.go` | skill kind |
| `export/evalview/export.go` | skill span type + role |
| `export/agentevals/export.go` | skill as tool |
| `daemon/sessions.go` | skill in ToolCount |
| `webui/web/src/lib/node-legend.ts` | skill legend entry |
| `webui/web/style.css` | `--node-skill` (dark + light) |
| `webui/web/src/lib/graph/outline.ts` | skill label |
| `webui/web/src/lib/conversation.ts` | skill is tool node |
| `tui/vocab.go` | skill label |
| `*_test.*` across the above | skill cases |
