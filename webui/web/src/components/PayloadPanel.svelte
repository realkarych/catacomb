<script lang="ts">
  import { fetchNodePayload, ForbiddenError, NotFoundError } from '../lib/api';
  import { prettyJSON, payloadState, truncateAtNewline, remainingLineCount, type PayloadState } from '../lib/payload-view';
  import { isConversationNode, conversationText } from '../lib/conversation';
  import type { PayloadView } from '../lib/types';

  const CONV_LIMIT = 1200;
  const TOOL_LIMIT = 3000;

  type DisplayState = 'idle' | 'loading' | 'not-found' | 'error' | PayloadState;

  function computeDisplayState(fs: typeof fetchState, v: PayloadView | null, fb: boolean): DisplayState {
    if (fs === 'idle' || fs === 'loading') return fs;
    if (fs === 'not-found') return 'not-found';
    if (fs === 'error') return 'error';
    return payloadState(v, fb);
  }

  interface Props {
    hash: string;
    nodeId: string;
    nodeType: string;
    token: string;
    payloadHash: string;
  }
  let { hash, nodeId, nodeType, token, payloadHash }: Props = $props();

  let expanded = $state(false);
  let fetchState: 'idle' | 'loading' | 'not-found' | 'error' | 'done' = $state('idle');
  let view: PayloadView | null = $state(null);
  let forbidden = $state(false);

  const displayState = $derived(computeDisplayState(fetchState, view, forbidden));
  const asText = $derived(isConversationNode(nodeType));
  const limit = $derived(asText ? CONV_LIMIT : TOOL_LIMIT);

  $effect(() => {
    const id = nodeId;
    expanded = false;
    fetchState = 'idle';
    view = null;
    forbidden = false;

    if (!payloadHash) return;

    fetchState = 'loading';
    fetchNodePayload(hash, id, token).then((result) => {
      view = result;
      fetchState = 'done';
    }).catch((e) => {
      if (e instanceof ForbiddenError) {
        forbidden = true;
        fetchState = 'done';
      } else if (e instanceof NotFoundError) {
        fetchState = 'not-found';
      } else {
        fetchState = 'error';
      }
    });
  });

  async function copyText(text: string) {
    if (typeof navigator !== 'undefined' && navigator.clipboard) {
      await navigator.clipboard.writeText(text);
    }
  }

  function getInputText(): string {
    if (!view) return '';
    return asText ? conversationText(view.input) : prettyJSON(view.input);
  }

  function getOutputText(): string {
    if (!view) return '';
    return asText ? conversationText(view.output) : prettyJSON(view.output);
  }
</script>

<div class="payload-panel">
  {#if displayState === 'loading'}
    <div class="payload-skeleton" role="status" aria-live="polite">
      <div class="skeleton-line"></div>
      <div class="skeleton-line skeleton-line--mid"></div>
      <div class="skeleton-line skeleton-line--short"></div>
      <span class="sr-only">Loading…</span>
    </div>
  {:else if displayState === 'disabled'}
    <p class="payload-msg">
      Content viewing is off. Start the daemon with <code class="mono">--allow-payload-access</code> to enable.
    </p>
  {:else if displayState === 'not-found' || displayState === 'empty'}
    <p class="payload-msg">No stored payload for this node.</p>
  {:else if displayState === 'error'}
    <p class="payload-msg payload-msg--error">Failed to load content.</p>
  {:else if (displayState === 'redacted' || displayState === 'ready') && view}
    {#if view.redacted}
      <div class="redacted-notice" aria-label="Content was redacted">
        <span class="redacted-badge">redacted</span>
        <span class="redacted-count">{view.redactions.length} secret{view.redactions.length === 1 ? '' : 's'} redacted</span>
      </div>
    {/if}

    {#if view.input !== undefined && view.input !== null}
      {@const inputText = getInputText()}
      {@const inputResult = truncateAtNewline(inputText, limit)}
      <div class="payload-section">
        <div class="payload-section-header">
          <span class="payload-section-label">{asText ? 'Prompt' : 'Input'}</span>
          <button
            class="copy-btn"
            onclick={() => copyText(inputText)}
            aria-label="Copy input content"
          >
            Copy
          </button>
        </div>
        <pre class="payload-content" class:payload-text={asText} class:mono={!asText}>{expanded ? inputText : inputResult.shown}</pre>
        {#if !expanded && inputResult.hasMore}
          {@const n = remainingLineCount(inputResult.remaining)}
          <button class="show-more-btn" onclick={() => { expanded = true; }}>
            +{n} more {n === 1 ? 'line' : 'lines'}
          </button>
        {:else if expanded}
          <button class="show-more-btn" onclick={() => { expanded = false; }}>
            show less
          </button>
        {/if}
      </div>
    {/if}

    {#if view.output !== undefined && view.output !== null}
      {@const outputText = getOutputText()}
      {@const outputResult = truncateAtNewline(outputText, limit)}
      <div class="payload-section">
        <div class="payload-section-header">
          <span class="payload-section-label">{asText ? 'Response' : 'Output'}</span>
          <button
            class="copy-btn"
            onclick={() => copyText(outputText)}
            aria-label="Copy output content"
          >
            Copy
          </button>
        </div>
        <pre class="payload-content" class:payload-text={asText} class:mono={!asText}>{expanded ? outputText : outputResult.shown}</pre>
        {#if !expanded && outputResult.hasMore}
          {@const n = remainingLineCount(outputResult.remaining)}
          <button class="show-more-btn" onclick={() => { expanded = true; }}>
            +{n} more {n === 1 ? 'line' : 'lines'}
          </button>
        {:else if expanded}
          <button class="show-more-btn" onclick={() => { expanded = false; }}>
            show less
          </button>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style>
  .payload-panel {
    border-top: 1px solid var(--border);
    padding: var(--s3) var(--s4);
  }

  .payload-skeleton {
    padding: var(--s2) 0;
    display: flex;
    flex-direction: column;
    gap: var(--s2);
    min-height: 60px;
  }

  .skeleton-line {
    height: 10px;
    background: linear-gradient(90deg, var(--border) 25%, var(--surface) 50%, var(--border) 75%);
    background-size: 200% 100%;
    animation: shimmer 1.2s infinite;
    border-radius: var(--radius-sm);
    width: 100%;
  }

  .skeleton-line--mid {
    width: 75%;
  }

  .skeleton-line--short {
    width: 50%;
  }

  @keyframes shimmer {
    0% { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }

  @media (prefers-reduced-motion: reduce) {
    .skeleton-line {
      animation: none;
      background: var(--border);
    }
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

  .payload-text {
    white-space: pre-wrap;
    word-break: break-word;
    font-family: var(--font-ui);
    color: var(--text);
  }

  .show-more-btn {
    font-size: var(--text-xs);
    color: var(--text-faint);
    background: transparent;
    border: none;
    padding: var(--s1) 0;
    cursor: pointer;
    font-family: var(--font-ui);
    text-align: left;
  }

  .show-more-btn:hover {
    color: var(--text-dim);
  }

  .show-more-btn:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }
</style>
