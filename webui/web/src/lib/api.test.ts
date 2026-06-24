import { describe, it, expect, vi } from 'vitest';
import { fetchSessions, fetchSessionGraph, fetchNodePayload, NotFoundError, ForbiddenError } from './api';
import type { SessionSummary, SseEvent, PayloadView } from './types';

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

  it('encodes hash in URL', async () => {
    const f = mockFetch(200, []);
    await fetchSessionGraph('hash/with/slash', 'tok', f);
    expect(f).toHaveBeenCalledWith('/v1/sessions/hash%2Fwith%2Fslash/graph', {
      headers: { Authorization: 'Bearer tok' },
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

describe('ForbiddenError', () => {
  it('has name ForbiddenError', () => {
    const e = new ForbiddenError('test');
    expect(e.name).toBe('ForbiddenError');
    expect(e.message).toBe('test');
    expect(e).toBeInstanceOf(Error);
  });
});

describe('fetchNodePayload', () => {
  const samplePayload: PayloadView = {
    node_id: 'n1',
    payload_hash: 'abc',
    input: { cmd: 'ls' },
    output: { result: 'ok' },
    redactions: [],
    redacted: false,
  };

  it('returns PayloadView on 200', async () => {
    const f = mockFetch(200, samplePayload);
    const result = await fetchNodePayload('hash123', 'n1', 'mytoken', f);
    expect(result).toEqual(samplePayload);
    expect(f).toHaveBeenCalledWith('/v1/sessions/hash123/nodes/n1/payload', {
      headers: { Authorization: 'Bearer mytoken' },
    });
  });

  it('encodes hash and nodeId in URL', async () => {
    const f = mockFetch(200, samplePayload);
    await fetchNodePayload('hash/special', 'exec1:tool:p1', 'tok', f);
    expect(f).toHaveBeenCalledWith('/v1/sessions/hash%2Fspecial/nodes/exec1%3Atool%3Ap1/payload', {
      headers: { Authorization: 'Bearer tok' },
    });
  });

  it('throws ForbiddenError on 403', async () => {
    const f = mockFetch(403, null);
    await expect(fetchNodePayload('hash', 'n1', 'tok', f)).rejects.toBeInstanceOf(ForbiddenError);
  });

  it('throws NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchNodePayload('hash', 'n1', 'tok', f)).rejects.toBeInstanceOf(NotFoundError);
  });

  it('throws Error on 500', async () => {
    const f = mockFetch(500, null);
    await expect(fetchNodePayload('hash', 'n1', 'tok', f)).rejects.toBeInstanceOf(Error);
  });
});
