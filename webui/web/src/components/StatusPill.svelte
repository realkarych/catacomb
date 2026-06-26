<script lang="ts">
  import { displayLabel } from '../lib/status';

  interface Props {
    status: string;
  }
  let { status }: Props = $props();
</script>

<span class="status-pill" data-status={status}>
  <span class="status-dot" aria-hidden="true"></span>
  {displayLabel(status)}
</span>

<style>
  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--text-xs);
    font-weight: 500;
    letter-spacing: 0.03em;
    color: var(--text-faint);
    text-transform: lowercase;
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    flex-shrink: 0;
  }

  .status-pill[data-status='ok'] { color: var(--ok); }
  .status-pill[data-status='running'] { color: var(--running); }
  .status-pill[data-status='error'] { color: var(--error); }
  .status-pill[data-status='blocked'] { color: var(--blocked); }
  .status-pill[data-status='pending'] { color: var(--pending); }
</style>
