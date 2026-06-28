<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchSessions, fetchDiff } from '../lib/api';
  import { toHash } from '../lib/router';
  import { diffCounts, isEmptyDiff, changedFields } from '../lib/diff';
  import { shortHash } from '../lib/format/format';
  import type { DiffResult, SessionSummary } from '../lib/types';

  interface Props {
    token: string;
    a?: string;
    b?: string;
  }
  let { token, a, b }: Props = $props();

  let sessions: SessionSummary[] = $state([]);
  let sessionsError: string | null = $state(null);
  let selA: string = $state(a ?? '');
  let selB: string = $state(b ?? '');
  let diffResult: DiffResult | null = $state(null);
  let diffLoading = $state(false);
  let diffError: string | null = $state(null);

  onMount(async () => {
    try {
      sessions = await fetchSessions(token);
    } catch (e) {
      sessionsError = e instanceof Error ? e.message : 'Failed to load sessions';
    }
  });

  $effect(() => {
    selA = a ?? '';
    selB = b ?? '';
  });

  $effect(() => {
    if (typeof window === 'undefined') return;
    window.location.hash = toHash({ kind: 'diff', a: selA || undefined, b: selB || undefined });
  });

  $effect(() => {
    if (!selA || !selB || selA === selB) {
      diffResult = null;
      diffError = null;
      return;
    }
    const captA = selA;
    const captB = selB;
    let cancelled = false;
    diffLoading = true;
    diffError = null;
    diffResult = null;
    fetchDiff(captA, captB, token)
      .then((r) => {
        if (!cancelled) {
          diffResult = r;
          diffLoading = false;
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          diffError = e instanceof Error ? e.message : 'Diff failed';
          diffLoading = false;
        }
      });
    return () => {
      cancelled = true;
    };
  });
</script>

<div class="diff-view">
  <div class="diff-toolbar">
    <label for="diff-select-a">Session A</label>
    <select id="diff-select-a" bind:value={selA}>
      <option value="">— select —</option>
      {#each sessions as session (session.session)}
        <option value={session.session}>{session.label ?? shortHash(session.session)}</option>
      {/each}
    </select>
    <span class="diff-vs">vs</span>
    <label for="diff-select-b">Session B</label>
    <select id="diff-select-b" bind:value={selB}>
      <option value="">— select —</option>
      {#each sessions as session (session.session)}
        <option value={session.session}>{session.label ?? shortHash(session.session)}</option>
      {/each}
    </select>
  </div>

  {#if sessionsError}
    <p class="diff-sessions-error" role="alert">{sessionsError}</p>
  {:else if diffLoading}
    <p class="diff-status" role="status" aria-live="polite">Comparing…</p>
  {:else if diffError}
    <p class="diff-error" role="alert">{diffError}</p>
  {:else if diffResult}
    {#if isEmptyDiff(diffResult)}
      <p class="diff-identical">Sessions are identical.</p>
    {:else}
      {@const counts = diffCounts(diffResult)}
      <div class="diff-counts">
        {#if counts.added > 0}<span class="diff-count diff-count-added">{counts.added} added</span>{/if}
        {#if counts.removed > 0}<span class="diff-count diff-count-removed">{counts.removed} removed</span>{/if}
        {#if counts.changed > 0}<span class="diff-count diff-count-changed">{counts.changed} changed</span>{/if}
        {#if counts.unchanged > 0}<span class="diff-count diff-count-unchanged">{counts.unchanged} unchanged</span>{/if}
      </div>
      {#if diffResult.changed.length > 0}
        <ul class="diff-changed-list">
          {#each diffResult.changed as step (step.a_step_key)}
            <li class="diff-changed-step">
              <span class="diff-tool-name">{step.tool}</span>
              <span class="diff-fields">
                {#each changedFields(step.deltas) as field}
                  <span class="diff-field-badge">{field}</span>
                {/each}
              </span>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .diff-view {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .diff-toolbar {
    display: flex;
    align-items: center;
    gap: var(--s3);
    padding: var(--s3) var(--s4);
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .diff-toolbar label {
    font-size: var(--text-xs);
    font-weight: 600;
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .diff-toolbar select {
    padding: var(--s1) var(--s3);
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--text-sm);
    font-family: var(--font-ui);
    outline: none;
    min-width: 140px;
  }

  .diff-toolbar select:focus-visible {
    border-color: var(--ring);
    box-shadow: 0 0 0 2px var(--ring);
  }

  .diff-vs {
    font-size: var(--text-xs);
    color: var(--text-faint);
    font-weight: 500;
  }

  .diff-status,
  .diff-identical {
    padding: var(--s5) var(--s4);
    font-size: var(--text-sm);
    color: var(--text-faint);
  }

  .diff-error,
  .diff-sessions-error {
    padding: var(--s5) var(--s4);
    font-size: var(--text-sm);
    color: var(--error);
  }

  .diff-counts {
    display: flex;
    align-items: center;
    gap: var(--s3);
    padding: var(--s3) var(--s4);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .diff-count {
    font-size: var(--text-xs);
    font-weight: 500;
    padding: 2px var(--s2);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
  }

  .diff-count-added { color: var(--ok); }
  .diff-count-removed { color: var(--error); }
  .diff-count-changed { color: var(--accent); }
  .diff-count-unchanged { color: var(--text-faint); }

  .diff-changed-list {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    flex: 1;
  }

  .diff-changed-step {
    display: flex;
    align-items: center;
    gap: var(--s3);
    padding: var(--s2) var(--s4);
    border-bottom: 1px solid var(--border);
    font-size: var(--text-sm);
  }

  .diff-tool-name {
    font-family: var(--font-mono);
    color: var(--text);
    font-size: var(--text-xs);
    flex-shrink: 0;
  }

  .diff-fields {
    display: flex;
    gap: var(--s2);
    flex-wrap: wrap;
  }

  .diff-field-badge {
    font-size: var(--text-xs);
    padding: 1px var(--s2);
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-dim);
    font-family: var(--font-mono);
  }
</style>
