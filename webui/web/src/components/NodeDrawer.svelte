<script lang="ts">
  import { selectedNodeId, navigateToNode, nodesById, sessionGraph } from '../lib/stores/stores.svelte';
  import { formatDuration, formatTokens, formatCost, shortHash } from '../lib/format/format';
  import { costProvenance } from '../lib/pricing/provenance';
  import StatusPill from './StatusPill.svelte';
  import PayloadPanel from './PayloadPanel.svelte';
  import { shouldShowStatus, isSessionLive } from '../lib/status';
  import { sessionsById } from '../lib/stores/stores.svelte';
  import { annotationEntries } from '../lib/annotations';
  import { markerSpanMembers, markerSpanAggregate } from '../lib/graph/marker-span';

  interface Props {
    hash: string;
    token: string;
    focusOnOpen?: boolean;
  }
  let { hash, token, focusOnOpen = false }: Props = $props();

  const isLive = $derived(isSessionLive(sessionsById[hash], Date.now()));

  const node = $derived(
    selectedNodeId.value ? (nodesById[selectedNodeId.value] ?? null) : null
  );

  const isOpen = $derived(selectedNodeId.value !== null);
  const notFound = $derived(selectedNodeId.value !== null && node === null);

  const provenance = $derived(node ? costProvenance(node) : 'unknown');

  const summaryParts = $derived(
    node
      ? [
          node.tokens_in !== undefined ? `in ${formatTokens(node.tokens_in)}` : null,
          node.tokens_out !== undefined ? `out ${formatTokens(node.tokens_out)}` : null,
          node.cost_usd !== undefined ? formatCost(node.cost_usd) : null,
          node.duration_ms !== undefined ? formatDuration(node.duration_ms) : null,
        ].filter((s): s is string => s !== null)
      : [],
  );

  const model = $derived(
    (node?.attrs?.['model_id'] as string | undefined) ??
      (node?.attrs?.['model'] as string | undefined) ??
      '—',
  );

  const spanInfo = $derived(
    node && node.type === 'marker'
      ? (() => {
          const graph = sessionGraph(hash);
          const members = markerSpanMembers(node.id, graph.edges);
          if (members.length === 0) return null;
          return markerSpanAggregate(members, Object.fromEntries(graph.nodes.map((n) => [n.id, n])));
        })()
      : null
  );

  let drawerEl: HTMLDivElement | undefined = $state();

  let copied = $state.raw<Set<string>>(new Set());

  function markCopied(key: string) {
    copied = new Set(copied).add(key);
    setTimeout(() => {
      const next = new Set(copied);
      next.delete(key);
      copied = next;
    }, 1000);
  }

  function close() {
    const closingId = selectedNodeId.value;
    navigateToNode(hash, null);
    if (typeof document === 'undefined') return;
    setTimeout(() => {
      const target =
        (closingId
          ? document.querySelector<HTMLElement>(
              `[role="treeitem"][data-node-id="${CSS.escape(closingId)}"]`,
            )
          : null) ??
        document.querySelector<HTMLElement>('[role="tree"][aria-label="Session outline"]');
      if (target && target.isConnected) {
        target.focus();
      }
    }, 350);
  }

  function getFocusables(): HTMLElement[] {
    if (!drawerEl) return [];
    return Array.from(
      drawerEl.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((el) => !el.hasAttribute('disabled'));
  }

  $effect(() => {
    if (!isOpen) return;
    if (typeof document === 'undefined') return;

    if (focusOnOpen) {
      const firstFocusable = getFocusables()[0] ?? (drawerEl ?? null);
      firstFocusable?.focus();
    }

    function onKeydown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        close();
      }
    }

    document.addEventListener('keydown', onKeydown);
    return () => {
      document.removeEventListener('keydown', onKeydown);
    };
  });

  const nodeAnnotations = $derived(node ? annotationEntries(node) : []);

  async function copyToClipboard(text: string, key: string) {
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      await navigator.clipboard.writeText(text);
      markCopied(key);
    }
  }
</script>

