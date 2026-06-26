# Outline UX Refinement — Design Doc

Date: 2026-06-26
Status: Proposed (owner review)
Scope: catacomb web UI Outline view + drawer + reducer parentage. READ-ONLY critique synthesized into one implementable plan. The owner likes the Outline direction; this hardens it and rips out the graph.

Design spirit: minimalist / functional. Silence when healthy. No decorative chrome. Every pixel earns its place. Content first, meta second.

---

## Guiding principles (resolve cross-lens conflicts up front)

1. **Content is primary.** The conversation/tool text is the reason the drawer exists. Meta (tokens, cost) is secondary and glanceable.
2. **Type-appropriate density.** A tool row and an assistant row are different things; do not force one stat format on both.
3. **Silence when healthy.** Lifecycle statuses (`pending`, `unknown`, `running` on a finished session) are noise on historical data. Only outcome statuses (`error`, `ok`, `blocked`) and live-session state are signal.
4. **One legend, no per-row chrome.** Explain the numbers once, statically, rather than decorating every row.
5. **No new runtime dependencies.** Pure-logic helpers get 100% vitest; no comments in Go.

---

## Complaint 1 — "Reveal content" button is dumb; show content inline with a cut

**Decision: remove the reveal button. Fetch eagerly on selection. Show content first, truncated with "show more". Meta moves below.**

Current state (verified): `PayloadPanel.svelte:75-82` gates the entire content section behind a `Reveal content` button; the fetch only fires in `reveal()` (line 40). `NodeDrawer.svelte:112-126` renders the metrics block (Duration / Tokens in / Tokens out / Cost / Model) *above* `PayloadPanel` (line 128). Content has a fixed `max-height: 240px` scroll box (`PayloadPanel.svelte:344`) with no indication of how much is cut. `PayloadPanel` is mounted unconditionally even when the node has no `payload_hash`, so structural nodes (session/marker/subagent) get a button that dead-ends at "No stored payload".

Changes:

- **`webui/web/src/components/NodeDrawer.svelte`**
  - Reorder: move `PayloadPanel` (line 128) **above** the `metrics-section` (lines 112-126).
  - Collapse the metrics block into the existing `<details class="advanced-section">` summary line, or render it as a single compact summary row by default. Default chosen below (Open Q1): keep Duration + Cost always visible as a one-line summary; the full grid lives in Advanced. Tokens/Model are not lost — they move into the summary string for assistant turns.
  - Guard the panel: `{#if node.payload_hash} <PayloadPanel … payloadHash={node.payload_hash} /> {:else} <p class="no-payload">No content stored for this node.</p> {/if}`. Eliminates the click→404 dead-end.
  - Pass `payloadHash` prop through.
- **`webui/web/src/components/PayloadPanel.svelte`**
  - Add `payloadHash` prop. Change the reset `$effect` (line 32) to also call the fetch when `payloadHash` is present. Delete the `Reveal content` / `Hide` button pair (lines 75-93) and the `revealed` state.
  - Replace the `max-height: 240px` scroll box with script-level truncation + a "show more" button. Single `expanded` boolean per node (reset on node change), shared across input+output sections.
  - Replace the bare spinner with a fixed-height skeleton (3 shimmer lines) so the drawer does not jump on content arrival.
- **`webui/web/src/lib/payload-view.ts`** — add pure `truncate(text, limit): { visible, rest }` and `wordCount(text): number`. 100% vitest. Truncate at the last newline before the limit so we never cut mid-line.
- **`webui/web/src/lib/conversation.ts`** — `conversationText` currently `JSON.stringify`s any non-string (line 13-14). Claude messages are content-block arrays. Extend it to flatten `[{type:'text',text}, {type:'tool_use',name}, {type:'tool_result'}]` into readable prose before falling back to stringify. Pure function, add vitest. (Shared win: also improves the outline snippet path which calls the same function.)

