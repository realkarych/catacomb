<script lang="ts">
  interface Props {
    label: string;
    value: string;
  }
  let { label, value }: Props = $props();
</script>

<div class="metric-row">
  <span class="metric-label">{label}</span>
  <span class="metric-value">{value}</span>
</div>

<style>
  .metric-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--s3);
    padding: var(--s1) 0;
  }

  .metric-label {
    font-size: var(--text-sm);
    color: var(--text-dim);
    flex-shrink: 0;
  }

  .metric-value {
    font-size: var(--text-sm);
    color: var(--text);
    text-align: right;
    font-variant-numeric: tabular-nums;
    word-break: break-all;
  }
</style>
