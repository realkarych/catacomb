<script lang="ts">
  import { sessionsById, filterState, resetFilter, sessionGraph } from '../lib/stores/stores.svelte';
  import { isActive } from '../lib/filters';

  interface Props {
    hash: string;
    totalCount: number;
    filteredCount: number;
  }
  let { hash, totalCount, filteredCount }: Props = $props();

  const session = $derived(sessionsById[hash]);

  const graph = $derived(sessionGraph(hash));

  const presentStatuses = $derived(
    [...new Set(graph.nodes.map((n) => n.status).filter((s): s is string => s !== undefined))]
  );

  const presentTypes = $derived(
    [...new Set(graph.nodes.map((n) => n.type))]
  );

  const hasErrors = $derived((session?.error_count ?? 0) > 0);

  const active = $derived(isActive(filterState));

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
        >{s}</button>
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
        >{t.replace(/_/g, ' ')}</button>
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

  @media (prefers-reduced-motion: reduce) {
    .filter-chip,
    .filter-search,
    .filter-reset {
      transition: none;
    }
  }
</style>
