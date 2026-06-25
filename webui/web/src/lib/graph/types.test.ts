import { describe, it, expect } from 'vitest';
import type { Hierarchy, Aggregate } from './types';

describe('graph view-layer types', () => {
  it('Aggregate is constructible with all fields', () => {
    const agg: Aggregate = {
      count: 3,
      tokensIn: 10,
      tokensOut: 20,
      costUsd: 0.5,
      status: 'ok',
      hasError: false,
    };
    expect(agg.count).toBe(3);
    expect(agg.status).toBe('ok');
  });

  it('Hierarchy shape is implementable', () => {
    const h: Hierarchy = {
      childrenOf: () => [],
      parentOf: () => undefined,
      ancestorsOf: () => [],
      descendantsOf: () => [],
      roots: [],
      orphans: [],
    };
    expect(h.roots).toEqual([]);
    expect(h.childrenOf('x')).toEqual([]);
  });
});
