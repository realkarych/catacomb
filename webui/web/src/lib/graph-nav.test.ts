import { describe, it, expect } from 'vitest';
import { nextNodeByDirection } from './graph-nav';
import type { Node, Edge } from './types';

function n(id: string): Node {
  return { id, run_id: 'r1', type: 'marker', rev: 1 };
}

function e(id: string, src: string, dst: string, type = 'parent_child'): Edge {
  return { id, run_id: 'r1', type, src, dst, rev: 1 };
}

describe('nextNodeByDirection', () => {
  describe('empty graph', () => {
    it('returns null for empty nodes regardless of dir and currentId', () => {
      expect(nextNodeByDirection(null, [], [], 'right')).toBe(null);
      expect(nextNodeByDirection('a', [], [], 'left')).toBe(null);
      expect(nextNodeByDirection(null, [], [], 'up')).toBe(null);
      expect(nextNodeByDirection(null, [], [], 'down')).toBe(null);
    });
  });

  describe('currentId === null → layout root', () => {
    it('returns the lowest-id node with no incoming parent_child edge', () => {
      const nodes = [n('b'), n('a'), n('c')];
      const edges = [e('e1', 'a', 'b'), e('e2', 'a', 'c')];
      expect(nextNodeByDirection(null, nodes, edges, 'right')).toBe('a');
    });

    it('all nodes are roots (no edges) → returns lowest id', () => {
      const nodes = [n('z'), n('a'), n('m')];
      expect(nextNodeByDirection(null, nodes, [], 'left')).toBe('a');
    });

    it('returns a node when all have incoming (cycle-like) → lowest id fallback', () => {
      const nodes = [n('b'), n('c')];
      const edges = [e('e1', 'b', 'c'), e('e2', 'c', 'b')];
      expect(nextNodeByDirection(null, nodes, edges, 'down')).toBe('b');
    });
  });

  describe('right — first outgoing-edge target (sorted by target id)', () => {
    it('returns the single outgoing target', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'a', 'b')];
      expect(nextNodeByDirection('a', nodes, edges, 'right')).toBe('b');
    });

    it('deterministic: picks lowest target id when multiple outgoing edges', () => {
      const nodes = [n('a'), n('b'), n('c'), n('d')];
      const edges = [e('e1', 'a', 'd'), e('e2', 'a', 'b'), e('e3', 'a', 'c')];
      expect(nextNodeByDirection('a', nodes, edges, 'right')).toBe('b');
    });

    it('no outgoing edges → returns currentId (no-op)', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'b', 'a')];
      expect(nextNodeByDirection('a', nodes, edges, 'right')).toBe('a');
    });
  });

  describe('left — first incoming-edge source (sorted by source id)', () => {
    it('returns the single incoming source', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'a', 'b')];
      expect(nextNodeByDirection('b', nodes, edges, 'left')).toBe('a');
    });

    it('deterministic: picks lowest source id when multiple incoming edges', () => {
      const nodes = [n('a'), n('b'), n('c'), n('d')];
      const edges = [e('e1', 'c', 'd'), e('e2', 'a', 'd'), e('e3', 'b', 'd')];
      expect(nextNodeByDirection('d', nodes, edges, 'left')).toBe('a');
    });

    it('no incoming edges → returns currentId (no-op)', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'a', 'b')];
      expect(nextNodeByDirection('a', nodes, edges, 'left')).toBe('a');
    });
  });

  describe('up/down — siblings (nodes sharing a parent via parent_child)', () => {
    it('up returns previous sibling (sorted by id)', () => {
      const nodes = [n('root'), n('a'), n('b'), n('c')];
      const edges = [
        e('e1', 'root', 'a'),
        e('e2', 'root', 'b'),
        e('e3', 'root', 'c'),
      ];
      expect(nextNodeByDirection('b', nodes, edges, 'up')).toBe('a');
    });

    it('down returns next sibling (sorted by id)', () => {
      const nodes = [n('root'), n('a'), n('b'), n('c')];
      const edges = [
        e('e1', 'root', 'a'),
        e('e2', 'root', 'b'),
        e('e3', 'root', 'c'),
      ];
      expect(nextNodeByDirection('b', nodes, edges, 'down')).toBe('c');
    });

    it('up on first sibling → no-op (returns currentId)', () => {
      const nodes = [n('root'), n('a'), n('b')];
      const edges = [e('e1', 'root', 'a'), e('e2', 'root', 'b')];
      expect(nextNodeByDirection('a', nodes, edges, 'up')).toBe('a');
    });

    it('down on last sibling → no-op (returns currentId)', () => {
      const nodes = [n('root'), n('a'), n('b')];
      const edges = [e('e1', 'root', 'a'), e('e2', 'root', 'b')];
      expect(nextNodeByDirection('b', nodes, edges, 'down')).toBe('b');
    });

    it('node with no parent_child incoming → no-op for up/down', () => {
      const nodes = [n('a'), n('b')];
      const edges = [e('e1', 'a', 'b', 'sequence')];
      expect(nextNodeByDirection('a', nodes, edges, 'up')).toBe('a');
      expect(nextNodeByDirection('a', nodes, edges, 'down')).toBe('a');
    });

    it('only child (single sibling) → no-op for both up and down', () => {
      const nodes = [n('root'), n('only')];
      const edges = [e('e1', 'root', 'only')];
      expect(nextNodeByDirection('only', nodes, edges, 'up')).toBe('only');
      expect(nextNodeByDirection('only', nodes, edges, 'down')).toBe('only');
    });

    it('sibling ordering is by id (alphabetical), not insertion order', () => {
      const nodes = [n('root'), n('z'), n('a'), n('m')];
      const edges = [
        e('e1', 'root', 'z'),
        e('e2', 'root', 'a'),
        e('e3', 'root', 'm'),
      ];
      expect(nextNodeByDirection('m', nodes, edges, 'up')).toBe('a');
      expect(nextNodeByDirection('m', nodes, edges, 'down')).toBe('z');
    });
  });

  describe('non-parent_child edges are ignored for up/down', () => {
    it('sequence edge does not count as parent for sibling nav', () => {
      const nodes = [n('a'), n('b'), n('c')];
      const edges = [
        e('e1', 'a', 'b', 'sequence'),
        e('e2', 'a', 'c', 'sequence'),
      ];
      expect(nextNodeByDirection('b', nodes, edges, 'up')).toBe('b');
    });
  });

  describe('determinism', () => {
    it('same call twice returns the same value', () => {
      const nodes = [n('a'), n('b'), n('c')];
      const edges = [e('e1', 'a', 'b'), e('e2', 'a', 'c')];
      const r1 = nextNodeByDirection('a', nodes, edges, 'right');
      const r2 = nextNodeByDirection('a', nodes, edges, 'right');
      expect(r1).toBe(r2);
    });
  });

  describe('visible-set filtering', () => {
    it('skips hidden children when navigating right', () => {
      const nodes = [n('s'), n('a'), n('b')];
      const edges = [e('e1', 's', 'a'), e('e2', 's', 'b')];
      const visible = new Set(['s', 'b']);
      expect(nextNodeByDirection('s', nodes, edges, 'right', visible)).toBe('b');
    });

    it('returns currentId when the only target is hidden', () => {
      const nodes = [n('s'), n('a')];
      const edges = [e('e1', 's', 'a')];
      const visible = new Set(['s']);
      expect(nextNodeByDirection('s', nodes, edges, 'right', visible)).toBe('s');
    });

    it('root selection ignores hidden nodes', () => {
      const nodes = [n('hidden'), n('vis')];
      const visible = new Set(['vis']);
      expect(nextNodeByDirection(null, nodes, [], 'down', visible)).toBe('vis');
    });

    it('up/down skip hidden siblings', () => {
      const nodes = [n('root'), n('a'), n('b'), n('c')];
      const edges = [e('e1', 'root', 'a'), e('e2', 'root', 'b'), e('e3', 'root', 'c')];
      const visible = new Set(['root', 'a', 'c']);
      expect(nextNodeByDirection('a', nodes, edges, 'down', visible)).toBe('c');
    });

    it('without a visible set behaves exactly as before', () => {
      const nodes = [n('s'), n('a'), n('b')];
      const edges = [e('e1', 's', 'a'), e('e2', 's', 'b')];
      expect(nextNodeByDirection('s', nodes, edges, 'right')).toBe('a');
    });
  });
});
