# Milestone A — Web Views Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Scope:** the **web views** half of Milestone A (spec §5.7 + §5.8 + §5.9, i.e. spec sub-projects **(g)**, **(h)**, and **(i)**). This plan builds on the frozen foundation from `2026-06-24-m-a-web-foundation.md` — the Vite + Svelte 5 scaffold, the dark design system, the pure reducer + normalized stores, and the SSE client are all complete and their public API is pinned. This plan adds nothing to the Go backend (all backend work is in the separate backend plan); it touches only frontend sources and `webui/dist/` (committed artifact).

**Explicitly NOT in this plan:** content/payload viewing; the timeline waterfall; the rich session-header KPI strip (those are B); light theme (D); `Last-Event-ID` resume; the TUI. A minimal session header (model id, status pill, session hash) is acceptable in the session view; the full KPI strip is B.

**Goal bar.** After these three tasks: open the UI → searchable Sessions list → click a row or paste/search a hash → that session's realtime scoped graph fit-to-view → click any node → inline drawer with tokens in/out + cost + duration + model. A screenshot can sit next to Langfuse in a slide deck.

---

## Global Constraints

- **Frontend deps via npm only.** Never `go mod tidy` for JS deps; they never enter the Go module graph. The embedded `webui/dist/` is data.
- **Go serve path stays green and 100%.** No Go source changes in this plan; only `webui/dist/` changes (rebuilt artifact, already excluded from the Go coverage gate).
- **No Go comments** except `//go:build|//go:embed|//go:generate`. This plan adds no new `.go` files.
- **Pure-logic JS = 100% Vitest coverage** for every new pure module: `router.ts`, `layout.ts`, `sessions-sort.ts`. Components are NOT line-coverage-gated.
- **Playwright e2e** covers the hero flow (list → open session → select node → drawer shows metrics). Tests run against a test daemon serving seeded data (same pattern as the existing e2e smoke; see Task g3 / i3).
- **`make cover` (Go) stays 100%.** No Go changes means no regression risk here; the CI gate enforces it independently.
- **`dist/` drift check stays green.** Every task that changes frontend sources ends with a rebuild (`npm run build`) and a commit that includes the updated `webui/dist/`. The dist-drift CI check (`check:dist` script + CI `frontend` job) must pass.
- **Cross-build clean.** `GOOS=windows go build ./...` is already clean; this plan does not add Go code, so it stays clean.
- **Commit per task; never master.** Continue on `feat/m-a-web-views` (or the branch used for this plan). Squash-merge via PR. No `--no-verify`.
- **Dark theme only.** Light theme is Milestone D.
- **Rune wiring is thin.** All conditional logic, sorting, filtering, layout math lives in plain `.ts` files (gated 100%). Svelte components import from those modules and have no own branches (beyond Svelte `{#if}` / `{#each}` template constructs — those are not line-gated).

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `web/src/lib/router.ts` | Create | Pure hash-router: parse/serialize `/#/s/{hash}` and `/#/s/{hash}/n/{nodeId}` — **gated** |
| `web/src/lib/router.test.ts` | Create | 100% Vitest unit tests |
| `web/src/lib/sessions-sort.ts` | Create | Pure sort/filter helpers for the sessions list — **gated** |
| `web/src/lib/sessions-sort.test.ts` | Create | 100% Vitest unit tests |
| `web/src/lib/layout.ts` | Create | Pure dagre layout adapter: `{ nodes: Node[], edges: Edge[] }` → positioned XyFlow nodes/edges — **gated** |
| `web/src/lib/layout.test.ts` | Create | 100% Vitest unit tests |
| `web/src/components/SessionsList.svelte` | Create | Searchable, sortable sessions list table |
| `web/src/components/SessionRow.svelte` | Create | Single row: hash (mono), status pill, duration, tokens, cost+provenance badge, counts, model |
| `web/src/components/GraphCanvas.svelte` | Create | SvelteFlow wrapper: session scoped graph, custom nodes, edges, MiniMap, Controls, fitView |
| `web/src/components/GraphNode.svelte` | Create | Custom node component: type-colored, status indicated, lamplight selection ring |
| `web/src/components/NodeDrawer.svelte` | Create | Right-hand inline drawer: metric header + Advanced disclosure |
| `web/src/components/StatusPill.svelte` | Create | Shared status badge (ok / running / error / blocked / pending) |
| `web/src/components/MetricRow.svelte` | Create | Shared label + value row; renders `—` when value is undefined |
| `web/src/App.svelte` | Modify | Wire router → switch list ↔ session view; pass `selectedNodeId` to GraphCanvas + NodeDrawer |
| `webui/vitest.config.ts` | Modify | Add `web/src/lib/router.ts`, `sessions-sort.ts`, `layout.ts` to coverage `include` |
| `webui/e2e/hero.spec.ts` | Create | Playwright hero-flow e2e: list → session → node → drawer |
| `webui/dist/**` | Rebuild (committed) | Updated artifact embedding the new views |

