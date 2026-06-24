<script lang="ts">
  import { selectedNodeId, selectNode, nodesById } from '../lib/stores/stores.svelte';
  import { formatDuration, formatTokens, formatCost, shortHash } from '../lib/format/format';
  import { costProvenance } from '../lib/pricing/provenance';
  import { toHash } from '../lib/router';
  import StatusPill from './StatusPill.svelte';
  import MetricRow from './MetricRow.svelte';
  import PayloadPanel from './PayloadPanel.svelte';

  interface Props {
    hash: string;
    token: string;
  }
  let { hash, token }: Props = $props();

  const node = $derived(
    selectedNodeId.value ? (nodesById[selectedNodeId.value] ?? null) : null
  );

  const isOpen = $derived(selectedNodeId.value !== null);
  const notFound = $derived(selectedNodeId.value !== null && node === null);

  const provenance = $derived(node ? costProvenance(node) : 'unknown');

  function close() {
    selectNode(null);
    if (typeof window !== 'undefined') {
      window.location.hash = toHash({ kind: 'session', hash });
    }
  }

  $effect(() => {
    if (!isOpen) return;
    function onKeydown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        close();
      }
    }
    document.addEventListener('keydown', onKeydown);
    return () => document.removeEventListener('keydown', onKeydown);
  });

  async function copyToClipboard(text: string) {
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      await navigator.clipboard.writeText(text);
    }
  }
</script>

<div
  class="node-drawer"
  class:node-drawer--open={isOpen}
  aria-label="Node details"
  aria-hidden={!isOpen}
  role="complementary"
