<script lang="ts">
  import { sessionsById } from '../lib/stores/stores.svelte';
  import { formatCost, formatTokens, formatDuration } from '../lib/format/format';

  interface Props {
    hash: string;
  }
  let { hash }: Props = $props();

  const session = $derived(sessionsById[hash]);
</script>

{#if session}
  <div class="session-kpi" aria-label="Session metrics">
    <span class="kpi-item">
      <span class="kpi-value mono">{formatCost(session.cost_usd)}</span>
      {#if session.cost_usd !== undefined}
        <span
          class="kpi-badge"
          class:kpi-badge--reported={session.cost_source === 'reported'}
          title={session.cost_source === 'reported' ? 'Cost reported by API' : 'Cost estimated'}
        >{session.cost_source === 'reported' ? 'rep' : 'est'}</span>
      {/if}
    </span>
    <span class="kpi-sep" aria-hidden="true">·</span>
    <span class="kpi-item">
      <span class="kpi-label">tok</span>
      <span class="kpi-value mono">{formatTokens(session.tokens_in)}→{formatTokens(session.tokens_out)}</span>
    </span>
    <span class="kpi-sep" aria-hidden="true">·</span>
    <span class="kpi-item">
      <span class="kpi-value mono">{formatDuration(session.duration_ms)}</span>
    </span>
    {#if session.model_id}
      <span class="kpi-sep" aria-hidden="true">·</span>
      <span class="kpi-item">
        <span class="kpi-value kpi-model">{session.model_id}</span>
      </span>
    {/if}
    {#if session.status === 'running'}
      <span class="kpi-sep" aria-hidden="true">·</span>
      <span class="live-badge" aria-label="Session is live">
        <span class="live-dot" aria-hidden="true"></span>
        live
      </span>
    {/if}
    <span class="kpi-sep" aria-hidden="true">·</span>
    <span class="kpi-item">
      <span class="kpi-value mono">{session.node_count} nodes</span>
      <span class="kpi-sep-inner" aria-hidden="true">·</span>
      <span class="kpi-value mono">{session.tool_count} tools</span>
    </span>
    {#if session.error_count > 0}
      <span class="kpi-sep" aria-hidden="true">·</span>
      <span class="kpi-chip kpi-chip--error" aria-label="{session.error_count} errors">
        {session.error_count} err
      </span>
    {/if}
  </div>
{/if}

<style>
  .session-kpi {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0 var(--s2);
    padding: var(--s1) var(--s4);
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    font-size: var(--text-xs);
    color: var(--text-dim);
  }

  .kpi-item {
    display: inline-flex;
    align-items: center;
    gap: var(--s1);
  }

  .kpi-label {
    color: var(--text-faint);
  }

  .kpi-value {
    color: var(--text-dim);
  }

  .kpi-value.kpi-model {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    color: var(--text-faint);
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kpi-sep {
    color: var(--text-faint);
    user-select: none;
  }

  .kpi-sep-inner {
    color: var(--text-faint);
    user-select: none;
  }

  .kpi-badge {
    font-size: 9px;
    padding: 1px 3px;
    border-radius: 2px;
    background: var(--surface-2);
    color: var(--text-faint);
    border: 1px solid var(--border);
    font-family: var(--font-mono);
    line-height: 1;
    vertical-align: middle;
  }

  .kpi-badge--reported {
    color: var(--ok);
    border-color: var(--ok);
  }

  .kpi-chip {
    font-family: var(--font-mono);
    font-size: var(--text-xs);
    padding: 1px var(--s2);
    border-radius: var(--radius-sm);
    background: transparent;
    border: 1px solid currentColor;
  }

  .kpi-chip--error {
    color: var(--error);
  }

  .live-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--text-xs);
    font-family: var(--font-mono);
    font-weight: 500;
    letter-spacing: 0.04em;
    color: var(--running);
  }

  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--running);
    flex-shrink: 0;
    animation: live-pulse 1.5s ease-in-out infinite;
  }

  @keyframes live-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
  }

  @media (prefers-reduced-motion: reduce) {
    .live-dot {
      animation: none;
    }
  }
</style>