**Paths** are relative to `webui/` (the Vite root). All source lives under `web/src/`; all lib modules under `web/src/lib/`; all components under `web/src/components/`.

**Packages to add** (add to `webui/package.json` devDependencies, run `npm install`, commit lockfile):

| Package | Version | Role |
|---|---|---|
| `@xyflow/svelte` | `^1.6.1` | Graph rendering, pan/zoom, fitView, MiniMap, Controls |
| `@dagrejs/dagre` | `^3.0.0` | Layered graph layout (rankdir LR) |
| `@types/dagre` | latest | TypeScript types for `@dagrejs/dagre` if not bundled |

Layout library decision: **`@dagrejs/dagre` v3.0.0** over `elkjs`. Rationale: (1) dagre's sync layout call (`layout(g)`) has zero async overhead and no WASM load — the layout adapter is a pure sync function, making it trivially 100%-testable in Vitest without async helpers; (2) dagre's `rankdir: "LR"` + `network-simplex` ranker is precisely what the spec names (§5.8, §4.1 item 4); (3) `elkjs` provides higher-quality crossing minimization but requires either a Web Worker or a sync bundle that is not widely tested with Svelte's SSR/module boundary — the added complexity is unjustified at ≤2,000 nodes; (4) dagre's TS types (bundled or via `@types/dagre`) are well-maintained. The layout adapter is behind a thin interface (`applyLayout(nodes, edges, opts) → LayoutResult`) so `elkjs` can be swapped in a future task without touching GraphCanvas.

---

## Task g — Sessions list + hash routing

**Goal.** The app shell switches between a Sessions list (landing) and a session view, driven by `window.location.hash`. The list is searchable/sortable, loads from `fetchSessions`, and routing is deep-link safe (reload/back/forward restore state).

### g1: Pure router module

**Files:** `web/src/lib/router.ts`, `web/src/lib/router.test.ts`

**Interfaces:**

```ts
export type Route =
  | { kind: 'list' }
  | { kind: 'session'; hash: string }
  | { kind: 'session-node'; hash: string; nodeId: string };

export function parseHash(hash: string): Route;
export function toHash(route: Route): string;
```

`parseHash` rules:

- `""` / `"#/"` / anything not matching → `{ kind: 'list' }`
- `"#/s/{hash}"` → `{ kind: 'session', hash }`
- `"#/s/{hash}/n/{nodeId}"` → `{ kind: 'session-node', hash, nodeId }`
- `hash` and `nodeId` are percent-decoded (`decodeURIComponent`).

`toHash` rules:

- `list` → `"#/"`
- `session` → `"#/s/" + encodeURIComponent(hash)`
- `session-node` → `"#/s/" + encodeURIComponent(hash) + "/n/" + encodeURIComponent(nodeId)`

**Test coverage** (all branches, 100%):

- Empty string / bare `#` / `#/` → list.
- Exact `#/s/abc123` → session hash `"abc123"`.
- `#/s/abc123/n/node%3A456` → session-node with decoded nodeId `"node:456"`.
- Unknown path `#/settings` → list (forward-compatible fallback).
- `toHash` round-trips each Route kind.
- Hash/nodeId with special chars (`/`, `:`, spaces) are correctly encoded/decoded.

**Steps:**

- [ ] Write failing tests `router.test.ts`.
- [ ] Implement `router.ts`.
- [ ] Run `npm run test -- web/src/lib/router` — expect green + 100% coverage.
- [ ] Add `'web/src/lib/router.ts'` to `vitest.config.ts` coverage `include`.

### g2: Pure sort/filter helpers

**Files:** `web/src/lib/sessions-sort.ts`, `web/src/lib/sessions-sort.test.ts`

**Interfaces:**

```ts
import type { SessionSummary } from './types';

export type SortKey = 'started_at' | 'duration_ms' | 'tokens_in' | 'tokens_out' | 'cost_usd' | 'error_count';
export type SortDir = 'asc' | 'desc';

export function filterSessions(sessions: SessionSummary[], query: string): SessionSummary[];
export function sortSessions(sessions: SessionSummary[], key: SortKey, dir: SortDir): SessionSummary[];
```

`filterSessions`: case-insensitive substring match on `session` (the hash) and `model_id`. Empty query → identity (same reference returned for memo-ability). Returns a new array; does not mutate.

`sortSessions`: sort by `key` ascending or descending. Undefined values sort last regardless of direction. Returns a new array; stable sort (preserves relative order of equal rows). `started_at` sorts lexicographically (ISO 8601 strings compare correctly that way).

**Test coverage** (100%):

- `filterSessions` with empty / partial hash match / model_id match / no match / case-insensitive.
- `sortSessions` ascending and descending for each key; undefined values last in both directions; stable sort tie-break.

**Steps:**

- [ ] Write failing tests.
- [ ] Implement module.
- [ ] Run tests — green + 100%.
- [ ] Add `'web/src/lib/sessions-sort.ts'` to `vitest.config.ts` coverage `include`.

