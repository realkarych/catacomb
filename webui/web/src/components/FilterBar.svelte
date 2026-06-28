<script lang="ts">
  import {
    sessionsById,
    filterState,
    resetFilter,
    sessionGraph,
    outlineShowSystem,
    outlineActions,
  } from '../lib/stores/stores.svelte';
  import { isActive } from '../lib/filters';
  import { nodeTypeInfo } from '../lib/node-legend';
  import { displayLabel, isOutcomeStatus, isSessionLive } from '../lib/status';
  import { untrack } from 'svelte';

  interface Props {
    hash: string;
    totalCount: number;
    filteredCount: number;
    outlineActive: boolean;
  }
  let { hash, totalCount, filteredCount, outlineActive }: Props = $props();

  const session = $derived(sessionsById[hash]);

  const graph = $derived(sessionGraph(hash));

  const presentStatuses = $derived(
    [...new Set(graph.nodes.map((n) => n.status).filter((s): s is string => s !== undefined))].filter(
      (s) => isOutcomeStatus(s) || (s === 'running' && isSessionLive(session, Date.now()))
    )
  );

  const presentTypes = $derived(
    [...new Set(graph.nodes.map((n) => n.type))]
  );

  const hasErrors = $derived((session?.error_count ?? 0) > 0);

  const active = $derived(isActive(filterState));

  $effect(() => {
    const present = new Set(presentStatuses);
    const current = untrack(() => filterState.statuses);
    const toRemove = current.filter((s) => !present.has(s));
    for (const s of toRemove) {
      const idx = filterState.statuses.indexOf(s);
      if (idx >= 0) filterState.statuses.splice(idx, 1);
    }
  });

  function toggleStatus(s: string) {
    const idx = filterState.statuses.indexOf(s);
    if (idx >= 0) {
      filterState.statuses.splice(idx, 1);
    } else {
      filterState.statuses.push(s);
    }
  }

  function toggleType(t: string) {
    const idx = filterState.types.indexOf(t);
    if (idx >= 0) {
      filterState.types.splice(idx, 1);
    } else {
      filterState.types.push(t);
    }
  }
</script>

