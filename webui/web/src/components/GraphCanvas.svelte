<script lang="ts">
  import '@xyflow/svelte/dist/style.css';
  import { untrack } from 'svelte';
  import { SvelteFlow } from '@xyflow/svelte';
  import type { Node as XyFlowNode, Edge as XyFlowEdge, NodeTypes } from '@xyflow/svelte';
  import { sessionGraph, selectNode } from '../lib/stores/stores.svelte';
  import { applyLayout } from '../lib/layout';
  import type { XyNode } from '../lib/layout';
  import GraphNode from './GraphNode.svelte';
  import FlowInternals from './FlowInternals.svelte';

  interface Props {
    hash: string;
  }

  let { hash }: Props = $props();

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
      xyEdges = result.edges as unknown as XyFlowEdge[];
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
    const _hash = hash;
    void _hash;
    pendingFitView = true;
  });

  const isEmpty = $derived(xyNodes.length === 0);
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
      minZoom={0.1}
      maxZoom={2}
      onnodeclick={({ node }) => selectNode(node.id)}
    >
      <FlowInternals
        {pendingFitView}
        onFitViewDone={() => { pendingFitView = false; }}
      />
    </SvelteFlow>
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
    font-family: var(--font-ui) !important;
  }
</style>