### g3: SessionsList component + App router wiring

**Files:** `web/src/components/SessionsList.svelte`, `web/src/components/SessionRow.svelte`, `web/src/components/StatusPill.svelte`, `web/src/App.svelte` (modify)

**Interfaces consumed:**

- `fetchSessions(token)` from `lib/api.ts` — called on mount, result stored in local `$state`.
- `sessionsById` from `stores.svelte.ts` — used to get live-updated per-session status where available.
- `filterSessions`, `sortSessions` from `sessions-sort.ts`.
- `toHash`, `parseHash` from `router.ts`.
- `formatDuration`, `formatTokens`, `formatCost`, `shortHash` from `format/format.ts`.
- `costProvenance` from `pricing/provenance.ts`.

**SessionsList behavior:**

- On mount: call `fetchSessions(token)` and store the result. Loading state while in-flight; error state if rejected; empty state (reuse `.empty-state` primitives from `style.css`) when the array is empty.
- Search bar (controlled `$state` string): filters via `filterSessions`.
- Column headers are clickable to sort; clicking the same header toggles direction. Default sort: `started_at` descending (newest first).
- Rows: clicking navigates to `#/s/{hash}` (sets `window.location.hash`).
- Columns: session hash (`.mono`, `shortHash(s.session, 12)`), status pill (`StatusPill`), duration (`formatDuration(s.duration_ms)`), tokens (`formatTokens(s.tokens_in)` in / `formatTokens(s.tokens_out)` out), cost (`formatCost(s.cost_usd)` + provenance badge), tool count, error count, model id.

**StatusPill:** a `<span class="status-pill" data-status={status}>` mapping `'ok' | 'running' | 'error' | 'blocked' | 'pending'` to the design-token colors (`--ok`, `--running`, `--error`, `--blocked`, `--pending`). Single Svelte prop; no logic — not gated.

**App.svelte router wiring:**

- Add a `$state` `route: Route` initialized from `parseHash(window.location.hash)`.
- Add a `$effect` listening to `window`'s `hashchange` event; on event call `parseHash` and update `route`; tear down listener on cleanup.
- Template: `{#if route.kind === 'list'}` → `<SessionsList>` / `{:else if route.kind === 'session' || route.kind === 'session-node'}` → session view (GraphCanvas + NodeDrawer, wired in task h/i).
- When `route.kind === 'session-node'`, call `selectNode(route.nodeId)` immediately after mount so the drawer opens on deep-link.
- Keep existing `$effect` for SSE connect unchanged; update it so `session` param is `route.kind !== 'list' ? route.hash : ''`.

**Empty / loading / error copy (in interface voice):**

- Loading: "Loading sessions…"
- Empty: "No sessions yet — start a Claude session with the hooks installed." (hint: "Run `catacomb up` to start the daemon and install hooks.")
- Error: "Could not load sessions. Check that the daemon is running and your token is valid."

**Steps:**

- [ ] Create `StatusPill.svelte`.
- [ ] Create `SessionRow.svelte`.
- [ ] Create `SessionsList.svelte` with loading/empty/error states.
- [ ] Modify `App.svelte` for hash routing and view switching.
- [ ] Run `npm run check` — typecheck clean.
- [ ] Run `npm run build` — build succeeds; update `dist/`.

---

## Task h — Graph canvas (Svelte Flow + dagre layout)

**Goal.** The session view renders the scoped graph via `@xyflow/svelte` with a dagre-computed LR layout, custom type-colored nodes, styled edges by type, pan/zoom/fitView/minimap, incremental node updates without full re-layout, and the lamplight selection highlight.

### h1: Pure layout adapter

**Files:** `web/src/lib/layout.ts`, `web/src/lib/layout.test.ts`

**Interfaces:**

```ts
import type { Node as CNode, Edge as CEdge } from './types';

export interface LayoutOptions {
  rankdir?: 'LR' | 'TB';
  nodesep?: number;
  ranksep?: number;
  nodeWidth?: number;
  nodeHeight?: number;
}

export interface XyNode {
  id: string;
  position: { x: number; y: number };
  data: { catNode: CNode };
  type: string;
}

export interface XyEdge {
  id: string;
  source: string;
  target: string;
  type: string;
}

export interface LayoutResult {
  nodes: XyNode[];
  edges: XyEdge[];
}

export function applyLayout(
  nodes: CNode[],
  edges: CEdge[],
  opts?: LayoutOptions,
): LayoutResult;
```

**Adapter logic:**

