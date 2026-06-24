<script lang="ts">
  import { connectionState, handleEvent } from './lib/stores/stores.svelte';
  import { connect } from './lib/sse/client';
  import { parseHash, toHash } from './lib/router';
  import type { Route } from './lib/router';
  import SessionsList from './components/SessionsList.svelte';
  import SessionView from './components/SessionView.svelte';

  const token = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '').get('token') ?? '';

  let route: Route = $state(parseHash(typeof window !== 'undefined' ? window.location.hash : ''));

  $effect(() => {
    if (typeof window === 'undefined') return;
    function onHashChange() {
      route = parseHash(window.location.hash);
    }
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  });

  $effect(() => {
    if (!token) return;
    const sessionHash = route.kind !== 'list' ? route.hash : '';
    const conn = connect({
      session: sessionHash,
      token,
      onStatus: (s) => { connectionState.status = s; },
      onEvent: handleEvent,
    });
    return () => conn.close();
  });

  const statusLabel: Record<string, string> = {
    idle: 'disconnected',
    connecting: 'connecting',
    open: 'connected',
    error: 'disconnected',
  };
</script>

<div class="app-shell">
  <header class="topbar">
    <span class="wordmark">
      <span class="wordmark-lamp" aria-hidden="true"></span>
      Catacomb
    </span>
    <span class="conn-pill" data-state={connectionState.status} role="status" aria-live="polite">
      <span class="conn-dot" aria-hidden="true"></span>
      {statusLabel[connectionState.status] ?? connectionState.status}
    </span>
  </header>
  <main class="content">
    {#if route.kind === 'list'}
      <SessionsList {token} />
    {:else if route.kind === 'session'}
      <SessionView hash={route.hash} />
    {:else if route.kind === 'session-node'}
      <SessionView hash={route.hash} nodeId={route.nodeId} />
    {/if}
  </main>
</div>
