import { describe, it, expect, vi } from 'vitest';
import {
  fetchSessions,
  fetchSessionGraph,
  fetchSubagentSubtree,
  fetchNodePayload,
  fetchDiff,
  fetchSessionPhases,
  NotFoundError,
  ForbiddenError,
} from './api';
import type { SessionSummary, SseEvent, PayloadView, DiffResult } from './types';

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

describe('fetchSubagentSubtree', () => {
  it('returns events on 200', async () => {
    const events: SseEvent[] = [{ kind: 'node_upsert', rev: 2 }];
    const f = mockFetch(200, events);
    const result = await fetchSubagentSubtree('hash123', 'agent-1', 'mytoken', f);
    expect(result).toEqual(events);
    expect(f).toHaveBeenCalledWith('/v1/sessions/hash123/subagent/agent-1', {
      headers: { Authorization: 'Bearer mytoken' },
    });
  });

  it('encodes hash and agentId in URL', async () => {
    const f = mockFetch(200, []);
    await fetchSubagentSubtree('hash/with/slash', 'agent/x', 'tok', f);
    expect(f).toHaveBeenCalledWith('/v1/sessions/hash%2Fwith%2Fslash/subagent/agent%2Fx', {
      headers: { Authorization: 'Bearer tok' },
    });
  });

  it('throws NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchSubagentSubtree('hash', 'a', 'tok', f)).rejects.toBeInstanceOf(NotFoundError);
  });

  it('throws Error on 500', async () => {
    const f = mockFetch(500, null);
    await expect(fetchSubagentSubtree('hash', 'a', 'tok', f)).rejects.toBeInstanceOf(Error);
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

describe('fetchDiff', () => {
  const sampleResult: DiffResult = {
    added: [],
    removed: [],
    changed: [],
    unchanged: [],
  };

  it('returns DiffResult on 200', async () => {
    const f = mockFetch(200, sampleResult);
    const result = await fetchDiff('hash-a', 'hash-b', 'mytoken', {}, f);
    expect(result).toEqual(sampleResult);
    expect(f).toHaveBeenCalledWith(
      '/v1/diff?a=hash-a&b=hash-b&token=mytoken',
    );
  });

  it('encodes a, b, and token in URL', async () => {
    const f = mockFetch(200, sampleResult);
    await fetchDiff('a/1', 'b/2', 'tok/x', {}, f);
    expect(f).toHaveBeenCalledWith('/v1/diff?a=a%2F1&b=b%2F2&token=tok%2Fx');
  });

  it('throws NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchDiff('a', 'b', 'tok', {}, f)).rejects.toBeInstanceOf(NotFoundError);
  });

  it('throws Error on 500', async () => {
    const f = mockFetch(500, null);
    await expect(fetchDiff('a', 'b', 'tok', {}, f)).rejects.toBeInstanceOf(Error);
  });
});

describe('fetchDiff phase params', () => {
  const sampleResult: DiffResult = { added: [], removed: [], changed: [], unchanged: [] };

  it('appends aPhase and bPhase when set', async () => {
    const f = mockFetch(200, sampleResult);
    await fetchDiff('a', 'b', 'tok', { aPhase: 'plan', bPhase: 'impl,1' }, f);
    expect(f).toHaveBeenCalledWith('/v1/diff?a=a&b=b&token=tok&aPhase=plan&bPhase=impl%2C1');
  });

  it('omits phase params when not set', async () => {
    const f = mockFetch(200, sampleResult);
    await fetchDiff('a', 'b', 'tok', {}, f);
    expect(f).toHaveBeenCalledWith('/v1/diff?a=a&b=b&token=tok');
  });

  it('throws on 400 (invalid/absent phase)', async () => {
    const f = mockFetch(400, null);
    await expect(fetchDiff('a', 'b', 'tok', { aPhase: 'ghost' }, f)).rejects.toThrow(/phase/);
  });
});

describe('fetchSessionPhases', () => {
  function nodeEvent(type: string, name?: string): SseEvent {
    return { kind: 'node_upsert', rev: 1, node: { id: type + (name ?? ''), run_id: 'r', type, name, rev: 1 } };
  }

  it('returns de-duplicated marker names in order', async () => {
    const events: SseEvent[] = [
      nodeEvent('tool_call', 'Bash'),
      nodeEvent('marker', 'plan'),
      nodeEvent('marker', 'impl'),
      nodeEvent('marker', 'plan'),
      nodeEvent('marker'),
      { kind: 'edge_upsert', rev: 1 },
    ];
    const f = mockFetch(200, events);
    const result = await fetchSessionPhases('h', 'tok', f);
    expect(result).toEqual(['plan', 'impl']);
  });

  it('propagates NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchSessionPhases('h', 'tok', f)).rejects.toBeInstanceOf(NotFoundError);
  });
});
