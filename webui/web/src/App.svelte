<script lang="ts">
  import { untrack } from 'svelte';
  import { connectionState, handleEvent, upsertSession, selectNode, desync, lastSeenRev, recordParseError, setDesyncStale } from './lib/stores/stores.svelte';
  import { connect } from './lib/sse/client';
  import { fetchSessions, fetchSessionGraph, NotFoundError } from './lib/api';
  import { parseHash } from './lib/router';
  import type { Route } from './lib/router';
  import SessionsList from './components/SessionsList.svelte';
  import SessionView from './components/SessionView.svelte';

  const token = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '').get('token') ?? '';

  const initialHash = typeof window !== 'undefined' ? window.location.hash : '';
  const initialRoute = parseHash(initialHash);
  let route: Route = $state(initialRoute);

  $effect(() => {
    if (typeof window === 'undefined') return;
    function onHashChange() {
      route = parseHash(window.location.hash);
    }
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  });

  $effect(() => {
    selectNode(route.kind === 'session-node' ? route.nodeId : null);
  });

  $effect(() => {
    fetchSessions(token).then((sessions) => {
      for (const s of sessions) {
        upsertSession(s);
      }
    }).catch(() => {});
  });

  const _initSSEHash = initialRoute.kind !== 'list' ? initialRoute.hash : '';
  let connectedHash = $state(_initSSEHash);

  $effect(() => {
    connectedHash = route.kind !== 'list' ? route.hash : '';
  });

  $effect(() => {
    if (!token) return;
    const conn = connect({
      session: connectedHash,
      token,
      onStatus: (s) => {
        connectionState.status = s;
        if (s === 'open') {
          setDesyncStale(false);
        } else if (s === 'error') {
          setDesyncStale(true);
        }
      },
      onEvent: handleEvent,
      onParseError: (_raw, _err) => {
        recordParseError();
      },
      getLastRev: () => untrack(() => lastSeenRev.value),
    });
    return () => conn.close();
  });

  type SessionLoadStatus = 'idle' | 'loading' | 'ok' | 'not-found' | 'error';
  let sessionLoadStatus: SessionLoadStatus = $state('idle');

  $effect(() => {
    const hash = connectedHash;
    if (!hash || !token) {
      sessionLoadStatus = 'idle';
      return;
    }
    sessionLoadStatus = 'loading';
    fetchSessionGraph(hash, token)
      .then((events) => {
        for (const ev of events) {
          handleEvent(ev);
        }
        sessionLoadStatus = 'ok';
      })
      .catch((err) => {
        if (err instanceof NotFoundError) {
          sessionLoadStatus = 'not-found';
        } else {
          sessionLoadStatus = 'error';
        }
      });
  });

  const degradedLabel: Record<string, string> = {
    connecting: 'reconnecting…',
    error: 'disconnected — retrying',
  };
</script>

<div class="app-shell">
  <header class="topbar">
    <span class="wordmark">
      <span class="wordmark-lamp" aria-hidden="true"></span>
      Catacomb
    </span>
    {#if connectionState.status === 'connecting' || connectionState.status === 'error' || desync.stale}
      <span class="conn-pill" data-state={desync.stale && connectionState.status === 'open' ? 'stale' : connectionState.status} role="status" aria-live="polite">
        <span class="conn-dot" aria-hidden="true"></span>
        {#if desync.stale && connectionState.status === 'open'}
          stream data may be stale
        {:else}
          {degradedLabel[connectionState.status] ?? 'reconnecting…'}
        {/if}
      </span>
    {/if}
  </header>
  <main class="content">
    {#if route.kind === 'list'}
      <SessionsList {token} />
    {:else if route.kind === 'session'}
      <SessionView hash={route.hash} loadStatus={sessionLoadStatus} {token} />
    {:else if route.kind === 'session-node'}
      <SessionView hash={route.hash} nodeId={route.nodeId} loadStatus={sessionLoadStatus} {token} />
    {/if}
  </main>
</div>
