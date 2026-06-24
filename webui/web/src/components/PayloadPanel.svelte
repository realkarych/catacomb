<script lang="ts">
  import { fetchNodePayload, ForbiddenError, NotFoundError } from '../lib/api';
  import { prettyJSON, payloadState } from '../lib/payload-view';
  import type { PayloadView } from '../lib/types';

  interface Props {
    hash: string;
    nodeId: string;
    token: string;
  }
  let { hash, nodeId, token }: Props = $props();

  let revealed = $state(false);
  let loadState: 'idle' | 'loading' | 'forbidden' | 'not-found' | 'error' | 'ok' = $state('idle');
  let view: PayloadView | null = $state(null);
  let forbidden = $state(false);

  $effect(() => {
    nodeId;
    revealed = false;
    loadState = 'idle';
    view = null;
    forbidden = false;
  });

  async function reveal() {
    revealed = true;
    loadState = 'loading';
    forbidden = false;
    view = null;
    try {
      view = await fetchNodePayload(hash, nodeId, token);
      loadState = 'ok';
    } catch (e) {
      if (e instanceof ForbiddenError) {
        loadState = 'forbidden';
        forbidden = true;
      } else if (e instanceof NotFoundError) {
        loadState = 'not-found';
      } else {
        loadState = 'error';
      }
    }
  }

  function collapse() {
    revealed = false;
    loadState = 'idle';
    view = null;
    forbidden = false;
  }

  async function copyText(text: string) {
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      await navigator.clipboard.writeText(text);
    }
  }
</script>

<div class="payload-panel">
  {#if !revealed}
    <button
      class="reveal-btn"
      onclick={reveal}
      aria-label="Reveal node content"
    >
      Reveal content
    </button>
  {:else}
    <div class="payload-header">
      <span class="payload-label">Content</span>
      <button
        class="collapse-btn"
        onclick={collapse}
        aria-label="Collapse content"
      >
        Hide
      </button>
    </div>

    {#if loadState === 'loading'}
      <div class="payload-loading" role="status" aria-live="polite">
        <span class="spinner" aria-hidden="true"></span>
        <span class="sr-only">Loading…</span>
      </div>
    {:else if loadState === 'forbidden'}
      <p class="payload-msg">
        Content viewing is off. Start the daemon with <code class="mono">--allow-payload-access</code> to enable.
      </p>
    {:else if loadState === 'not-found'}
      <p class="payload-msg">No stored payload for this node.</p>
    {:else if loadState === 'error'}
      <p class="payload-msg payload-msg--error">Failed to load content.</p>
    {:else if loadState === 'ok' && view}
      {#if view.redacted}
        <div class="redacted-notice" aria-label="Content was redacted">
          <span class="redacted-badge">redacted</span>
          <span class="redacted-count">{view.redactions.length} secret{view.redactions.length === 1 ? '' : 's'} redacted</span>
        </div>
      {/if}

      {#if view.input !== undefined && view.input !== null}
        <div class="payload-section">
          <div class="payload-section-header">
            <span class="payload-section-label">Input</span>
            <button
              class="copy-btn"
              onclick={() => copyText(prettyJSON(view?.input))}
              aria-label="Copy input content"
            >
              Copy
            </button>
          </div>
          <pre class="payload-content mono">{prettyJSON(view.input)}</pre>
        </div>
      {/if}

      {#if view.output !== undefined && view.output !== null}
        <div class="payload-section">
          <div class="payload-section-header">
            <span class="payload-section-label">Output</span>
            <button
              class="copy-btn"
              onclick={() => copyText(prettyJSON(view?.output))}
              aria-label="Copy output content"
            >
              Copy
            </button>
          </div>
          <pre class="payload-content mono">{prettyJSON(view.output)}</pre>
        </div>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .payload-panel {
    border-top: 1px solid var(--border);
    padding: var(--s3) var(--s4);
  }

  .reveal-btn {
    font-size: var(--text-sm);
    color: var(--text-faint);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--s1) var(--s3);
    cursor: pointer;
    font-family: var(--font-ui);
    transition: color 0.12s, border-color 0.12s;
    width: 100%;
    text-align: left;
  }

  .reveal-btn:hover {
    color: var(--text-dim);
    border-color: var(--border-strong);
  }

  .reveal-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .payload-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--s2);
  }

  .payload-label {
    font-size: var(--text-xs);
    color: var(--text-faint);
    font-weight: 500;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .collapse-btn {
    font-size: var(--text-xs);
    color: var(--text-faint);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 1px var(--s2);
    cursor: pointer;
    font-family: var(--font-ui);
    transition: color 0.12s, border-color 0.12s;
  }

  .collapse-btn:hover {
    color: var(--text-dim);
    border-color: var(--border-strong);
  }

  .collapse-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .payload-loading {
    display: flex;
    align-items: center;
    gap: var(--s2);
    padding: var(--s2) 0;
  }

  .spinner {
    width: 12px;
    height: 12px;
    border: 1px solid var(--border);
    border-top-color: var(--text-faint);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    flex-shrink: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation: none;
      border-top-color: var(--border);
    }
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .payload-msg {
    font-size: var(--text-sm);
    color: var(--text-faint);
    line-height: var(--lh-normal);
    padding: var(--s1) 0;
  }

  .payload-msg--error {
    color: var(--error);
  }

  .redacted-notice {
    display: flex;
    align-items: center;
    gap: var(--s2);
    margin-bottom: var(--s2);
  }

  .redacted-badge {
    font-size: var(--text-xs);
    color: var(--text-faint);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 1px 5px;
    font-family: var(--font-mono);
  }

  .redacted-count {
    font-size: var(--text-xs);
    color: var(--text-faint);
  }

  .payload-section {
    margin-bottom: var(--s3);
  }

  .payload-section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--s1);
  }

  .payload-section-label {
    font-size: var(--text-xs);
    color: var(--text-faint);
    font-weight: 500;
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

  .payload-content {
    font-size: var(--text-xs);
    color: var(--text-dim);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--s2) var(--s3);
    overflow-x: auto;
    overflow-y: auto;
    max-height: 240px;
    white-space: pre;
    word-break: normal;
    line-height: var(--lh-relaxed);
  }
</style>
