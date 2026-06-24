<script lang="ts">
  import '@xyflow/svelte/dist/style.css';
  import { untrack } from 'svelte';
  import { SvelteFlow } from '@xyflow/svelte';
  import type { Node as XyFlowNode, Edge as XyFlowEdge, NodeTypes } from '@xyflow/svelte';
  import { sessionGraph, selectNode, selectedNodeId, filteredNodeIds } from '../lib/stores/stores.svelte';
  import { dimmedEdgeIds } from '../lib/filters';
  import { applyLayout } from '../lib/layout';
  import type { XyNode } from '../lib/layout';
  import GraphNode from './GraphNode.svelte';
  import FlowInternals from './FlowInternals.svelte';
  import NodeLegend from './NodeLegend.svelte';

  interface Props {
    hash: string;
    refit?: number;
  }

  let { hash, refit = 0 }: Props = $props();

  const nodeTypes: NodeTypes = { default: GraphNode as never };

  let xyNodes = $state.raw<XyFlowNode[]>([]);
  let xyEdges = $state.raw<XyFlowEdge[]>([]);
  let prevTopologyKey = '';
  let pendingFitView = $state(false);

  function toTopologyKey(nodes: { id: string }[], edges: { id: string }[]): string {
    return JSON.stringify([
      [...nodes.map((n) => n.id)].sort(),
      [...edges.map((e) => e.id)].sort(),
    ]);
  }

  $effect(() => {
    const graph = sessionGraph(hash);
    const topologyKey = toTopologyKey(graph.nodes, graph.edges);

    if (topologyKey !== prevTopologyKey) {
      prevTopologyKey = topologyKey;
      const result = applyLayout(graph.nodes, graph.edges);
      xyNodes = result.nodes.map((n) => ({ ...n, type: 'default' })) as unknown as XyFlowNode[];
      const matchingIds = untrack(() => filteredNodeIds.value);
      const dimmed = matchingIds ? dimmedEdgeIds(graph.edges, matchingIds) : new Set<string>();
      xyEdges = result.edges.map((e) => ({
        ...e,
        className: dimmed.has(e.id) ? 'edge--dimmed' : undefined,
      })) as unknown as XyFlowEdge[];
      pendingFitView = true;
    } else if (graph.nodes.length > 0) {
      // Data-only update (positions unchanged): refresh each node's catNode
      // payload in place. Read the current xyNodes via untrack so this effect
      // does NOT depend on the array it also writes — otherwise the
      // unconditional new-array identity from a write would re-trigger the
      // effect forever (effect_update_depth_exceeded). We additionally skip the
      // reassignment entirely when nothing changed, keeping the effect
      // idempotent and avoiding spurious Svelte Flow re-renders.
      const nodeMap = new Map(graph.nodes.map((n) => [n.id, n]));
      const current = untrack(() => xyNodes);
      let changed = false;
      const next = current.map((xyN) => {
        const catNode = nodeMap.get(xyN.id);
        if (!catNode) return xyN;
        const currentData = (xyN.data as XyNode['data'] | undefined)?.catNode;
        if (currentData === catNode) return xyN;
        changed = true;
        return { ...xyN, data: { catNode } };
      });
      if (changed) {
        xyNodes = next;
      }
    }
  });

  $effect(() => {
    const matchingIds = filteredNodeIds.value;
    const currentEdges = untrack(() => xyEdges);
    if (currentEdges.length === 0) return;
    const graph = untrack(() => sessionGraph(hash));
    const dimmed = matchingIds ? dimmedEdgeIds(graph.edges, matchingIds) : new Set<string>();
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
    pendingFitView = true;
  });

  $effect(() => {
    const _refit = refit;
    void _refit;
    pendingFitView = true;
  });

  const isEmpty = $derived(xyNodes.length === 0);
  const presentTypes = $derived(
    [...new Set(xyNodes.map((n) => ((n.data as { catNode?: { type?: string } } | undefined)?.catNode?.type ?? 'marker')))]
  );
</script>

<div class="graph-canvas-root">
  {#if isEmpty}
    <div class="empty-state">
      <div class="empty-state-icon" aria-hidden="true">⛏</div>
      <p class="empty-state-headline">Waiting for events…</p>
      <p class="empty-state-hint">No nodes yet for this session</p>
    </div>
  {:else}
    <SvelteFlow
      bind:nodes={xyNodes}
      bind:edges={xyEdges}
      {nodeTypes}
      fitView
      fitViewOptions={{ maxZoom: 1.0 }}
      minZoom={0.1}
      maxZoom={2}
      onnodeclick={({ node }) => selectNode(node.id)}
    >
      <FlowInternals
        {pendingFitView}
        focusNodeId={selectedNodeId.value}
        onFitViewDone={() => { pendingFitView = false; }}
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
</style>
