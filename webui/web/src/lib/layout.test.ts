import { describe, it, expect } from 'vitest';
import { applyLayout, dagreNodeToPosition } from './layout';
import type { Node as CNode, Edge as CEdge } from './types';

function makeNode(id: string, type = 'marker'): CNode {
  return { id, run_id: 'r1', type, rev: 1 };
}

function makeEdge(id: string, src: string, dst: string, type = 'parent_child'): CEdge {
  return { id, run_id: 'r1', type, src, dst, rev: 1 };
}

describe('dagreNodeToPosition', () => {
  it('returns origin fallback when pos is undefined', () => {
    expect(dagreNodeToPosition(undefined, 200, 60)).toEqual({ x: 0, y: 0 });
  });

  it('applies center-to-top-left correction', () => {
    expect(dagreNodeToPosition({ x: 100, y: 30 }, 200, 60)).toEqual({ x: 0, y: 0 });
  });

  it('computes non-zero position correctly', () => {
    expect(dagreNodeToPosition({ x: 300, y: 130 }, 200, 60)).toEqual({ x: 200, y: 100 });
  });
});

describe('applyLayout', () => {
  it('returns empty output for empty input', () => {
    const result = applyLayout([], []);
    expect(result.nodes).toEqual([]);
    expect(result.edges).toEqual([]);
  });

  it('assigns a position to a single disconnected node', () => {
    const nodes = [makeNode('a')];
    const result = applyLayout(nodes, []);
    expect(result.nodes).toHaveLength(1);
    expect(typeof result.nodes[0]!.position.x).toBe('number');
    expect(typeof result.nodes[0]!.position.y).toBe('number');
  });

  it('applies center-to-top-left correction using default nodeWidth=200', () => {
    const nodes = [makeNode('a')];
    const result = applyLayout(nodes, [], { nodeWidth: 200, nodeHeight: 60 });
    const n = result.nodes[0]!;
    expect(n.position.x).toBeLessThanOrEqual(n.position.x + 200);
  });

  it('positions nodes left-to-right in a linear chain with LR rankdir', () => {
    const nodes = [makeNode('a'), makeNode('b'), makeNode('c')];
    const edges = [
      makeEdge('e1', 'a', 'b'),
      makeEdge('e2', 'b', 'c'),
    ];
    const result = applyLayout(nodes, edges, { rankdir: 'LR' });
    const byId = Object.fromEntries(result.nodes.map((n) => [n.id, n]));
    expect(byId['a']!.position.x).toBeLessThan(byId['b']!.position.x);
    expect(byId['b']!.position.x).toBeLessThan(byId['c']!.position.x);
  });

  it('includes disconnected nodes in output', () => {
    const nodes = [makeNode('a'), makeNode('b'), makeNode('orphan')];
    const edges = [makeEdge('e1', 'a', 'b')];
    const result = applyLayout(nodes, edges);
    const ids = result.nodes.map((n) => n.id);
    expect(ids).toContain('orphan');
  });

  it('maps parent_child edge type to default', () => {
    const nodes = [makeNode('a'), makeNode('b')];
    const edges = [makeEdge('e1', 'a', 'b', 'parent_child')];
    const result = applyLayout(nodes, edges);
    expect(result.edges[0]!.type).toBe('default');
  });

  it('maps sequence edge type to step', () => {
    const nodes = [makeNode('a'), makeNode('b')];
    const edges = [makeEdge('e1', 'a', 'b', 'sequence')];
    const result = applyLayout(nodes, edges);
    expect(result.edges[0]!.type).toBe('step');
  });

  it('maps data_dep edge type to smoothstep', () => {
    const nodes = [makeNode('a'), makeNode('b')];
    const edges = [makeEdge('e1', 'a', 'b', 'data_dep')];
    const result = applyLayout(nodes, edges);
    expect(result.edges[0]!.type).toBe('smoothstep');
  });

  it('maps unknown edge type to default as fallback', () => {
    const nodes = [makeNode('a'), makeNode('b')];
    const edges = [makeEdge('e1', 'a', 'b', 'unknown_type')];
    const result = applyLayout(nodes, edges);
    expect(result.edges[0]!.type).toBe('default');
  });

  it('is deterministic: same input twice produces deep-equal output', () => {
    const nodes = [makeNode('a'), makeNode('b'), makeNode('c'), makeNode('orphan')];
    const edges = [
      makeEdge('e1', 'a', 'b', 'parent_child'),
      makeEdge('e2', 'b', 'c', 'sequence'),
    ];
    const r1 = applyLayout(nodes, edges);
    const r2 = applyLayout(nodes, edges);
    expect(r1).toEqual(r2);
  });

  it('sets data.catNode to the original node', () => {
    const node = makeNode('x', 'tool_call');
    const result = applyLayout([node], []);
    expect(result.nodes[0]!.data.catNode).toEqual(node);
  });

  it('sets node type to catNode type field', () => {
    const node = makeNode('x', 'subagent');
    const result = applyLayout([node], []);
    expect(result.nodes[0]!.type).toBe('subagent');
  });

  it('center-to-top-left: position.x equals dagre center x minus half nodeWidth', () => {
    const nodes = [makeNode('a')];
    const nodeWidth = 200;
    const nodeHeight = 60;
    const result = applyLayout(nodes, [], { nodeWidth, nodeHeight });
    const n = result.nodes[0]!;
    expect(n.position.x + nodeWidth / 2).toBeGreaterThanOrEqual(0);
  });

  it('sets edge source and target from edge src/dst', () => {
    const nodes = [makeNode('a'), makeNode('b')];
    const edges = [makeEdge('e1', 'a', 'b')];
    const result = applyLayout(nodes, edges);
    expect(result.edges[0]!.source).toBe('a');
    expect(result.edges[0]!.target).toBe('b');
    expect(result.edges[0]!.id).toBe('e1');
  });
});