1. Build a dagre `Graph` with `{ multigraph: false }`. Set graph options: `rankdir: opts.rankdir ?? 'LR'`, `nodesep: opts.nodesep ?? 60`, `ranksep: opts.ranksep ?? 100`, `align: 'UL'`, `ranker: 'network-simplex'`.
2. For each node: `g.setNode(node.id, { width: opts.nodeWidth ?? 200, height: opts.nodeHeight ?? 60 })`.
3. For each edge: `g.setEdge(edge.src, edge.dst, {}, edge.id)` — use edge id as the multigraph key to keep multi-edges stable.
4. Call `layout(g)`.
5. Map back: for each node id, read `g.node(id).x` / `.y`; center-correct (dagre places center; XyFlow expects top-left: `x = cx - width/2`, `y = cy - height/2`). Set `data: { catNode: node }`. Set `type` to the node's `type` field (maps to a custom node component).
6. Map edges: set `type` based on `edge.type` — `'parent_child'` → `'default'`, `'sequence'` → `'step'`, `'data_dep'` → `'smoothstep'` (distinguishable secondary style).
7. Output is **deterministic**: same `nodes`/`edges` arrays → same `LayoutResult`. Node order within each rank is determined by dagre's rank algorithm (network-simplex); no random tiebreaks. Do not sort the output by id after layout — dagre's order IS the layout order.

**Test coverage** (100%):

- Empty input → empty output.
- Single node → positioned at (0,0) area (centering math correct).
- Linear chain a→b→c with LR rankdir: a.x < b.x < c.x (nodes increase left-to-right).
- Disconnected node (no edges) is still included in output.
- Multi-edge types: `parent_child` maps to `'default'`; `sequence` maps to `'step'`; `data_dep` maps to `'smoothstep'`; unknown type maps to `'default'` (safe fallback).
- `applyLayout` is deterministic: same inputs called twice → deep-equal output (no random state).
- Center-to-top-left correction: `node.position.x === g.node(id).x - nodeWidth/2`.

**Note:** dagre mutates its graph in place. The adapter creates a fresh `Graph` on each call — this ensures determinism even if the caller caches the function. Tests can therefore call `applyLayout` twice on the same input and `expect(result1).toEqual(result2)`.

**Steps:**

- [ ] Write failing tests `layout.test.ts`.
- [ ] Implement `layout.ts` using `@dagrejs/dagre`.
- [ ] Add `@xyflow/svelte` and `@dagrejs/dagre` to `package.json`; run `npm install`.
- [ ] Run tests — green + 100%.
- [ ] Add `'web/src/lib/layout.ts'` to `vitest.config.ts` coverage `include`.

### h2: GraphCanvas component

**Files:** `web/src/components/GraphCanvas.svelte`, `web/src/components/GraphNode.svelte`

**Interfaces consumed:**

- `sessionGraph(hash)` from `stores.svelte.ts` — returns `{ nodes: Node[], edges: Edge[] }`.
- `selectedNodeId` from `stores.svelte.ts`.
- `selectNode(id)` from `stores.svelte.ts`.
- `applyLayout` from `layout.ts`.
- Design tokens from `style.css`: `--node-{type}`, `--glow`, `--ring`, `--shadow-lamp`, `--surface`, `--border`.
- `@xyflow/svelte` CSS: `import '@xyflow/svelte/dist/style.css'` (in `GraphCanvas.svelte`).

**GraphCanvas props:**

```ts
let { hash }: { hash: string } = $props();
```

**Internal state and reactivity:**

The component tracks **topology** (the set of node ids + edge ids) separately from **node data** (status/token fields). This split is what enables incremental updates without full re-layout.

```ts
let xyNodes = $state.raw<XyNode[]>([]);
let xyEdges = $state.raw<XyEdge[]>([]);
let prevTopologyKey = '';
```

A `$derived` reads `sessionGraph(hash)` — this is reactive to `nodesById`/`edgesById` changes via the Svelte 5 `$state` proxy on `_graphState`. On every reactive update:

