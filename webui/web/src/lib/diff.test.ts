import { describe, it, expect } from 'vitest';
import { diffCounts, isEmptyDiff, changedFields } from './diff';
import type { DiffResult, DiffDeltas } from './types';

const emptyResult: DiffResult = { added: [], removed: [], changed: [], unchanged: [] };

describe('diffCounts', () => {
  it('all zero on empty result', () => {
    expect(diffCounts(emptyResult)).toEqual({ added: 0, removed: 0, changed: 0, unchanged: 0 });
  });
  it('counts each category', () => {
    const r: DiffResult = {
      added: [{ type: 'tool_call', tool: 'bash', step_key: 'k1', content_key: 'c1' }],
      removed: [],
      changed: [],
      unchanged: [{ type: 'tool_call', tool: 'bash', a_step_key: 'k1', b_step_key: 'k1', a_content_key: 'c1', b_content_key: 'c1', tier: 'step_key' }],
    };
    expect(diffCounts(r)).toEqual({ added: 1, removed: 0, changed: 0, unchanged: 1 });
  });
});

describe('isEmptyDiff', () => {
  it('true when added/removed/changed are all empty', () => {
    expect(isEmptyDiff(emptyResult)).toBe(true);
    expect(isEmptyDiff({ ...emptyResult, unchanged: [{ type: 't', tool: 'x', a_step_key: 'k', b_step_key: 'k', a_content_key: 'c', b_content_key: 'c', tier: 'step_key' }] })).toBe(true);
  });
  it('false when added has items', () => {
    expect(isEmptyDiff({ ...emptyResult, added: [{ type: 't', tool: 'x', step_key: 'k', content_key: 'c' }] })).toBe(false);
  });
  it('false when removed has items', () => {
    expect(isEmptyDiff({ ...emptyResult, removed: [{ type: 't', tool: 'x', step_key: 'k', content_key: 'c' }] })).toBe(false);
  });
  it('false when changed has items', () => {
    const changed = [{ type: 't', tool: 'x', a_step_key: 'k', b_step_key: 'k', a_content_key: 'c', b_content_key: 'c', tier: 'step_key', deltas: {} }];
    expect(isEmptyDiff({ ...emptyResult, changed })).toBe(false);
  });
});

describe('changedFields', () => {
  it('returns empty array for empty deltas', () => {
    expect(changedFields({})).toEqual([]);
  });
  it('returns set field names in order', () => {
    const d: DiffDeltas = {
      cost_usd: { before: 0.01, after: 0.02, delta: 0.01 },
      tokens_in: { before: 100, after: 200, delta: 100 },
    };
    expect(changedFields(d)).toEqual(['cost_usd', 'tokens_in']);
  });
  it('returns all fields when all set', () => {
    const d: DiffDeltas = {
      args: { before: 'a', after: 'b' },
      status: { before: 'ok', after: 'error' },
      cost_usd: { before: 0, after: 1, delta: 1 },
      duration_ms: { before: 100, after: 200, delta: 100 },
      tokens_in: { before: 10, after: 20, delta: 10 },
      tokens_out: { before: 5, after: 10, delta: 5 },
    };
    expect(changedFields(d)).toEqual(['args', 'status', 'cost_usd', 'duration_ms', 'tokens_in', 'tokens_out']);
  });
});
