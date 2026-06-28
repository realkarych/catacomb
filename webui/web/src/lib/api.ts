import type { SessionSummary, SseEvent, PayloadView, DiffResult } from './types';

export class NotFoundError extends Error {
  constructor(msg: string) {
    super(msg);
    this.name = 'NotFoundError';
  }
}

export class ForbiddenError extends Error {
  constructor(msg: string) {
    super(msg);
    this.name = 'ForbiddenError';
  }
}

export async function fetchSessions(token: string, f = fetch): Promise<SessionSummary[]> {
  const res = await f('/v1/sessions', { headers: { Authorization: `Bearer ${token}` } });
  if (res.status === 404) throw new NotFoundError('sessions not found');
  if (!res.ok) throw new Error(`fetchSessions failed: ${res.status}`);
  return res.json() as Promise<SessionSummary[]>;
}

export async function fetchSessionGraph(hash: string, token: string, f = fetch): Promise<SseEvent[]> {
  const res = await f(`/v1/sessions/${encodeURIComponent(hash)}/graph`, { headers: { Authorization: `Bearer ${token}` } });
  if (res.status === 404) throw new NotFoundError(`session ${hash} not found`);
  if (!res.ok) throw new Error(`fetchSessionGraph failed: ${res.status}`);
  return res.json() as Promise<SseEvent[]>;
}

export async function fetchSubagentSubtree(
  hash: string,
  agentId: string,
  token: string,
  f = fetch,
): Promise<SseEvent[]> {
  const res = await f(
    `/v1/sessions/${encodeURIComponent(hash)}/subagent/${encodeURIComponent(agentId)}`,
    { headers: { Authorization: `Bearer ${token}` } },
  );
  if (res.status === 404) throw new NotFoundError(`subagent ${agentId} not found`);
  if (!res.ok) throw new Error(`fetchSubagentSubtree failed: ${res.status}`);
  return res.json() as Promise<SseEvent[]>;
}

export async function fetchNodePayload(
  hash: string,
  nodeId: string,
  token: string,
  f = fetch,
): Promise<PayloadView> {
  const res = await f(`/v1/sessions/${encodeURIComponent(hash)}/nodes/${encodeURIComponent(nodeId)}/payload`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 403) throw new ForbiddenError('payload access disabled');
  if (res.status === 404) throw new NotFoundError(`node ${nodeId} payload not found`);
  if (!res.ok) throw new Error(`fetchNodePayload failed: ${res.status}`);
  return res.json() as Promise<PayloadView>;
}

export async function fetchDiff(a: string, b: string, token: string, f = fetch): Promise<DiffResult> {
  const url = `/v1/diff?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}&token=${encodeURIComponent(token)}`;
  const res = await f(url);
  if (res.status === 404) throw new NotFoundError('session not found');
  if (!res.ok) throw new Error(`fetchDiff failed: ${res.status}`);
  return res.json() as Promise<DiffResult>;
}
