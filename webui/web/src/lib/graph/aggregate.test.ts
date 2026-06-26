import { describe, it, expect } from 'vitest';
import { aggregateOf, rowAggregate, descendantCount } from './aggregate';
import { buildHierarchy } from './hierarchy';
import type { Node, Edge } from '../types';

function n(id: string, extra: Partial<Node> = {}): Node {
  return { id, run_id: 'r1', type: 'tool_call', rev: 1, ...extra };
}
function e(id: string, src: string, dst: string): Edge {
  return { id, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
}
function index(nodes: Node[]): Record<string, Node> {
  return Object.fromEntries(nodes.map((nd) => [nd.id, nd]));
}

describe('aggregateOf', () => {
  it('a leaf has an empty aggregate', () => {
    const nodes = [n('a')];
    const h = buildHierarchy(nodes, []);
    expect(aggregateOf('a', h, index(nodes))).toEqual({
      count: 0,
      tokensIn: 0,
      tokensOut: 0,
      costUsd: 0,
      status: 'ok',
      hasError: false,
    });
  });

  it('sums tokens and cost across the subtree, ignoring the node itself', () => {
    const nodes = [
      n('p', { tokens_in: 999, tokens_out: 999, cost_usd: 9 }),
      n('a', { tokens_in: 10, tokens_out: 20, cost_usd: 0.1 }),
      n('b', { tokens_in: 5, tokens_out: 7, cost_usd: 0.2 }),
    ];
    const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
    const h = buildHierarchy(nodes, edges);
    expect(aggregateOf('p', h, index(nodes))).toEqual({
      count: 2,
      tokensIn: 15,
      tokensOut: 27,
      costUsd: 0.30000000000000004,
      status: 'ok',
      hasError: false,
    });
  });

  it('treats missing numeric fields as 0', () => {
    const nodes = [n('p'), n('a', { tokens_in: 4 }), n('b')];
    const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
    const h = buildHierarchy(nodes, edges);
    const agg = aggregateOf('p', h, index(nodes));
    expect(agg.tokensIn).toBe(4);
    expect(agg.tokensOut).toBe(0);
    expect(agg.costUsd).toBe(0);
    expect(agg.count).toBe(2);
  });

  it('status precedence: error beats running beats ok', () => {
    const nodes = [
      n('p'),
      n('a', { status: 'ok' }),
      n('b', { status: 'running' }),
      n('c', { status: 'error' }),
    ];
    const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b'), e('e3', 'p', 'c')];
    const h = buildHierarchy(nodes, edges);
    const agg = aggregateOf('p', h, index(nodes));
    expect(agg.status).toBe('error');
    expect(agg.hasError).toBe(true);
  });

  it('running when some running and none errored', () => {
    const nodes = [n('p'), n('a', { status: 'ok' }), n('b', { status: 'running' })];
    const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
    const h = buildHierarchy(nodes, edges);
    const agg = aggregateOf('p', h, index(nodes));
    expect(agg.status).toBe('running');
    expect(agg.hasError).toBe(false);
  });

  it('ok when all descendants ok or statusless', () => {
    const nodes = [n('p'), n('a', { status: 'ok' }), n('b')];
    const edges = [e('e1', 'p', 'a'), e('e2', 'p', 'b')];
    const h = buildHierarchy(nodes, edges);
    expect(aggregateOf('p', h, index(nodes)).status).toBe('ok');
  });

  it('rolls up nested subtrees', () => {
    const nodes = [
      n('p'),
      n('a', { tokens_in: 1 }),
      n('a1', { tokens_in: 2, status: 'error' }),
    ];
    const edges = [e('e1', 'p', 'a'), e('e2', 'a', 'a1')];
    const h = buildHierarchy(nodes, edges);
    const agg = aggregateOf('p', h, index(nodes));
    expect(agg.count).toBe(2);
    expect(agg.tokensIn).toBe(3);
    expect(agg.status).toBe('error');
  });

  it('skips descendant ids absent from byId', () => {
    const nodes = [n('p'), n('a', { tokens_in: 4 })];
    const edges = [e('e1', 'p', 'a')];
    const h = buildHierarchy(nodes, edges);
    const partial = { p: nodes[0]! };
    const agg = aggregateOf('p', h, partial);
    expect(agg.count).toBe(0);
    expect(agg.tokensIn).toBe(0);
  });
});

describe('descendantCount', () => {
  it('reads a numeric descendant_count from attrs', () => {
    expect(descendantCount(n('s', { type: 'subagent', attrs: { descendant_count: 5 } }))).toBe(5);
  });

  it('returns 0 when the attr is absent or non-numeric', () => {
    expect(descendantCount(n('s', { type: 'subagent' }))).toBe(0);
    expect(descendantCount(n('s', { type: 'subagent', attrs: { descendant_count: 'x' } }))).toBe(0);
  });
});

describe('rowAggregate', () => {
  it('uses the live descendant sum once the subtree is loaded', () => {
    const nodes = [
      n('s', { type: 'subagent', attrs: { descendant_count: 99 } }),
      n('a', { tokens_in: 10, tokens_out: 20, cost_usd: 0.1 }),
    ];
    const edges = [e('e1', 's', 'a')];
    const h = buildHierarchy(nodes, edges);
    const agg = rowAggregate(nodes[0]!, h, index(nodes));
    expect(agg.count).toBe(1);
    expect(agg.tokensIn).toBe(10);
    expect(agg.tokensOut).toBe(20);
  });

  it('falls back to backend attrs for an unloaded subagent', () => {
    const node = n('s', {
      type: 'subagent',
      attrs: {
        descendant_count: 7,
        descendant_tokens_in: 100,
        descendant_tokens_out: 40,
        descendant_cost_usd: 0.5,
      },
    });
    const h = buildHierarchy([node], []);
    expect(rowAggregate(node, h, index([node]))).toEqual({
      count: 7,
      tokensIn: 100,
      tokensOut: 40,
      costUsd: 0.5,
      status: 'ok',
      hasError: false,
    });
  });

  it('defaults missing token/cost attrs to 0 in the fallback', () => {
    const node = n('s', { type: 'subagent', attrs: { descendant_count: 3 } });
    const h = buildHierarchy([node], []);
    expect(rowAggregate(node, h, index([node]))).toEqual({
      count: 3,
      tokensIn: 0,
      tokensOut: 0,
      costUsd: 0,
      status: 'ok',
      hasError: false,
    });
  });

  it('does not fall back for a non-subagent node without descendants', () => {
    const node = n('p');
    const h = buildHierarchy([node], []);
    expect(rowAggregate(node, h, index([node])).count).toBe(0);
  });

  it('does not fall back for a subagent lacking descendant_count', () => {
    const node = n('s', { type: 'subagent' });
    const h = buildHierarchy([node], []);
    expect(rowAggregate(node, h, index([node])).count).toBe(0);
  });
});
