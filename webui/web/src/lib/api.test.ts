import { describe, it, expect, vi } from 'vitest';
import { fetchSessions, fetchSessionGraph, NotFoundError } from './api';
import type { SessionSummary, SseEvent } from './types';

function mockFetch(status: number, body: unknown): typeof fetch {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response);
}

describe('fetchSessions', () => {
  it('returns sessions on 200', async () => {
    const sessions: SessionSummary[] = [
      {
        session: 'abc',
        status: 'active',
        tokens_in: 10,
        tokens_out: 20,
        node_count: 1,
        tool_count: 0,
        error_count: 0,
        run_ids: ['r1'],
      },
    ];
    const f = mockFetch(200, sessions);
    const result = await fetchSessions('mytoken', f);
    expect(result).toEqual(sessions);
    expect(f).toHaveBeenCalledWith('/v1/sessions', {
      headers: { Authorization: 'Bearer mytoken' },
    });
  });

  it('throws NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchSessions('tok', f)).rejects.toBeInstanceOf(NotFoundError);
  });

  it('throws Error on 500', async () => {
    const f = mockFetch(500, null);
    await expect(fetchSessions('tok', f)).rejects.toBeInstanceOf(Error);
  });
});

describe('fetchSessionGraph', () => {
  it('returns events on 200', async () => {
    const events: SseEvent[] = [{ kind: 'node_upsert', rev: 1 }];
    const f = mockFetch(200, events);
    const result = await fetchSessionGraph('hash123', 'mytoken', f);
    expect(result).toEqual(events);
    expect(f).toHaveBeenCalledWith('/v1/sessions/hash123/graph', {
      headers: { Authorization: 'Bearer mytoken' },
    });
  });

  it('throws NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchSessionGraph('hash', 'tok', f)).rejects.toBeInstanceOf(NotFoundError);
  });

  it('throws Error on 500', async () => {
    const f = mockFetch(500, null);
    await expect(fetchSessionGraph('hash', 'tok', f)).rejects.toBeInstanceOf(Error);
  });
});

describe('NotFoundError', () => {
  it('has name NotFoundError', () => {
    const e = new NotFoundError('test');
    expect(e.name).toBe('NotFoundError');
    expect(e.message).toBe('test');
    expect(e).toBeInstanceOf(Error);
  });
});
