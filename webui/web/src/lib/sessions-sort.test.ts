import { describe, it, expect } from 'vitest';
import { filterSessions, sortSessions } from './sessions-sort';
import type { SessionSummary } from './types';

function makeSession(overrides: Partial<SessionSummary> = {}): SessionSummary {
  return {
    session: 'abc123def456',
    status: 'ok',
    tokens_in: 100,
    tokens_out: 200,
    node_count: 5,
    tool_count: 3,
    error_count: 0,
    run_ids: [],
    ...overrides,
  };
}

const sessions: SessionSummary[] = [
  makeSession({ session: 'aaa111', model_id: 'claude-opus-4', started_at: '2024-01-03T00:00:00Z', duration_ms: 3000, tokens_in: 300, tokens_out: 600, cost_usd: 0.03, error_count: 2 }),
  makeSession({ session: 'bbb222', model_id: 'claude-sonnet-3-7', started_at: '2024-01-01T00:00:00Z', duration_ms: 1000, tokens_in: 100, tokens_out: 200, cost_usd: 0.01, error_count: 0 }),
  makeSession({ session: 'ccc333', model_id: 'claude-haiku-3-5', started_at: '2024-01-02T00:00:00Z', duration_ms: 2000, tokens_in: 200, tokens_out: 400, cost_usd: undefined, error_count: 1 }),
];

describe('filterSessions', () => {
  it('empty query returns same reference (identity)', () => {
    expect(filterSessions(sessions, '')).toBe(sessions);
  });

  it('matches on session hash substring (case-insensitive)', () => {
    expect(filterSessions(sessions, 'AAA')).toHaveLength(1);
    expect(filterSessions(sessions, 'AAA')[0].session).toBe('aaa111');
  });

  it('matches on model_id substring (case-insensitive)', () => {
    expect(filterSessions(sessions, 'SONNET')).toHaveLength(1);
    expect(filterSessions(sessions, 'SONNET')[0].session).toBe('bbb222');
  });

  it('partial hash match', () => {
    expect(filterSessions(sessions, '111')).toHaveLength(1);
  });

  it('no match returns empty array', () => {
    expect(filterSessions(sessions, 'zzz')).toHaveLength(0);
  });

  it('does not mutate input array', () => {
    const copy = [...sessions];
    filterSessions(sessions, 'aaa');
    expect(sessions).toEqual(copy);
  });

  it('returns new array (not same reference) when query is non-empty', () => {
    const result = filterSessions(sessions, 'aaa');
    expect(result).not.toBe(sessions);
  });

  it('matches multiple sessions', () => {
    expect(filterSessions(sessions, 'claude')).toHaveLength(3);
  });

  it('handles sessions without model_id', () => {
    const s = [makeSession({ session: 'test123', model_id: undefined })];
    expect(filterSessions(s, 'test')).toHaveLength(1);
  });

  it('matches on session when model_id is undefined', () => {
    const s = [makeSession({ session: 'aaa111', model_id: undefined })];
    expect(filterSessions(s, 'aaa')).toHaveLength(1);
  });

  it('no match when neither session nor model_id matches', () => {
    const s = [makeSession({ session: 'xyz', model_id: 'claude-xyz' })];
    expect(filterSessions(s, 'notfound')).toHaveLength(0);
  });

  it('matches on model_id only (not session)', () => {
    const s = [makeSession({ session: 'xyz', model_id: 'claude-sonnet' })];
    const result = filterSessions(s, 'sonnet');
    expect(result).toHaveLength(1);
    expect(result[0].session).toBe('xyz');
  });

  it('matches on session only (not model_id)', () => {
    const s = [makeSession({ session: 'aabbcc', model_id: 'model-xyz' })];
    const result = filterSessions(s, 'aabb');
    expect(result).toHaveLength(1);
    expect(result[0].session).toBe('aabbcc');
  });

  it('filters out session that matches neither field', () => {
    const s = [
      makeSession({ session: 'nomatch1', model_id: 'nomatch2' }),
      makeSession({ session: 'aaa111', model_id: 'claude-opus' }),
    ];
    const result = filterSessions(s, 'aaa');
    expect(result).toHaveLength(1);
    expect(result[0].session).toBe('aaa111');
  });

  it('whitespace query filters correctly', () => {
    const s = [
      makeSession({ session: 'aaa111', model_id: 'claude-opus' }),
      makeSession({ session: 'bbb222', model_id: 'claude-sonnet' }),
    ];
    // Whitespace-only query is not empty, so it should filter
    const result = filterSessions(s, ' ');
    expect(result).toHaveLength(0);
  });

  it('model_id undefined and session no match', () => {
    const s = [makeSession({ session: 'xyz999', model_id: undefined })];
    expect(filterSessions(s, 'nothere')).toHaveLength(0);
  });

  it('query matches model_id when undefined coalesce evaluates', () => {
    const s = [makeSession({ session: 'nomatch', model_id: undefined })];
    // Empty model_id coalesces to '', which won't match anything
    expect(filterSessions(s, 'q')).toHaveLength(0);
  });
});

