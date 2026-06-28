<script lang="ts">
  import type { SessionSummary } from '../lib/types';
  import { formatDuration, formatTokens, formatCost, shortHash, formatDate } from '../lib/format/format';
  import { toHash } from '../lib/router';
  import { sessionDisplayStatus } from '../lib/status';
  import StatusPill from './StatusPill.svelte';

  interface Props {
    session: SessionSummary;
  }
  let { session }: Props = $props();

  const displayStatus = $derived(sessionDisplayStatus(session, Date.now()));

  function navigate() {
    window.location.hash = toHash({ kind: 'session', hash: session.session });
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      navigate();
    }
  }
</script>

<tr
  class="session-row"
  tabindex="0"
  onclick={navigate}
  onkeydown={onKeyDown}
  aria-label="Session {shortHash(session.session, 12)}"
>
  <td class="cell cell-hash mono">{shortHash(session.session, 12)}</td>
  <td class="cell cell-status">{#if displayStatus}<StatusPill status={displayStatus} />{/if}</td>
  <td class="cell cell-num">{formatDate(session.started_at)}</td>
  <td class="cell cell-num">{formatDuration(session.duration_ms)}</td>
  <td class="cell cell-num">{formatTokens(session.tokens_in)} / {formatTokens(session.tokens_out)}</td>
  <td class="cell cell-cost">
    <span>{formatCost(session.cost_usd)}</span>
    {#if session.cost_source}
      <span class="provenance-badge" data-source={session.cost_source}>{session.cost_source}</span>
    {/if}
  </td>
  <td class="cell cell-num">{session.tool_count}</td>
  <td class="cell cell-num">{session.error_count}</td>
  <td class="cell cell-model">{session.model_id ?? '—'}</td>
</tr>

<style>
  .session-row {
    cursor: pointer;
    border-bottom: 1px solid var(--border);
    transition: background 0.12s ease;
  }

  .session-row:hover {
    background: var(--surface-2);
  }

  .session-row:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: -2px;
  }

  .cell {
    padding: var(--s2) var(--s3);
    font-size: var(--text-sm);
    color: var(--text);
    white-space: nowrap;
  }

  .cell-hash {
    color: var(--accent);
  }

  .cell-num {
    color: var(--text-dim);
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .cell-cost {
    display: flex;
    align-items: center;
    gap: var(--s1);
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }

  .cell-model {
    color: var(--text-faint);
    font-size: var(--text-xs);
  }

  .provenance-badge {
    font-size: var(--text-xs);
    padding: 1px 5px;
    border-radius: var(--radius-sm);
    background: var(--surface-2);
    color: var(--text-faint);
    border: 1px solid var(--border);
    letter-spacing: 0.02em;
  }

  .provenance-badge[data-source='reported'] {
    color: var(--ok);
    border-color: var(--ok);
  }

  .provenance-badge[data-source='estimated'] {
    color: var(--text-faint);
  }
</style>
