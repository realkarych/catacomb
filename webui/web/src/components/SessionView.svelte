<script lang="ts">
  import { toHash } from '../lib/router';
  import GraphCanvas from './GraphCanvas.svelte';
  import NodeDrawer from './NodeDrawer.svelte';

  interface Props {
    hash: string;
    nodeId?: string;
  }
  let { hash, nodeId }: Props = $props();

  function goBack() {
    window.location.hash = toHash({ kind: 'list' });
  }
</script>

<div class="session-view">
  <div class="session-header">
    <button class="back-btn" onclick={goBack} aria-label="Back to sessions list">
      ← Sessions
    </button>
    <span class="session-hash mono">{hash}</span>
    {#if nodeId}
      <span class="node-id-label">Node: <span class="mono">{nodeId}</span></span>
    {/if}
  </div>
  <div class="graph-area">
    <GraphCanvas {hash} />
    <NodeDrawer {hash} />
  </div>
</div>

<style>
  .session-view {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .session-header {
    display: flex;
    align-items: center;
    gap: var(--s3);
    padding: var(--s2) var(--s4);
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .back-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--s1);
    padding: var(--s1) var(--s3);
    font-size: var(--text-sm);
    font-family: var(--font-ui);
    color: var(--text-dim);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: color 0.12s, border-color 0.12s;
  }

  .back-btn:hover {
    color: var(--accent);
    border-color: var(--accent);
  }

  .back-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .session-hash {
    font-size: var(--text-sm);
    color: var(--accent);
  }

  .node-id-label {
    font-size: var(--text-xs);
    color: var(--text-faint);
  }

  .graph-area {
    flex: 1;
    min-height: 0;
    position: relative;
    overflow: hidden;
  }
</style>
