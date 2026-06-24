<script lang="ts">
  import { presentNodeTypes } from '../lib/node-legend';

  interface Props {
    types: string[];
  }

  let { types }: Props = $props();

  const entries = $derived(presentNodeTypes(types));
</script>

{#if entries.length > 0}
  <div class="node-legend" aria-label="Node type legend">
    {#each entries as entry (entry.token)}
      <div class="legend-row">
        <span class="legend-swatch" style="background: var({entry.token});"></span>
        <span class="legend-label">{entry.label}</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .node-legend {
    position: absolute;
    top: var(--s3);
    left: var(--s3);
    z-index: 10;
    background: oklch(0.20 0.009 70 / 0.82);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--s2) var(--s3);
    display: flex;
    flex-direction: column;
    gap: 5px;
    backdrop-filter: blur(4px);
    pointer-events: none;
  }

  .legend-row {
    display: flex;
    align-items: center;
    gap: var(--s2);
  }

  .legend-swatch {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .legend-label {
    font-size: var(--text-xs);
    color: var(--text-dim);
    font-family: var(--font-ui);
    line-height: 1;
  }
</style>
