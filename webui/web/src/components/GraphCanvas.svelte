<script lang="ts">
  import '@xyflow/svelte/dist/style.css';
  import { untrack } from 'svelte';
  import { SvelteFlow } from '@xyflow/svelte';
  import type { Node as XyFlowNode, Edge as XyFlowEdge, NodeTypes } from '@xyflow/svelte';
  import { sessionGraph, navigateToNode, selectedNodeId, filteredNodeIds } from '../lib/stores/stores.svelte';
  import { dimmedEdgeIds } from '../lib/filters';
  import { applyLayout, collapseView, collapseTopologyKey, anchorOffset } from '../lib/layout';
  import type { XyNode } from '../lib/layout';
  import { buildHierarchy } from '../lib/graph/hierarchy';
  import { DEFAULT_COLLAPSE, toggle as toggleCollapse, collapseAll, expandAll } from '../lib/graph/collapse';
  import { aggregateOf } from '../lib/graph/aggregate';
  import GraphNode from './GraphNode.svelte';
  import FlowInternals from './FlowInternals.svelte';
  import NodeLegend from './NodeLegend.svelte';

  interface Props {
    hash: string;
    refit?: number;
    onNodeActivate?: () => void;
    onVisibleChange?: (ids: Set<string>) => void;
  }

  let { hash, refit = 0, onNodeActivate, onVisibleChange }: Props = $props();

  const nodeTypes: NodeTypes = { default: GraphNode as never };

  let xyNodes = $state.raw<XyFlowNode[]>([]);
  let xyEdges = $state.raw<XyFlowEdge[]>([]);
  let prevTopologyKey = '';
  let pendingFitView = $state(true);
  let pendingRestoreViewport = $state(false);

  let collapsed = $state.raw<Set<string>>(new Set());
  let seen = new Set<string>();
  let userToggled = new Set<string>();
  let showOrphans = $state(false);
  let anchorId: string | null = null;
  let preserveViewportOnNext = false;

  function handleToggle(id: string) {
    anchorId = id;
    userToggled.add(id);
    seen.add(id);
    collapsed = toggleCollapse(collapsed, id);
  }

  function handleCollapseAll() {
    const graph = sessionGraph(hash);
    const h = buildHierarchy(graph.nodes, graph.edges);
    anchorId = selectedNodeId.value;
    if (!anchorId) preserveViewportOnNext = true;
    collapsed = collapseAll(graph.nodes, h);
  }

  function handleExpandAll() {
    anchorId = selectedNodeId.value;
    if (!anchorId) preserveViewportOnNext = true;
    collapsed = expandAll();
  }

  $effect(() => {
    const graph = sessionGraph(hash);

    if (graph.nodes.length > 0) {
      const h = buildHierarchy(graph.nodes, graph.edges);
      const prev = untrack(() => collapsed);
      const next = new Set(prev);
      let changed = false;
      for (const n of graph.nodes) {
        if (seen.has(n.id)) continue;
        if (!DEFAULT_COLLAPSE(n)) {
          seen.add(n.id);
          continue;
        }
        if (h.childrenOf(n.id).length === 0) continue;
        seen.add(n.id);
        if (!userToggled.has(n.id) && !next.has(n.id)) {
          next.add(n.id);
          changed = true;
        }
      }
      if (changed) collapsed = next;
    }

    const topologyKey = collapseTopologyKey(graph.nodes, graph.edges, collapsed) + (showOrphans ? ':o' : '');

    if (topologyKey !== prevTopologyKey) {
      prevTopologyKey = topologyKey;
      const view = collapseView(graph.nodes, graph.edges, collapsed);
      const byId = Object.fromEntries(graph.nodes.map((n) => [n.id, n]));

      const hier = view.hierarchy;
      const orphanSet = new Set(hier.orphans);
      const shownNodes = showOrphans ? view.nodes : view.nodes.filter((n) => !orphanSet.has(n.id));
      const shownEdges = showOrphans ? view.edges : view.edges.filter((e) => !orphanSet.has(e.src) && !orphanSet.has(e.dst));

      const oldPos = Object.fromEntries(
        (untrack(() => xyNodes)).map((n) => [n.id, { x: n.position.x, y: n.position.y }]),
      );

      const result = applyLayout(shownNodes, shownEdges);

      const newPos = Object.fromEntries(result.nodes.map((n) => [n.id, { ...n.position }]));
      const off = anchorOffset(anchorId, oldPos, newPos);

      xyNodes = result.nodes.map((n) => {
        const id = n.id;
        const collapsible = view.hierarchy.childrenOf(id).length > 0;
        const isCollapsed = collapsed.has(id);
        return {
          ...n,
          position: { x: n.position.x + off.dx, y: n.position.y + off.dy },
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
      const dimmed = matchingIds ? dimmedEdgeIds(shownEdges, matchingIds) : new Set<string>();
      xyEdges = result.edges.map((e) => ({
        ...e,
        className: dimmed.has(e.id) ? 'edge--dimmed' : undefined,
      })) as unknown as XyFlowEdge[];
      if (anchorId) { pendingFitView = false; anchorId = null; } else if (preserveViewportOnNext) { preserveViewportOnNext = false; pendingFitView = false; pendingRestoreViewport = true; } else { pendingFitView = true; }
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

  $effect(() => {
    const matchingIds = filteredNodeIds.value;
    const currentEdges = untrack(() => xyEdges);
    if (currentEdges.length === 0) return;
    const graph = untrack(() => sessionGraph(hash));
    const view = collapseView(graph.nodes, graph.edges, untrack(() => collapsed));
    const hier = view.hierarchy;
    const orphanSet = new Set(hier.orphans);
    const shownEdges = untrack(() => showOrphans) ? view.edges : view.edges.filter((e) => !orphanSet.has(e.src) && !orphanSet.has(e.dst));
    const dimmed = matchingIds ? dimmedEdgeIds(shownEdges, matchingIds) : new Set<string>();
    const next = currentEdges.map((e) => {
      const shouldDim = dimmed.has(e.id);
      const wasDimmed = (e as { className?: string }).className === 'edge--dimmed';
      if (shouldDim === wasDimmed) return e;
      return { ...e, className: shouldDim ? 'edge--dimmed' : undefined };
    });
    xyEdges = next;
  });

  $effect(() => {
    const _hash = hash;
    void _hash;
    seen = new Set();
    userToggled = new Set();
    collapsed = new Set();
    pendingFitView = true;
  });

  $effect(() => {
    const _refit = refit;
    void _refit;
    pendingFitView = true;
  });

  const visibleIds = $derived(new Set(xyNodes.map((n) => n.id)));

  $effect(() => {
    onVisibleChange?.(visibleIds);
  });

  const isEmpty = $derived(xyNodes.length === 0);
  const presentTypes = $derived(
    [...new Set(xyNodes.map((n) => ((n.data as { catNode?: { type?: string } } | undefined)?.catNode?.type ?? 'marker')))]
  );

  const orphanCount = $derived(
    buildHierarchy(sessionGraph(hash).nodes, sessionGraph(hash).edges).orphans.length,
  );

  let canvasEl: HTMLDivElement | undefined = $state();
</script>

<div
  bind:this={canvasEl}
  class="graph-canvas-root"
  role="application"
  aria-label="Session graph"
  tabindex="-1"
>
  {#if isEmpty}
    <div class="empty-state">
      <div class="empty-state-icon" aria-hidden="true">⛏</div>
      <p class="empty-state-headline">Waiting for events…</p>
      <p class="empty-state-hint">No nodes yet for this session</p>
    </div>
  {:else}
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
    <SvelteFlow
      bind:nodes={xyNodes}
      bind:edges={xyEdges}
      {nodeTypes}
      fitViewOptions={{ maxZoom: 1.0 }}
      minZoom={0.1}
      maxZoom={2}
      onnodeclick={({ node }) => { navigateToNode(hash, node.id); onNodeActivate?.(); }}
    >
      <FlowInternals
        {pendingFitView}
        {pendingRestoreViewport}
        focusNodeId={selectedNodeId.value}
        onFitViewDone={() => { pendingFitView = false; }}
        onRestoreViewportDone={() => { pendingRestoreViewport = false; }}
      />
    </SvelteFlow>
    <NodeLegend types={presentTypes} />
  {/if}
</div>

<style>
  .graph-canvas-root {
    width: 100%;
    height: 100%;
    position: relative;
  }

  :global(.svelte-flow) {
    background: var(--bg) !important;
  }

  :global(.svelte-flow .svelte-flow__edge-path) {
    stroke: var(--border) !important;
    stroke-width: 1.5;
  }

  :global(.svelte-flow .svelte-flow__edge.selected .svelte-flow__edge-path) {
    stroke: var(--accent) !important;
  }

  :global(.svelte-flow .svelte-flow__edge.edge--dimmed) {
    opacity: 0.15;
  }

  :global(.svelte-flow .svelte-flow__controls) {
    background: var(--surface) !important;
    border: 1px solid var(--border) !important;
    border-radius: var(--radius) !important;
  }

  :global(.svelte-flow .svelte-flow__controls-button) {
    background: var(--surface) !important;
    border: none !important;
    color: var(--text-dim) !important;
    fill: var(--text-dim) !important;
  }

  :global(.svelte-flow .svelte-flow__controls-button:hover) {
    background: var(--surface-2) !important;
    color: var(--text) !important;
    fill: var(--text) !important;
  }

  :global(.svelte-flow .svelte-flow__minimap) {
    border: 1px solid var(--border) !important;
    border-radius: var(--radius) !important;
    overflow: hidden;
  }

  :global(.svelte-flow .svelte-flow__attribution) {
    display: none;
  }

  :global(.svelte-flow .svelte-flow__node) {
    background: transparent !important;
    border: none !important;
    padding: 0 !important;
    box-shadow: none !important;
    border-radius: 0 !important;
    width: auto !important;
    color: inherit !important;
    font-family: var(--font-ui) !important;
  }

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
</style>
