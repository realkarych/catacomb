import { describe, it, expect } from 'vitest';
import { markerSpanMembers, markerSpanAggregate } from './marker-span';
import type { Edge } from '../types';

function makeEdge(type: string, src: string, dst: string): Edge {
  return { id: `${src}-${dst}`, run_id: 'r1', type, src, dst, rev: 1 };
}

describe('markerSpanMembers', () => {
  it('returns [] when no edges', () => {
    expect(markerSpanMembers('m1', [])).toEqual([]);
  });

  it('excludes edges of wrong type', () => {
    const edges = [makeEdge('parent', 'm1', 'n1')];
    expect(markerSpanMembers('m1', edges)).toEqual([]);
  });

  it('excludes marker_span edges with wrong src', () => {
    const edges = [makeEdge('marker_span', 'm2', 'n1')];
    expect(markerSpanMembers('m1', edges)).toEqual([]);
  });

  it('returns dst IDs for matching edges', () => {
    const edges = [
      makeEdge('marker_span', 'm1', 'n1'),
      makeEdge('marker_span', 'm1', 'n2'),
      makeEdge('marker_span', 'm2', 'n3'),
      makeEdge('parent', 'm1', 'n4'),
    ];
    expect(markerSpanMembers('m1', edges)).toEqual(['n1', 'n2']);
  });
});

describe('markerSpanAggregate', () => {
  it('returns all zeros for empty memberIds', () => {
    expect(markerSpanAggregate([], {})).toEqual({
      memberCount: 0,
      tokensIn: 0,
      tokensOut: 0,
      costUsd: 0,
      durationMs: 0,
    });
  });

  it('sums all fields for nodes with all fields present', () => {
    const nodes = {
      n1: { tokens_in: 10, tokens_out: 20, cost_usd: 0.5, duration_ms: 1000 },
      n2: { tokens_in: 5, tokens_out: 15, cost_usd: 0.25, duration_ms: 500 },
    };
    expect(markerSpanAggregate(['n1', 'n2'], nodes)).toEqual({
      memberCount: 2,
      tokensIn: 15,
      tokensOut: 35,
      costUsd: 0.75,
      durationMs: 1500,
    });
  });

  it('treats missing tokens_out/cost_usd/duration_ms as 0', () => {
    const nodes = {
      n1: { tokens_in: 10 },
    };
    expect(markerSpanAggregate(['n1'], nodes)).toEqual({
      memberCount: 1,
      tokensIn: 10,
      tokensOut: 0,
      costUsd: 0,
      durationMs: 0,
    });
  });

  it('treats missing tokens_in as 0 when node exists', () => {
    const nodes = {
      n1: { tokens_out: 7, cost_usd: 0.1, duration_ms: 200 },
    };
    expect(markerSpanAggregate(['n1'], nodes)).toEqual({
      memberCount: 1,
      tokensIn: 0,
      tokensOut: 7,
      costUsd: 0.1,
      durationMs: 200,
    });
  });

  it('skips node ids not in nodesById', () => {
    expect(markerSpanAggregate(['missing'], {})).toEqual({
      memberCount: 1,
      tokensIn: 0,
      tokensOut: 0,
      costUsd: 0,
      durationMs: 0,
    });
  });
});
