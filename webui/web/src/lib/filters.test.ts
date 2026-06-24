import { describe, it, expect } from 'vitest';
import { emptyFilter, isActive, filterNodes, dimmedEdgeIds } from './filters';
import type { Node, Edge } from './types';

function makeNode(overrides: Partial<Node> = {}): Node {
  return {
    id: 'n1',
    run_id: 'r1',
    type: 'tool_call',
    status: 'ok',
    name: 'MyTool',
    rev: 1,
    ...overrides,
  };
}

describe('emptyFilter', () => {
  it('returns a filter with all defaults', () => {
    const f = emptyFilter();
    expect(f.query).toBe('');
    expect(f.statuses).toEqual([]);
    expect(f.types).toEqual([]);
    expect(f.hasError).toBe(false);
  });
});

describe('isActive', () => {
  it('returns false for empty filter', () => {
    expect(isActive(emptyFilter())).toBe(false);
  });

  it('returns true when query is non-empty', () => {
    expect(isActive({ ...emptyFilter(), query: 'foo' })).toBe(true);
  });

  it('returns false when query is only whitespace', () => {
    expect(isActive({ ...emptyFilter(), query: '   ' })).toBe(false);
  });

  it('returns true when statuses is non-empty', () => {
    expect(isActive({ ...emptyFilter(), statuses: ['ok'] })).toBe(true);
  });

  it('returns true when types is non-empty', () => {
    expect(isActive({ ...emptyFilter(), types: ['tool_call'] })).toBe(true);
  });

  it('returns true when hasError is true', () => {
    expect(isActive({ ...emptyFilter(), hasError: true })).toBe(true);
  });
});

describe('filterNodes', () => {
  const nodes: Node[] = [
    makeNode({ id: 'n1', type: 'tool_call', status: 'ok', name: 'BashTool' }),
    makeNode({ id: 'n2', type: 'user_prompt', status: 'error', name: 'User Input' }),
    makeNode({ id: 'n3', type: 'assistant_turn', status: 'running', name: 'Assistant Response' }),
    makeNode({ id: 'n4', type: 'tool_call', status: 'ok', name: 'ReadFile' }),
    makeNode({ id: 'n5', type: 'session', status: 'ok', name: 'Root' }),
    makeNode({ id: 'n6', type: 'marker', status: undefined, name: undefined }),
  ];

  it('returns all nodes when filter is inactive', () => {
    const result = filterNodes(nodes, emptyFilter());
    expect(result).toBe(nodes);
  });

  it('filters by hasError — keeps only error status nodes', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), hasError: true });
    expect(result.map((n) => n.id)).toEqual(['n2']);
  });

  it('filters by statuses', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), statuses: ['ok'] });
    expect(result.map((n) => n.id)).toEqual(['n1', 'n4', 'n5']);
  });

  it('filters by statuses — excludes nodes with undefined status', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), statuses: ['ok', 'running'] });
    expect(result.map((n) => n.id)).toEqual(['n1', 'n3', 'n4', 'n5']);
  });

  it('filters by types', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), types: ['tool_call'] });
    expect(result.map((n) => n.id)).toEqual(['n1', 'n4']);
  });

  it('filters by multiple types', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), types: ['tool_call', 'session'] });
    expect(result.map((n) => n.id)).toEqual(['n1', 'n4', 'n5']);
  });

  it('filters by query matching name (case-insensitive)', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), query: 'bash' });
    expect(result.map((n) => n.id)).toEqual(['n1']);
  });

  it('filters by query matching type when name is absent', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), query: 'marker' });
    expect(result.map((n) => n.id)).toEqual(['n6']);
  });

  it('filters by query — partial match', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), query: 'user' });
    expect(result.map((n) => n.id)).toEqual(['n2']);
  });

  it('filters by query — case-insensitive', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), query: 'ROOT' });
    expect(result.map((n) => n.id)).toEqual(['n5']);
  });

  it('filters by query with leading/trailing whitespace', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), query: '  bash  ' });
    expect(result.map((n) => n.id)).toEqual(['n1']);
  });

  it('falls back to id when name and type are absent', () => {
    const idOnlyNode = makeNode({ id: 'unique-xyz', type: undefined as unknown as string, name: undefined });
    const result = filterNodes([idOnlyNode], { ...emptyFilter(), query: 'unique' });
    expect(result.map((n) => n.id)).toEqual(['unique-xyz']);
  });

  it('combines multiple predicates (AND logic)', () => {
    const result = filterNodes(nodes, {
      ...emptyFilter(),
      types: ['tool_call'],
      statuses: ['ok'],
    });
    expect(result.map((n) => n.id)).toEqual(['n1', 'n4']);
  });

  it('returns empty array when no nodes match', () => {
    const result = filterNodes(nodes, { ...emptyFilter(), query: 'zzz-no-match' });
    expect(result).toEqual([]);
  });
});

describe('dimmedEdgeIds', () => {
  function makeEdge(id: string, src: string, dst: string): Edge {
    return { id, run_id: 'r1', type: 'call', src, dst, rev: 1 };
  }

  const edges: Edge[] = [
    makeEdge('e1', 'n1', 'n2'),
    makeEdge('e2', 'n2', 'n3'),
    makeEdge('e3', 'n3', 'n4'),
  ];

  it('returns empty set when all endpoints match', () => {
    const matching = new Set(['n1', 'n2', 'n3', 'n4']);
    expect(dimmedEdgeIds(edges, matching).size).toBe(0);
  });

  it('dims edge when src is not matching', () => {
    const matching = new Set(['n2', 'n3', 'n4']);
    const dimmed = dimmedEdgeIds(edges, matching);
    expect(dimmed.has('e1')).toBe(true);
    expect(dimmed.has('e2')).toBe(false);
    expect(dimmed.has('e3')).toBe(false);
  });

  it('dims edge when dst is not matching', () => {
    const matching = new Set(['n1', 'n2', 'n3']);
    const dimmed = dimmedEdgeIds(edges, matching);
    expect(dimmed.has('e3')).toBe(true);
    expect(dimmed.has('e1')).toBe(false);
    expect(dimmed.has('e2')).toBe(false);
  });

  it('dims edge when both endpoints are not matching', () => {
    const matching = new Set(['n1', 'n2']);
    const dimmed = dimmedEdgeIds(edges, matching);
    expect(dimmed.has('e1')).toBe(false);
    expect(dimmed.has('e2')).toBe(true);
    expect(dimmed.has('e3')).toBe(true);
  });

  it('returns empty set for empty edges', () => {
    expect(dimmedEdgeIds([], new Set(['n1'])).size).toBe(0);
  });

  it('returns all edges dimmed when matching set is empty', () => {
    const dimmed = dimmedEdgeIds(edges, new Set());
    expect(dimmed.size).toBe(edges.length);
  });
});
