<script lang="ts">
  import { untrack } from 'svelte';
  import { sessionGraph, selectedNodeId, navigateToNode, filterState } from '../lib/stores/stores.svelte';
  import { buildHierarchy } from '../lib/graph/hierarchy';
  import { flattenOutline, defaultOutlineCollapsed, outlineLabel } from '../lib/graph/outline';
  import type { OutlineRow } from '../lib/graph/outline';
  import { toggle as toggleCollapse, collapseAll, expandAll } from '../lib/graph/collapse';
  import { aggregateOf } from '../lib/graph/aggregate';
  import { badgeStatLine, badgeStatusColor } from '../lib/graph/badge';
  import { nodeTypeInfo } from '../lib/node-legend';
  import { filterNodes, isActive } from '../lib/filters';
  import { formatTokens, formatCost, formatDuration } from '../lib/format/format';
  import { isConversationNode, conversationText } from '../lib/conversation';
  import { fetchNodePayload } from '../lib/api';

  interface Props {
    hash: string;
    token: string;
  }
  let { hash, token }: Props = $props();

  const ROW_H = 30;
  const OVERSCAN = 8;
  const SNIPPET_MAX = 64;

  const graph = $derived(sessionGraph(hash));
  const byId = $derived(Object.fromEntries(graph.nodes.map((n) => [n.id, n])));
  const hierarchy = $derived(buildHierarchy(graph.nodes, graph.edges));

  let collapsed = $state.raw<Set<string>>(new Set());
  let seen = new Set<string>();
  let userToggled = new Set<string>();
  let prevHash: string | null = null;

  $effect(() => {
    const h0 = hash;
    const g = sessionGraph(h0);
    untrack(() => {
      if (prevHash !== h0) {
        prevHash = h0;
        seen = new Set();
        userToggled = new Set();
        collapsed = new Set();
        snippets = {};
        attempted = new Set();
        scrollTop = 0;
        if (scrollEl) scrollEl.scrollTop = 0;
      }
    });
    if (g.nodes.length === 0) return;
    const h = buildHierarchy(g.nodes, g.edges);
    const rootSet = new Set(h.roots);
    const prev = untrack(() => collapsed);
    const next = new Set(prev);
    let changed = false;
    for (const n of g.nodes) {
      if (seen.has(n.id)) continue;
      if (rootSet.has(n.id)) continue;
      if (h.childrenOf(n.id).length === 0) continue;
      seen.add(n.id);
      if (!userToggled.has(n.id) && !next.has(n.id)) {
        next.add(n.id);
        changed = true;
      }
    }
    if (changed) collapsed = next;
  });

  const rows = $derived(flattenOutline(graph.nodes, hierarchy, collapsed));

  const matching = $derived(
    isActive(filterState) ? new Set(filterNodes(graph.nodes, filterState).map((n) => n.id)) : null,
  );

  let scrollEl: HTMLDivElement | undefined = $state();
  let scrollTop = $state(0);
  let viewportH = $state(0);

  const startIndex = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN));
  const endIndex = $derived(
    Math.min(rows.length, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN),
  );
  const visibleRows = $derived(rows.slice(startIndex, endIndex));
  const totalHeight = $derived(rows.length * ROW_H);

  function onScroll() {
    if (scrollEl) scrollTop = scrollEl.scrollTop;
  }

  $effect(() => {
    const el = scrollEl;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => {
      viewportH = el.clientHeight;
    });
    ro.observe(el);
    viewportH = el.clientHeight;
    return () => ro.disconnect();
  });

  let snippets = $state.raw<Record<string, string>>({});
  let attempted = new Set<string>();

  async function loadSnippet(id: string) {
    attempted.add(id);
    try {
      const view = await fetchNodePayload(hash, id, token);
      const raw = conversationText(view.input ?? view.output);
      const firstLine = raw.split('\n', 1)[0]?.trim() ?? '';
      if (!firstLine) return;
      const snippet = firstLine.length > SNIPPET_MAX ? firstLine.slice(0, SNIPPET_MAX) + '…' : firstLine;
      untrack(() => {
        snippets = { ...snippets, [id]: snippet };
      });
    } catch {
      attempted.add(id);
    }
  }

  $effect(() => {
    const vis = visibleRows;
    untrack(() => {
      for (const row of vis) {
        if (!isConversationNode(row.node.type)) continue;
        if (attempted.has(row.id)) continue;
        void loadSnippet(row.id);
      }
    });
  });

  function handleToggle(id: string, e: Event) {
    e.stopPropagation();
    userToggled.add(id);
    seen.add(id);
    collapsed = toggleCollapse(collapsed, id);
  }

  function handleCollapseAll() {
    for (const n of graph.nodes) {
      if (hierarchy.childrenOf(n.id).length > 0) userToggled.add(n.id);
    }
    collapsed = collapseAll(graph.nodes, hierarchy);
  }

  function handleExpandAll() {
    for (const n of graph.nodes) userToggled.add(n.id);
    collapsed = expandAll();
  }

  function handleReset() {
    userToggled = new Set();
    collapsed = defaultOutlineCollapsed(graph.nodes, hierarchy);
  }

  function prefersReducedMotion(): boolean {
    return (
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    );
  }

  function scrollRowIntoView(index: number) {
    if (!scrollEl) return;
    const behavior: ScrollBehavior = prefersReducedMotion() ? 'auto' : 'smooth';
    const top = index * ROW_H;
    const bottom = top + ROW_H;
    const viewTop = scrollEl.scrollTop;
    const viewBottom = viewTop + scrollEl.clientHeight;
    if (top < viewTop) {
      scrollEl.scrollTo({ top, behavior });
    } else if (bottom > viewBottom) {
      scrollEl.scrollTo({ top: bottom - scrollEl.clientHeight, behavior });
    }
  }

  function selectedIndex(): number {
    const id = selectedNodeId.value;
    if (id === null) return -1;
    return rows.findIndex((r) => r.id === id);
  }

  function moveSelection(delta: number) {
    if (rows.length === 0) return;
    const cur = selectedIndex();
    let next: number;
    if (cur === -1) {
      next = delta > 0 ? 0 : rows.length - 1;
    } else {
      next = Math.min(rows.length - 1, Math.max(0, cur + delta));
    }
    const row = rows[next];
    if (!row) return;
    navigateToNode(hash, row.id);
    scrollRowIntoView(next);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      moveSelection(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      moveSelection(-1);
    } else if (e.key === 'ArrowRight') {
      const idx = selectedIndex();
      const row = idx >= 0 ? rows[idx] : undefined;
      if (row && row.hasChildren && row.collapsed) {
        e.preventDefault();
        userToggled.add(row.id);
        seen.add(row.id);
        collapsed = toggleCollapse(collapsed, row.id);
      } else if (row && row.hasChildren && !row.collapsed) {
        e.preventDefault();
        moveSelection(1);
      }
    } else if (e.key === 'ArrowLeft') {
      const idx = selectedIndex();
      const row = idx >= 0 ? rows[idx] : undefined;
      if (!row) return;
      if (row.hasChildren && !row.collapsed) {
        e.preventDefault();
        userToggled.add(row.id);
        seen.add(row.id);
        collapsed = toggleCollapse(collapsed, row.id);
      } else {
        const parent = hierarchy.parentOf(row.id);
        if (parent !== undefined) {
          e.preventDefault();
          const pIdx = rows.findIndex((r) => r.id === parent);
          if (pIdx >= 0) {
            navigateToNode(hash, parent);
            scrollRowIntoView(pIdx);
          }
        }
      }
    } else if (e.key === 'Enter') {
      const idx = selectedIndex();
      const row = idx >= 0 ? rows[idx] : undefined;
      if (row) {
        e.preventDefault();
        navigateToNode(hash, row.id);
      }
    }
  }

  function rowStats(row: OutlineRow): { text: string; color: string } {
    if (row.collapsed && row.hasChildren) {
      const agg = aggregateOf(row.id, hierarchy, byId);
      return { text: badgeStatLine(agg), color: badgeStatusColor(agg.status) };
    }
    const n = row.node;
    const text = `${formatTokens(n.tokens_in)}→${formatTokens(n.tokens_out)} · ${formatCost(n.cost_usd)} · ${formatDuration(n.duration_ms)}`;
    return { text, color: badgeStatusColor(n.status === 'error' ? 'error' : n.status === 'running' ? 'running' : 'ok') };
  }