Defaults: truncation limit **1200 chars for conversation nodes, 3000 for tool nodes** (tool output is denser/longer). Single `expanded` toggle for the whole node.

---

## Complaint 2 — Cryptic stats; "0→0", bare numbers; need legend / tooltips

**Decision: one static legend strip + per-type labeled stats + a `title` tooltip on the stat span. Kill the bare arrow.**

Current state (verified): `Outline.svelte:254` renders `${tokensIn}→${tokensOut} · ${cost} · ${duration}` for every leaf regardless of type; `badge.ts:5` renders `${count} · ${in}→${out} · ${cost}` for collapsed aggregates. The `→` arrow has no label anywhere; the leading count (`92`) has no unit; `formatTokens(undefined)` is `—` and `formatTokens(0)` is `0`, so tool leaves read `—→— · — · 307ms` and zero-token turns read `0→…`.

Changes — extract a pure `rowStatLine(node, collapsed, agg?)` returning `{ text, title, color }` into a new **`webui/web/src/lib/graph/outline-stats.ts`** (100% vitest), called from `Outline.svelte:248-256`:

- **Aggregate (collapsed parent):** `N nodes · in X · out Y · $Z`. Replace bare count with `N nodes`. Drop the arrow; use `in`/`out` words. (Update `badge.ts:badgeStatLine` accordingly, or move its logic into `outline-stats.ts`.)
- **assistant_turn (leaf):** `in 9.7k · out 58k · $1.51 · 1.2s`. Explicit `in`/`out` words remove all ambiguity without needing the legend.
- **user_prompt (leaf):** nothing (no tokens, no cost). Just the type glyph.
- **tool_call / mcp_call (leaf):** duration only, e.g. `307ms` (see Complaint 5). No token/cost columns. If `duration_ms` is undefined: empty string (no dashes).
- **`title` attribute** on `.outline-stat-text` (`Outline.svelte:319`) carries the expanded human-readable form, e.g. `"Tokens in: 9,740 · Tokens out: 58,300 · Cost: $1.51 · Duration: 1.2s"`. Native browser tooltip; zero new components.

**Static legend strip:** insert one dim, right-aligned `<div class="outline-legend">` between `.outline-toolbar` (line 271) and `.outline-scroll` (line 272). Three short lines mapping each row family to its columns:

```
assistant  in · out · cost · duration
tool       duration · outcome dot
collapsed  N nodes · in · out · cost
```

Static markup, zero runtime cost, collapses gracefully on narrow viewports.

---

## Complaint 3 — Rip out the graph view (graph-removal map)

**Decision: delete the graph entirely. Verified against the Outline's import chain — the Outline shares only `badge.ts`, `aggregate.ts`, `collapse.ts`, `hierarchy.ts`, and `outline*.ts`, none of which the graph uniquely owns.**

### DELETE (zero non-graph consumers — verified)

- `webui/web/src/components/GraphCanvas.svelte` — sole consumer of `@xyflow/svelte` and `lib/layout.ts`.
- `webui/web/src/components/FlowInternals.svelte` — used only by GraphCanvas.
- `webui/web/src/components/GraphNode.svelte` — used only by GraphCanvas. (Imports `badge.ts`, which is KEPT.)
- `webui/web/src/lib/layout.ts` + `layout.test.ts` — dagre wrapper, graph-exclusive. The Outline uses `buildOutlineHierarchy` + `collapse.ts` directly, never `collapseView`.
- `webui/web/src/lib/graph/lift-edges.ts` + `lift-edges.test.ts` — imported only by `layout.ts`.
- e2e: `graph.spec.ts`, `graph-collapse.spec.ts`, `a11y-keyboard.spec.ts` (all graph-DOM-only; the a11y keyboard logic is gated `if (viewMode !== 'graph') return`).

### EDIT — `webui/web/src/components/SessionView.svelte`

