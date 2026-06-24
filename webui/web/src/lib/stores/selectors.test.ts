import { describe, it, expect } from 'vitest';
import { upsertById, removeById, sessionGraphFrom } from './selectors';
import { emptyState, applyDelta } from '../reducer/reducer';
import type { Node, Edge } from '../types';

function makeNode(id: string, run_id: string): Node {
  return { id, run_id, type: 'tool_call', rev: 1 };
}

function makeEdge(id: string, run_id: string, src: string, dst: string): Edge {
  return { id, run_id, type: 'parent_child', src, dst, rev: 1 };
}

describe('upsertById', () => {
  it('inserts a new item', () => {
    const map: Record<string, Node> = {};
    const node = makeNode('n1', 'r1');
    const result = upsertById(map, node);
    expect(result['n1']).toEqual(node);
  });

  it('replaces an existing item with the same id', () => {
    const original = makeNode('n1', 'r1');
    const map: Record<string, Node> = { n1: original };
    const updated: Node = { ...original, name: 'Updated' };
    const result = upsertById(map, updated);
    expect(result['n1']?.name).toBe('Updated');
  });

  it('does not mutate the original map', () => {
    const map: Record<string, Node> = {};
    const node = makeNode('n1', 'r1');
    upsertById(map, node);
    expect(Object.keys(map)).toHaveLength(0);
  });

  it('preserves other entries', () => {
    const n1 = makeNode('n1', 'r1');
    const n2 = makeNode('n2', 'r1');
    const map: Record<string, Node> = { n1 };
    const result = upsertById(map, n2);
    expect(result['n1']).toEqual(n1);
    expect(result['n2']).toEqual(n2);
  });
});

describe('removeById', () => {
  it('removes an existing item', () => {
    const node = makeNode('n1', 'r1');
    const map: Record<string, Node> = { n1: node };
    const result = removeById(map, 'n1');
    expect(result['n1']).toBeUndefined();
  });

  it('is a no-op when id is absent', () => {
    const node = makeNode('n1', 'r1');
    const map: Record<string, Node> = { n1: node };
    const result = removeById(map, 'missing');
    expect(result['n1']).toEqual(node);
  });

  it('does not mutate the original map', () => {
    const node = makeNode('n1', 'r1');
    const map: Record<string, Node> = { n1: node };
    removeById(map, 'n1');
    expect(map['n1']).toEqual(node);
  });

  it('returns empty map when only item is removed', () => {
    const node = makeNode('n1', 'r1');
    const map: Record<string, Node> = { n1: node };
    const result = removeById(map, 'n1');
    expect(Object.keys(result)).toHaveLength(0);
  });
});

describe('sessionGraphFrom', () => {
  it('returns empty result for empty runIds', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    const result = sessionGraphFrom(state, new Set());
    expect(result.nodes).toEqual([]);
    expect(result.edges).toEqual([]);
  });

  it('returns nodes for matching runIds', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r2') });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.nodes).toHaveLength(1);
    expect(result.nodes[0]?.id).toBe('n1');
  });

  it('excludes nodes from non-matching runIds', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r2') });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.nodes.map((n) => n.id)).not.toContain('n2');
  });

  it('includes nodes across multiple matching runIds', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r2') });
    applyDelta(state, { kind: 'node_upsert', rev: 3, node: makeNode('n3', 'r3') });
    const result = sessionGraphFrom(state, new Set(['r1', 'r2']));
    expect(result.nodes).toHaveLength(2);
  });

  it('includes edges where both endpoints are in the node set', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r1') });
    applyDelta(state, {
      kind: 'edge_upsert',
      rev: 3,
      edge: makeEdge('e1', 'r1', 'n1', 'n2'),
    });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.edges).toHaveLength(1);
    expect(result.edges[0]?.id).toBe('e1');
  });

  it('excludes edges where src is outside the node set', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r2') });
    applyDelta(state, {
      kind: 'edge_upsert',
      rev: 3,
      edge: makeEdge('e1', 'r1', 'n2', 'n1'),
    });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.edges).toHaveLength(0);
  });

  it('excludes edges where dst is outside the node set', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r2') });
    applyDelta(state, {
      kind: 'edge_upsert',
      rev: 3,
      edge: makeEdge('e1', 'r1', 'n1', 'n2'),
    });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.edges).toHaveLength(0);
  });

  it('returns nodes sorted by id', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 3, node: makeNode('n3', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r1') });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.nodes.map((n) => n.id)).toEqual(['n1', 'n2', 'n3']);
  });

  it('returns edges sorted by id', () => {
    const state = emptyState();
    applyDelta(state, { kind: 'node_upsert', rev: 1, node: makeNode('n1', 'r1') });
    applyDelta(state, { kind: 'node_upsert', rev: 2, node: makeNode('n2', 'r1') });
    applyDelta(state, {
      kind: 'edge_upsert',
      rev: 3,
      edge: makeEdge('e3', 'r1', 'n1', 'n2'),
    });
    applyDelta(state, {
      kind: 'edge_upsert',
      rev: 4,
      edge: makeEdge('e1', 'r1', 'n1', 'n2'),
    });
    applyDelta(state, {
      kind: 'edge_upsert',
      rev: 5,
      edge: makeEdge('e2', 'r1', 'n1', 'n2'),
    });
    const result = sessionGraphFrom(state, new Set(['r1']));
    expect(result.edges.map((e) => e.id)).toEqual(['e1', 'e2', 'e3']);
  });
});
