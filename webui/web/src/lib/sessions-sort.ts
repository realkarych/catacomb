import type { SessionSummary } from './types';

export type SortKey = 'started_at' | 'duration_ms' | 'tokens_in' | 'tokens_out' | 'cost_usd' | 'error_count';
export type SortDir = 'asc' | 'desc';

export function filterSessions(sessions: SessionSummary[], query: string): SessionSummary[] {
  if (!query) return sessions;
  const q = query.toLowerCase();
  return sessions.filter(
    s =>
      s.session.toLowerCase().includes(q) ||
      (s.model_id ?? '').toLowerCase().includes(q)
  );
}

export function sortSessions(sessions: SessionSummary[], key: SortKey, dir: SortDir): SessionSummary[] {
  const arr = [...sessions];
  arr.sort((a, b) => {
    const av = a[key] as number | string | undefined;
    const bv = b[key] as number | string | undefined;
    if (av === undefined && bv === undefined) return 0;
    if (av === undefined) return 1;
    if (bv === undefined) return -1;
    if (av < bv) return dir === 'asc' ? -1 : 1;
    if (av > bv) return dir === 'asc' ? 1 : -1;
    return 0;
  });
  return arr;
}