<div
  bind:this={drawerEl}
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
          {#if node.status !== undefined && shouldShowStatus(node.status, isLive)}
            <StatusPill status={node.status} />
          {/if}
        </div>
        <button class="close-btn" onclick={close} aria-label="Close node details">×</button>
      </div>

      {#if node.payload_hash}
        <PayloadPanel {hash} nodeId={node.id} nodeType={node.type} {token} payloadHash={node.payload_hash} />
      {/if}

      {#if summaryParts.length > 0}
        <div class="meta-summary">
          <span class="meta-text">{summaryParts.join(' · ')}</span>
          {#if node.cost_usd !== undefined && provenance !== 'unknown'}
            <span class="provenance-badge" data-provenance={provenance}>{provenance}</span>
          {/if}
        </div>
      {/if}

      <details class="advanced-section">
        <summary class="advanced-summary">Advanced</summary>
        <div class="advanced-body">
          <div class="advanced-row">
            <span class="advanced-label">Model</span>
            <span class="advanced-value mono">{model}</span>
          </div>

          {#if node.step_key}
            <div class="advanced-row">
              <span class="advanced-label">Step key</span>
              <span class="advanced-value mono">{shortHash(node.step_key, 16)}</span>
              {#if node.step_key_method}
                <span class="advanced-value" style="color: var(--text-faint); font-size: var(--text-xs);">{node.step_key_method}</span>
              {/if}
              <button
                class="copy-btn"
                class:copied={copied.has('step_key')}
                onclick={() => copyToClipboard(node.step_key ?? '', 'step_key')}
                aria-label="Copy step key"
              >
                {copied.has('step_key') ? 'Copied' : 'Copy'}
              </button>
            </div>
          {/if}

          {#if node.type === 'marker' && node.phase_key}
            <div class="advanced-row">
              <span class="advanced-label">Phase key</span>
              <span class="advanced-value mono">{shortHash(node.phase_key, 16)}</span>
              <button
                class="copy-btn"
                class:copied={copied.has('phase_key')}
                onclick={() => copyToClipboard(node.phase_key ?? '', 'phase_key')}
                aria-label="Copy phase key"
              >
                {copied.has('phase_key') ? 'Copied' : 'Copy'}
              </button>
            </div>
          {/if}

          {#if node.type === 'marker'}
            {#if spanInfo}
              <div class="advanced-row">
                <span class="advanced-label">Span</span>
                <span class="advanced-value mono">
                  {spanInfo.memberCount} nodes
                  {#if spanInfo.tokensIn > 0 || spanInfo.tokensOut > 0}
                    · in {formatTokens(spanInfo.tokensIn)} out {formatTokens(spanInfo.tokensOut)}
                  {/if}
                  {#if spanInfo.costUsd > 0}
                    · {formatCost(spanInfo.costUsd)}
                  {/if}
                  {#if spanInfo.durationMs > 0}
                    · {formatDuration(spanInfo.durationMs)}
                  {/if}
                </span>
              </div>
            {/if}
          {/if}

          <div class="advanced-row">
            <span class="advanced-label">ID</span>
            <span class="advanced-value mono">{node.id}</span>
            <button
              class="copy-btn"
              class:copied={copied.has('id')}
              onclick={() => copyToClipboard(node.id, 'id')}
              aria-label="Copy node ID"
            >
              {copied.has('id') ? 'Copied' : 'Copy'}
            </button>
          </div>

          <div class="advanced-row">
            <span class="advanced-label">Run ID</span>
            <span class="advanced-value mono">{node.run_id}</span>
            <button
              class="copy-btn"
              class:copied={copied.has('run')}
              onclick={() => copyToClipboard(node.run_id, 'run')}
              aria-label="Copy run ID"
            >
              {copied.has('run') ? 'Copied' : 'Copy'}
            </button>
          </div>

          {#if node.payload_hash}
            <div class="advanced-row">
              <span class="advanced-label">Payload hash</span>
              <span class="advanced-value mono">{shortHash(node.payload_hash, 16)}</span>
              <button
                class="copy-btn"
                class:copied={copied.has('hash')}
                onclick={() => copyToClipboard(node.payload_hash ?? '', 'hash')}
                aria-label="Copy full payload hash"
              >
                {copied.has('hash') ? 'Copied' : 'Copy'}
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

          {#if nodeAnnotations.length > 0}
            <div class="advanced-section-divider"></div>
            <div class="annotations-section">
              <span class="advanced-label">Annotations</span>
              <ul class="annotations-list">
                {#each nodeAnnotations as entry}
                  <li class="annotation-item">
                    <span class="annotation-key">{entry.key}</span>
                    <span class="annotation-sep">:</span>
                    <span class="annotation-value mono">{entry.value}</span>
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

  .meta-summary {
    display: flex;
    align-items: center;
    gap: var(--s2);
    padding: var(--s2) var(--s4);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .meta-text {
    font-size: var(--text-sm);
    font-family: var(--font-mono);
    color: var(--text-dim);
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

  .copy-btn.copied {
    color: var(--ok);
    border-color: var(--ok);
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

  .advanced-section-divider {
    height: 1px;
    background: var(--border);
    margin: var(--s2) 0;
  }

  .annotations-section {
    padding: var(--s1) 0;
  }

  .annotations-list {
    list-style: none;
    padding: 0;
    margin-top: var(--s1);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .annotation-item {
    display: flex;
    align-items: center;
    gap: var(--s1);
    font-size: var(--text-xs);
    color: var(--text-dim);
  }

  .annotation-key {
    color: var(--text-faint);
    flex-shrink: 0;
  }

  .annotation-sep {
    color: var(--text-faint);
  }

  .annotation-value {
    color: var(--text-dim);
    word-break: break-all;
  }
</style>
