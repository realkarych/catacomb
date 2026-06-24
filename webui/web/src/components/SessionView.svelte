<script lang="ts">
  import { toHash } from '../lib/router';
  import GraphCanvas from './GraphCanvas.svelte';
  import NodeDrawer from './NodeDrawer.svelte';
  import Timeline from './Timeline.svelte';
  import { sessionGraph } from '../lib/stores/stores.svelte';
  import { buildTimeline } from '../lib/timeline';

  interface Props {
    hash: string;
    nodeId?: string;
    loadStatus?: string;
    token: string;
  }
  let { hash, nodeId, loadStatus = 'idle', token }: Props = $props();

  let fitKey = $state(0);
  let prevHadNode = false;
  let viewMode: 'graph' | 'timeline' = $state('graph');

  const hasTimingData = $derived(buildTimeline(sessionGraph(hash).nodes).rows.length > 0);

  $effect(() => {
    const hasNode = !!nodeId;
    if (!hasNode && prevHadNode) {
      fitKey += 1;
    }
    prevHadNode = hasNode;
  });

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
    {#if hasTimingData}
      <div class="view-switcher" role="group" aria-label="View mode">
        <button
          class="view-btn"
          data-active={viewMode === 'graph' ? 'true' : undefined}
          onclick={() => (viewMode = 'graph')}
          aria-pressed={viewMode === 'graph'}
        >Graph</button>
        <button
          class="view-btn"
          data-active={viewMode === 'timeline' ? 'true' : undefined}
          onclick={() => (viewMode = 'timeline')}
          aria-pressed={viewMode === 'timeline'}
        >Timeline</button>
      </div>
    {/if}
  </div>
  {#if loadStatus === 'not-found'}
    <div class="not-found-state">
      <div class="not-found-icon" aria-hidden="true">🔍</div>
      <p class="not-found-headline">Session not found</p>
      <p class="not-found-hint">No session with hash <span class="mono">{hash}</span></p>
      <button class="back-link" onclick={goBack}>← Back to sessions</button>
    </div>
  {:else}
    <div class="graph-area">
      <div class="canvas-wrap">
        {#if viewMode === 'timeline'}
          <Timeline {hash} />
        {:else}
          <GraphCanvas {hash} refit={fitKey} />
        {/if}
      </div>
      <NodeDrawer {hash} {token} />
    </div>
  {/if}
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
    display: flex;
    flex-direction: row;
    overflow: hidden;
  }

  .canvas-wrap {
    flex: 1;
    min-width: 0;
    position: relative;
    overflow: hidden;
  }

  .not-found-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--s3);
    color: var(--text-faint);
  }

  .not-found-icon {
    font-size: 2rem;
  }

  .not-found-headline {
    font-size: var(--text-base);
    font-weight: 500;
    color: var(--text-dim);
    margin: 0;
  }

  .not-found-hint {
    font-size: var(--text-sm);
    margin: 0;
  }

  .back-link {
    font-size: var(--text-sm);
    font-family: var(--font-ui);
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    padding: var(--s1) var(--s3);
    cursor: pointer;
    transition: opacity 0.12s;
  }

  .back-link:hover {
    opacity: 0.8;
  }

  .back-link:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .view-switcher {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    margin-left: auto;
  }

  .view-btn {
    padding: var(--s1) var(--s3);
    font-size: var(--text-sm);
    font-family: var(--font-ui);
    color: var(--text-dim);
    background: transparent;
    border: none;
    cursor: pointer;
    transition: background 0.1s, color 0.1s;
  }

  .view-btn:hover {
    background: var(--surface-2);
    color: var(--text);
  }

  .view-btn[data-active='true'] {
    background: var(--surface-2);
    color: var(--accent);
  }

  .view-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: -2px;
  }

  @media (prefers-reduced-motion: reduce) {
    .view-btn {
      transition: none;
    }
  }
</style>