- Remove `import GraphCanvas` (line 3) and `import { nextNodeByDirection }` (lines 12-13).
- Narrow `viewMode` type `'outline' | 'graph' | 'timeline'` → `'outline' | 'timeline'` (line 25).
- Delete graph-only scaffolding: `fitKey` (23), `prevHadNode` (24), `canvasWrapEl` (27), `visibleIds` (28), the `fitKey`-increment `$effect` (44-50), `arrowDirMap` (56-61), `onWindowArrowKeydown` (63-98), its `addEventListener` `$effect` (100-105), `onNodeActivate` (107-109).
- Remove the Graph view-switcher button (128-133) and the `{:else}` GraphCanvas branch (160-162).
- `drawerFocusOnOpen`: was only set true by `onNodeActivate`. Either drop it (always false) or, better (see Open Q3 default), wire it from the Outline row-click so keyboard users land in the drawer.

### DROP npm deps — `webui/package.json`

- `@dagrejs/dagre` (line 17) and `@xyflow/svelte` (line 24). After deletions no source imports either. Removes the XyFlow stylesheet import too. Run `npm install` to update the lockfile.

### REWRITE partially-infected e2e (do not delete wholesale — they test view-agnostic behavior)

- `payload.spec.ts`, `kpi-filter.spec.ts`, `hero.spec.ts` (graph-specific tests), `reactivity.spec.ts`: replace `getByRole('button',{name:'Graph'})` + `.svelte-flow__node` selectors with outline-row clicks (`.outline-row` filtered by text). `reactivity.spec.ts` may be deleted if its scenarios were purely GraphCanvas effect-loop regressions; otherwise rewrite the live-update assertion against the outline.

### KEEP (Outline dependency chain — verified imports)

`lib/graph/hierarchy.ts`, `collapse.ts`, `aggregate.ts`, `badge.ts` (shared, used at `Outline.svelte:9,251,255`), `outline.ts`, `outline-tree.ts`, `types.ts`, `node-legend.ts`, `conversation.ts`, `format/format.ts`, `filters.ts`, `NodeDrawer.svelte`, `PayloadPanel.svelte`.

### CLEANUP for the 100% coverage rule

- `dimmedEdgeIds` in `filters.ts` (+ its test block) becomes dead after GraphCanvas removal — **delete the function and its tests** (policy disallows ignore annotations).
- `lib/graph-nav.ts` + `graph-nav.test.ts`: dead after removal. Default — **delete both** (arrow-key tree nav is out of scope for v1; see Open Q3). If the owner wants outline arrow-key nav later, the pure `nextNodeByDirection` is view-agnostic and can be re-wired.
- `NodeLegend.svelte`: orphaned (only GraphCanvas used it). The new outline legend is plain static markup, so **delete `NodeLegend.svelte`** rather than repurpose it.

---

## Complaint 4 — Statuses don't reflect reality; keep outcome statuses, hide lifecycle except realtime

**Decision: derive `isLiveSession`; show only `error`/`ok`/`blocked` outcomes on historical data; show running/pending only live. Remove the `?? 'pending'` lie.**

Current state (verified): `NodeDrawer.svelte:107` renders `<StatusPill status={node.status ?? 'pending'} />` — every historical node with no status shows "pending". `Outline.svelte:255` and `aggregate.ts:25` collapse everything that isn't `error`/`running` to green `ok`, so `unknown`, `cancelled`, `superseded`, `abandoned`, `blocked` are all greenwashed. The reducer cascades `StatusUnknown` onto still-open children on `session_end` (`reduce.go:39,443-444`), so finished sessions are full of `unknown` nodes.

Changes:

