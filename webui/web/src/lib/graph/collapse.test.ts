import { describe, it, expect } from 'vitest';
import {
  DEFAULT_COLLAPSE,
  defaultCollapsed,
  visibleNodeIds,
  toggle,
  collapseAll,
  expandAll,
} from './collapse';
import { buildHierarchy } from './hierarchy';
import type { Node, Edge } from '../types';

function n(id: string, type: string): Node {
  return { id, run_id: 'r1', type, rev: 1 };
}
function e(id: string, src: string, dst: string): Edge {
  return { id, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
}

function sessionFixture() {
  const nodes = [
    n('s', 'session'),
    n('u', 'user_prompt'),
    n('at', 'assistant_turn'),
    n('t1', 'tool_call'),
    n('t2', 'mcp_call'),
    n('sub', 'subagent'),
    n('subchild', 'assistant_turn'),
  ];
  const edges = [
    e('e1', 's', 'u'),
    e('e2', 'u', 'at'),
    e('e3', 'at', 't1'),
    e('e4', 'at', 't2'),
    e('e5', 'u', 'sub'),
    e('e6', 'sub', 'subchild'),
  ];
  return { nodes, edges, h: buildHierarchy(nodes, edges) };
}

describe('DEFAULT_COLLAPSE predicate', () => {
  it('targets assistant_turn and subagent only', () => {
    expect(DEFAULT_COLLAPSE(n('a', 'assistant_turn'))).toBe(true);
    expect(DEFAULT_COLLAPSE(n('a', 'subagent'))).toBe(true);
    expect(DEFAULT_COLLAPSE(n('a', 'session'))).toBe(false);
    expect(DEFAULT_COLLAPSE(n('a', 'user_prompt'))).toBe(false);
    expect(DEFAULT_COLLAPSE(n('a', 'tool_call'))).toBe(false);
  });
});

describe('defaultCollapsed', () => {
  it('collapses assistant_turn and subagent that have children', () => {
    const { nodes, h } = sessionFixture();
    expect(defaultCollapsed(nodes, h)).toEqual(new Set(['at', 'sub']));
  });

  it('does not collapse a predicate-matching node with no children', () => {
    const nodes = [n('s', 'session'), n('at', 'assistant_turn')];
    const edges = [e('e1', 's', 'at')];
    const h = buildHierarchy(nodes, edges);
    expect(defaultCollapsed(nodes, h)).toEqual(new Set<string>());
  });

  it('honours a swapped predicate', () => {
    const { nodes, h } = sessionFixture();
    const onlyUser = (node: Node) => node.type === 'user_prompt';
    expect(defaultCollapsed(nodes, h, onlyUser)).toEqual(new Set(['u']));
  });
});

describe('visibleNodeIds', () => {
  it('with nothing collapsed every node is visible', () => {
    const { nodes, h } = sessionFixture();
    const vis = visibleNodeIds(nodes, h, new Set());
    expect(vis.size).toBe(nodes.length);
  });

  it('a collapsed node stays visible but its descendants are hidden', () => {
    const { nodes, h } = sessionFixture();
    const vis = visibleNodeIds(nodes, h, new Set(['at']));
    expect(vis.has('at')).toBe(true);
    expect(vis.has('t1')).toBe(false);
    expect(vis.has('t2')).toBe(false);
    expect(vis.has('sub')).toBe(true);
  });

  it('collapsing an ancestor hides the whole subtree including nested groups', () => {
    const { nodes, h } = sessionFixture();
    const vis = visibleNodeIds(nodes, h, new Set(['u']));
    expect(vis.has('u')).toBe(true);
    expect(vis.has('at')).toBe(false);
    expect(vis.has('sub')).toBe(false);
    expect(vis.has('subchild')).toBe(false);
  });

  it('default policy yields the spine (session, prompt, turn, top-level subagent)', () => {
    const { nodes, h } = sessionFixture();
    const vis = visibleNodeIds(nodes, h, defaultCollapsed(nodes, h));
    expect([...vis].sort()).toEqual(['at', 's', 'sub', 'u']);
  });
});

describe('toggle', () => {
  it('adds an absent id and returns a new set', () => {
    const a = new Set(['x']);
    const b = toggle(a, 'y');
    expect(b).toEqual(new Set(['x', 'y']));
    expect(a).toEqual(new Set(['x']));
  });

  it('removes a present id and returns a new set', () => {
    const a = new Set(['x', 'y']);
    const b = toggle(a, 'x');
    expect(b).toEqual(new Set(['y']));
    expect(a).toEqual(new Set(['x', 'y']));
  });
});

describe('collapseAll / expandAll', () => {
  it('collapseAll collapses every node that has children', () => {
    const { nodes, h } = sessionFixture();
    expect(collapseAll(nodes, h)).toEqual(new Set(['s', 'u', 'at', 'sub']));
  });

  it('expandAll is the empty set', () => {
    expect(expandAll()).toEqual(new Set<string>());
  });
});
