import { describe, it, expect } from 'vitest';
import { buildHierarchy } from './hierarchy';
import type { Node, Edge } from '../types';

function n(id: string, parent_id?: string): Node {
  return { id, run_id: 'r1', type: 'marker', rev: 1, ...(parent_id ? { parent_id } : {}) };
}
function e(id: string, src: string, dst: string, type = 'parent_child'): Edge {
  return { id, run_id: 'r1', type, src, dst, rev: 1 };
}

describe('buildHierarchy', () => {
  it('empty input → empty hierarchy', () => {
    const h = buildHierarchy([], []);
    expect(h.roots).toEqual([]);
    expect(h.orphans).toEqual([]);
    expect(h.childrenOf('x')).toEqual([]);
    expect(h.parentOf('x')).toBeUndefined();
    expect(h.ancestorsOf('x')).toEqual([]);
    expect(h.descendantsOf('x')).toEqual([]);
  });

  it('builds parent/children from parent_child edges', () => {
    const nodes = [n('s'), n('a'), n('b')];
    const edges = [e('e1', 's', 'a'), e('e2', 's', 'b')];
    const h = buildHierarchy(nodes, edges);
    expect(h.childrenOf('s')).toEqual(['a', 'b']);
    expect(h.parentOf('a')).toBe('s');
    expect(h.parentOf('b')).toBe('s');
    expect(h.roots).toEqual(['s']);
  });

  it('sorts children ascending by id regardless of edge order', () => {
    const nodes = [n('s'), n('z'), n('a'), n('m')];
    const edges = [e('e1', 's', 'z'), e('e2', 's', 'a'), e('e3', 's', 'm')];
    expect(buildHierarchy(nodes, edges).childrenOf('s')).toEqual(['a', 'm', 'z']);
  });

  it('falls back to node.parent_id when no parent_child edge exists', () => {
    const nodes = [n('s'), n('a', 's')];
    const h = buildHierarchy(nodes, []);
    expect(h.parentOf('a')).toBe('s');
    expect(h.childrenOf('s')).toEqual(['a']);
  });

  it('parent_child edge wins over node.parent_id when both present', () => {
    const nodes = [n('s'), n('t'), n('a', 's')];
    const edges = [e('e1', 't', 'a')];
    expect(buildHierarchy(nodes, edges).parentOf('a')).toBe('t');
  });

  it('ignores parent_id pointing at an absent node', () => {
    const nodes = [n('a', 'ghost')];
    const h = buildHierarchy(nodes, []);
    expect(h.parentOf('a')).toBeUndefined();
    expect(h.orphans).toEqual(['a']);
  });

  it('non-parent_child edges do not create hierarchy links', () => {
    const nodes = [n('a'), n('b')];
    const edges = [e('e1', 'a', 'b', 'sequence'), e('e2', 'a', 'b', 'data_dep')];
    const h = buildHierarchy(nodes, edges);
    expect(h.parentOf('b')).toBeUndefined();
    expect(h.orphans).toEqual(['a', 'b']);
  });

  it('roots are parentless nodes that have children; orphans are parentless and childless', () => {
    const nodes = [n('s'), n('a'), n('lonely')];
    const edges = [e('e1', 's', 'a')];
    const h = buildHierarchy(nodes, edges);
    expect(h.roots).toEqual(['s']);
    expect(h.orphans).toEqual(['lonely']);
  });

  it('ancestorsOf walks nearest-first to the root', () => {
    const nodes = [n('s'), n('a'), n('b')];
    const edges = [e('e1', 's', 'a'), e('e2', 'a', 'b')];
    expect(buildHierarchy(nodes, edges).ancestorsOf('b')).toEqual(['a', 's']);
  });

  it('ancestorsOf is cycle-safe', () => {
    const nodes = [n('a'), n('b')];
    const edges = [e('e1', 'a', 'b'), e('e2', 'b', 'a')];
    const anc = buildHierarchy(nodes, edges).ancestorsOf('a');
    expect(anc).toContain('b');
    expect(new Set(anc).size).toBe(anc.length);
  });

  it('descendantsOf returns the full subtree in pre-order, cycle-safe', () => {
    const nodes = [n('s'), n('a'), n('b'), n('c')];
    const edges = [e('e1', 's', 'a'), e('e2', 's', 'b'), e('e3', 'a', 'c')];
    expect(buildHierarchy(nodes, edges).descendantsOf('s')).toEqual(['a', 'c', 'b']);
  });

  it('descendantsOf of a leaf is empty; of an unknown id is empty', () => {
    const nodes = [n('s'), n('a')];
    const edges = [e('e1', 's', 'a')];
    const h = buildHierarchy(nodes, edges);
    expect(h.descendantsOf('a')).toEqual([]);
    expect(h.descendantsOf('ghost')).toEqual([]);
  });

  it('roots are sorted when several disjoint trees exist', () => {
    const nodes = [n('z'), n('zc'), n('a'), n('ac')];
    const edges = [e('e1', 'z', 'zc'), e('e2', 'a', 'ac')];
    expect(buildHierarchy(nodes, edges).roots).toEqual(['a', 'z']);
  });

  it('ignores parent_child edge whose src or dst is absent from nodes', () => {
    const nodes = [n('a'), n('b')];
    const edgeMissingSrc = [e('e1', 'ghost', 'a')];
    expect(buildHierarchy(nodes, edgeMissingSrc).parentOf('a')).toBeUndefined();
    const edgeMissingDst = [e('e2', 'a', 'ghost')];
    expect(buildHierarchy(nodes, edgeMissingDst).parentOf('a')).toBeUndefined();
  });

  it('descendantsOf is cycle-safe', () => {
    const nodes = [n('a'), n('b'), n('c')];
    const edges = [e('e1', 'a', 'b'), e('e2', 'b', 'c'), e('e3', 'c', 'a')];
    const desc = buildHierarchy(nodes, edges).descendantsOf('a');
    expect(new Set(desc).size).toBe(desc.length);
    expect(desc).toContain('b');
  });
});