- New **`webui/web/src/lib/status.ts`** (pure, 100% vitest): `isOutcomeStatus(s): boolean` → true only for `error`, `ok`, `blocked`; `displayLabel(s): string` for human-readable pill text.
- **`SessionView.svelte`** — derive `isLiveSession = sessionsById[hash]?.status === 'running'`; thread as a prop into `Outline` and `NodeDrawer`. (`SessionSummary.status` already exists in `types.ts`.)
- **`Outline.svelte` `rowStats`/`rowStatLine`** — dot color rules:
  - `error` → red, `blocked` → blocked color, always shown.
  - live + `running` → running color (pulse, already a token); live + `pending` → dim static dot.
  - else (`ok` or, on historical sessions, any lifecycle status) → **no dot / transparent** rather than green. Healthy + finished = silent.
- **`NodeDrawer.svelte:107`** — drop `?? 'pending'`; wrap pill in `{#if isLiveSession || isOutcomeStatus(node.status)}`. `StatusPill` uses `displayLabel`.
- **`aggregate.ts`** — collapsed bubble: `error` (red) wins; otherwise silent on historical sessions, `running` only when live. Add an `anyNonOutcome` flag if needed, but the row-level gate already suppresses the green-by-default.
- No backend change required for this; the `unknown` cascade can stay (it is simply not rendered on historical sessions). See Open Q2 for whether to also stop emitting `unknown`.

---

## Complaint 5 — Tool rows lack inline meta (tool, args, output, time)

**Decision: lazily load a tool snippet (`keyArg → outputSnippet`) for visible tool rows; show duration + outcome dot; drop token/cost columns for tools.**

Current state (verified): `Outline.svelte:124` skips all non-conversation nodes from snippet loading, so tool rows show only the tool name. `outlineLabel` returns empty `secondary` for tools (`outline.ts:73-76`). Tools carry no tokens/cost in the model, so `rowStats` produces `—→— · — · 307ms`. The structured input/output is only behind the gated `/payload` endpoint.

Changes:

- **`webui/web/src/lib/conversation.ts`** (pure, 100% vitest):
  - `isToolNode(t): boolean` → `tool_call || mcp_call`.
  - `toolKeyArg(input): string` — heuristic over `PREFERRED_KEYS = ['command','file_path','path','url','query','input']`, fallback to first non-empty string value.
  - `toolOutputSnippet(output): string` — `OUTPUT_KEYS = ['stdout','content','result','output','text']`, fallback empty.
- **`Outline.svelte`**
  - Loosen the snippet guard (line 124) to also accept tool nodes; require `payload_hash` (line 125 already does). Add a `loadToolSnippet(id)` that builds `keyArg + ' → ' + outputSnippet`, truncated to `SNIPPET_MAX`. Seed `attempted` for tool nodes too (prevents re-fetch on every repaint).
  - Render the combined snippet in the existing `.outline-snippet` span (so the snippet `isConversationNode` guard at line 288 must also allow tool nodes).
  - `rowStatLine` tool branch: `text = formatDuration(duration_ms)` (empty if undefined), color = outcome dot. No tokens/cost.
- Do **not** render raw `prettyJSON` in rows. Keep fetch lazy (visible window only). Stay silent (tool name + duration only) when payload access is off — acceptable graceful degradation.

Result row examples:
```
Bash   ls -la /some/path → exit 0…            307ms  •
Read   /src/components/Foo.svelte → import …  124ms  •
```

---

## Complaint 6 — More UX problems found (fresh-eyes)

Bundled minimal fixes, all low-risk:

