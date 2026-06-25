# W2 — Collapsible Graph Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:test-driven-development`. Every pure module below is written test-first: write the failing test, run it, watch it fail, then write the implementation, run it, watch it pass. Do not write implementation before its test exists and fails.

**Goal:** Make the catacomb web graph readable at session scale (800+ nodes) by collapsing the `parent_child` hierarchy into an expandable spine. On load the graph shows a compact spine (~20–40 nodes); turns and subagents expand on demand; live SSE deltas under a collapsed ancestor stay hidden and only bump that ancestor's aggregate badge. Pure view-layer modules do the hierarchy/visibility/lift/aggregate math (vitest 100%); `GraphCanvas.svelte` is thin wiring (Playwright e2e). The reducer is untouched — collapse is a pure view concern.

**Architecture:** New pure modules under `webui/web/src/lib/graph/` (`types.ts`, `hierarchy.ts`, `collapse.ts`, `aggregate.ts`, `lift-edges.ts`), each with a co-located `*.test.ts`. They sit between the existing reducer/selector layer (`sessionGraph(hash)` → full `{nodes, edges}`) and the existing dagre layout (`applyLayout`). `GraphCanvas.svelte` holds a `collapsed: Set<string>` view state, seeds it via `defaultCollapsed`, derives the visible node set + lifted edges, and feeds only those into `applyLayout`. The data flow becomes: reducer (full graph) → collapse view layer (new) → dagre → `GraphCanvas`. A small TUI parity change aligns `catacomb observe`'s default expand state with the web policy.

**Tech Stack:** Svelte 5 (runes) + Vite web UI, `@xyflow/svelte` 1.6 + `@dagrejs/dagre` 3.0, vitest 4 + Playwright 1.61, Go bubbletea TUI. No new runtime dependency.

## Global Constraints

- Frontend pure logic (`web/src/lib/graph/*`) is gated at vitest 100% per-file; Svelte components are NOT line-gated and are covered by Playwright e2e instead.
- `npm run typecheck`, `npm run test`, and `npm run build` must all pass. The committed `webui/dist/` must be rebuilt and in sync — CI fails on a stale `dist/`.
- No new runtime dependency. Use the existing `@xyflow/svelte` and `@dagrejs/dagre` only.
- No comments in Go code (TUI parity task) — only `//go:build`, `//go:embed`, `//go:generate` directives. Enforced by `internal/codepolicy`. Go stays at 100% coverage (`make cover`).
- Minimalist/functional design: the aggregate badge is glyph + label + a compact `count · tokens · cost` stat line + status color — no decoration, no extra chrome. Light theme auto-follows the OS via existing CSS variables; no theme toggle.
- Determinism: every map/set materialised for layout or display is produced in a stable, id-sorted order so layout and e2e are reproducible (match the existing `sessionGraphFrom` and `graph-nav` sort discipline).

---

### Task 1: Graph view-layer types (`lib/graph/types.ts`)

Define the two new shared types so every later module imports from one place. Reuse the existing `Node`/`Edge` from `lib/types.ts` — do not redeclare them.

**Files**

- Create: `webui/web/src/lib/graph/types.ts`
- Test: `webui/web/src/lib/graph/types.test.ts`

**Interfaces**

- Consumes: `import type { Node, Edge } from '../types'` where `Node = { id: string; run_id: string; type: string; parent_id?: string; status?: string; tokens_in?: number; tokens_out?: number; cost_usd?: number; rev: number; … }` and `Edge = { id: string; run_id: string; type: string; src: string; dst: string; rev: number }`.
- Produces:

  ```ts
  export interface Hierarchy {
    childrenOf(id: string): string[];
    parentOf(id: string): string | undefined;
    ancestorsOf(id: string): string[];
    descendantsOf(id: string): string[];
    roots: string[];
    orphans: string[];
  }

  export interface Aggregate {
    count: number;
    tokensIn: number;
    tokensOut: number;
    costUsd: number;
    status: 'ok' | 'running' | 'error';
    hasError: boolean;
  }
  ```

**Steps**

