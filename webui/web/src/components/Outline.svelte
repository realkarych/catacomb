<script lang="ts">
  import { untrack } from 'svelte';
  import { sessionGraph, selectedNodeId, navigateToNode, filterState, sessionsById, handleEvent } from '../lib/stores/stores.svelte';
  import { buildHierarchy } from '../lib/graph/hierarchy';
  import { flattenOutline, defaultOutlineCollapsed, outlineLabel, isSystemPrompt } from '../lib/graph/outline';
  import { shouldShowStatus, isSessionLive } from '../lib/status';
  import type { OutlineRow } from '../lib/graph/outline';
  import type { Node } from '../lib/types';
  import { toggle as toggleCollapse, collapseAll, expandAll } from '../lib/graph/collapse';
  import { rowAggregate, descendantCount, isLazySubagent } from '../lib/graph/aggregate';
  import { rowStatLine } from '../lib/graph/outline-stats';
  import { nodeTypeInfo } from '../lib/node-legend';
  import { filterNodes, isActive } from '../lib/filters';
  import {
    isConversationNode,
    isToolNode,
    conversationText,
    toolKeyArg,
    toolOutputSnippet,
    cleanRedacted,
  } from '../lib/conversation';
  import { fetchNodePayload, fetchSubagentSubtree, NotFoundError, ForbiddenError } from '../lib/api';

  interface Props {
    hash: string;
    token: string;
  }
  let { hash, token }: Props = $props();

  const isLive = $derived(isSessionLive(sessionsById[hash], Date.now()));
  let showSystem = $state(false);

  const ROW_H = 30;
  const OVERSCAN = 8;
  const SNIPPET_MAX = 64;

  const graph = $derived(sessionGraph(hash));
  const byId = $derived(Object.fromEntries(graph.nodes.map((n) => [n.id, n])));
  const hierarchy = $derived(buildHierarchy(graph.nodes, graph.edges));

  let collapsed = $state.raw<Set<string>>(new Set());
  let seen = new Set<string>();
  let userToggled = new Set<string>();
  let loadedAgents = new Set<string>();
  let loadingAgents = $state.raw<Set<string>>(new Set());
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
        loadedAgents = new Set();
        loadingAgents = new Set();
        snippets = {};
        attempted = new Set();
        scrollTop = 0;
        showSystem = false;
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
      if (h.childrenOf(n.id).length === 0 && !isLazySubagent(n)) continue;
      seen.add(n.id);
      if (!userToggled.has(n.id) && !next.has(n.id)) {
        next.add(n.id);
        changed = true;
      }
    }
    if (changed) collapsed = next;
  });

  const systemIds = $derived(
    showSystem ? new Set<string>() : new Set(graph.nodes.filter(isSystemPrompt).map((n) => n.id)),
  );

  const rows = $derived(
    flattenOutline(graph.nodes, hierarchy, collapsed).filter(
      (r) => !systemIds.has(r.id) && !hierarchy.ancestorsOf(r.id).some((a) => systemIds.has(a)),
    ),
  );

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

  function clip(text: string): string {
    return text.length > SNIPPET_MAX ? text.slice(0, SNIPPET_MAX) + '…' : text;
  }

  function conversationSnippet(view: { input?: unknown; output?: unknown }): string {
    const raw = conversationText(view.input ?? view.output);
    const firstLine = raw.split('\n', 1)[0]?.trim() ?? '';
    if (!firstLine) return '';
    return clip(cleanRedacted(firstLine));
  }

  function toolSnippet(view: { input?: unknown; output?: unknown }): string {
    const keyArg = cleanRedacted(toolKeyArg(view.input));
    const out = cleanRedacted(toolOutputSnippet(view.output));
    const combined = keyArg && out ? `${keyArg} → ${out}` : keyArg || out;
    if (!combined) return '';
    return clip(combined);
  }

  async function loadSnippet(id: string, isTool: boolean, currentHash: string, currentToken: string) {
    attempted.add(id);
    try {
      const view = await fetchNodePayload(currentHash, id, currentToken);
      if (hash !== currentHash) return;
      const snippet = isTool ? toolSnippet(view) : conversationSnippet(view);
      if (!snippet) return;
      untrack(() => {
        snippets = { ...snippets, [id]: snippet };
      });
    } catch (err) {
      if (err instanceof NotFoundError || err instanceof ForbiddenError) return;
    }
  }

  $effect(() => {
    const vis = visibleRows;
    const currentHash = hash;
    const currentToken = token;
    untrack(() => {
      for (const row of vis) {
        const tool = isToolNode(row.node.type);
        if (!isConversationNode(row.node.type) && !tool) continue;
        if (!row.node.payload_hash) continue;
        if (attempted.has(row.id)) continue;
        void loadSnippet(row.id, tool, currentHash, currentToken);
      }
    });
  });

  function isExpandable(row: OutlineRow): boolean {
    return row.hasChildren || isLazySubagent(row.node);
  }

  function isAgentLoading(node: Node): boolean {
    return node.type === 'subagent' && !!node.agent_id && loadingAgents.has(node.agent_id);
  }

  function needsLoad(node: Node): boolean {
    return (
      node.type === 'subagent' &&
      !!node.agent_id &&
      !loadedAgents.has(node.agent_id) &&
      descendantCount(node) > 0
    );
  }

  async function expandWithLoad(node: Node, currentHash: string, currentToken: string) {
    const agentId = node.agent_id;
    if (!agentId || loadingAgents.has(agentId)) return;
    loadingAgents = new Set(loadingAgents).add(agentId);
    try {
      const events = await fetchSubagentSubtree(currentHash, agentId, currentToken);
      for (const ev of events) handleEvent(ev);
      loadedAgents.add(agentId);
      if (collapsed.has(node.id)) {
        const next = new Set(collapsed);
        next.delete(node.id);
        collapsed = next;
      }
    } catch {
      // Leave the agent collapsed and unmarked so the next expand retries.
    } finally {
      const next = new Set(loadingAgents);
      next.delete(agentId);
      loadingAgents = next;
    }
  }

  function expandRow(id: string, node: Node) {
    userToggled.add(id);
    seen.add(id);
    if (needsLoad(node)) {
      void expandWithLoad(node, hash, token);
    } else {
      collapsed = toggleCollapse(collapsed, id);
    }
  }

  function handleToggle(id: string, e: Event) {
    e.stopPropagation();
    const node = byId[id];
    if (collapsed.has(id) && node) {
      expandRow(id, node);
      return;
    }
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
      if (row && isExpandable(row) && row.collapsed) {
        e.preventDefault();
        expandRow(row.id, row.node);
      } else if (row && isExpandable(row) && !row.collapsed) {
        e.preventDefault();
        moveSelection(1);
      }
    } else if (e.key === 'ArrowLeft') {
      const idx = selectedIndex();
      const row = idx >= 0 ? rows[idx] : undefined;
      if (!row) return;
      if (isExpandable(row) && !row.collapsed) {
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

  function rowStats(row: OutlineRow): { text: string; title: string; color: string } {
    const expandable = isExpandable(row);
    const aggregate =
      row.collapsed && expandable ? rowAggregate(row.node, hierarchy, byId) : undefined;
    return rowStatLine(row.node, { collapsed: row.collapsed, hasChildren: expandable, aggregate });
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
      <button
        class="outline-toolbar-btn"
        type="button"
        aria-pressed={showSystem}
        onclick={() => (showSystem = !showSystem)}
      >Show system</button>
    </div>
    <div class="outline-legend" aria-label="Stat legend">
      <span class="outline-legend-item"><span class="outline-legend-key">assistant</span> in · out · cost · duration</span>
      <span class="outline-legend-item"><span class="outline-legend-key">tool</span> arg → output · duration</span>
      <span class="outline-legend-item"><span class="outline-legend-key">collapsed</span> N nodes · in · out · cost · duration</span>
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
          {@const snippet = isConversationNode(row.node.type) || isToolNode(row.node.type) ? snippets[row.id] : undefined}
          {@const expandable = isExpandable(row)}
          {@const loading = isAgentLoading(row.node)}
          <div
            class="outline-row"
            role="treeitem"
            aria-level={row.depth + 1}
            aria-expanded={expandable ? !row.collapsed : undefined}
            aria-selected={isSelected ? 'true' : undefined}
            data-selected={isSelected ? 'true' : undefined}
            data-node-id={row.id}
            data-filtered-out={isFilteredOut ? 'true' : undefined}
            style="position: absolute; top: {(startIndex + i) * ROW_H}px; height: {ROW_H}px; padding-left: {row.depth * 16 + 8}px;{isFilteredOut ? ' opacity: 0.4;' : ''}"
            tabindex="-1"
            onclick={() => navigateToNode(hash, row.id)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); navigateToNode(hash, row.id); } }}
          >
            {#if expandable}
              <button
                class="outline-chevron"
                type="button"
                aria-label={row.collapsed ? 'Expand' : 'Collapse'}
                aria-busy={loading ? 'true' : undefined}
                onclick={(e) => handleToggle(row.id, e)}
              >
                {#if loading}
                  <span class="outline-chevron-spinner" aria-hidden="true"></span>
                {:else}
                  <svg
                    class="outline-chevron-icon"
                    class:outline-chevron-icon--open={!row.collapsed}
                    viewBox="0 0 24 24"
                    width="13"
                    height="13"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2.5"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    aria-hidden="true"
                  ><path d="M9 6l6 6-6 6" /></svg>
                {/if}
              </button>
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
              <span class="outline-stat-text" title={stats.title || undefined}>{stats.text}</span>
              {#if shouldShowStatus(row.node.status, isLive)}
                <span class="outline-dot" style="background: {stats.color};" aria-hidden="true"></span>
              {/if}
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

  .outline-legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--s1) var(--s4);
    padding: var(--s1) var(--s4);
    border-bottom: 1px solid var(--border);
    font-size: var(--text-xs);
    font-family: var(--font-mono);
    color: var(--text-faint);
    flex-shrink: 0;
  }

  .outline-legend-item {
    white-space: nowrap;
  }

  .outline-legend-key {
    color: var(--text-dim);
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
    width: 18px;
    height: 18px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
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

  .outline-chevron-icon {
    display: block;
    transition: transform 120ms ease;
  }

  .outline-chevron-icon--open {
    transform: rotate(90deg);
  }

  .outline-chevron-spinner {
    width: 11px;
    height: 11px;
    border: 1.5px solid currentColor;
    border-top-color: transparent;
    border-radius: 50%;
    animation: outline-chevron-spin 0.6s linear infinite;
  }

  @keyframes outline-chevron-spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .outline-chevron-icon {
      transition: none;
    }

    .outline-chevron-spinner {
      animation: none;
    }
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