- **Redacted snippet leak (HIGH).** `loadSnippet` (`Outline.svelte:108-111`) shows the raw `‹redacted:high-entropy›` placeholder as the snippet — confusing and it reveals a secret was pasted. Guard: if the first line starts with `‹redacted:`, store `[redacted]` (dim/italic). Same guard in `conversationText` consumers.
- **Synthetic prompt rows clutter the TOC (HIGH).** `command-name`, `command-stdout`, `caveat`, `task-notification` arrive as `user_prompt` and pollute the outline. Two-layer fix: (a) reducer stores `prompt_kind` in `n.Attrs` (default `human`) — `Attrs` is already `map[string]any`, no schema change; (b) `flattenOutline`/`outlineLabel` skip or down-weight non-`human` kinds. Default — **hide non-human prompt rows; no toggle in v1** (owner said they clutter). See Open Q4 for whether a "show system messages" toggle is wanted.
- **Truncated row text has no tooltip (LOW).** Add `title={primary + secondary + snippet}` to `.outline-label` (`Outline.svelte:313`). One line, native tooltip.
- **No live indicator (MEDIUM).** With `isLiveSession` (Complaint 4) derived, add a small `live` badge in `SessionHeader` with a pulsing dot (respect `prefers-reduced-motion`). Uses existing `--running` token.
- **Inline snippet loading state (LOW/MEDIUM).** Optional shimmer placeholder for in-flight snippets. Defer unless it bothers the owner — silence is also fine.
- **Filter chips expose raw statuses (LOW).** `FilterBar` derives chips from raw status strings; apply `displayLabel` and drop lifecycle statuses on historical sessions for consistency with Complaint 4.

Deferred (not v1): orphan-node visual marker, session-list relative time, `filterNodes` searching snippet content, redundant type-dot vs status-dot consolidation, keyboard Enter-expands-collapsed-parent semantics. These are real but low-value; listed for the backlog.

---

## Backend reparent recommendation (the key decision)

**Recommendation: reparent `assistant_turn` under its preceding `user_prompt` in `reduce.go`. Retire the frontend `outline-tree.ts` synthesis (collapse it to plain `buildHierarchy`).**

Verified facts:
- `reduce.go` `user_prompt` case creates a `session → prompt` edge (`upsertEdge(SessionNodeID, n.ID)`).
- `reduce.go` `assistant_turn` case creates **no parent edge at all** — turns are genuinely parentless at the source.
- `reduce.go` `applyTool` already parents tool calls under their turn via `MessageID` correlation (`setStructParent(structKindTurn, AssistantTurnID(...), id)`), else under the session.
- `outline-tree.ts` `buildOutlineHierarchy` re-derives `turn → preceding prompt` purely in the frontend (`precedingPrompt`, chronological), with cycle guards, then re-sorts children chronologically.

So the *only* missing edge is `prompt → turn`. The frontend reconstructs it heuristically (by `t_start` ordering) on every render.

Why reparent in the reducer:
- **Single source of truth.** The hierarchy is correct for *all* consumers — web Outline, the bubbletea TUI, any future exporter — not just this one Svelte file.
- **Deletes ~120 lines** of `outline-tree.ts` heuristic + its cycle/ordering tests; the Outline can call `buildHierarchy` directly. Less surface, easier 100% coverage.
- **The reducer already has the ordering signal** (sequence / event time) more reliably than the frontend's `t_start ?? id` fallback. The `precedingPrompt` heuristic can mis-attribute a turn when timestamps are missing; the reducer sees the event stream in order.

The reducer change: in the `assistant_turn` case, attach the turn to the most-recent `user_prompt` for that execution (track "current prompt id" per execution in graph state), falling back to the session node when no prompt precedes it. This mirrors the existing `structKindTurn` tool-parenting machinery, so the pattern is already in the codebase.

Tradeoff / cost:
- It is a **backend behavioral change** touching `reduce.go` and its golden/reducer tests (`reduce_test.go`), under the 100% coverage + no-comments rules.
- Edge IDs and the emitted delta stream change shape; any snapshot tests and the TUI's assumptions need updating in the same PR.
- Slightly higher blast radius than a pure-frontend tweak.

Net: the synthesis is a workaround for a backend modeling gap. Fixing it at the source is the correct, DRY move and directly helps the TUI. Sequence it as its **own PR** before/independent of the cosmetic outline work so the diff is reviewable. If the owner wants to keep blast radius minimal for this UX round, the synthesis can stay as-is short-term — it is functionally correct — but mark it as tech debt to retire.

---

## Prioritized change list (one task per item)

