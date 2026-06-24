<script lang="ts">
  import { connectionState } from './lib/stores/stores.svelte';
</script>

<div class="app-shell">
  <header class="topbar">
    <span class="wordmark">Catacomb</span>
    <span class="conn" data-state={connectionState.status}>{connectionState.status}</span>
  </header>
  <main class="content">
    <slot />
  </main>
</div>