<div class="filter-bar" class:filter-bar--active={active} aria-label="Filter nodes">
  <input
    class="filter-search mono"
    type="search"
    placeholder="search nodes…"
    bind:value={filterState.query}
    aria-label="Search nodes by name or type"
  />

  {#if presentStatuses.length > 0}
    <div class="filter-group" role="group" aria-label="Filter by status">
      {#each presentStatuses as s}
        <button
          class="filter-chip"
          class:filter-chip--active={filterState.statuses.includes(s)}
          onclick={() => toggleStatus(s)}
          aria-pressed={filterState.statuses.includes(s)}
        >{displayLabel(s)}</button>
      {/each}
    </div>
  {/if}

  {#if presentTypes.length > 0}
    <div class="filter-group" role="group" aria-label="Filter by type">
      {#each presentTypes as t}
        <button
          class="filter-chip"
          class:filter-chip--active={filterState.types.includes(t)}
          onclick={() => toggleType(t)}
          aria-pressed={filterState.types.includes(t)}
        >{nodeTypeInfo(t).label}</button>
      {/each}
    </div>
  {/if}

  {#if hasErrors}
    <button
      class="filter-chip filter-chip--err"
      class:filter-chip--active={filterState.hasError}
      onclick={() => { filterState.hasError = !filterState.hasError; }}
      aria-pressed={filterState.hasError}
    >has errors</button>
  {/if}

  {#if active}
    <span class="filter-count" aria-live="polite">
      {filteredCount} of {totalCount}
    </span>
    <button class="filter-reset" onclick={resetFilter} aria-label="Clear all filters">×</button>
  {/if}

  {#if outlineActive}
    <div
      class="outline-controls"
      class:outline-controls--lead={!active}
      role="toolbar"
      aria-label="Outline controls"
    >
      <button class="outline-ctl-btn" type="button" onclick={() => outlineActions.collapseAll?.()}>Collapse all</button>
      <button class="outline-ctl-btn" type="button" onclick={() => outlineActions.expandAll?.()}>Expand all</button>
      <button class="outline-ctl-btn" type="button" onclick={() => outlineActions.reset?.()}>Reset</button>
      <button
        class="outline-ctl-btn"
        type="button"
        aria-pressed={outlineShowSystem.value}
        onclick={() => (outlineShowSystem.value = !outlineShowSystem.value)}
      >Show system</button>
      <div class="outline-help">
        <button
          class="outline-ctl-btn outline-help-btn"
          type="button"
          aria-label="Stat legend"
          aria-describedby="outline-legend"
        >?</button>
        <div class="outline-legend" id="outline-legend" role="tooltip">
          <span class="outline-legend-item"><span class="outline-legend-key">assistant</span> tokens in · out · cost · duration</span>
          <span class="outline-legend-item"><span class="outline-legend-key">tool</span> arg → output · duration</span>
          <span class="outline-legend-item"><span class="outline-legend-key">collapsed</span> node count · in · out · cost · duration</span>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .filter-bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--s2);
    padding: var(--s2) var(--s4);
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }

  .filter-bar--active {
    border-bottom-color: var(--accent);
  }

  .filter-search {
    height: 24px;
    padding: 0 var(--s2);
    font-size: var(--text-xs);
    font-family: var(--font-mono);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    outline: none;
    min-width: 140px;
    transition: border-color 0.1s;
  }

  .filter-search:focus {
    border-color: var(--accent);
  }

  .filter-group {
    display: flex;
    gap: var(--s1);
    flex-wrap: wrap;
  }

  .filter-chip {
    height: 22px;
    padding: 0 var(--s2);
    font-size: var(--text-xs);
    font-family: var(--font-ui);
    color: var(--text-faint);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background 0.1s, color 0.1s, border-color 0.1s;
    white-space: nowrap;
  }

  .filter-chip:hover {
    color: var(--text-dim);
    border-color: var(--text-faint);
  }

  .filter-chip--active {
    background: var(--surface-2);
    color: var(--accent);
    border-color: var(--accent);
  }

  .filter-chip--err {
    color: var(--text-faint);
  }

  .filter-chip--err.filter-chip--active {
    color: var(--error);
    border-color: var(--error);
    background: var(--bg);
  }

  .filter-count {
    font-size: var(--text-xs);
    font-family: var(--font-mono);
    color: var(--text-faint);
    margin-left: auto;
  }

  .filter-reset {
    height: 22px;
    width: 22px;
    padding: 0;
    font-size: var(--text-sm);
    color: var(--text-faint);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: color 0.1s, border-color 0.1s;
  }

  .filter-reset:hover {
    color: var(--text-dim);
    border-color: var(--text-faint);
  }

  .outline-controls {
    display: inline-flex;
    align-items: center;
    gap: var(--s2);
  }

  .outline-controls--lead {
    margin-left: auto;
  }

  .outline-ctl-btn {
    font-size: var(--text-xs);
    font-family: var(--font-ui);
    color: var(--text-dim);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--s1) var(--s2);
    cursor: pointer;
  }

  .outline-ctl-btn:hover {
    color: var(--text);
    border-color: var(--accent);
  }

  .outline-ctl-btn[aria-pressed='true'] {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--surface-2);
  }

  .outline-ctl-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .outline-help {
    position: relative;
    display: inline-flex;
  }

  .outline-help-btn {
    width: 22px;
    padding: 0;
    line-height: 1;
  }

  .outline-legend {
    position: absolute;
    top: calc(100% + var(--s1));
    right: 0;
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: var(--s1);
    padding: var(--s2) var(--s3);
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-2);
    font-size: var(--text-xs);
    font-family: var(--font-mono);
    color: var(--text-faint);
    white-space: nowrap;
    visibility: hidden;
    opacity: 0;
    transition: opacity 0.12s ease;
  }

  .outline-help:hover .outline-legend,
  .outline-help:focus-within .outline-legend {
    visibility: visible;
    opacity: 1;
  }

  .outline-legend-item {
    white-space: nowrap;
  }

  .outline-legend-key {
    color: var(--text-dim);
  }

  @media (prefers-reduced-motion: reduce) {
    .filter-chip,
    .filter-search,
    .filter-reset,
    .outline-legend {
      transition: none;
    }
  }
</style>
