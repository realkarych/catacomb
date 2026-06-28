<script lang="ts">
  import { sessionGraph, selectNode, selectedNodeId, filteredNodeIds } from '../lib/stores/stores.svelte';
  import { buildTimeline, timelineLabel } from '../lib/timeline';
  import { nodeTypeInfo } from '../lib/node-legend';
  import { formatDuration } from '../lib/format/format';

  interface Props {
    hash: string;
  }
  let { hash }: Props = $props();

  const ROW_H = 26;
  const OVERSCAN = 8;

  const model = $derived(buildTimeline(sessionGraph(hash).nodes));
  const rows = $derived(model.rows);

  const ticks = $derived(() => {
    if (model.spanMs === 0) return [];
    return [0, 0.25, 0.5, 0.75, 1].map((frac) => ({
      frac,
      label: formatDuration(Math.round(model.spanMs * frac)),
    }));
  });

  let scrollEl: HTMLDivElement | undefined = $state();
  let scrollTop = $state(0);
  let viewportH = $state(0);
  let prevHash: string | null = null;

  $effect(() => {
    if (prevHash !== hash) {
      prevHash = hash;
      scrollTop = 0;
      if (scrollEl) scrollEl.scrollTop = 0;
    }
  });

  const startIndex = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN));
  const endIndex = $derived(
    Math.min(rows.length, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN),
  );
  const visibleRows = $derived(rows.slice(startIndex, endIndex));
  const totalHeight = $derived(rows.length * ROW_H);

  function onScroll() {
    if (scrollEl) scrollTop = scrollEl.scrollTop;
  }

  $effect(() => {
    const el = scrollEl;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => {
      viewportH = el.clientHeight;
    });
    ro.observe(el);
    viewportH = el.clientHeight;
    return () => ro.disconnect();
  });
</script>

{#if model.rows.length === 0}
  <p class="timeline-empty">No timing data yet</p>
{:else}
  <div class="timeline-root">
    <div class="timeline-axis" aria-hidden="true">
      {#each ticks() as tick}
        <span class="tick-label" style="left: {tick.frac * 100}%">{tick.label}</span>
      {/each}
    </div>
    <div bind:this={scrollEl} class="timeline-scroll" onscroll={onScroll}>
      <div class="timeline-spacer" style="height: {totalHeight}px;">
        {#each visibleRows as row, i (row.id)}
          {@const info = nodeTypeInfo(row.type)}
          {@const isSelected = selectedNodeId.value === row.id}
          {@const isFilteredOut = filteredNodeIds.value !== null && !filteredNodeIds.value.has(row.id)}
          <button
            class="timeline-row"
            data-selected={isSelected ? 'true' : undefined}
            data-filtered-out={isFilteredOut ? 'true' : undefined}
            style="position: absolute; top: {(startIndex + i) * ROW_H}px; height: {ROW_H}px;{isFilteredOut ? ' opacity: 0.4;' : ''}"
            aria-label="{row.label} ({row.type}){row.unknownDuration ? ', timing unknown' : ', duration ' + formatDuration(row.durationMs)}"
            onclick={() => selectNode(row.id)}
          >
            <span class="row-label">{timelineLabel(row.label)}</span>
            <div class="bar-track">
              {#if row.unknownDuration}
                <span
                  class="bar-marker"
                  data-unknown="true"
                  style="left: {row.offsetFrac * 100}%; background: var({info.token});"
                  aria-label="unknown timing"
                ></span>
              {:else}
                <span
                  class="bar"
                  style="left: {row.offsetFrac * 100}%; width: {row.widthFrac * 100}%; background: var({info.token});"
                ></span>
              {/if}
            </div>
          </button>
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .timeline-empty {
    color: var(--text-faint);
    font-size: var(--text-sm);
    padding: var(--s5);
    text-align: center;
  }

  .timeline-root {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    padding: var(--s3) var(--s4);
  }

  .timeline-axis {
    position: relative;
    height: 20px;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    margin-bottom: var(--s2);
  }

  .tick-label {
    position: absolute;
    transform: translateX(-50%);
    font-size: var(--text-xs);
    color: var(--text-faint);
    white-space: nowrap;
  }

  .timeline-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    position: relative;
  }

  .timeline-spacer {
    position: relative;
    width: 100%;
  }

  .timeline-row {
    display: flex;
    align-items: center;
    gap: var(--s3);
    padding: var(--s1) var(--s2);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
    text-align: left;
    width: 100%;
    box-sizing: border-box;
    transition: background 0.1s, border-color 0.1s;
  }

  .timeline-row:hover {
    background: var(--surface-2);
  }

  .timeline-row[data-selected='true'] {
    background: var(--surface-2);
    border-color: var(--accent);
  }

  .timeline-row:focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }

  .row-label {
    font-size: var(--text-xs);
    color: var(--text-dim);
    white-space: nowrap;
    flex-shrink: 0;
    width: 170px;
  }

  .bar-track {
    position: relative;
    flex: 1;
    height: 12px;
  }

  .bar {
    position: absolute;
    height: 100%;
    border-radius: 2px;
    min-width: 2px;
  }

  .bar-marker {
    position: absolute;
    width: 8px;
    height: 8px;
    top: 50%;
    transform: translateY(-50%) rotate(45deg);
    border-radius: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    .timeline-row {
      transition: none;
    }
  }
</style>
