<script lang="ts">
  import { onMount } from 'svelte';
  import type { SessionSummary } from '../lib/types';
  import { fetchSessions } from '../lib/api';
  import { filterSessions, sortSessions } from '../lib/sessions-sort';
  import type { SortKey, SortDir } from '../lib/sessions-sort';
  import { toHash } from '../lib/router';
  import { upsertSession } from '../lib/stores/stores.svelte';
  import SessionRow from './SessionRow.svelte';

  interface Props {
    token: string;
  }
  let { token }: Props = $props();

  let sessions: SessionSummary[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);
  let searchQuery = $state('');
  let sortKey: SortKey = $state('started_at');
  let sortDir: SortDir = $state('desc');

  const sortableColumns: { key: SortKey; label: string }[] = [
    { key: 'started_at', label: 'Started' },
    { key: 'duration_ms', label: 'Duration' },
    { key: 'tokens_in', label: 'Tokens in' },
    { key: 'tokens_out', label: 'Tokens out' },
    { key: 'cost_usd', label: 'Cost' },
    { key: 'error_count', label: 'Errors' },
  ];

  onMount(async () => {
    try {
      sessions = await fetchSessions(token);
      for (const s of sessions) {
        upsertSession(s);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Unknown error';
    } finally {
      loading = false;
    }
  });

  const filtered = $derived(filterSessions(sessions, searchQuery));
  const sorted = $derived(sortSessions(filtered, sortKey, sortDir));

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      sortKey = key;
      sortDir = 'desc';
    }
  }

  function onSearchKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      const q = searchQuery.trim();
      if (q.length >= 8) {
        window.location.hash = toHash({ kind: 'session', hash: q });
      }
    }
  }

  function getSortLabel(key: SortKey): string {
    if (sortKey !== key) return 'Sort';
    return sortDir === 'asc' ? 'Sorted ascending' : 'Sorted descending';
  }
</script>

<div class="sessions-list">
  <div class="list-toolbar">
    <input
      class="search-input"
      type="search"
      placeholder="Search by session hash or model…"
      bind:value={searchQuery}
      onkeydown={onSearchKeyDown}
      aria-label="Search sessions"
    />
    <div class="sort-controls" role="group" aria-label="Sort sessions">
      {#each sortableColumns as col}
        <button
          class="sort-btn"
          class:active={sortKey === col.key}
          onclick={() => toggleSort(col.key)}
          aria-pressed={sortKey === col.key}
          aria-label="{col.label}: {getSortLabel(col.key)}"
        >
          {col.label}
          {#if sortKey === col.key}
            <span class="sort-indicator" aria-hidden="true">{sortDir === 'asc' ? '↑' : '↓'}</span>
          {/if}
        </button>
      {/each}
    </div>
  </div>

  {#if loading}
    <div class="empty-state" role="status" aria-live="polite">
      <div class="empty-state-icon" aria-hidden="true">⏳</div>
      <p class="empty-state-headline">Loading sessions…</p>
    </div>
  {:else if error}
    <div class="empty-state" role="alert">
      <div class="empty-state-icon" aria-hidden="true">⚠</div>
      <p class="empty-state-headline">Could not load sessions</p>
      <p class="empty-state-hint">Check that the daemon is running and your token is valid.</p>
      <p class="empty-state-hint error-detail">{error}</p>
    </div>
  {:else if sorted.length === 0 && sessions.length === 0}
    <div class="empty-state">
      <div class="empty-state-icon" aria-hidden="true">⛏</div>
      <p class="empty-state-headline">No sessions yet — start a Claude session with the hooks installed.</p>
      <p class="empty-state-hint">Run <code>catacomb up</code> to start the daemon and install hooks.</p>
    </div>
  {:else if sorted.length === 0}
    <div class="empty-state">
      <div class="empty-state-icon" aria-hidden="true">🔍</div>
      <p class="empty-state-headline">No sessions match your search.</p>
    </div>
  {:else}
    <div class="table-wrap" role="region" aria-label="Sessions list">
      <table class="sessions-table">
        <thead>
          <tr>
            <th class="th" scope="col">Session</th>
            <th class="th" scope="col">Status</th>
            <th class="th th-num" scope="col">Started</th>
            <th class="th th-num" scope="col">Duration</th>
            <th class="th th-num" scope="col">Tokens in/out</th>
            <th class="th th-num" scope="col">Cost</th>
            <th class="th th-num" scope="col">Tools</th>
            <th class="th th-num" scope="col">Errors</th>
            <th class="th" scope="col">Model</th>
          </tr>
        </thead>
        <tbody>
          {#each sorted as session (session.session)}
            <SessionRow {session} />
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .sessions-list {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .list-toolbar {
    display: flex;
    align-items: center;
    gap: var(--s3);
    padding: var(--s3) var(--s4);
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .search-input {
    flex: 1;
    min-width: 180px;
    max-width: 360px;
    padding: var(--s1) var(--s3);
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--text-sm);
    font-family: var(--font-ui);
    outline: none;
  }

  .search-input:focus-visible {
    border-color: var(--ring);
    box-shadow: 0 0 0 2px var(--ring);
  }

  .sort-controls {
    display: flex;
    gap: var(--s1);
    flex-wrap: wrap;
  }

  .sort-btn {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 3px var(--s2);
    font-size: var(--text-xs);
    font-family: var(--font-ui);
    color: var(--text-faint);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: color 0.12s, border-color 0.12s;
  }

  .sort-btn:hover {
    color: var(--text-dim);
    border-color: var(--border-strong);
  }

  .sort-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .sort-btn.active {
    color: var(--accent);
    border-color: var(--accent);
  }

  .sort-indicator {
    font-size: var(--text-xs);
  }

  .table-wrap {
    flex: 1;
    overflow-x: auto;
    overflow-y: auto;
  }

  .sessions-table {
    width: 100%;
    border-collapse: collapse;
    min-width: 640px;
  }

  .th {
    padding: var(--s2) var(--s3);
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--text-faint);
    text-align: left;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    user-select: none;
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .th-num {
    text-align: right;
  }

  .error-detail {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    opacity: 0.7;
  }
</style>
