<script lang="ts">
  import { connectionState } from './lib/stores/stores.svelte';

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
    <div class="empty-state">
      <div class="empty-state-icon" aria-hidden="true">⛏</div>
      <p class="empty-state-headline">No session selected</p>
      <p class="empty-state-hint">Select a session from the sidebar to view its graph.</p>
    </div>
  </main>
</div>