>
  {#if notFound}
    <div class="drawer-inner">
      <div class="drawer-header">
        <span class="drawer-title">Node not found</span>
        <button class="close-btn" onclick={close} aria-label="Close node details">×</button>
      </div>
      <p class="not-found-msg">Node not found or still loading.</p>
    </div>
  {:else if node}
    <div class="drawer-inner">
      <div class="drawer-header">
        <div class="drawer-title-row">
          <span class="drawer-title">{node.name ?? node.type}</span>
          <StatusPill status={node.status ?? 'pending'} />
        </div>
        <button class="close-btn" onclick={close} aria-label="Close node details">×</button>
      </div>

      <div class="metrics-section">
        <MetricRow label="Duration" value={formatDuration(node.duration_ms)} />
        <MetricRow label="Tokens in" value={formatTokens(node.tokens_in)} />
        <MetricRow label="Tokens out" value={formatTokens(node.tokens_out)} />
        <div class="metric-row-cost">
          <MetricRow label="Cost" value={formatCost(node.cost_usd)} />
          {#if provenance !== 'unknown'}
            <span class="provenance-badge" data-provenance={provenance}>{provenance}</span>
          {/if}
        </div>
        <MetricRow
          label="Model"
          value={(node.attrs?.['model_id'] as string | undefined) ?? (node.attrs?.['model'] as string | undefined) ?? '—'}
        />
      </div>

      <PayloadPanel {hash} nodeId={node.id} {token} />

      <details class="advanced-section">
        <summary class="advanced-summary">Advanced</summary>
        <div class="advanced-body">
          <div class="advanced-row">
            <span class="advanced-label">ID</span>
            <span class="advanced-value mono">{node.id}</span>
            <button
              class="copy-btn"
              onclick={() => copyToClipboard(node.id)}
              aria-label="Copy node ID"
            >
              Copy
            </button>
          </div>

          <div class="advanced-row">
            <span class="advanced-label">Run ID</span>
            <span class="advanced-value mono">{node.run_id}</span>
            <button
              class="copy-btn"
              onclick={() => copyToClipboard(node.run_id)}
              aria-label="Copy run ID"
            >
              Copy
            </button>
          </div>

          {#if node.payload_hash}
            <div class="advanced-row">
              <span class="advanced-label">Payload hash</span>
              <span class="advanced-value mono">{shortHash(node.payload_hash, 16)}</span>
              <button
                class="copy-btn"
                onclick={() => copyToClipboard(node.payload_hash ?? '')}
                aria-label="Copy full payload hash"
              >
                Copy
              </button>
            </div>
          {/if}

          {#if node.tier}
            <div class="advanced-row">
              <span class="advanced-label">Tier</span>
              <span class="advanced-value mono">{node.tier}</span>
            </div>
          {/if}

          {#if node.sources && node.sources.length > 0}
            <div class="advanced-sources">
              <span class="advanced-label">Sources</span>
              <ul class="sources-list">
                {#each node.sources as src}
                  <li class="source-item">
                    <span class="mono">{src.source}</span>
                    <span class="source-sep">/</span>
                    <span class="mono">{shortHash(src.obs_id)}</span>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
        </div>
      </details>
    </div>
  {/if}
</div>

<style>
  .node-drawer {
    width: 0;
    flex-shrink: 0;
    background: var(--surface);
    border-left: none;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    transition: width 0.2s ease;
    visibility: hidden;
  }

  @media (prefers-reduced-motion: reduce) {
    .node-drawer {
      transition: none;
    }
  }

  .node-drawer--open {
    width: 360px;
    border-left: 1px solid var(--border);
    visibility: visible;
  }

  @media (max-width: 700px) {
    .node-drawer--open {
      width: 100%;
    }
  }

  .drawer-inner {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .drawer-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--s2);
    padding: var(--s3) var(--s4);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .drawer-title-row {
    display: flex;
    align-items: center;
    gap: var(--s2);
    flex-wrap: wrap;
    flex: 1;
    min-width: 0;
  }

  .drawer-title {
    font-size: var(--text-base);
    font-weight: 500;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .close-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    font-size: var(--text-md);
    line-height: 1;
    color: var(--text-dim);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
    flex-shrink: 0;
    transition: color 0.12s, border-color 0.12s;
  }

  .close-btn:hover {
    color: var(--text);
    border-color: var(--border);
  }

  .close-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .metrics-section {
    padding: var(--s3) var(--s4);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .metric-row-cost {
    display: flex;
    align-items: center;
    gap: var(--s2);
  }

  .metric-row-cost > :global(.metric-row) {
    flex: 1;
  }

  .provenance-badge {
    font-size: var(--text-xs);
    padding: 1px 5px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    color: var(--text-faint);
    white-space: nowrap;
    flex-shrink: 0;
  }

  .provenance-badge[data-provenance='reported'] {
    color: var(--ok);
    border-color: var(--ok);
  }

  .provenance-badge[data-provenance='estimated'] {
    color: var(--blocked);
    border-color: var(--blocked);
  }

  .advanced-section {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .advanced-summary {
    font-size: var(--text-sm);
    color: var(--text-dim);
    padding: var(--s3) var(--s4);
    cursor: pointer;
    user-select: none;
    border-bottom: 1px solid var(--border);
    list-style: none;
  }

  .advanced-summary::marker,
  .advanced-summary::-webkit-details-marker {
    display: none;
  }

  .advanced-summary:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: -2px;
  }

  .advanced-summary::after {
    content: ' ▸';
    color: var(--text-faint);
    font-size: var(--text-xs);
  }

  details[open] .advanced-summary::after {
    content: ' ▾';
  }

  .advanced-body {
    padding: var(--s2) var(--s4);
    display: flex;
    flex-direction: column;
    gap: var(--s1);
  }

  .advanced-row {
    display: flex;
    align-items: center;
    gap: var(--s2);
    padding: var(--s1) 0;
  }

  .advanced-label {
    font-size: var(--text-xs);
    color: var(--text-faint);
    flex-shrink: 0;
    width: 88px;
  }

  .advanced-value {
    font-size: var(--text-xs);
    color: var(--text-dim);
    flex: 1;
    word-break: break-all;
  }

  .copy-btn {
    font-size: var(--text-xs);
    color: var(--text-faint);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 1px 6px;
    cursor: pointer;
    flex-shrink: 0;
    font-family: var(--font-ui);
    transition: color 0.12s, border-color 0.12s;
  }

  .copy-btn:hover {
    color: var(--accent);
    border-color: var(--accent);
  }

  .copy-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .advanced-sources {
    padding: var(--s1) 0;
  }

  .sources-list {
    list-style: none;
    margin-top: var(--s1);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .source-item {
    display: flex;
    align-items: center;
    gap: var(--s1);
    font-size: var(--text-xs);
    color: var(--text-dim);
  }

  .source-sep {
    color: var(--text-faint);
  }

  .not-found-msg {
    padding: var(--s4);
    font-size: var(--text-sm);
    color: var(--text-faint);
  }
</style>