**P0 — graph removal (unblocks everything, shrinks bundle)**
1. Delete graph components + `layout.ts`/`lift-edges.ts` + their tests + 3 graph e2e specs (Complaint 3 DELETE list).
2. Edit `SessionView.svelte`: strip graph scaffolding, narrow `viewMode` type, remove Graph button + branch.
3. Drop `@xyflow/svelte` + `@dagrejs/dagre` from `package.json`; `npm install`.
4. Delete dead `dimmedEdgeIds` (+ tests), `graph-nav.ts` (+ tests), `NodeLegend.svelte`.
5. Rewrite graph-coupled e2e (`payload`, `kpi-filter`, `hero`, `reactivity`) to drive outline rows.

**P0 — drawer content-first (Complaint 1)**
6. `conversation.ts`: content-block-array extraction in `conversationText` (+ vitest).
7. `payload-view.ts`: `truncate` + `wordCount` (+ vitest).
8. `PayloadPanel.svelte`: eager fetch, remove reveal button, inline truncation + show-more, skeleton loader, `payloadHash` prop.
9. `NodeDrawer.svelte`: reorder content-above-meta, guard on `payload_hash`, collapse metrics to summary, pass `payloadHash`.

**P1 — stats legend + per-type stats (Complaint 2)**
10. `outline-stats.ts`: `rowStatLine` per-type with `in/out` words + `title` (+ vitest); update/absorb `badgeStatLine`.
11. `Outline.svelte`: use `rowStatLine`, add `title` to stat span, add static legend strip.

**P1 — status semantics (Complaint 4)**
12. `status.ts`: `isOutcomeStatus`, `displayLabel` (+ vitest).
13. Thread `isLiveSession` through `SessionView` → `Outline` + `NodeDrawer`; apply dot/pill gating; drop `?? 'pending'`; `StatusPill` uses `displayLabel`.

**P1 — tool inline meta (Complaint 5)**
14. `conversation.ts`: `isToolNode`, `toolKeyArg`, `toolOutputSnippet` (+ vitest).
15. `Outline.svelte`: lazy tool-snippet load + render; tool stat branch (duration + dot only).

**P2 — fresh-eyes fixes (Complaint 6)**
16. Redacted-snippet guard in `Outline.svelte` snippet loader.
17. Reducer `prompt_kind` tagging + frontend hide of non-human prompts (`reduce.go`, `outline.ts`).
18. Row `title` tooltip; live badge in `SessionHeader`; `FilterBar` chip labels via `displayLabel`.

**P2 — backend reparent (own PR, see recommendation)**
19. `reduce.go`: parent `assistant_turn` under preceding `user_prompt`; update `reduce_test.go`.
20. Retire `outline-tree.ts` synthesis → `buildHierarchy` directly in `Outline.svelte`; delete now-dead tests.

---

## Open questions for the owner (defaults chosen for the rest)

1. **Drawer metrics placement.** Default: content first; Duration + Cost remain as a one-line summary above the fold; Tokens/Model move into the assistant stat string + Advanced. Acceptable, or do you want the full metrics grid always visible?
2. **`unknown` status at the source.** Default: keep the reducer's `unknown` cascade but never render it on historical sessions. Alternatively change `reduce.go` to emit `ok` on graceful `session_end` and reserve `unknown` for abnormal closure. Which?
3. **Outline keyboard arrow-nav.** Default: delete `graph-nav.ts` (drop arrow-key tree traversal in v1); Enter/click still open the drawer. Want it re-wired to the outline instead?
4. **Synthetic prompt rows.** Default: hide non-human prompts (caveat / command-stdout / task-notification) entirely. Want a "show system messages" toggle, or keep some kinds (e.g. caveat) visible with distinct styling?
5. **Backend reparent timing.** Default: do it as its own PR (cleaner hierarchy for TUI + web, retires `outline-tree.ts`). Confirm you want the backend change this round, or defer and keep the frontend synthesis short-term.
