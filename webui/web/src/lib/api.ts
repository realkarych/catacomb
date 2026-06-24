import type { SessionSummary, SseEvent } from './types';

export class NotFoundError extends Error {
  constructor(msg: string) {
    super(msg);
    this.name = 'NotFoundError';
  }
}

export async function fetchSessions(token: string, f = fetch): Promise<SessionSummary[]> {
  const res = await f('/v1/sessions', { headers: { Authorization: `Bearer ${token}` } });
  if (res.status === 404) throw new NotFoundError('sessions not found');
  if (!res.ok) throw new Error(`fetchSessions failed: ${res.status}`);
  return res.json() as Promise<SessionSummary[]>;
}

export async function fetchSessionGraph(hash: string, token: string, f = fetch): Promise<SseEvent[]> {
  const res = await f(`/v1/sessions/${hash}/graph`, { headers: { Authorization: `Bearer ${token}` } });
  if (res.status === 404) throw new NotFoundError(`session ${hash} not found`);
  if (!res.ok) throw new Error(`fetchSessionGraph failed: ${res.status}`);
  return res.json() as Promise<SseEvent[]>;
}