describe('sortSessions', () => {
  it('sorts started_at ascending', () => {
    const result = sortSessions(sessions, 'started_at', 'asc');
    expect(result.map(s => s.session)).toEqual(['bbb222', 'ccc333', 'aaa111']);
  });

  it('sorts started_at descending', () => {
    const result = sortSessions(sessions, 'started_at', 'desc');
    expect(result.map(s => s.session)).toEqual(['aaa111', 'ccc333', 'bbb222']);
  });

  it('sorts duration_ms ascending', () => {
    const result = sortSessions(sessions, 'duration_ms', 'asc');
    expect(result.map(s => s.session)).toEqual(['bbb222', 'ccc333', 'aaa111']);
  });

  it('sorts duration_ms descending', () => {
    const result = sortSessions(sessions, 'duration_ms', 'desc');
    expect(result.map(s => s.session)).toEqual(['aaa111', 'ccc333', 'bbb222']);
  });

  it('sorts cost_usd ascending; undefined sorts last', () => {
    const result = sortSessions(sessions, 'cost_usd', 'asc');
    expect(result.map(s => s.session)).toEqual(['bbb222', 'aaa111', 'ccc333']);
  });

  it('sorts cost_usd descending; undefined still sorts last', () => {
    const result = sortSessions(sessions, 'cost_usd', 'desc');
    expect(result.map(s => s.session)).toEqual(['aaa111', 'bbb222', 'ccc333']);
  });

  it('sorts tokens_in ascending', () => {
    const result = sortSessions(sessions, 'tokens_in', 'asc');
    expect(result.map(s => s.session)).toEqual(['bbb222', 'ccc333', 'aaa111']);
  });

  it('sorts tokens_out ascending', () => {
    const result = sortSessions(sessions, 'tokens_out', 'asc');
    expect(result.map(s => s.session)).toEqual(['bbb222', 'ccc333', 'aaa111']);
  });

  it('sorts error_count ascending', () => {
    const result = sortSessions(sessions, 'error_count', 'asc');
    expect(result[0].error_count).toBe(0);
    expect(result[result.length - 1].error_count).toBe(2);
  });

  it('sort is stable: equal values preserve order', () => {
    const s1 = makeSession({ session: 'x1', error_count: 0, tokens_in: 50 });
    const s2 = makeSession({ session: 'x2', error_count: 0, tokens_in: 50 });
    const result = sortSessions([s1, s2], 'error_count', 'asc');
    expect(result[0].session).toBe('x1');
    expect(result[1].session).toBe('x2');
  });

  it('does not mutate input array', () => {
    const copy = [...sessions];
    sortSessions(sessions, 'started_at', 'asc');
    expect(sessions).toEqual(copy);
  });

  it('returns new array', () => {
    const result = sortSessions(sessions, 'started_at', 'asc');
    expect(result).not.toBe(sessions);
  });

  it('handles all undefined values', () => {
    const s = [
      makeSession({ session: 's1', cost_usd: undefined }),
      makeSession({ session: 's2', cost_usd: undefined }),
    ];
    const result = sortSessions(s, 'cost_usd', 'asc');
    expect(result).toHaveLength(2);
  });

  it('handles mixed defined and undefined, ascending', () => {
    const s = [
      makeSession({ session: 's1', cost_usd: 0.5 }),
      makeSession({ session: 's2', cost_usd: undefined }),
      makeSession({ session: 's3', cost_usd: 0.1 }),
    ];
    const result = sortSessions(s, 'cost_usd', 'asc');
    expect(result[0].session).toBe('s3');
    expect(result[1].session).toBe('s1');
    expect(result[2].session).toBe('s2');
  });

  it('handles greater-than comparison', () => {
    const s = [
      makeSession({ session: 's1', tokens_in: 100 }),
      makeSession({ session: 's2', tokens_in: 50 }),
    ];
    const result = sortSessions(s, 'tokens_in', 'asc');
    expect(result[0].session).toBe('s2');
    expect(result[1].session).toBe('s1');
  });
});