</script>

{#if rows.length === 0}
  <div class="outline-empty">
    <div class="outline-empty-icon" aria-hidden="true">⛏</div>
    <p class="outline-empty-headline">Waiting for events…</p>
    <p class="outline-empty-hint">No nodes yet for this session</p>
  </div>
{:else}
  <div class="outline-root">
    <div class="outline-toolbar" role="toolbar" aria-label="Outline controls">
      <button class="outline-toolbar-btn" type="button" onclick={handleCollapseAll}>Collapse all</button>
      <button class="outline-toolbar-btn" type="button" onclick={handleExpandAll}>Expand all</button>
      <button class="outline-toolbar-btn" type="button" onclick={handleReset}>Reset</button>
    </div>
    <div
      bind:this={scrollEl}
      class="outline-scroll"
      role="tree"
      aria-label="Session outline"
      tabindex="0"
      onscroll={onScroll}
      onkeydown={onKeydown}
    >
      <div class="outline-spacer" style="height: {totalHeight}px;">
        {#each visibleRows as row, i (row.id)}
          {@const info = nodeTypeInfo(row.node.type)}
          {@const label = outlineLabel(row.node)}
          {@const stats = rowStats(row)}
          {@const isSelected = selectedNodeId.value === row.id}
          {@const isFilteredOut = matching !== null && !matching.has(row.id)}
          {@const snippet = isConversationNode(row.node.type) ? snippets[row.id] : undefined}
          <div
            class="outline-row"
            role="treeitem"
            aria-level={row.depth + 1}
            aria-expanded={row.hasChildren ? !row.collapsed : undefined}
            aria-selected={isSelected ? 'true' : undefined}
            data-selected={isSelected ? 'true' : undefined}
            data-filtered-out={isFilteredOut ? 'true' : undefined}
            style="position: absolute; top: {(startIndex + i) * ROW_H}px; height: {ROW_H}px; padding-left: {row.depth * 16 + 8}px;{isFilteredOut ? ' opacity: 0.4;' : ''}"
            tabindex="-1"
            onclick={() => navigateToNode(hash, row.id)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); navigateToNode(hash, row.id); } }}
          >
            {#if row.hasChildren}
              <button
                class="outline-chevron"
                type="button"
                aria-label={row.collapsed ? 'Expand' : 'Collapse'}
                onclick={(e) => handleToggle(row.id, e)}
              >{row.collapsed ? '▸' : '▾'}</button>
            {:else}
              <span class="outline-chevron outline-chevron--empty" aria-hidden="true"></span>
            {/if}
            <span class="outline-glyph" style="background: var({info.token});" aria-hidden="true"></span>
            <span class="outline-label">
              <span class="outline-primary">{label.primary}</span>
              {#if label.secondary}<span class="outline-secondary">{label.secondary}</span>{/if}
              {#if snippet}<span class="outline-snippet">{snippet}</span>{/if}
            </span>
            <span class="outline-stats">
              <span class="outline-stat-text">{stats.text}</span>
              <span class="outline-dot" style="background: {stats.color};" aria-hidden="true"></span>
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .outline-root {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .outline-toolbar {
    display: flex;
    gap: var(--s2);
    padding: var(--s2) var(--s4);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .outline-toolbar-btn {
    font-size: var(--text-xs);
    font-family: var(--font-ui);
    color: var(--text-dim);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--s1) var(--s2);
    cursor: pointer;
  }

  .outline-toolbar-btn:hover {
    color: var(--text);
    border-color: var(--accent);
  }

  .outline-toolbar-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .outline-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    position: relative;
  }

  .outline-scroll:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: -2px;
  }

  .outline-spacer {
    position: relative;
    width: 100%;
  }

  .outline-row {
    display: flex;
    align-items: center;
    gap: var(--s2);
    width: 100%;
    padding-right: var(--s4);
    background: transparent;
    border: none;
    border-left: 2px solid transparent;
    cursor: pointer;
    text-align: left;
    box-sizing: border-box;
  }

  .outline-row:hover {
    background: var(--surface-2);
  }

  .outline-row[data-selected='true'] {
    background: var(--surface-2);
    border-left-color: var(--accent);
  }

  .outline-chevron {
    flex-shrink: 0;
    width: 16px;
    height: 16px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: var(--text-xs);
    color: var(--text-faint);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    padding: 0;
  }

  .outline-chevron:hover {
    color: var(--text);
  }

  .outline-chevron:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 1px;
  }

  .outline-chevron--empty {
    cursor: default;
  }

  .outline-glyph {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 2px;
  }

  .outline-label {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: var(--s2);
    overflow: hidden;
    white-space: nowrap;
  }

  .outline-primary {
    font-size: var(--text-sm);
    color: var(--text-dim);
    flex-shrink: 0;
  }

  .outline-row[data-selected='true'] .outline-primary {
    color: var(--text);
  }

  .outline-secondary {
    font-size: var(--text-xs);
    color: var(--text-faint);
    flex-shrink: 0;
  }

  .outline-snippet {
    font-size: var(--text-xs);
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .outline-stats {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: var(--s2);
  }

  .outline-stat-text {
    font-size: var(--text-xs);
    color: var(--text-faint);
    font-family: var(--font-mono);
    white-space: nowrap;
  }

  .outline-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }
</style>
