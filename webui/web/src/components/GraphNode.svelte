<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte';
  import type { NodeProps } from '@xyflow/svelte';
  import type { Node as CNode } from '../lib/types';
  import { selectedNodeId, selectNode, filteredNodeIds } from '../lib/stores/stores.svelte';
  import { formatTokens, shortHash } from '../lib/format/format';
  import { toHash } from '../lib/router';

  type GraphNodeData = { catNode: CNode };
  type Props = NodeProps<import('@xyflow/svelte').Node<GraphNodeData>>;

  let { id, data }: Props = $props();

  const catNode = $derived(data.catNode);
  // Selection is driven by our own store (kept in sync with the route + drawer),
  // not Svelte Flow's per-node `selected` prop — we never toggle Flow's internal
  // selection, so that prop is always false and would leave every node un-lit
  // (and all dimmed). Reading selectedNodeId here lights exactly the chosen node.
  const isSelected = $derived(selectedNodeId.value === id);
  const hasOtherSelected = $derived(selectedNodeId.value !== null && !isSelected);
  const isFilteredOut = $derived(filteredNodeIds.value !== null && !filteredNodeIds.value.has(id));
  const isDimmed = $derived(hasOtherSelected || isFilteredOut);

  const statusColor = $derived(() => {
    const s = catNode.status;
    if (s === 'ok') return 'var(--ok)';
    if (s === 'running') return 'var(--running)';
    if (s === 'error') return 'var(--error)';
    if (s === 'blocked') return 'var(--blocked)';
    return 'var(--pending)';
  });

  const isRunning = $derived(catNode.status === 'running');

  function handleClick() {
    selectNode(id);
    if (typeof window !== 'undefined') {
      const hash = window.location.hash;
      const sessionMatch = hash.match(/#\/s\/([^/]+)/);
      if (sessionMatch?.[1]) {
        window.location.hash = toHash({ kind: 'session-node', hash: sessionMatch[1], nodeId: id });
      }
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleClick();
    }
  }
</script>

<div
  class="graph-node"
  class:graph-node--selected={isSelected}
  class:graph-node--dimmed={isDimmed}
  style="--node-color: var(--node-{catNode.type}, var(--node-marker));"
  role="button"
  tabindex="0"
  aria-label={`${catNode.type} ${catNode.name ?? catNode.type} — ${catNode.status ?? 'pending'}`}
  aria-current={isSelected ? 'true' : undefined}
  onclick={handleClick}
  onkeydown={handleKeydown}
>
  <Handle type="target" position={Position.Left} class="graph-handle" />

  <div class="graph-node-header">
    <span class="graph-node-name">{catNode.name ?? catNode.type}</span>
    <span
      class="graph-node-status"
      class:graph-node-status--running={isRunning}
      style="background: {statusColor()};"
      aria-label={catNode.status ?? 'pending'}
    ></span>
  </div>

  <div class="graph-node-id mono">{id.startsWith('session:') ? shortHash(id.slice(8) || id) : shortHash(id)}</div>

  {#if catNode.tokens_in !== undefined || catNode.tokens_out !== undefined}
    <div class="graph-node-tokens">
      {formatTokens(catNode.tokens_in)}→{formatTokens(catNode.tokens_out)}
    </div>
  {/if}

  <Handle type="source" position={Position.Right} class="graph-handle" />
</div>

<style>
  .graph-node {
    background: var(--surface);
    border: 1.5px solid var(--node-color);
    border-radius: var(--radius);
    padding: var(--s2) var(--s3);
    min-width: 160px;
    max-width: 200px;
    cursor: pointer;
    user-select: none;
    transition: box-shadow 0.15s ease, opacity 0.15s ease;
    position: relative;
    font-family: var(--font-ui);
  }

  .graph-node:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .graph-node--selected {
    box-shadow: var(--shadow-lamp);
    outline: 1px solid var(--ring);
  }

  .graph-node--dimmed {
    opacity: 0.6;
  }

  .graph-node-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--s2);
  }

  .graph-node-name {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
  }

  .graph-node-status {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .graph-node-status--running {
    animation: status-pulse 1.4s ease-in-out infinite;
  }

  @media (prefers-reduced-motion: reduce) {
    .graph-node-status--running {
      animation: none;
    }
  }

  @keyframes status-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.35; }
  }

  .graph-node-id {
    font-size: var(--text-xs);
    color: var(--text-faint);
    margin-top: 2px;
  }

  .graph-node-tokens {
    font-size: var(--text-xs);
    color: var(--text-dim);
    margin-top: var(--s1);
    font-family: var(--font-mono);
  }

  :global(.graph-handle) {
    background: transparent !important;
    border-color: var(--border) !important;
    width: 8px !important;
    height: 8px !important;
  }

  :global(.graph-handle:hover) {
    background: var(--accent) !important;
  }
</style>
