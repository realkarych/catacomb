import { describe, it, expect } from 'vitest';
import { liftEdges } from './lift-edges';
import { buildHierarchy } from './hierarchy';
import type { Node, Edge } from '../types';

function n(id: string): Node {
  return { id, run_id: 'r1', type: 'marker', rev: 1 };
}
function pc(id: string, src: string, dst: string): Edge {
  return { id, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
}
function dep(id: string, src: string, dst: string, type = 'data_dep'): Edge {
  return { id, run_id: 'r1', type, src, dst, rev: 1 };
}

describe('liftEdges', () => {
  it('keeps edges whose endpoints are both visible', () => {
    const nodes = [n('s'), n('a')];
    const h = buildHierarchy(nodes, [pc('e1', 's', 'a')]);
    const edges = [pc('e1', 's', 'a')];
    const out = liftEdges(edges, new Set(['s', 'a']), h);
    expect(out).toEqual([pc('e1', 's', 'a')]);
  });

  it('lifts a hidden endpoint to its nearest visible ancestor', () => {
    const nodes = [n('s'), n('at'), n('t1'), n('dep')];
    const structure = [pc('e1', 's', 'at'), pc('e2', 'at', 't1'), pc('e3', 's', 'dep')];
    const h = buildHierarchy(nodes, structure);
    const edges = [...structure, dep('d1', 'dep', 't1')];
    const out = liftEdges(edges, new Set(['s', 'at', 'dep']), h);
    const lifted = out.find((e) => e.id === 'd1');
    expect(lifted).toBeDefined();
    expect(lifted!.src).toBe('dep');
    expect(lifted!.dst).toBe('at');
    expect(lifted!.type).toBe('data_dep');
  });

  it('drops a self-edge produced by lifting both endpoints to the same ancestor', () => {
    const nodes = [n('at'), n('t1'), n('t2')];
    const structure = [pc('e1', 'at', 't1'), pc('e2', 'at', 't2')];
    const h = buildHierarchy(nodes, structure);
    const edges = [...structure, dep('d1', 't1', 't2', 'sequence')];
    const out = liftEdges(edges, new Set(['at']), h);
    expect(out.find((e) => e.id === 'd1')).toBeUndefined();
    expect(out.find((e) => e.src === e.dst)).toBeUndefined();
  });

  it('dedupes by (src,dst,type), keeping the first edge id', () => {
    const nodes = [n('at'), n('t1'), n('bt'), n('t2')];
    const structure = [pc('e1', 'at', 't1'), pc('e2', 'bt', 't2')];
    const h = buildHierarchy(nodes, structure);
    const edges = [
      ...structure,
      dep('d1', 't1', 't2', 'data_dep'),
      dep('d2', 't1', 't2', 'data_dep'),
    ];
    const out = liftEdges(edges, new Set(['at', 'bt']), h);
    const deps = out.filter((e) => e.type === 'data_dep');
    expect(deps).toHaveLength(1);
    expect(deps[0]!.id).toBe('d1');
    expect(deps[0]!.src).toBe('at');
    expect(deps[0]!.dst).toBe('bt');
  });

  it('keeps two lifted edges that differ only by type', () => {
    const nodes = [n('at'), n('t1'), n('bt'), n('t2')];
    const structure = [pc('e1', 'at', 't1'), pc('e2', 'bt', 't2')];
    const h = buildHierarchy(nodes, structure);
    const edges = [
      ...structure,
      dep('d1', 't1', 't2', 'data_dep'),
      dep('d2', 't1', 't2', 'sequence'),
    ];
    const out = liftEdges(edges, new Set(['at', 'bt']), h);
    const nonStructural = out.filter((e) => e.type !== 'parent_child');
    expect(nonStructural).toHaveLength(2);
    expect(nonStructural.map((e) => e.type).sort()).toEqual(['data_dep', 'sequence']);
  });

  it('drops an edge whose endpoint has no visible ancestor', () => {
    const nodes = [n('s'), n('orphanParent'), n('orphanChild')];
    const structure = [pc('e1', 'orphanParent', 'orphanChild')];
    const h = buildHierarchy(nodes, structure);
    const edges = [dep('d1', 's', 'orphanChild')];
    const out = liftEdges(edges, new Set(['s']), h);
    expect(out).toEqual([]);
  });

  it('keeps a collapsed subagent connected to the spine via its parent edge', () => {
    const nodes = [n('u'), n('sub'), n('subchild')];
    const structure = [pc('e1', 'u', 'sub'), pc('e2', 'sub', 'subchild')];
    const h = buildHierarchy(nodes, structure);
    const out = liftEdges(structure, new Set(['u', 'sub']), h);
    expect(out.find((e) => e.id === 'e1')).toEqual(pc('e1', 'u', 'sub'));
    expect(out.find((e) => e.id === 'e2')).toBeUndefined();
  });

  it('empty input → empty output', () => {
    const h = buildHierarchy([], []);
    expect(liftEdges([], new Set(), h)).toEqual([]);
  });
});