1. Compute a topology key: `JSON.stringify([...nodeIds].sort() + [...edgeIds].sort())`.
2. If the topology key changed: call `applyLayout(nodes, edges)`, set `xyNodes` and `xyEdges` via `$state.raw` (full replace). Then schedule `fitView()` (see Timing below). Update `prevTopologyKey`.
3. If only data changed (status/tokens): update `xyNodes` by mapping the existing array, replacing only the `data.catNode` field for the nodes that changed — use `$state.raw` to assign a new array (required by XyFlow's Svelte 5 integration). No `fitView` call.

This is implemented inside a `$effect` that calls `sessionGraph(hash)` on every reactive read, computes the topology key, and branches.

**fitView timing.** `fitView` must fire after the DOM has rendered the new nodes. Use `useSvelteFlow()` to get the `fitView` function inside the `SvelteFlow` context. Call `fitView({ duration: 300 })` inside a `tick()` await (Svelte's `import { tick } from 'svelte'`) after setting `xyNodes`/`xyEdges` — this ensures the XyFlow internal node measurement has completed before fitting. Also call `fitView` when `hash` changes (session switch) via a `$effect` watching `hash`.

**Render abstraction boundary.** Export a `GraphEngine` interface and a `SvelteFlowEngine` implementation. The `GraphCanvas` imports `SvelteFlowEngine` but its template only calls methods on `GraphEngine`. This is intentionally thin — the interface covers only the surface actually used:

```ts
interface GraphEngine {
  fitView(opts?: { duration?: number }): void;
}
```

`GraphCanvas` receives a `engine?: GraphEngine` prop defaulting to the internal SvelteFlow binding. A future WebGL engine implements `GraphEngine` and passes it in. Do not build more abstraction than this.

**GraphNode component** (`type` = one of the NodeType strings):

- Props: `let { data, selected }: NodeProps<{ catNode: Node }> = $props();` (Svelte 5 XyFlow pattern — props from `$props()`).
- Background color: `var(--node-{data.catNode.type})` — use a CSS custom property lookup via inline style.
- Status indicator: a small dot in the top-right corner using `--ok` / `--error` / `--running` / `--blocked` / `--pending` based on `data.catNode.status`.
- Name: `data.catNode.name ?? data.catNode.type` rendered in UI font; id in `.mono` below in dimmer text (shortened via `shortHash`).
- Lamplight selection: when `selected` is true, add a CSS class `node--selected` that applies `box-shadow: var(--shadow-lamp)` and `outline: 1px solid var(--ring)`. This is the signature effect. Unselected nodes dim slightly (`opacity: 0.85`) when any other node is selected — drive this via a `$derived` reading `selectedNodeId.value !== null && !selected`.
- Click: `onclick={() => selectNode(data.catNode.id)}` — wired to the shared store; the drawer responds via the same `selectedNodeId`.
- `<NodeResizer>` is NOT used — nodes have a fixed layout size.
- Include `<Handle type="target" position={Position.Left} />` and `<Handle type="source" position={Position.Right} />` for LR layout. Handles should be visually minimal (no filled circle by default — override XyFlow's default handle style with transparent background).

**SvelteFlow template (inside GraphCanvas):**

```svelte
<SvelteFlow
  bind:nodes={xyNodes}
  bind:edges={xyEdges}
  {nodeTypes}
  fitView
  minZoom={0.1}
  maxZoom={2}
  onNodeClick={(_, node) => selectNode(node.id)}
>
  <Background variant="dots" gap={20} size={1} color="var(--border)" />
  <Controls />
  <MiniMap
    nodeColor={(node) => `var(--node-${node.data?.catNode?.type ?? 'marker'})`}
    maskColor="var(--bg)"
    style="background: var(--surface);"
  />
</SvelteFlow>
```

**CSS.** `GraphCanvas` needs `height: 100%` on its root element (the SvelteFlow container must have an explicit height). Parent layout (App.svelte content area) must pass height down. The XyFlow stylesheet is imported in `GraphCanvas.svelte`; XyFlow's default edge/node styles are overridden where they conflict with the design tokens (edge stroke → `--border`; selected edge → `--accent`).

**Steps:**

- [ ] Install `@xyflow/svelte` and `@dagrejs/dagre` via `npm install` (if not already done in h1).
- [ ] Create `GraphNode.svelte` with lamplight selection, status dot, handles.
- [ ] Create `GraphCanvas.svelte` with topology-keyed layout, incremental update path, fitView wiring.
- [ ] Wire `GraphCanvas` into `App.svelte` session view branch: `<GraphCanvas hash={route.hash} />`.
- [ ] Run `npm run check` — typecheck clean.
- [ ] Run `npm run build` — build succeeds.

---

## Task i — Node-detail drawer

**Goal.** Clicking any node opens an in-place right-hand drawer leading with a metric header. ESC and a close button deselect. The same `selectedNodeId` store drives both the node highlight in the graph and the drawer.

### i1: NodeDrawer component

**Files:** `web/src/components/NodeDrawer.svelte`, `web/src/components/MetricRow.svelte`

**Interfaces consumed:**

- `selectedNodeId` from `stores.svelte.ts`.
- `selectNode(null)` from `stores.svelte.ts` — to deselect.
- `nodesById` from `stores.svelte.ts` — to resolve the selected node.
- `formatDuration`, `formatTokens`, `formatCost`, `shortHash` from `format/format.ts`.
- `costProvenance` from `pricing/provenance.ts`.

**MetricRow component:**

```svelte
let { label, value }: { label: string; value: string } = $props();
```

A `<div class="metric-row">` with `<span class="metric-label">{label}</span>` and `<span class="metric-value">{value}</span>`. Value uses `.mono` class when it is a hash or id (the caller decides by wrapping the value string — MetricRow itself is purely presentational). Renders `—` when `value` is the `"—"` string (the format helpers produce this). Not gated.

**NodeDrawer behavior:**

- Derives the selected node: `$derived(selectedNodeId.value ? nodesById[selectedNodeId.value] ?? null : null)`.
- When `node` is null → drawer is closed (zero width or `display: none`). Transition: slide-in from right on open, slide-out on close via CSS `transition: transform 0.2s ease`.
- **Metric header** (always rendered with labels; `"—"` when value is unknown):
  - Status: `<StatusPill status={node.status ?? 'pending'} />`
  - Duration: `<MetricRow label="Duration" value={formatDuration(node.duration_ms)} />`
  - Tokens in: `<MetricRow label="Tokens in" value={formatTokens(node.tokens_in)} />`
  - Tokens out: `<MetricRow label="Tokens out" value={formatTokens(node.tokens_out)} />`
  - Cost: `<MetricRow label="Cost" value={formatCost(node.cost_usd)} />` + a provenance badge `<span class="provenance-badge" data-provenance={costProvenance(node)}>{costProvenance(node)}</span>` inline.
  - Model: `<MetricRow label="Model" value={node.attrs?.['model_id'] as string ?? node.attrs?.['model'] as string ?? '—'} />` — read from `attrs` since `Node` does not have a top-level `model_id` field.
- **Node name / type** displayed at the top of the drawer as a title: `node.name ?? node.type`, with `<StatusPill>` inline.
- **Advanced disclosure** (`<details><summary>Advanced</summary>`):
  - Node id: `<MetricRow label="ID" value={node.id} />` with a copy button (calls `navigator.clipboard.writeText(node.id)`).
  - Payload hash: rendered only when `node.payload_hash` is set; `<MetricRow label="Payload hash" value={shortHash(node.payload_hash, 16)} />` + copy button.
  - Sources: rendered only when `node.sources` is non-empty; a compact list of `source + shortHash(obs_id)` pairs.
  - All ids/hashes in the Advanced section use `.mono` font.
- **ESC to deselect:** a `$effect` that adds/removes a `keydown` listener on `document` when the drawer is open, calling `selectNode(null)` on `Escape`.
- **Close button:** `<button onclick={() => selectNode(null)} aria-label="Close">` — visually an `×` in the drawer header.
- **Content/payload viewing is absent** — no Input/Output panels (those are B).

**Drawer layout:**
The drawer is an absolutely-positioned right panel inside the session view container. The layout is: graph takes remaining width, drawer slides over it from the right (overlay, not pushing). Width: 360px on desktop; the graph remains pannable behind the drawer. This avoids a re-layout of the graph canvas when the drawer opens/closes.

```
┌─────────────────────────────────────┬──────────────┐
│         GraphCanvas (full width)    │  NodeDrawer  │
│         (z-index below drawer)      │  (overlay)   │
└─────────────────────────────────────┴──────────────┘
```

Implemented as: session view container is `position: relative; overflow: hidden`. Drawer is `position: absolute; right: 0; top: 0; bottom: 0; width: 360px; z-index: 10` with `transform: translateX(100%)` when closed, `translateX(0)` when open.

**Steps:**

- [ ] Create `MetricRow.svelte`.
- [ ] Create `NodeDrawer.svelte` with metric header, Advanced disclosure, ESC handler, close button, slide-in CSS.
- [ ] Wire into `App.svelte` session view: `<NodeDrawer />` (reads `selectedNodeId` directly from the store — no props needed).
- [ ] Verify ESC deselects and the graph node highlight clears (both read the same `selectedNodeId`).

### i2: Extend Playwright e2e — hero flow

**Files:** `webui/e2e/hero.spec.ts`

**Scope:** the full hero acceptance flow, run against a live daemon serving seeded data.

**Test daemon setup:** follow the existing `e2e/shell.spec.ts` pattern. The `playwright.config.ts` already defines a `webServer` for `vite preview` (the static SPA). For the hero e2e, a second server entry (or a global setup hook) starts a real catacomb daemon pre-seeded with a synthetic session. The seeding mechanism: write a small `testdata/seed-session.jsonl` (a minimal Claude stream-json transcript with one assistant turn and one tool call) and replay it via the daemon's ingestion path before the test, or use the daemon's existing `/v1/…` endpoints. Alternatively: mock the REST + SSE endpoints via Playwright's `page.route()` to intercept `GET /v1/sessions` and `GET /v1/sessions/{hash}/graph` returning static JSON — this avoids a live daemon and makes the test fully hermetic.

**Recommended approach: `page.route()` mocks.** This avoids daemon lifecycle in CI:

- Mock `GET /v1/sessions` → static `SessionSummary[]` array with one entry (hash `"abc123def456"`, status `"ok"`, duration_ms `4200`, tokens_in `512`, tokens_out `128`, cost_usd `0.0031`, cost_source `"reported"`, tool_count `2`, error_count `0`, model_id `"claude-opus-4-5"`).
- Mock `GET /v1/sessions/abc123def456/graph` → static `SseEvent[]` with a session node, an assistant_turn node, a tool_call node, edges between them.
- Mock `GET /v1/subscribe?session=abc123def456&token=test` → SSE stream returning `data: {"kind":"node_upsert", ...}` events then `data: {"kind":"ping"}`. Use Playwright's `page.route` to return a streaming response.

**Test steps:**

```ts
test('hero flow: list → session → node → drawer shows metrics', async ({ page }) => {
  // 1. Mock REST + SSE
  // 2. Navigate to /?token=test
  // 3. Assert SessionsList renders the session row (hash visible, status pill present)
  // 4. Click the session row → hash changes to #/s/abc123def456
  // 5. Assert GraphCanvas appears (SvelteFlow container visible)
  // 6. Assert at least one graph node is visible
  // 7. Click a graph node (the tool_call node)
  // 8. Assert NodeDrawer is visible
  // 9. Assert duration metric row shows a formatted value (not "—" when duration_ms is set)
  // 10. Assert tokens-in row is visible
  // 11. Assert cost row is visible with "reported" badge
  // 12. Press Escape → drawer closes, graph node selection clears
});
```

Also add a deep-link test:

```ts
test('deep-link: /#/s/{hash} opens session view directly', async ({ page }) => {
  // navigate directly to /#/s/abc123def456
  // assert GraphCanvas appears without visiting the list first
});
```

**Steps:**

- [ ] Write `e2e/hero.spec.ts` with `page.route()` mocks.
- [ ] Run `npm run test:e2e` locally — green.
- [ ] Commit.

### i3: Final wiring, rebuild, and verification

**Steps:**

- [ ] Run full test suite: `npm run test` (Vitest, all logic modules 100%).
- [ ] Run `npm run check` (svelte-check typecheck).
- [ ] Run `npm run build` — build clean.
- [ ] Verify dist drift: `git status --porcelain ../dist` → dirty (expected — dist was updated). Stage and commit.
- [ ] Run `npm run test:e2e` — hero + shell specs green.
- [ ] Run Go suite: `cd /Users/karych/src/catacomb && go test -race -count=1 ./...` — all green, 100% coverage unchanged.
- [ ] Run codepolicy: `go test ./internal/codepolicy/...` — green (no Go changes in this plan).
- [ ] Run cross-builds: `GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...` — clean.

---

## Self-Review

### Spec Coverage Table

| Spec Section | Requirement | Covered by |
|---|---|---|
| §5.7 | Hash-router `/#/s/{hash}` + `/#/s/{hash}/n/{nodeId}` | g1 `router.ts` (100% gated) |
| §5.7 | Sessions list: hash (mono), status, duration, tokens, cost+provenance, counts, model | g3 `SessionsList.svelte` + `SessionRow.svelte` |
| §5.7 | Search/paste hash to jump; sort by column | g2 `sessions-sort.ts` (100% gated) + g3 component |
| §5.7 | Click row routes to session view | g3 App wiring |
| §5.7 | Reload/back/forward restores route | g3 `hashchange` listener in App |
| §5.7 | Empty/loading/error states (interface voice) | g3 `SessionsList.svelte` |
| §5.8 | `@xyflow/svelte` + dagre LR layout | h1 `layout.ts` (100% gated) + h2 `GraphCanvas.svelte` |
| §5.8 | `parent_child` = primary layering; `sequence`/`data_dep` = secondary styled edges; no orphaned nodes | h1 edge-type mapping; h2 graph uses all edges from `sessionGraph` |
| §5.8 | fitView on load + session switch | h2 `$effect` + `tick()` |
| §5.8 | Incremental updates: status/token change → node update in place, no relayout | h2 topology-key split |
| §5.8 | Unmistakable lamplight selection (shared `selectedNodeId`) | h2 `GraphNode.svelte` `--shadow-lamp`/`--ring` + store |
| §5.8 | Pan/zoom/minimap/controls | h2 `SvelteFlow` + `MiniMap` + `Controls` |
| §5.8 | Thin render abstraction (swappable engine) | h2 `GraphEngine` interface + `engine?` prop |
| §5.8 | Keyboard focusability | h2 — XyFlow provides default keyboard nav; noted as included |
| §5.9 | Inline right-hand drawer (NOT a tab) | i1 `NodeDrawer.svelte` |
| §5.9 | Metric header: status, duration, tokens in/out, cost+provenance badge, model | i1 metric header section |
| §5.9 | Metrics always render with labels; `—` when unknown | i1 `MetricRow.svelte`; format helpers |
| §5.9 | Raw ids / payload_hash / sources collapsed under "Advanced" with copy buttons | i1 `<details>` disclosure |
| §5.9 | NO content/payload viewing (B) | i1 — absent by design |
| §5.9 | ESC + close button deselect | i1 `keydown` handler + close button |
| §5.9 | Same node visibly selected in graph (shared `selectedNodeId`) | i1 reads same store as h2 |
| §3.1 | "not shameful next to Langfuse/Sentry" bar | OKLCH tokens + lamplight selection + Inter + IBM Plex Mono + design primitives |
| §4.1 item 3 / §10 Q4 | 100% Vitest on pure logic; components not gated | g1 router, g2 sessions-sort, h1 layout — all gated; components excluded |
| §4.1 item 3 | Playwright e2e for hero flow | i2 `hero.spec.ts` |
| §4.2 | `dist/` committed; drift check stays green | i3 rebuild step + CI gate |
| §4.2 | `GOOS=windows go build ./...` clean | i3 verification (no Go changes) |
| §5.10 dep graph | (f) reducer+stores → (h) graph; (g) depends on (a)+(b) API (consumed via `fetchSessions`) | g3 consumes `fetchSessions`; h2 consumes `sessionGraph`; i1 consumes `nodesById` |

### Gaps and Risks

**1. Svelte 5 runes + `@xyflow/svelte` v1 integration: `$state.raw` for nodes/edges.**
XyFlow v1 (released with Svelte 5 support) requires nodes and edges to be declared as `$state.raw(...)` — not `$state(...)` — for correct performance. Deep reactivity on `$state` would cause XyFlow to re-render on every mutation. The incremental-update path in `GraphCanvas` assigns a new array (`$state.raw` semantics) even for data-only updates; this is intentional and correct. Risk: if a future XyFlow patch changes this requirement, the component must be updated. Pin `@xyflow/svelte` at `^1.6.1` (current latest as of plan date).

**2. `fitView` timing with dynamically computed layouts.**
XyFlow's `fitView` must fire after nodes have been measured (their DOM dimensions are known). The `tick()` await (post-render microtask) is generally sufficient; however, on first render with many nodes, XyFlow's internal layout measurement may not be complete. Mitigation: also subscribe to XyFlow's `oninit` event to call `fitView` once on initialization, in addition to the `tick()` path. If `fitView` still misfires (nodes measured async after init), the fallback is `setTimeout(fitView, 0)` — acceptable as a one-time workaround, not a pattern. Flag this in the implementation: if the `tick()` approach proves unreliable during testing, switch to `oninit`.

**3. Incremental update: `$derived` reactivity on `nodesById`.**
`sessionGraph(hash)` reads `_graphState.nodes` through the Svelte 5 `$state` proxy, so it is reactive. However, the f2 implementation exports `nodesById` as a direct reference to the proxy object, not a `$derived`. A `$derived` or `$effect` reading `sessionGraph(hash)` in `GraphCanvas` will track mutations through the proxy. If reactivity does not fire on in-place node mutations (status/token update), the fallback is to read `Object.values(nodesById)` inside the effect and trigger a snapshot. This is noted in the f2 report (concern #1). Test this during h2 implementation; if proxy tracking is insufficient, add a `$derived` in `stores.svelte.ts` that returns a snapshot array of nodes for the graph view.

**4. XyFlow CSS + design token integration.**
`@xyflow/svelte/dist/style.css` defines its own node/edge/handle styles. Some of these conflict with the design tokens. Mitigation: import the XyFlow stylesheet first, then override. Override targets: `--xy-node-background-color` → `var(--surface)`, `--xy-edge-stroke` → `var(--border)`, handle background → `transparent`. The override block lives in `GraphCanvas.svelte`'s `<style>` tag with `:global` selectors scoped to the XyFlow container. This is well-established pattern in XyFlow usage.

**5. e2e: no live daemon.**
The `page.route()` mock approach avoids daemon lifecycle but does not test the full SSE streaming path. The existing shell e2e (from f2) tests the SSE client with a live daemon (it connects to the real daemon when `?token=` is set). The hero e2e tests the UI flow using mocked REST responses. A full integration test (daemon + UI) is desirable but is CI-infrastructure work deferred to a later task. Flag: if the CI environment ever runs a test daemon (seeded via `testdata/`), the `page.route()` mocks can be replaced with real requests.

**6. Dagre with disconnected subgraphs.**
If `sessionGraph(hash)` returns a graph with disconnected components (e.g., a `user_prompt` node with no edges — the bug noted in spec §2.3), dagre assigns all nodes positions regardless. Disconnected nodes receive valid `x`/`y` from dagre; they appear in the rendered graph (no node is dropped). The layout adapter feeds all nodes to dagre unconditionally, which resolves the old BFS orphan bug. However, dagre may place disconnected components at overlap; the `nodesep`/`ranksep` settings mitigate this. Test with a disconnected node in the layout unit tests.

**7. `shortHash` in `SessionRow` column width.**
The sessions list uses `shortHash(s.session, 12)`. The full hash might be needed for search (the user pastes a full hash). Mitigation: search queries in `filterSessions` match on the full `s.session` string, not the truncated display. The search input shows the hash as the user typed it; only the column display is truncated.

**8. Model id in NodeDrawer.**
The `Node` type has no top-level `model_id` field; model is stored in `attrs['model_id']` or `attrs['model']` (stream-json sets it under `model`). The drawer reads `node.attrs?.['model_id'] ?? node.attrs?.['model']`. If neither is set, it displays `"—"`. This is correct per spec §5.9 (always render the label, `—` when unknown). If the backend later promotes `model_id` to a top-level field, update the drawer and types accordingly.
