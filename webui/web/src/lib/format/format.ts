export function formatDuration(ms?: number): string {
  if (ms === undefined) return '—';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3600000) {
    const m = Math.floor(ms / 60000);
    const s = String(Math.floor((ms % 60000) / 1000)).padStart(2, '0');
    return `${m}m ${s}s`;
  }
  const h = Math.floor(ms / 3600000);
  const m = String(Math.floor((ms % 3600000) / 60000)).padStart(2, '0');
  return `${h}h ${m}m`;
}

export function formatTokens(n?: number): string {
  if (n === undefined) return '—';
  if (n === 0) return '0';
  if (n >= 10000) return `${(n / 1000).toFixed(1)}k`;
  return n.toLocaleString('en-US');
}

export function formatCost(usd?: number): string {
  if (usd === undefined) return '—';
  if (usd === 0) return '$0.00';
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

export function shortHash(h?: string, n = 8): string {
  if (!h) return '—';
  return h.slice(0, n);
}

export function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
}