- [ ] Write the test asserting the module's type shape is usable at runtime (a type-only module has no runtime export, so assert via a typed literal that the compiler accepts and vitest runs). Create `webui/web/src/lib/graph/types.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import type { Hierarchy, Aggregate } from './types';

  describe('graph view-layer types', () => {
    it('Aggregate is constructible with all fields', () => {
      const agg: Aggregate = {
        count: 3,
        tokensIn: 10,
        tokensOut: 20,
        costUsd: 0.5,
        status: 'ok',
        hasError: false,
      };
      expect(agg.count).toBe(3);
      expect(agg.status).toBe('ok');
    });

    it('Hierarchy shape is implementable', () => {
      const h: Hierarchy = {
        childrenOf: () => [],
        parentOf: () => undefined,
        ancestorsOf: () => [],
        descendantsOf: () => [],
        roots: [],
        orphans: [],
      };
      expect(h.roots).toEqual([]);
      expect(h.childrenOf('x')).toEqual([]);
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/types.test.ts` — EXPECT FAIL (`Cannot find module './types'`).
- [ ] Create `webui/web/src/lib/graph/types.ts` with exactly the `Hierarchy` and `Aggregate` interfaces from the Interfaces block above, plus `import type { Node, Edge } from '../types';` re-exported as `export type { Node, Edge };` so downstream modules can import node/edge types from the graph barrel if they choose:

  ```ts
  import type { Node, Edge } from '../types';

  export type { Node, Edge };

  export interface Hierarchy {
    childrenOf(id: string): string[];
    parentOf(id: string): string | undefined;
    ancestorsOf(id: string): string[];
    descendantsOf(id: string): string[];
    roots: string[];
    orphans: string[];
  }

  export interface Aggregate {
    count: number;
    tokensIn: number;
    tokensOut: number;
    costUsd: number;
    status: 'ok' | 'running' | 'error';
    hasError: boolean;
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/types.test.ts` — EXPECT PASS.
- [ ] Add `web/src/lib/graph/**` to the `coverage.include` array in `webui/vitest.config.ts` (insert after the existing `'web/src/lib/graph-nav.ts'` line):

  ```ts
        'web/src/lib/graph-nav.ts',
        'web/src/lib/graph/**',
  ```

- [ ] Run `cd webui && npm run test` — EXPECT PASS with `web/src/lib/graph/types.ts` listed at 100% (a type-only file reports 100% trivially because it emits no executable lines).
- [ ] Commit:

  ```text
  feat(webui): add graph view-layer types (Hierarchy, Aggregate)

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 2: Hierarchy builder (`lib/graph/hierarchy.ts`)

Build parent↔children maps from `parent_child` edges, falling back to `node.parent_id` when no edge is present. Expose query methods plus `roots` and `orphans`.

**Files**

- Create: `webui/web/src/lib/graph/hierarchy.ts`
- Test: `webui/web/src/lib/graph/hierarchy.test.ts`

**Interfaces**

- Consumes: `Node[]`, `Edge[]` (from `lib/types.ts`); `Hierarchy` (from `./types`). Relevant fields: `Node.id`, `Node.parent_id?`; `Edge.type` (`'parent_child' | 'sequence' | 'data_dep'`), `Edge.src`, `Edge.dst`.
- Produces: `export function buildHierarchy(nodes: Node[], edges: Edge[]): Hierarchy`.

**Semantics**

- Parent of a node: the `src` of the single `parent_child` edge whose `dst` is the node; if no such edge, `node.parent_id` when that id refers to a node present in `nodes`.
- `childrenOf(id)` returns the ids of nodes whose parent is `id`, sorted ascending; unknown id → `[]`.
- `parentOf(id)` returns the parent id or `undefined`.
- `ancestorsOf(id)` walks parents to the root, nearest first; cycle-safe (stop if an id repeats).
- `descendantsOf(id)` is the full subtree under `id` (excluding `id`), in deterministic pre-order; cycle-safe.
- `roots`: nodes with no parent, sorted, that have at least one child (the spine roots).
- `orphans`: nodes with no parent AND no children, sorted (genuinely disconnected `hook_event`/`marker`).

**Steps**

- [ ] Write `webui/web/src/lib/graph/hierarchy.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import { buildHierarchy } from './hierarchy';
  import type { Node, Edge } from '../types';

  function n(id: string, parent_id?: string): Node {
    return { id, run_id: 'r1', type: 'marker', rev: 1, ...(parent_id ? { parent_id } : {}) };
  }
  function e(id: string, src: string, dst: string, type = 'parent_child'): Edge {
    return { id, run_id: 'r1', type, src, dst, rev: 1 };
  }

  describe('buildHierarchy', () => {
    it('empty input → empty hierarchy', () => {
      const h = buildHierarchy([], []);
      expect(h.roots).toEqual([]);
      expect(h.orphans).toEqual([]);
      expect(h.childrenOf('x')).toEqual([]);
      expect(h.parentOf('x')).toBeUndefined();
      expect(h.ancestorsOf('x')).toEqual([]);
      expect(h.descendantsOf('x')).toEqual([]);
    });

    it('builds parent/children from parent_child edges', () => {
      const nodes = [n('s'), n('a'), n('b')];
      const edges = [e('e1', 's', 'a'), e('e2', 's', 'b')];
      const h = buildHierarchy(nodes, edges);
      expect(h.childrenOf('s')).toEqual(['a', 'b']);
      expect(h.parentOf('a')).toBe('s');
      expect(h.parentOf('b')).toBe('s');
      expect(h.roots).toEqual(['s']);
    });

    it('sorts children ascending by id regardless of edge order', () => {
      const nodes = [n('s'), n('z'), n('a'), n('m')];
      const edges = [e('e1', 's', 'z'), e('e2', 's', 'a'), e('e3', 's', 'm')];
      expect(buildHierarchy(nodes, edges).childrenOf('s')).toEqual(['a', 'm', 'z']);
    });

    it('falls back to node.parent_id when no parent_child edge exists', () => {
      const nodes = [n('s'), n('a', 's')];
      const h = buildHierarchy(nodes, []);
      expect(h.parentOf('a')).toBe('s');
      expect(h.childrenOf('s')).toEqual(['a']);
    });

    it('parent_child edge wins over node.parent_id when both present', () => {
      const nodes = [n('s'), n('t'), n('a', 's')];
      const edges = [e('e1', 't', 'a')];
      expect(buildHierarchy(nodes, edges).parentOf('a')).toBe('t');
    });

    it('ignores parent_id pointing at an absent node', () => {
      const nodes = [n('a', 'ghost')];
      const h = buildHierarchy(nodes, []);
      expect(h.parentOf('a')).toBeUndefined();
      expect(h.orphans).toEqual(['a']);
    });

    it('non-parent_child edges do not create hierarchy links', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'a', 'b', 'sequence'), e('e2', 'a', 'b', 'data_dep')];
      const h = buildHierarchy(nodes, edges);
      expect(h.parentOf('b')).toBeUndefined();
      expect(h.orphans).toEqual(['a', 'b']);
    });

    it('roots are parentless nodes that have children; orphans are parentless and childless', () => {
      const nodes = [n('s'), n('a'), n('lonely')];
      const edges = [e('e1', 's', 'a')];
      const h = buildHierarchy(nodes, edges);
      expect(h.roots).toEqual(['s']);
      expect(h.orphans).toEqual(['lonely']);
    });

    it('ancestorsOf walks nearest-first to the root', () => {
      const nodes = [n('s'), n('a'), n('b')];
      const edges = [e('e1', 's', 'a'), e('e2', 'a', 'b')];
      expect(buildHierarchy(nodes, edges).ancestorsOf('b')).toEqual(['a', 's']);
    });

    it('ancestorsOf is cycle-safe', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'a', 'b'), e('e2', 'b', 'a')];
      const anc = buildHierarchy(nodes, edges).ancestorsOf('a');
      expect(anc).toContain('b');
      expect(new Set(anc).size).toBe(anc.length);
    });

    it('descendantsOf returns the full subtree in pre-order, cycle-safe', () => {
      const nodes = [n('s'), n('a'), n('b'), n('c')];
      const edges = [e('e1', 's', 'a'), e('e2', 's', 'b'), e('e3', 'a', 'c')];
      expect(buildHierarchy(nodes, edges).descendantsOf('s')).toEqual(['a', 'c', 'b']);
    });

    it('descendantsOf of a leaf is empty; of an unknown id is empty', () => {
      const nodes = [n('s'), n('a')];
      const edges = [e('e1', 's', 'a')];
      const h = buildHierarchy(nodes, edges);
      expect(h.descendantsOf('a')).toEqual([]);
      expect(h.descendantsOf('ghost')).toEqual([]);
    });

    it('roots are sorted when several disjoint trees exist', () => {
      const nodes = [n('z'), n('zc'), n('a'), n('ac')];
      const edges = [e('e1', 'z', 'zc'), e('e2', 'a', 'ac')];
      expect(buildHierarchy(nodes, edges).roots).toEqual(['a', 'z']);
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/hierarchy.test.ts` — EXPECT FAIL (`Cannot find module './hierarchy'`).
- [ ] Create `webui/web/src/lib/graph/hierarchy.ts`:

  ```ts
  import type { Node, Edge, Hierarchy } from './types';

  export function buildHierarchy(nodes: Node[], edges: Edge[]): Hierarchy {
    const present = new Set(nodes.map((nd) => nd.id));
    const parent = new Map<string, string>();
    const children = new Map<string, string[]>();

    for (const ed of edges) {
      if (ed.type !== 'parent_child') continue;
      if (!present.has(ed.src) || !present.has(ed.dst)) continue;
      parent.set(ed.dst, ed.src);
    }
    for (const nd of nodes) {
      if (parent.has(nd.id)) continue;
      if (nd.parent_id && present.has(nd.parent_id)) {
        parent.set(nd.id, nd.parent_id);
      }
    }
    for (const nd of nodes) {
      const p = parent.get(nd.id);
      if (p === undefined) continue;
      const arr = children.get(p);
      if (arr) arr.push(nd.id);
      else children.set(p, [nd.id]);
    }
    for (const arr of children.values()) arr.sort();

    const roots: string[] = [];
    const orphans: string[] = [];
    for (const nd of nodes) {
      if (parent.has(nd.id)) continue;
      if ((children.get(nd.id)?.length ?? 0) > 0) roots.push(nd.id);
      else orphans.push(nd.id);
    }
    roots.sort();
    orphans.sort();

    const childrenOf = (id: string): string[] => children.get(id) ?? [];
    const parentOf = (id: string): string | undefined => parent.get(id);

    const ancestorsOf = (id: string): string[] => {
      const out: string[] = [];
      const seen = new Set<string>([id]);
      let cur = parent.get(id);
      while (cur !== undefined && !seen.has(cur)) {
        out.push(cur);
        seen.add(cur);
        cur = parent.get(cur);
      }
      return out;
    };

    const descendantsOf = (id: string): string[] => {
      if (!present.has(id)) return [];
      const out: string[] = [];
      const seen = new Set<string>([id]);
      const walk = (node: string): void => {
        for (const c of childrenOf(node)) {
          if (seen.has(c)) continue;
          seen.add(c);
          out.push(c);
          walk(c);
        }
      };
      walk(id);
      return out;
    };

    return { childrenOf, parentOf, ancestorsOf, descendantsOf, roots, orphans };
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/hierarchy.test.ts` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `hierarchy.ts` at 100%. If any branch is uncovered, add the missing-case test before proceeding.
- [ ] Commit:

  ```text
  feat(webui): add buildHierarchy parent/children view-model

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 3: Collapse state + visibility (`lib/graph/collapse.ts`)

Pure functions over a `collapsed: Set<string>`: the default-collapse policy, the visible-node derivation, and immutable toggle/collapse-all/expand-all helpers.

**Files**

- Create: `webui/web/src/lib/graph/collapse.ts`
- Test: `webui/web/src/lib/graph/collapse.test.ts`

**Interfaces**

- Consumes: `Node[]`, `Hierarchy` (from `./types`); `Set<string>` collapsed ids. Relevant field: `Node.type` (`'session' | 'user_prompt' | 'assistant_turn' | 'tool_call' | 'mcp_call' | 'subagent' | 'hook_event' | 'marker'`).
- Produces:

  ```ts
  export type CollapsePredicate = (node: Node) => boolean;
  export const DEFAULT_COLLAPSE: CollapsePredicate;
  export function defaultCollapsed(nodes: Node[], hierarchy: Hierarchy, predicate?: CollapsePredicate): Set<string>;
  export function visibleNodeIds(nodes: Node[], hierarchy: Hierarchy, collapsed: Set<string>): Set<string>;
  export function toggle(collapsed: Set<string>, id: string): Set<string>;
  export function collapseAll(nodes: Node[], hierarchy: Hierarchy): Set<string>;
  export function expandAll(): Set<string>;
  ```

**Semantics**

- `DEFAULT_COLLAPSE`: collapse `assistant_turn` and `subagent` node types (the spec's tuned policy, expressed as a one-line swappable predicate).
- `defaultCollapsed`: returns the set of ids of nodes that (a) satisfy the predicate AND (b) actually have children in the hierarchy (collapsing a childless node is meaningless). Predicate defaults to `DEFAULT_COLLAPSE`.
- `visibleNodeIds`: a node is visible iff none of its ancestors is in `collapsed`. The collapsed node itself stays visible (you see the collapsed spine node, not its subtree).
- `toggle`: returns a NEW set with `id` added if absent, removed if present. Never mutates the input.
- `collapseAll`: every node that has children, collapsed.
- `expandAll`: empty set.

**Steps**

- [ ] Write `webui/web/src/lib/graph/collapse.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import {
    DEFAULT_COLLAPSE,
    defaultCollapsed,
    visibleNodeIds,
    toggle,
    collapseAll,
    expandAll,
  } from './collapse';
  import { buildHierarchy } from './hierarchy';
  import type { Node, Edge } from '../types';

  function n(id: string, type: string): Node {
    return { id, run_id: 'r1', type, rev: 1 };
  }
  function e(id: string, src: string, dst: string): Edge {
    return { id, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
  }

  function sessionFixture() {
    const nodes = [
      n('s', 'session'),
      n('u', 'user_prompt'),
      n('at', 'assistant_turn'),
      n('t1', 'tool_call'),
      n('t2', 'mcp_call'),
      n('sub', 'subagent'),
      n('subchild', 'assistant_turn'),
    ];
    const edges = [
      e('e1', 's', 'u'),
      e('e2', 'u', 'at'),
      e('e3', 'at', 't1'),
      e('e4', 'at', 't2'),
      e('e5', 'u', 'sub'),
      e('e6', 'sub', 'subchild'),
    ];
    return { nodes, edges, h: buildHierarchy(nodes, edges) };
  }

  describe('DEFAULT_COLLAPSE predicate', () => {
    it('targets assistant_turn and subagent only', () => {
      expect(DEFAULT_COLLAPSE(n('a', 'assistant_turn'))).toBe(true);
      expect(DEFAULT_COLLAPSE(n('a', 'subagent'))).toBe(true);
      expect(DEFAULT_COLLAPSE(n('a', 'session'))).toBe(false);
      expect(DEFAULT_COLLAPSE(n('a', 'user_prompt'))).toBe(false);
      expect(DEFAULT_COLLAPSE(n('a', 'tool_call'))).toBe(false);
    });
  });

  describe('defaultCollapsed', () => {
    it('collapses assistant_turn and subagent that have children', () => {
      const { nodes, h } = sessionFixture();
      expect(defaultCollapsed(nodes, h)).toEqual(new Set(['at', 'sub']));
    });

    it('does not collapse a predicate-matching node with no children', () => {
      const nodes = [n('s', 'session'), n('at', 'assistant_turn')];
      const edges = [e('e1', 's', 'at')];
      const h = buildHierarchy(nodes, edges);
      expect(defaultCollapsed(nodes, h)).toEqual(new Set<string>());
    });

    it('honours a swapped predicate', () => {
      const { nodes, h } = sessionFixture();
      const onlyUser = (node: Node) => node.type === 'user_prompt';
      expect(defaultCollapsed(nodes, h, onlyUser)).toEqual(new Set(['u']));
    });
  });

  describe('visibleNodeIds', () => {
    it('with nothing collapsed every node is visible', () => {
      const { nodes, h } = sessionFixture();
      const vis = visibleNodeIds(nodes, h, new Set());
      expect(vis.size).toBe(nodes.length);
    });

    it('a collapsed node stays visible but its descendants are hidden', () => {
      const { nodes, h } = sessionFixture();
      const vis = visibleNodeIds(nodes, h, new Set(['at']));
      expect(vis.has('at')).toBe(true);
      expect(vis.has('t1')).toBe(false);
      expect(vis.has('t2')).toBe(false);
      expect(vis.has('sub')).toBe(true);
    });

    it('collapsing an ancestor hides the whole subtree including nested groups', () => {
      const { nodes, h } = sessionFixture();
      const vis = visibleNodeIds(nodes, h, new Set(['u']));
      expect(vis.has('u')).toBe(true);
      expect(vis.has('at')).toBe(false);
      expect(vis.has('sub')).toBe(false);
      expect(vis.has('subchild')).toBe(false);
    });

    it('default policy yields the spine (session, prompt, turn, top-level subagent)', () => {
      const { nodes, h } = sessionFixture();
      const vis = visibleNodeIds(nodes, h, defaultCollapsed(nodes, h));
      expect([...vis].sort()).toEqual(['at', 's', 'sub', 'u']);
    });
  });

  describe('toggle', () => {
    it('adds an absent id and returns a new set', () => {
      const a = new Set(['x']);
      const b = toggle(a, 'y');
      expect(b).toEqual(new Set(['x', 'y']));
      expect(a).toEqual(new Set(['x']));
    });

    it('removes a present id and returns a new set', () => {
      const a = new Set(['x', 'y']);
      const b = toggle(a, 'x');
      expect(b).toEqual(new Set(['y']));
      expect(a).toEqual(new Set(['x', 'y']));
    });
  });

  describe('collapseAll / expandAll', () => {
    it('collapseAll collapses every node that has children', () => {
      const { nodes, h } = sessionFixture();
      expect(collapseAll(nodes, h)).toEqual(new Set(['s', 'u', 'at', 'sub']));
    });

    it('expandAll is the empty set', () => {
      expect(expandAll()).toEqual(new Set<string>());
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/collapse.test.ts` — EXPECT FAIL (`Cannot find module './collapse'`).
- [ ] Create `webui/web/src/lib/graph/collapse.ts`:

  ```ts
  import type { Node, Hierarchy } from './types';

  export type CollapsePredicate = (node: Node) => boolean;

  export const DEFAULT_COLLAPSE: CollapsePredicate = (node) =>
    node.type === 'assistant_turn' || node.type === 'subagent';

  function hasChildren(hierarchy: Hierarchy, id: string): boolean {
    return hierarchy.childrenOf(id).length > 0;
  }

  export function defaultCollapsed(
    nodes: Node[],
    hierarchy: Hierarchy,
    predicate: CollapsePredicate = DEFAULT_COLLAPSE,
  ): Set<string> {
    const out = new Set<string>();
    for (const node of nodes) {
      if (predicate(node) && hasChildren(hierarchy, node.id)) out.add(node.id);
    }
    return out;
  }

  export function visibleNodeIds(
    nodes: Node[],
    hierarchy: Hierarchy,
    collapsed: Set<string>,
  ): Set<string> {
    const out = new Set<string>();
    for (const node of nodes) {
      const hidden = hierarchy.ancestorsOf(node.id).some((a) => collapsed.has(a));
      if (!hidden) out.add(node.id);
    }
    return out;
  }

  export function toggle(collapsed: Set<string>, id: string): Set<string> {
    const next = new Set(collapsed);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  }

  export function collapseAll(nodes: Node[], hierarchy: Hierarchy): Set<string> {
    const out = new Set<string>();
    for (const node of nodes) {
      if (hasChildren(hierarchy, node.id)) out.add(node.id);
    }
    return out;
  }

  export function expandAll(): Set<string> {
    return new Set<string>();
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/collapse.test.ts` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `collapse.ts` at 100%.
- [ ] Commit:

  ```text
  feat(webui): add collapse policy + visibility derivation

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 4: Subtree aggregate rollup (`lib/graph/aggregate.ts`)

Roll up a collapsed node's subtree into one `Aggregate` for the badge. Status precedence is `error > running > ok`; absent numerics count as 0.

**Files**

- Create: `webui/web/src/lib/graph/aggregate.ts`
- Test: `webui/web/src/lib/graph/aggregate.test.ts`

**Interfaces**

- Consumes: `id: string`, `Hierarchy` (from `./types`), `byId: Record<string, Node>`. Relevant fields: `Node.status`, `Node.tokens_in?`, `Node.tokens_out?`, `Node.cost_usd?`.
- Produces: `export function aggregateOf(id: string, hierarchy: Hierarchy, byId: Record<string, Node>): Aggregate`.

**Semantics**

- The aggregate covers the descendants of `id` (the hidden subtree), NOT `id` itself — the badge summarises what is collapsed away.
- `count` = number of descendants.
- `tokensIn`/`tokensOut`/`costUsd` = sum over descendants, treating `undefined` as 0.
- `hasError` = any descendant has `status === 'error'`.
- `status` = `'error'` if any descendant errored, else `'running'` if any descendant is running, else `'ok'`.
- A descendant id absent from `byId` is skipped (defensive; should not happen with consistent inputs).

**Steps**

- [ ] Write `webui/web/src/lib/graph/aggregate.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import { aggregateOf } from './aggregate';
  import { buildHierarchy } from './hierarchy';
  import type { Node, Edge } from '../types';

  function n(id: string, extra: Partial<Node> = {}): Node {
    return { id, run_id: 'r1', type: 'tool_call', rev: 1, ...extra };
  }
  function e(id: string, src: string, dst: string): Edge {
    return { id, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
  }
  function index(nodes: Node[]): Record<string, Node> {
    return Object.fromEntries(nodes.map((nd) => [nd.id, nd]));
  }

  describe('aggregateOf', () => {
    it('a leaf has an empty aggregate', () => {
      const nodes = [n('a')];
      const h = buildHierarchy(nodes, []);
      expect(aggregateOf('a', h, index(nodes))).toEqual({
        count: 0,
        tokensIn: 0,
        tokensOut: 0,
        costUsd: 0,
        status: 'ok',
        hasError: false,
      });
    });

    it('sums tokens and cost across the subtree, ignoring the node itself', () => {
      const nodes = [
        n('p', { tokens_in: 999, tokens_out: 999, cost_usd: 9 }),
        n('a', { tokens_in: 10, tokens_out: 20, cost_usd: 0.1 }),
        n('b', { tokens_in: 5, tokens_out: 7, cost_usd: 0.2 }),
      ];
      const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
      const h = buildHierarchy(nodes, edges);
      expect(aggregateOf('p', h, index(nodes))).toEqual({
        count: 2,
        tokensIn: 15,
        tokensOut: 27,
        costUsd: 0.30000000000000004,
        status: 'ok',
        hasError: false,
      });
    });

    it('treats missing numeric fields as 0', () => {
      const nodes = [n('p'), n('a', { tokens_in: 4 }), n('b')];
      const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
      const h = buildHierarchy(nodes, edges);
      const agg = aggregateOf('p', h, index(nodes));
      expect(agg.tokensIn).toBe(4);
      expect(agg.tokensOut).toBe(0);
      expect(agg.costUsd).toBe(0);
      expect(agg.count).toBe(2);
    });

    it('status precedence: error beats running beats ok', () => {
      const nodes = [
        n('p'),
        n('a', { status: 'ok' }),
        n('b', { status: 'running' }),
        n('c', { status: 'error' }),
      ];
      const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b'), e('e3', 'p', 'c')];
      const h = buildHierarchy(nodes, edges);
      const agg = aggregateOf('p', h, index(nodes));
      expect(agg.status).toBe('error');
      expect(agg.hasError).toBe(true);
    });

    it('running when some running and none errored', () => {
      const nodes = [n('p'), n('a', { status: 'ok' }), n('b', { status: 'running' })];
      const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
      const h = buildHierarchy(nodes, edges);
      const agg = aggregateOf('p', h, index(nodes));
      expect(agg.status).toBe('running');
      expect(agg.hasError).toBe(false);
    });

    it('ok when all descendants ok or statusless', () => {
      const nodes = [n('p'), n('a', { status: 'ok' }), n('b')];
      const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
      const h = buildHierarchy(nodes, edges);
      expect(aggregateOf('p', h, index(nodes)).status).toBe('ok');
    });

    it('rolls up nested subtrees', () => {
      const nodes = [
        n('p'),
        n('a', { tokens_in: 1 }),
        n('a1', { tokens_in: 2, status: 'error' }),
      ];
      const edges = [e('e1', 'p', 'a'), e('e2', 'a', 'a1')];
      const h = buildHierarchy(nodes, edges);
      const agg = aggregateOf('p', h, index(nodes));
      expect(agg.count).toBe(2);
      expect(agg.tokensIn).toBe(3);
      expect(agg.status).toBe('error');
    });

    it('skips descendant ids absent from byId', () => {
      const nodes = [n('p'), n('a', { tokens_in: 4 })];
      const edges = [e('e1', 'p', 'a')];
      const h = buildHierarchy(nodes, edges);
      const partial = { p: nodes[0]! };
      const agg = aggregateOf('p', h, partial);
      expect(agg.count).toBe(0);
      expect(agg.tokensIn).toBe(0);
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/aggregate.test.ts` — EXPECT FAIL (`Cannot find module './aggregate'`).
- [ ] Create `webui/web/src/lib/graph/aggregate.ts`:

  ```ts
  import type { Node, Hierarchy, Aggregate } from './types';

  export function aggregateOf(
    id: string,
    hierarchy: Hierarchy,
    byId: Record<string, Node>,
  ): Aggregate {
    let count = 0;
    let tokensIn = 0;
    let tokensOut = 0;
    let costUsd = 0;
    let anyError = false;
    let anyRunning = false;

    for (const descId of hierarchy.descendantsOf(id)) {
      const node = byId[descId];
      if (!node) continue;
      count += 1;
      tokensIn += node.tokens_in ?? 0;
      tokensOut += node.tokens_out ?? 0;
      costUsd += node.cost_usd ?? 0;
      if (node.status === 'error') anyError = true;
      else if (node.status === 'running') anyRunning = true;
    }

    const status: Aggregate['status'] = anyError ? 'error' : anyRunning ? 'running' : 'ok';
    return { count, tokensIn, tokensOut, costUsd, status, hasError: anyError };
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/aggregate.test.ts` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `aggregate.ts` at 100%.
- [ ] Commit:

  ```text
  feat(webui): add subtree aggregate rollup for collapsed badges

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 5: Edge lifting (`lib/graph/lift-edges.ts`)

Remap each edge endpoint that is hidden to its nearest visible ancestor, drop self-edges, and dedupe by `(src, dst, type)`. This keeps a collapsed subagent connected to the spine and preserves cross-turn `data_dep`/`sequence` as turn-to-turn links.

**Files**

- Create: `webui/web/src/lib/graph/lift-edges.ts`
- Test: `webui/web/src/lib/graph/lift-edges.test.ts`

**Interfaces**

- Consumes: `Edge[]`, `visible: Set<string>`, `Hierarchy` (from `./types`).
- Produces: `export function liftEdges(edges: Edge[], visible: Set<string>, hierarchy: Hierarchy): Edge[]`.

**Semantics**

- For each edge, lift `src` and `dst`: if the endpoint is in `visible`, keep it; otherwise walk `ancestorsOf(endpoint)` (nearest-first) to the first id in `visible`.
- If either endpoint cannot be lifted to any visible ancestor, drop the edge.
- After lifting, drop self-edges (`src === dst`).
- Dedupe by the `(src, dst, type)` triple. Keep the FIRST occurrence (input is id-sorted upstream, so this is deterministic) and keep that edge's original `id` so SvelteFlow keys stay stable.
- Output order: input order, minus drops/dupes (input is already id-sorted by `sessionGraphFrom`).

**Steps**

- [ ] Write `webui/web/src/lib/graph/lift-edges.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import { liftEdges } from './lift-edges';
  import { buildHierarchy } from './hierarchy';
  import type { Node, Edge } from '../types';

  function n(id: string): Node {
    return { id, run_id: 'r1', type: 'marker', rev: 1 };
  }
  function pc(id: string, src: string, dst: string): Edge {
    return { id, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
  }
  function dep(id: string, src: string, dst: string, type = 'data_dep'): Edge {
    return { id, run_id: 'r1', type, src, dst, rev: 1 };
  }

  describe('liftEdges', () => {
    it('keeps edges whose endpoints are both visible', () => {
      const nodes = [n('s'), n('a')];
      const h = buildHierarchy(nodes, [pc('e1', 's', 'a')]);
      const edges = [pc('e1', 's', 'a')];
      const out = liftEdges(edges, new Set(['s', 'a']), h);
      expect(out).toEqual([pc('e1', 's', 'a')]);
    });

    it('lifts a hidden endpoint to its nearest visible ancestor', () => {
      const nodes = [n('s'), n('at'), n('t1'), n('dep')];
      const structure = [pc('e1', 's', 'at'), pc('e2', 'at', 't1'), pc('e3', 's', 'dep')];
      const h = buildHierarchy(nodes, structure);
      const edges = [...structure, dep('d1', 'dep', 't1')];
      const out = liftEdges(edges, new Set(['s', 'at', 'dep']), h);
      const lifted = out.find((e) => e.id === 'd1');
      expect(lifted).toBeDefined();
      expect(lifted!.src).toBe('dep');
      expect(lifted!.dst).toBe('at');
      expect(lifted!.type).toBe('data_dep');
    });

    it('drops a self-edge produced by lifting both endpoints to the same ancestor', () => {
      const nodes = [n('at'), n('t1'), n('t2')];
      const structure = [pc('e1', 'at', 't1'), pc('e2', 'at', 't2')];
      const h = buildHierarchy(nodes, structure);
      const edges = [...structure, dep('d1', 't1', 't2', 'sequence')];
      const out = liftEdges(edges, new Set(['at']), h);
      expect(out.find((e) => e.id === 'd1')).toBeUndefined();
      expect(out.find((e) => e.src === e.dst)).toBeUndefined();
    });

    it('dedupes by (src,dst,type), keeping the first edge id', () => {
      const nodes = [n('at'), n('t1'), n('bt'), n('t2')];
      const structure = [pc('e1', 'at', 't1'), pc('e2', 'bt', 't2')];
      const h = buildHierarchy(nodes, structure);
      const edges = [
        ...structure,
        dep('d1', 't1', 't2', 'data_dep'),
        dep('d2', 't1', 't2', 'data_dep'),
      ];
      const out = liftEdges(edges, new Set(['at', 'bt']), h);
      const deps = out.filter((e) => e.type === 'data_dep');
      expect(deps).toHaveLength(1);
      expect(deps[0]!.id).toBe('d1');
      expect(deps[0]!.src).toBe('at');
      expect(deps[0]!.dst).toBe('bt');
    });

    it('keeps two lifted edges that differ only by type', () => {
      const nodes = [n('at'), n('t1'), n('bt'), n('t2')];
      const structure = [pc('e1', 'at', 't1'), pc('e2', 'bt', 't2')];
      const h = buildHierarchy(nodes, structure);
      const edges = [
        ...structure,
        dep('d1', 't1', 't2', 'data_dep'),
        dep('d2', 't1', 't2', 'sequence'),
      ];
      const out = liftEdges(edges, new Set(['at', 'bt']), h);
      const nonStructural = out.filter((e) => e.type !== 'parent_child');
      expect(nonStructural).toHaveLength(2);
      expect(nonStructural.map((e) => e.type).sort()).toEqual(['data_dep', 'sequence']);
    });

    it('drops an edge whose endpoint has no visible ancestor', () => {
      const nodes = [n('s'), n('orphanParent'), n('orphanChild')];
      const structure = [pc('e1', 'orphanParent', 'orphanChild')];
      const h = buildHierarchy(nodes, structure);
      const edges = [dep('d1', 's', 'orphanChild')];
      const out = liftEdges(edges, new Set(['s']), h);
      expect(out).toEqual([]);
    });

    it('keeps a collapsed subagent connected to the spine via its parent edge', () => {
      const nodes = [n('u'), n('sub'), n('subchild')];
      const structure = [pc('e1', 'u', 'sub'), pc('e2', 'sub', 'subchild')];
      const h = buildHierarchy(nodes, structure);
      const out = liftEdges(structure, new Set(['u', 'sub']), h);
      expect(out.find((e) => e.id === 'e1')).toEqual(pc('e1', 'u', 'sub'));
      expect(out.find((e) => e.id === 'e2')).toBeUndefined();
    });

    it('empty input → empty output', () => {
      const h = buildHierarchy([], []);
      expect(liftEdges([], new Set(), h)).toEqual([]);
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/lift-edges.test.ts` — EXPECT FAIL (`Cannot find module './lift-edges'`).
- [ ] Create `webui/web/src/lib/graph/lift-edges.ts`:

  ```ts
  import type { Edge, Hierarchy } from './types';

  function liftEndpoint(
    id: string,
    visible: Set<string>,
    hierarchy: Hierarchy,
  ): string | undefined {
    if (visible.has(id)) return id;
    for (const anc of hierarchy.ancestorsOf(id)) {
      if (visible.has(anc)) return anc;
    }
    return undefined;
  }

  export function liftEdges(
    edges: Edge[],
    visible: Set<string>,
    hierarchy: Hierarchy,
  ): Edge[] {
    const out: Edge[] = [];
    const seen = new Set<string>();
    for (const edge of edges) {
      const src = liftEndpoint(edge.src, visible, hierarchy);
      const dst = liftEndpoint(edge.dst, visible, hierarchy);
      if (src === undefined || dst === undefined) continue;
      if (src === dst) continue;
      const key = `${src} ${dst} ${edge.type}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push({ ...edge, src, dst });
    }
    return out;
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/lift-edges.test.ts` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `lift-edges.ts` at 100%. All five graph modules must now report 100%.
- [ ] Commit:

  ```text
  feat(webui): add edge lifting for collapsed subtrees

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 6: Arrow-nav skips hidden nodes (`lib/graph-nav.ts`)

Make window-level arrow traversal operate over the visible set so collapsed-away nodes are unreachable. Add a `visible?: Set<string>` parameter; when supplied, filter nodes and edges to the visible set before navigating. Default `undefined` preserves existing behaviour (so the existing tests still pass unchanged).

**Files**

- Modify: `webui/web/src/lib/graph-nav.ts`
- Test: `webui/web/src/lib/graph-nav.test.ts` (extend; keep all existing cases)

**Interfaces**

- Consumes: `currentId: string | null`, `nodes: Node[]`, `edges: Edge[]`, `dir: NavDir`, and a new optional `visible?: Set<string>`.
- Produces: `export function nextNodeByDirection(currentId, nodes, edges, dir, visible?): string | null` — same return contract, restricted to visible nodes when `visible` is given.

**Steps**

- [ ] Append a new `describe` block to `webui/web/src/lib/graph-nav.test.ts`:

  ```ts
  describe('visible-set filtering', () => {
    it('skips hidden children when navigating right', () => {
      const nodes = [n('s'), n('a'), n('b')];
      const edges = [e('e1', 's', 'a'), e('e2', 's', 'b')];
      const visible = new Set(['s', 'b']);
      expect(nextNodeByDirection('s', nodes, edges, 'right', visible)).toBe('b');
    });

    it('returns currentId when the only target is hidden', () => {
      const nodes = [n('s'), n('a')];
      const edges = [e('e1', 's', 'a')];
      const visible = new Set(['s']);
      expect(nextNodeByDirection('s', nodes, edges, 'right', visible)).toBe('s');
    });

    it('root selection ignores hidden nodes', () => {
      const nodes = [n('hidden'), n('vis')];
      const visible = new Set(['vis']);
      expect(nextNodeByDirection(null, nodes, [], 'down', visible)).toBe('vis');
    });

    it('up/down skip hidden siblings', () => {
      const nodes = [n('root'), n('a'), n('b'), n('c')];
      const edges = [e('e1', 'root', 'a'), e('e2', 'root', 'b'), e('e3', 'root', 'c')];
      const visible = new Set(['root', 'a', 'c']);
      expect(nextNodeByDirection('a', nodes, edges, 'down', visible)).toBe('c');
    });

    it('without a visible set behaves exactly as before', () => {
      const nodes = [n('s'), n('a'), n('b')];
      const edges = [e('e1', 's', 'a'), e('e2', 's', 'b')];
      expect(nextNodeByDirection('s', nodes, edges, 'right')).toBe('a');
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph-nav.test.ts` — EXPECT FAIL (the new cases fail; the 5th argument is ignored).
- [ ] Modify `webui/web/src/lib/graph-nav.ts` to accept and apply the optional visible set. Replace the signature and add a filtering preamble; the rest of the body operates on the filtered arrays:

  ```ts
  export function nextNodeByDirection(
    currentId: string | null,
    nodes: Node[],
    edges: Edge[],
    dir: NavDir,
    visible?: Set<string>,
  ): string | null {
    const vNodes = visible ? nodes.filter((nd) => visible.has(nd.id)) : nodes;
    const vEdges = visible
      ? edges.filter((ed) => visible.has(ed.src) && visible.has(ed.dst))
      : edges;
    if (vNodes.length === 0) return null;

    if (currentId === null) {
      const hasIncoming = new Set(vEdges.filter((e) => e.type === 'parent_child').map((e) => e.dst));
      const roots = vNodes.filter((nd) => !hasIncoming.has(nd.id)).map((nd) => nd.id).sort();
      return roots.length > 0 ? roots[0]! : vNodes.map((nd) => nd.id).sort()[0]!;
    }

    if (dir === 'right') {
      const targets = vEdges.filter((e) => e.src === currentId).map((e) => e.dst).sort();
      return targets.length > 0 ? targets[0]! : currentId;
    }

    if (dir === 'left') {
      const sources = vEdges.filter((e) => e.dst === currentId).map((e) => e.src).sort();
      return sources.length > 0 ? sources[0]! : currentId;
    }

    const parentEdge = vEdges.find((e) => e.type === 'parent_child' && e.dst === currentId);
    if (!parentEdge) return currentId;

    const parentId = parentEdge.src;
    const siblings = vEdges
      .filter((e) => e.type === 'parent_child' && e.src === parentId)
      .map((e) => e.dst)
      .sort();

    const idx = siblings.indexOf(currentId);
    if (dir === 'up') {
      return idx > 0 ? siblings[idx - 1]! : currentId;
    }
    return idx < siblings.length - 1 ? siblings[idx + 1]! : currentId;
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph-nav.test.ts` — EXPECT PASS (old + new cases).
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `graph-nav.ts` at 100%.
- [ ] Commit:

  ```text
  feat(webui): arrow nav skips collapsed-away nodes

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 7: Collapse-aware layout helper (`lib/layout.ts`)

Add a thin helper that composes the pure modules into the exact `{nodes, edges}` slice that goes to dagre, plus a stable topology key that includes the collapsed set. Keeping this in `layout.ts` (already vitest-gated) means the composition is unit-tested rather than buried in the Svelte component.

**Files**

- Modify: `webui/web/src/lib/layout.ts`
- Test: `webui/web/src/lib/layout.test.ts` (extend)

**Interfaces**

- Consumes: `Node[]`, `Edge[]`, `Hierarchy`, `collapsed: Set<string>`; plus the existing `buildHierarchy`, `visibleNodeIds`, `liftEdges`.
- Produces:

  ```ts
  export interface CollapseView {
    nodes: CNode[];
    edges: CEdge[];
    visible: Set<string>;
    hierarchy: Hierarchy;
  }
  export function collapseView(nodes: CNode[], edges: CEdge[], collapsed: Set<string>): CollapseView;
  export function collapseTopologyKey(nodes: { id: string }[], edges: { id: string }[], collapsed: Set<string>): string;
  ```

**Steps**

- [ ] Append to `webui/web/src/lib/layout.test.ts`:

  ```ts
  import { collapseView, collapseTopologyKey } from './layout';

  describe('collapseView', () => {
    it('returns only visible nodes and lifted edges', () => {
      const nodes = [
        makeNode('s', 'session'),
        makeNode('at', 'assistant_turn'),
        makeNode('t1', 'tool_call'),
      ];
      const edges = [makeEdge('e1', 's', 'at'), makeEdge('e2', 'at', 't1')];
      const view = collapseView(nodes, edges, new Set(['at']));
      expect(view.nodes.map((n) => n.id).sort()).toEqual(['at', 's']);
      expect(view.edges.find((e) => e.id === 'e2')).toBeUndefined();
      expect(view.visible.has('t1')).toBe(false);
      expect(view.hierarchy.parentOf('at')).toBe('s');
    });

    it('with nothing collapsed returns every node', () => {
      const nodes = [makeNode('s', 'session'), makeNode('a', 'tool_call')];
      const edges = [makeEdge('e1', 's', 'a')];
      const view = collapseView(nodes, edges, new Set());
      expect(view.nodes).toHaveLength(2);
      expect(view.edges).toHaveLength(1);
    });
  });

  describe('collapseTopologyKey', () => {
    it('changes when the collapsed set changes', () => {
      const nodes = [{ id: 'a' }, { id: 'b' }];
      const edges = [{ id: 'e1' }];
      const k1 = collapseTopologyKey(nodes, edges, new Set());
      const k2 = collapseTopologyKey(nodes, edges, new Set(['a']));
      expect(k1).not.toBe(k2);
    });

    it('is stable regardless of collapsed-set insertion order', () => {
      const nodes = [{ id: 'a' }];
      const edges: { id: string }[] = [];
      const k1 = collapseTopologyKey(nodes, edges, new Set(['x', 'y']));
      const k2 = collapseTopologyKey(nodes, edges, new Set(['y', 'x']));
      expect(k1).toBe(k2);
    });

    it('is stable regardless of node/edge order', () => {
      const k1 = collapseTopologyKey([{ id: 'b' }, { id: 'a' }], [{ id: 'e2' }, { id: 'e1' }], new Set());
      const k2 = collapseTopologyKey([{ id: 'a' }, { id: 'b' }], [{ id: 'e1' }, { id: 'e2' }], new Set());
      expect(k1).toBe(k2);
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/layout.test.ts` — EXPECT FAIL (`collapseView`/`collapseTopologyKey` not exported).
- [ ] Modify `webui/web/src/lib/layout.ts`: add imports and the two functions. Add at the top after the existing imports:

  ```ts
  import { buildHierarchy } from './graph/hierarchy';
  import { visibleNodeIds } from './graph/collapse';
  import { liftEdges } from './graph/lift-edges';
  import type { Hierarchy } from './graph/types';
  ```

  Then append:

  ```ts
  export interface CollapseView {
    nodes: CNode[];
    edges: CEdge[];
    visible: Set<string>;
    hierarchy: Hierarchy;
  }

  export function collapseView(
    nodes: CNode[],
    edges: CEdge[],
    collapsed: Set<string>,
  ): CollapseView {
    const hierarchy = buildHierarchy(nodes, edges);
    const visible = visibleNodeIds(nodes, hierarchy, collapsed);
    const visNodes = nodes.filter((n) => visible.has(n.id));
    const visEdges = liftEdges(edges, visible, hierarchy);
    return { nodes: visNodes, edges: visEdges, visible, hierarchy };
  }

  export function collapseTopologyKey(
    nodes: { id: string }[],
    edges: { id: string }[],
    collapsed: Set<string>,
  ): string {
    return JSON.stringify([
      [...nodes.map((n) => n.id)].sort(),
      [...edges.map((e) => e.id)].sort(),
      [...collapsed].sort(),
    ]);
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/layout.test.ts` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `layout.ts` at 100%.
- [ ] Commit:

  ```text
  feat(webui): add collapseView + collapse-aware topology key

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 8: Aggregate badge text helper (`lib/graph/badge.ts`)

Format the badge stat line as a pure function so the component stays logic-free and the formatting is unit-tested. Reuse the existing `format` helpers.

**Files**

- Create: `webui/web/src/lib/graph/badge.ts`
- Test: `webui/web/src/lib/graph/badge.test.ts`

**Interfaces**

- Consumes: `Aggregate` (from `./types`); `formatTokens`, `formatCost` (from `../format/format`).
- Produces:

  ```ts
  export function badgeStatLine(agg: Aggregate): string;
  export function badgeStatusColor(status: Aggregate['status']): string;
  ```

**Semantics**

- `badgeStatLine`: `"{count} · {tokensIn}→{tokensOut} · {cost}"` using `formatTokens` for token counts and `formatCost` for cost. Example: count 3, tokensIn 1500, tokensOut 12000, cost 0.0234 → `"3 · 1,500→12.0k · $0.02"`.
- `badgeStatusColor`: maps the rollup status to the existing CSS variable string — `error` → `var(--error)`, `running` → `var(--running)`, `ok` → `var(--ok)`.

**Steps**

- [ ] Write `webui/web/src/lib/graph/badge.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import { badgeStatLine, badgeStatusColor } from './badge';
  import type { Aggregate } from './types';

  function agg(extra: Partial<Aggregate> = {}): Aggregate {
    return {
      count: 0,
      tokensIn: 0,
      tokensOut: 0,
      costUsd: 0,
      status: 'ok',
      hasError: false,
      ...extra,
    };
  }

  describe('badgeStatLine', () => {
    it('formats count, tokens and cost with existing helpers', () => {
      expect(badgeStatLine(agg({ count: 3, tokensIn: 1500, tokensOut: 12000, costUsd: 0.0234 }))).toBe(
        '3 · 1,500→12.0k · $0.02',
      );
    });

    it('renders zeros explicitly', () => {
      expect(badgeStatLine(agg())).toBe('0 · 0→0 · $0.00');
    });

    it('uses 4-decimal cost for sub-cent totals', () => {
      expect(badgeStatLine(agg({ count: 1, tokensIn: 5, tokensOut: 7, costUsd: 0.0005 }))).toBe(
        '1 · 5→7 · $0.0005',
      );
    });
  });

  describe('badgeStatusColor', () => {
    it('maps each status to its CSS variable', () => {
      expect(badgeStatusColor('error')).toBe('var(--error)');
      expect(badgeStatusColor('running')).toBe('var(--running)');
      expect(badgeStatusColor('ok')).toBe('var(--ok)');
    });
  });
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/badge.test.ts` — EXPECT FAIL (`Cannot find module './badge'`).
- [ ] Create `webui/web/src/lib/graph/badge.ts`:

  ```ts
  import type { Aggregate } from './types';
  import { formatTokens, formatCost } from '../format/format';

  export function badgeStatLine(agg: Aggregate): string {
    return `${agg.count} · ${formatTokens(agg.tokensIn)}→${formatTokens(agg.tokensOut)} · ${formatCost(agg.costUsd)}`;
  }

  export function badgeStatusColor(status: Aggregate['status']): string {
    if (status === 'error') return 'var(--error)';
    if (status === 'running') return 'var(--running)';
    return 'var(--ok)';
  }
  ```

- [ ] Run `cd webui && npx vitest run web/src/lib/graph/badge.test.ts` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS, `badge.ts` at 100%.
- [ ] Commit:

  ```text
  feat(webui): add aggregate badge text + status-color helpers

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 9: GraphNode renders collapse affordance + aggregate badge

Extend the node component so a collapsible node shows the `▸/▾` toggle as a separate hit-target, and a collapsed node shows the aggregate badge. The toggle calls a callback passed through node `data`; it must NOT trigger body-click selection.

**Files**

- Modify: `webui/web/src/components/GraphNode.svelte`
- Covered by: Playwright e2e in Task 11 (components are not line-gated).

**Interfaces**

- Consumes (via `data`): `{ catNode: CNode; sessionHash: string; onActivate?: () => void; collapsible?: boolean; collapsed?: boolean; aggregate?: Aggregate; onToggle?: (id: string) => void }`.
- Produces: rendered `▸/▾` button + badge; `onToggle(id)` on toggle activation; existing `navigateToNode` on body activation.

**Steps**

- [ ] Add the new `data` fields to the `GraphNodeData` type and destructure them. Replace the existing `type GraphNodeData = …` line and the derived block below it:

  ```ts
  import type { Aggregate } from '../lib/graph/types';
  import { badgeStatLine, badgeStatusColor } from '../lib/graph/badge';

  type GraphNodeData = {
    catNode: CNode;
    sessionHash: string;
    onActivate?: () => void;
    collapsible?: boolean;
    collapsed?: boolean;
    aggregate?: Aggregate;
    onToggle?: (id: string) => void;
  };
  ```

  And add derived values alongside the existing ones:

  ```ts
  const collapsible = $derived(data.collapsible ?? false);
  const collapsed = $derived(data.collapsed ?? false);
  const aggregate = $derived(data.aggregate);
  const onToggle = $derived(data.onToggle);
  ```

- [ ] Add a toggle handler that stops propagation so the body click does not also fire:

  ```ts
  function handleToggle(ev: MouseEvent | KeyboardEvent) {
    ev.stopPropagation();
    ev.preventDefault();
    onToggle?.(id);
  }
  ```

- [ ] In the markup, add the toggle button inside `.graph-node-header` before `.graph-node-name`, gated on `collapsible`, and render the badge after `.graph-node-id` when `collapsed && aggregate`:

  ```svelte
  {#if collapsible}
    <button
      class="graph-node-toggle"
      type="button"
      aria-label={collapsed ? 'Expand node' : 'Collapse node'}
      aria-expanded={!collapsed}
      onclick={handleToggle}
      onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleToggle(e); }}
    >{collapsed ? '▸' : '▾'}</button>
  {/if}
  ```

  ```svelte
  {#if collapsed && aggregate}
    <div class="graph-node-badge" style="--badge-status: {badgeStatusColor(aggregate.status)};">
      <span class="graph-node-badge-stat mono">{badgeStatLine(aggregate)}</span>
    </div>
  {/if}
  ```

- [ ] Add minimalist styles (no decoration; status color is a left bar on the badge). Append to the component `<style>`:

  ```css
  .graph-node-toggle {
    background: transparent;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: var(--text-sm);
    line-height: 1;
    padding: 0 var(--s1) 0 0;
    flex-shrink: 0;
  }

  .graph-node-toggle:hover {
    color: var(--text);
  }

  .graph-node-toggle:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .graph-node-badge {
    margin-top: var(--s1);
    padding-left: var(--s2);
    border-left: 2px solid var(--badge-status);
  }

  .graph-node-badge-stat {
    font-size: var(--text-xs);
    color: var(--text-dim);
  }
  ```

- [ ] Run `cd webui && npm run typecheck` — EXPECT PASS (no type errors from the new `data` fields).
- [ ] Commit:

  ```text
  feat(webui): render collapse toggle + aggregate badge on graph nodes

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 10: Wire collapse into GraphCanvas + toolbar

Hold the `collapsed: Set` view state, seed it from `defaultCollapsed` when topology first loads, feed only visible nodes + lifted edges into dagre, key the layout effect on `collapseTopologyKey`, pass badge/toggle data to nodes, anchor the re-layout, and add Collapse-all / Expand-all / orphan "other (N)" controls. Arrow nav (in `SessionView`) is restricted to the visible set.

**Files**

- Modify: `webui/web/src/components/GraphCanvas.svelte`
- Modify: `webui/web/src/components/SessionView.svelte` (pass visible set to `nextNodeByDirection`)
- Covered by: Playwright e2e in Task 11.

**Interfaces**

- Consumes: `sessionGraph(hash)` → `{nodes, edges}`; `collapseView`, `collapseTopologyKey`, `applyLayout` (from `../lib/layout`); `defaultCollapsed`, `toggle`, `collapseAll`, `expandAll` (from `../lib/graph/collapse`); `aggregateOf` (from `../lib/graph/aggregate`); `buildHierarchy` (via `collapseView`).
- Produces: layouted visible nodes carrying `collapsible/collapsed/aggregate/onToggle` in `data`; toolbar buttons; orphan reveal state. Re-layout preserves the toggled node's screen position and the selection in view.

**Steps**

- [ ] Add collapse state and a one-time seed guard. After the existing `let prevTopologyKey = '';` declaration, add:

  ```ts
  import { collapseView, collapseTopologyKey } from '../lib/layout';
  import { buildHierarchy } from '../lib/graph/hierarchy';
  import { defaultCollapsed, toggle as toggleCollapse, collapseAll, expandAll } from '../lib/graph/collapse';
  import { aggregateOf } from '../lib/graph/aggregate';

  let collapsed = $state.raw<Set<string>>(new Set());
  let seeded = false;
  let showOrphans = $state(false);
  let anchorId: string | null = null;
  ```

- [ ] Replace the topology effect (lines ~36–78 of the current file) so it: seeds `collapsed` once, computes the collapse view, keys on `collapseTopologyKey`, and tags each node with collapse data. Replace the whole first `$effect(() => { … })` block with:

  ```ts
  $effect(() => {
    const graph = sessionGraph(hash);

    if (!seeded && graph.nodes.length > 0) {
      const h = buildHierarchy(graph.nodes, graph.edges);
      collapsed = defaultCollapsed(graph.nodes, h);
      seeded = true;
    }

    const topologyKey = collapseTopologyKey(graph.nodes, graph.edges, collapsed);

    if (topologyKey !== prevTopologyKey) {
      prevTopologyKey = topologyKey;
      const view = collapseView(graph.nodes, graph.edges, collapsed);
      const byId = Object.fromEntries(graph.nodes.map((n) => [n.id, n]));
      const result = applyLayout(view.nodes, view.edges);
      xyNodes = result.nodes.map((n) => {
        const id = n.id;
        const collapsible = view.hierarchy.childrenOf(id).length > 0;
        const isCollapsed = collapsed.has(id);
        return {
          ...n,
          type: 'default',
          data: {
            ...(n.data as object),
            sessionHash: hash,
            onActivate: onNodeActivate,
            collapsible,
            collapsed: isCollapsed,
            aggregate: isCollapsed ? aggregateOf(id, view.hierarchy, byId) : undefined,
            onToggle: handleToggle,
          },
        };
      }) as unknown as XyFlowNode[];
      const matchingIds = untrack(() => filteredNodeIds.value);
      const dimmed = matchingIds ? dimmedEdgeIds(view.edges, matchingIds) : new Set<string>();
      xyEdges = result.edges.map((e) => ({
        ...e,
        className: dimmed.has(e.id) ? 'edge--dimmed' : undefined,
      })) as unknown as XyFlowEdge[];
      pendingFitView = true;
    } else if (graph.nodes.length > 0) {
      const view = collapseView(graph.nodes, graph.edges, collapsed);
      const byId = Object.fromEntries(graph.nodes.map((n) => [n.id, n]));
      const nodeMap = new Map(view.nodes.map((n) => [n.id, n]));
      const current = untrack(() => xyNodes);
      let changed = false;
      const next = current.map((xyN) => {
        const catNode = nodeMap.get(xyN.id);
        if (!catNode) return xyN;
        const isCollapsed = collapsed.has(xyN.id);
        const data = {
          catNode,
          sessionHash: hash,
          onActivate: onNodeActivate,
          collapsible: view.hierarchy.childrenOf(xyN.id).length > 0,
          collapsed: isCollapsed,
          aggregate: isCollapsed ? aggregateOf(xyN.id, view.hierarchy, byId) : undefined,
          onToggle: handleToggle,
        };
        const prev = xyN.data as { catNode?: unknown; aggregate?: unknown } | undefined;
        if (prev?.catNode === catNode && prev?.aggregate === undefined && data.aggregate === undefined) {
          return xyN;
        }
        changed = true;
        return { ...xyN, data };
      });
      if (changed) xyNodes = next;
    }
  });
  ```

- [ ] Add the toggle handler that records the anchor (for stable re-layout) and recomputes `collapsed`. Place it above the effects:

  ```ts
  function handleToggle(id: string) {
    anchorId = id;
    collapsed = toggleCollapse(collapsed, id);
  }

  function handleCollapseAll() {
    const graph = sessionGraph(hash);
    const h = buildHierarchy(graph.nodes, graph.edges);
    anchorId = selectedNodeId.value;
    collapsed = collapseAll(graph.nodes, h);
  }

  function handleExpandAll() {
    anchorId = selectedNodeId.value;
    collapsed = expandAll();
  }
  ```

- [ ] Expose the visible set so `SessionView` arrow-nav can use it. Add a derived export-free store-style value and pass it down. Add after the effects:

  ```ts
  const visibleIds = $derived(new Set(xyNodes.map((n) => n.id)));
  ```

  Then in the markup, set it on the root element as a data attribute SvelteFlow ignores but `SessionView` can read, OR (preferred) lift the visible set via a callback prop. Add to `Props`:

  ```ts
  interface Props {
    hash: string;
    refit?: number;
    onNodeActivate?: () => void;
    onVisibleChange?: (ids: Set<string>) => void;
  }
  ```

  and an effect that reports it up:

  ```ts
  $effect(() => {
    onVisibleChange?.(visibleIds);
  });
  ```

- [ ] Add the toolbar markup above `<SvelteFlow …>` inside the `{:else}` branch:

  ```svelte
  <div class="graph-toolbar" role="toolbar" aria-label="Graph collapse controls">
    <button class="graph-toolbar-btn" type="button" onclick={handleCollapseAll}>Collapse all</button>
    <button class="graph-toolbar-btn" type="button" onclick={handleExpandAll}>Expand all</button>
    {#if orphanCount > 0}
      <button
        class="graph-toolbar-btn"
        type="button"
        aria-pressed={showOrphans}
        onclick={() => (showOrphans = !showOrphans)}
      >other ({orphanCount})</button>
    {/if}
  </div>
  ```

  with the orphan count derived from the hierarchy:

  ```ts
  const orphanCount = $derived(
    buildHierarchy(sessionGraph(hash).nodes, sessionGraph(hash).edges).orphans.length,
  );
  ```

- [ ] Implement orphan reveal: when `showOrphans` is true, the seeded `collapsed` is unchanged but orphan nodes (which are always visible — they have no collapsed ancestor) are already in the layout. Since orphans have no parent edge, they already render. The "other (N)" affordance therefore toggles a CSS class that highlights them; gate orphan visibility by filtering them OUT of the layout when `showOrphans` is false. Update `collapseView`'s caller: after computing `view`, drop orphan nodes unless revealed:

  ```ts
  const hier = view.hierarchy;
  const orphanSet = new Set(hier.orphans);
  const shownNodes = showOrphans ? view.nodes : view.nodes.filter((n) => !orphanSet.has(n.id));
  const shownEdges = showOrphans ? view.edges : view.edges.filter((e) => !orphanSet.has(e.src) && !orphanSet.has(e.dst));
  ```

  and feed `shownNodes`/`shownEdges` into `applyLayout` instead of `view.nodes`/`view.edges`. Add `showOrphans` to the topology key by appending it: extend the local `topologyKey` line to `collapseTopologyKey(graph.nodes, graph.edges, collapsed) + (showOrphans ? ':o' : '')`.

- [ ] Anchored re-layout: capture the toggled node's pre-layout screen position and translate after layout. In `FlowInternals.svelte` the fit happens via `fitView`. For anchoring, after `xyNodes` is recomputed and `anchorId` is set, compute the delta between the anchor's old and new positions and offset all node positions so the anchor stays put, honoring `prefers-reduced-motion`. Add a helper in `lib/layout.ts` (TDD it):

  - [ ] First add a test to `webui/web/src/lib/layout.test.ts`:

    ```ts
    import { anchorOffset } from './layout';

    describe('anchorOffset', () => {
      it('returns the delta that pins the anchor in place', () => {
        const oldPos = { a: { x: 10, y: 5 } };
        const newPos = { a: { x: 40, y: 25 } };
        expect(anchorOffset('a', oldPos, newPos)).toEqual({ dx: -30, dy: -20 });
      });

      it('returns zero when the anchor is missing from either map', () => {
        expect(anchorOffset('z', { a: { x: 1, y: 1 } }, {})).toEqual({ dx: 0, dy: 0 });
        expect(anchorOffset(null, {}, {})).toEqual({ dx: 0, dy: 0 });
      });
    });
    ```

  - [ ] Run `cd webui && npx vitest run web/src/lib/layout.test.ts` — EXPECT FAIL.
  - [ ] Add to `lib/layout.ts`:

    ```ts
    export function anchorOffset(
      anchorId: string | null,
      oldPos: Record<string, { x: number; y: number }>,
      newPos: Record<string, { x: number; y: number }>,
    ): { dx: number; dy: number } {
      if (!anchorId) return { dx: 0, dy: 0 };
      const o = oldPos[anchorId];
      const n = newPos[anchorId];
      if (!o || !n) return { dx: 0, dy: 0 };
      return { dx: o.x - n.x, dy: o.y - n.y };
    }
    ```

  - [ ] Run `cd webui && npx vitest run web/src/lib/layout.test.ts` — EXPECT PASS.
  - [ ] In `GraphCanvas.svelte`, before overwriting `xyNodes`, snapshot the old positions; after computing the new layout, apply `anchorOffset` to every node's `position` and clear `anchorId`. When `anchorId` is set, set `pendingFitView = false` (do not auto-fit on toggle — anchoring replaces the fit) so the view does not teleport; honor reduced motion by leaving the (already non-animated) position update as-is:

    ```ts
    const oldPos = Object.fromEntries(
      (untrack(() => xyNodes)).map((n) => [n.id, { x: n.position.x, y: n.position.y }]),
    );
    ```

    and after building `result`:

    ```ts
    const newPos = Object.fromEntries(result.nodes.map((n) => [n.id, { ...n.position }]));
    const off = anchorOffset(anchorId, oldPos, newPos);
    ```

    apply `position: { x: n.position.x + off.dx, y: n.position.y + off.dy }` when mapping `result.nodes` into `xyNodes`, and after the map set `if (anchorId) { pendingFitView = false; anchorId = null; } else { pendingFitView = true; }`.

- [ ] Add toolbar styles (minimalist):

  ```css
  .graph-toolbar {
    position: absolute;
    top: var(--s3);
    right: var(--s3);
    z-index: 10;
    display: flex;
    gap: var(--s2);
  }

  .graph-toolbar-btn {
    font-size: var(--text-xs);
    font-family: var(--font-ui);
    color: var(--text-dim);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--s1) var(--s2);
    cursor: pointer;
  }

  .graph-toolbar-btn:hover {
    color: var(--text);
    border-color: var(--accent);
  }

  .graph-toolbar-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .graph-toolbar-btn[aria-pressed='true'] {
    color: var(--accent);
    border-color: var(--accent);
  }
  ```

- [ ] In `SessionView.svelte`: hold a `visibleIds` state, pass `onVisibleChange={(ids) => (visibleIds = ids)}` to `<GraphCanvas>`, and pass `visibleIds` as the 5th argument to `nextNodeByDirection`. Add near the other `$state` declarations:

  ```ts
  let visibleIds = $state<Set<string>>(new Set());
  ```

  Update the nav call:

  ```ts
  const next = nextNodeByDirection(selectedNodeId.value, g.nodes, g.edges, dir, visibleIds);
  ```

  And the component usage:

  ```svelte
  <GraphCanvas {hash} refit={fitKey} onNodeActivate={onNodeActivate} onVisibleChange={(ids) => (visibleIds = ids)} />
  ```

- [ ] Run `cd webui && npm run typecheck` — EXPECT PASS.
- [ ] Run `cd webui && npm run test` — EXPECT PASS (pure layers, including the new `anchorOffset`, all 100%).
- [ ] Commit:

  ```text
  feat(webui): wire collapse view, badges, toolbar and anchored re-layout

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 11: Playwright e2e for collapsible graph

Cover the user-visible behaviour the spec enumerates: default spine, expand a turn, expand a subagent, collapse-all/expand-all, badge text, collapsed node keeps its spine edge, and a live SSE node under a collapsed parent bumps the badge without appearing.

**Files**

- Create: `webui/e2e/graph-collapse.spec.ts`

**Interfaces**

- Consumes: the Playwright mock-SSE harness pattern from `webui/e2e/graph.spec.ts` (route `/v1/sessions`, `/v1/sessions/{hash}/graph`, `/v1/events**` with `text/event-stream` bodies built by `buildSseBody`).
- Produces: assertions against `.svelte-flow__node`, `.graph-node-toggle`, `.graph-node-badge-stat`, `.graph-toolbar-btn`.

**Steps**

- [ ] Create `webui/e2e/graph-collapse.spec.ts` with a fixture session of session → user_prompt → assistant_turn → 2 tool_calls plus a subagent subtree, and a deferred SSE stream so a late tool_call can be injected:

  ```ts
  import { test, expect } from '@playwright/test';
  import type { SessionSummary, SseEvent } from '../web/src/lib/types';

  const hash = 'c0llapse0001c0llapse0001c0llap01';

  const sessions: SessionSummary[] = [
    {
      session: hash,
      status: 'running',
      tokens_in: 100,
      tokens_out: 200,
      node_count: 7,
      tool_count: 3,
      error_count: 0,
      run_ids: ['run1'],
    },
  ];

  const base: SseEvent[] = [
    { kind: 'node_upsert', rev: 1, node: { id: 's', run_id: 'run1', type: 'session', name: 'Session', status: 'ok', rev: 1 } },
    { kind: 'node_upsert', rev: 2, node: { id: 'u', run_id: 'run1', type: 'user_prompt', name: 'Prompt', status: 'ok', rev: 2 } },
    { kind: 'node_upsert', rev: 3, node: { id: 'at', run_id: 'run1', type: 'assistant_turn', name: 'Turn', status: 'ok', tokens_in: 10, tokens_out: 20, rev: 3 } },
    { kind: 'node_upsert', rev: 4, node: { id: 't1', run_id: 'run1', type: 'tool_call', name: 'Bash', status: 'ok', tokens_in: 5, tokens_out: 7, rev: 4 } },
    { kind: 'node_upsert', rev: 5, node: { id: 't2', run_id: 'run1', type: 'tool_call', name: 'Read', status: 'ok', tokens_in: 3, tokens_out: 4, rev: 5 } },
    { kind: 'node_upsert', rev: 6, node: { id: 'sub', run_id: 'run1', type: 'subagent', name: 'Worker', status: 'ok', rev: 6 } },
    { kind: 'node_upsert', rev: 7, node: { id: 'subc', run_id: 'run1', type: 'tool_call', name: 'Grep', status: 'ok', rev: 7 } },
    { kind: 'edge_upsert', rev: 8, edge: { id: 'e1', run_id: 'run1', type: 'parent_child', src: 's', dst: 'u', rev: 8 } },
    { kind: 'edge_upsert', rev: 9, edge: { id: 'e2', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'at', rev: 9 } },
    { kind: 'edge_upsert', rev: 10, edge: { id: 'e3', run_id: 'run1', type: 'parent_child', src: 'at', dst: 't1', rev: 10 } },
    { kind: 'edge_upsert', rev: 11, edge: { id: 'e4', run_id: 'run1', type: 'parent_child', src: 'at', dst: 't2', rev: 11 } },
    { kind: 'edge_upsert', rev: 12, edge: { id: 'e5', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'sub', rev: 12 } },
    { kind: 'edge_upsert', rev: 13, edge: { id: 'e6', run_id: 'run1', type: 'parent_child', src: 'sub', dst: 'subc', rev: 13 } },
  ];

  function body(events: SseEvent[]): string {
    return events.map((ev) => `data: ${JSON.stringify(ev)}\n\n`).join('');
  }

  test.beforeEach(async ({ page }) => {
    await page.route('/v1/sessions', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sessions) }),
    );
    await page.route(`/v1/sessions/${hash}/graph`, (route) =>
      route.fulfill({ status: 200, contentType: 'text/event-stream', body: body(base) }),
    );
    await page.route('/v1/events**', (route) =>
      route.fulfill({ status: 200, contentType: 'text/event-stream', body: body(base) }),
    );
  });

  function node(page: import('@playwright/test').Page, id: string) {
    return page.locator(`.svelte-flow__node[data-id="${id}"]`);
  }
  ```

- [ ] Add the default-spine test (tools hidden, turn + subagent collapsed and visible):

  ```ts
  test('default view shows the spine, not the leaves', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    await expect(node(page, 's')).toBeVisible();
    await expect(node(page, 'u')).toBeVisible();
    await expect(node(page, 'at')).toBeVisible();
    await expect(node(page, 'sub')).toBeVisible();
    await expect(node(page, 't1')).toHaveCount(0);
    await expect(node(page, 't2')).toHaveCount(0);
    await expect(node(page, 'subc')).toHaveCount(0);
  });
  ```

- [ ] Add the expand-a-turn test:

  ```ts
  test('expanding a turn reveals its tool calls', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    await node(page, 'at').locator('.graph-node-toggle').click();
    await expect(node(page, 't1')).toBeVisible();
    await expect(node(page, 't2')).toBeVisible();
  });
  ```

- [ ] Add the expand-a-subagent test:

  ```ts
  test('expanding a subagent reveals its subtree', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    await node(page, 'sub').locator('.graph-node-toggle').click();
    await expect(node(page, 'subc')).toBeVisible();
  });
  ```

- [ ] Add the badge-text test (collapsed turn shows `count · tokens · cost`):

  ```ts
  test('collapsed turn shows an aggregate badge', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    const badge = node(page, 'at').locator('.graph-node-badge-stat');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('2 ·');
  });
  ```

- [ ] Add the toggle-does-not-select test (clicking the toggle expands but does not open the drawer):

  ```ts
  test('toggle is separate from body-click selection', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    await node(page, 'at').locator('.graph-node-toggle').click();
    await expect(page).toHaveURL(new RegExp(`#/s/${hash}$`));
    await node(page, 'at').click();
    await expect(page).toHaveURL(new RegExp('/n/at$'));
  });
  ```

- [ ] Add the collapse-all / expand-all test:

  ```ts
  test('collapse all hides everything below the roots; expand all reveals leaves', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    await page.getByRole('button', { name: 'Expand all' }).click();
    await expect(node(page, 't1')).toBeVisible();
    await expect(node(page, 'subc')).toBeVisible();
    await page.getByRole('button', { name: 'Collapse all' }).click();
    await expect(node(page, 's')).toBeVisible();
    await expect(node(page, 'u')).toHaveCount(0);
  });
  ```

- [ ] Add the spine-edge-survives test (a collapsed subagent still has its edge to the parent):

  ```ts
  test('a collapsed subagent keeps its spine edge', async ({ page }) => {
    await page.goto(`/#/s/${hash}`);
    await expect(node(page, 'sub')).toBeVisible();
    await expect(page.locator('.svelte-flow__edge')).not.toHaveCount(0);
  });
  ```

- [ ] Add the live-SSE-under-collapsed-parent test. Re-route the event stream to append a late tool_call under the collapsed `at`, reload, and assert the new node never appears while the badge count climbs:

  ```ts
  test('a live node under a collapsed parent bumps the badge without appearing', async ({ page }) => {
    const withLate: SseEvent[] = [
      ...base,
      { kind: 'node_upsert', rev: 14, node: { id: 't3', run_id: 'run1', type: 'tool_call', name: 'Late', status: 'ok', tokens_in: 100, tokens_out: 100, rev: 14 } },
      { kind: 'edge_upsert', rev: 15, edge: { id: 'e7', run_id: 'run1', type: 'parent_child', src: 'at', dst: 't3', rev: 15 } },
    ];
    await page.route('/v1/events**', (route) =>
      route.fulfill({ status: 200, contentType: 'text/event-stream', body: body(withLate) }),
    );
    await page.route(`/v1/sessions/${hash}/graph`, (route) =>
      route.fulfill({ status: 200, contentType: 'text/event-stream', body: body(withLate) }),
    );
    await page.goto(`/#/s/${hash}`);
    await expect(node(page, 'at')).toBeVisible();
    await expect(node(page, 't3')).toHaveCount(0);
    await expect(node(page, 'at').locator('.graph-node-badge-stat')).toContainText('3 ·');
  });
  ```

- [ ] Run `cd webui && npm run build` (Playwright's `webServer` previews the built `dist/`), then `cd webui && npx playwright test e2e/graph-collapse.spec.ts` — EXPECT all PASS. If a selector is flaky, prefer `getByRole`/`data-id` over text; do not add arbitrary timeouts.
- [ ] Run the full e2e suite `cd webui && npx playwright test` — EXPECT PASS (the original `graph.spec.ts` still green; its 2-node graph has no `assistant_turn`/`subagent`, so default-collapse leaves it unchanged).
- [ ] Commit:

  ```text
  test(webui): e2e coverage for collapsible graph

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 12: TUI default-collapse parity (`tui/tree_model.go`)

Align `catacomb observe`'s open-state with the web policy. Today the TUI's `expanded map[string]bool` starts empty (everything collapsed) and `treeState.seed` does not pre-expand anything. The web default-collapses only `assistant_turn` and `subagent` (i.e. expands `session`/`user_prompt` and everything that is not a turn/subagent group). Express the same predicate in Go and seed `expanded` for every node whose subtree should open by default.

**Files**

- Modify: `webui/web/../../tui/tree_model.go` (path: `/Users/karych/src/catacomb/tui/tree_model.go`)
- Test: `webui/web/../../tui/tree_model_test.go` (path: `/Users/karych/src/catacomb/tui/tree_model_test.go`)

**Interfaces**

- Consumes: `Graph` (`g.Nodes map[string]Node`, `g.Edges []Edge`), `Node.Type`, `BuildTree(g) []TreeRow` (already gives `HasKids`).
- Produces: `treeState.seed` returns a `treeState` whose `expanded` map opens session/user_prompt and any non-turn/non-subagent parent by default; `assistant_turn` and `subagent` rows stay collapsed.

**Semantics (mirror of web `DEFAULT_COLLAPSE`)**

- A node is collapsed-by-default iff `Type == "assistant_turn" || Type == "subagent"`.
- `seed` pre-populates `expanded[id] = true` for every node that has children AND is NOT collapsed-by-default. (The TUI uses `expanded`, the inverse of the web's `collapsed`; expanding everything except the two collapsed types yields the same visible spine.)

**Steps**

- [ ] Add a failing test to `/Users/karych/src/catacomb/tui/tree_model_test.go`. Follow the existing test style in that file (no comments). Construct a graph via `SseEvent`s or the existing helpers, seed, and assert `Flatten` visibility:

  ```go
  func TestSeedDefaultExpandsSpineNotTurns(t *testing.T) {
  	ts := newTreeState()
  	evs := []SseEvent{
  		nodeUpsert("s", "session"),
  		nodeUpsert("u", "user_prompt"),
  		nodeUpsert("at", "assistant_turn"),
  		nodeUpsert("t1", "tool_call"),
  		nodeUpsert("sub", "subagent"),
  		nodeUpsert("subc", "tool_call"),
  		edgeUpsert("e1", "parent_child", "s", "u"),
  		edgeUpsert("e2", "parent_child", "u", "at"),
  		edgeUpsert("e3", "parent_child", "at", "t1"),
  		edgeUpsert("e4", "parent_child", "u", "sub"),
  		edgeUpsert("e5", "parent_child", "sub", "subc"),
  	}
  	ts = ts.seed(evs)
  	rows := Flatten(ts.graph, ts.expanded)
  	visible := map[string]bool{}
  	for _, r := range rows {
  		visible[r.Node.ID] = true
  	}
  	if !visible["s"] || !visible["u"] || !visible["at"] || !visible["sub"] {
  		t.Fatalf("spine not visible: %v", visible)
  	}
  	if visible["t1"] {
  		t.Fatalf("assistant_turn child should be collapsed by default")
  	}
  	if visible["subc"] {
  		t.Fatalf("subagent child should be collapsed by default")
  	}
  }
  ```

  If `nodeUpsert`/`edgeUpsert`/`SseEvent` constructors differ in that package, reuse whatever the existing `tree_model_test.go` / `reducer_test.go` already use (read those files first and copy the exact helper names — do not invent new ones).

- [ ] Run `cd /Users/karych/src/catacomb && go test ./tui/ -run TestSeedDefaultExpandsSpineNotTurns` — EXPECT FAIL (`t1`/`subc` currently visible only if a parent is expanded; with empty `expanded` the spine itself collapses, so this fails the spine assertions first — confirming the seed does nothing today).
- [ ] Implement the seed policy in `/Users/karych/src/catacomb/tui/tree_model.go`. Add a predicate and pre-expand in `seed`:

  ```go
  func collapsedByDefault(t string) bool {
  	return t == "assistant_turn" || t == "subagent"
  }
  ```

  and inside `seed`, after `ts.graph = g`, before `return ts`:

  ```go
  ts.expanded = make(map[string]bool)
  for _, row := range BuildTree(g) {
  	if row.HasKids && !collapsedByDefault(row.Node.Type) {
  		ts.expanded[row.Node.ID] = true
  	}
  }
  ```

- [ ] Run `cd /Users/karych/src/catacomb && go test ./tui/ -run TestSeedDefaultExpandsSpineNotTurns` — EXPECT PASS.
- [ ] Run `cd /Users/karych/src/catacomb && make cover` — EXPECT PASS at 100% (the new branch is exercised by the test; if `collapsedByDefault`'s `false` path is uncovered, the fixture's `tool_call` parent `sub`'s children already cover both branches — if not, add an assertion). Run `golangci-lint run ./tui/...` — EXPECT 0 issues, and confirm `internal/codepolicy` passes (no comments were added).
- [ ] Commit:

  ```text
  feat(tui): align observe default-expand with web collapse policy

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

---

### Task 13: Rebuild dist, full verification, branch + PR

Bring the committed build artifact in sync and run every gate the spec and CI require, then open the PR.

**Files**

- Modify: `webui/dist/**` (regenerated build output — committed)

**Steps**

- [ ] Ensure work is on a feature branch off `master` (create it at the start of execution if not already): `cd /Users/karych/src/catacomb && git checkout -b w2-collapsible-graph` (skip if already on it).
- [ ] Run the full frontend gate: `cd webui && npm run typecheck && npm run test` — EXPECT PASS, every `web/src/lib/graph/*` file plus `layout.ts` and `graph-nav.ts` at 100% per-file.
- [ ] Rebuild the committed artifact: `cd webui && npm run build` — EXPECT success, `webui/dist/` regenerated.
- [ ] Verify dist sync exactly as CI does: `cd webui && npm run check:dist` — EXPECT PASS (no diff in `../dist`). If it reports a diff, `git add ../dist` the rebuilt output.
- [ ] Run the full e2e suite: `cd webui && npx playwright test` — EXPECT all green.
- [ ] Run the Go gate (TUI parity task touched Go): `cd /Users/karych/src/catacomb && make cover` — EXPECT 100%; `golangci-lint run` — EXPECT 0 issues.
- [ ] Commit the rebuilt dist if not already committed:

  ```text
  chore(webui): rebuild dist for collapsible graph

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

- [ ] Push and open the PR: `cd /Users/karych/src/catacomb && git push -u origin w2-collapsible-graph && gh pr create --base master --title "W2: collapsible graph" --body "Implements Workstream 2 (collapsible graph) from docs/superpowers/specs/2026-06-25-graph-collapse-and-inspection-design.md."` — then wait for GitHub Actions and confirm green across ubuntu/macos/windows.

---

## Exit criteria

- [ ] vitest reports 100% per-file coverage for `web/src/lib/graph/types.ts`, `hierarchy.ts`, `collapse.ts`, `aggregate.ts`, `lift-edges.ts`, `badge.ts`, and the extended `layout.ts` + `graph-nav.ts`.
- [ ] `npm run typecheck` passes with no errors.
- [ ] `npm run build` succeeds and `npm run check:dist` shows the committed `webui/dist/` is in sync.
- [ ] Playwright e2e (`graph.spec.ts` + new `graph-collapse.spec.ts`) all green.
- [ ] Default view of the multi-turn + subagent fixture shows the spine (session → prompt → collapsed turn + collapsed subagent), not all nodes.
- [ ] Expanding a turn reveals its tools; expanding a subagent reveals its subtree; collapse-all/expand-all work; the aggregate badge shows `count · tokens · cost` with status color; a collapsed node keeps its spine edge; a live SSE node under a collapsed parent bumps the badge without appearing.
- [ ] Arrow nav skips hidden nodes; `Enter`/`Space` on a focused collapsible node toggles it; focus is retained across re-layout; toggling does not teleport the view (anchored re-layout) and honors `prefers-reduced-motion`.
- [ ] Go: `make cover` 100%, `golangci-lint` 0 issues, `internal/codepolicy` clean; `catacomb observe` opens at the same level of detail as the web (spine expanded, turns/subagents collapsed).
- [ ] Branch off `master` → PR → a fresh GitHub Actions run green on ubuntu, macos, and windows. No new coverage or lint exclusions introduced.
