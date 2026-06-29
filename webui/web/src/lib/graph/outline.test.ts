import { describe, it, expect } from 'vitest';
import { flattenOutline, defaultOutlineCollapsed, outlineLabel, isSystemPrompt } from './outline';
import { buildHierarchy } from './hierarchy';
import type { Node, Edge } from '../types';

function n(id: string, type: string, extras: Partial<Node> = {}): Node {
  return { id, run_id: 'r1', type, rev: 1, ...extras };
}
function e(src: string, dst: string): Edge {
  return { id: `${src}-${dst}`, run_id: 'r1', type: 'parent_child', src, dst, rev: 1 };
}

describe('flattenOutline', () => {
  it('linear tree emits 4 rows in DFS order with correct depths and hasChildren', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt'),
      n('t', 'assistant_turn'),
      n('tc', 'tool_call'),
    ];
    const edges = [e('s', 'p'), e('p', 't'), e('t', 'tc')];
    const h = buildHierarchy(nodes, edges);
    const rows = flattenOutline(nodes, h, new Set());

    expect(rows).toHaveLength(4);
    expect(rows[0]).toMatchObject({ id: 's', depth: 0, hasChildren: true, collapsed: false });
    expect(rows[1]).toMatchObject({ id: 'p', depth: 1, hasChildren: true, collapsed: false });
    expect(rows[2]).toMatchObject({ id: 't', depth: 2, hasChildren: true, collapsed: false });
    expect(rows[3]).toMatchObject({ id: 'tc', depth: 3, hasChildren: false, collapsed: false });
  });

  it('collapsing the prompt hides its children but still emits it', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt'),
      n('t', 'assistant_turn'),
      n('tc', 'tool_call'),
    ];
    const edges = [e('s', 'p'), e('p', 't'), e('t', 'tc')];
    const h = buildHierarchy(nodes, edges);
    const rows = flattenOutline(nodes, h, new Set(['p']));

    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ id: 's', depth: 0, hasChildren: true, collapsed: false });
    expect(rows[1]).toMatchObject({ id: 'p', depth: 1, hasChildren: true, collapsed: true });
  });

  it('siblings emitted in sorted-id (deterministic) order', () => {
    const nodes = [
      n('s', 'session'),
      n('c', 'tool_call'),
      n('a', 'tool_call'),
      n('b', 'tool_call'),
    ];
    const edges = [e('s', 'c'), e('s', 'a'), e('s', 'b')];
    const h = buildHierarchy(nodes, edges);
    const rows = flattenOutline(nodes, h, new Set());

    expect(rows.map((r) => r.id)).toEqual(['s', 'a', 'b', 'c']);
  });

  it('orphan node appended at depth 0 after roots', () => {
    const nodes = [n('s', 'session'), n('p', 'user_prompt'), n('o', 'marker')];
    const edges = [e('s', 'p')];
    const h = buildHierarchy(nodes, edges);
    const rows = flattenOutline(nodes, h, new Set());

    expect(rows.map((r) => r.id)).toEqual(['s', 'p', 'o']);
    expect(rows[2]).toMatchObject({ id: 'o', depth: 0, hasChildren: false, collapsed: false });
  });

  it('id missing from byId is skipped', () => {
    const allNodes = [n('s', 'session'), n('p', 'user_prompt'), n('t', 'assistant_turn')];
    const edges = [e('s', 'p'), e('p', 't')];
    const h = buildHierarchy(allNodes, edges);

    const subsetNodes = [n('s', 'session'), n('t', 'assistant_turn')];
    const rows = flattenOutline(subsetNodes, h, new Set());

    const ids = rows.map((r) => r.id);
    expect(ids).toContain('s');
    expect(ids).not.toContain('p');
    expect(ids).toContain('t');
  });

  it('cycle-safe: a node reachable via two paths is emitted only once', () => {
    const nodeA = n('a', 'session');
    const nodeB = n('b', 'user_prompt');
    const mockHierarchy = {
      roots: ['a'],
      orphans: ['b'],
      childrenOf: (id: string) => (id === 'a' ? ['b'] : []),
      parentOf: (id: string) => (id === 'b' ? 'a' : undefined),
      ancestorsOf: () => [],
      descendantsOf: () => [],
    };
    const rows = flattenOutline([nodeA, nodeB], mockHierarchy, new Set());

    const ids = rows.map((r) => r.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids).toContain('a');
    expect(ids).toContain('b');
  });

  it('node field on OutlineRow references the original Node object', () => {
    const nd = n('s', 'session');
    const h = buildHierarchy([nd], []);
    const rows = flattenOutline([nd], h, new Set());

    expect(rows[0]?.node).toBe(nd);
  });
});

describe('defaultOutlineCollapsed', () => {
  it('session→prompt→turn: session not in set; prompt+turn-with-children in set', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt'),
      n('t', 'assistant_turn'),
      n('tc', 'tool_call'),
    ];
    const edges = [e('s', 'p'), e('p', 't'), e('t', 'tc')];
    const h = buildHierarchy(nodes, edges);
    const collapsed = defaultOutlineCollapsed(nodes, h);

    expect(collapsed.has('s')).toBe(false);
    expect(collapsed.has('p')).toBe(true);
    expect(collapsed.has('t')).toBe(true);
    expect(collapsed.has('tc')).toBe(false);
  });

  it('childless node is never added to collapsed set', () => {
    const nodes = [n('s', 'session'), n('p', 'user_prompt')];
    const edges = [e('s', 'p')];
    const h = buildHierarchy(nodes, edges);
    const collapsed = defaultOutlineCollapsed(nodes, h);

    expect(collapsed.has('p')).toBe(false);
  });

  it('includes a childless lazy subagent with a positive backend descendant_count', () => {
    const nodes = [n('s', 'session'), n('sa', 'subagent', { attrs: { descendant_count: 4 } })];
    const edges = [e('s', 'sa')];
    const h = buildHierarchy(nodes, edges);
    const collapsed = defaultOutlineCollapsed(nodes, h);

    expect(collapsed.has('sa')).toBe(true);
  });

  it('excludes a childless subagent whose descendant_count is zero', () => {
    const nodes = [n('s', 'session'), n('sa', 'subagent', { attrs: { descendant_count: 0 } })];
    const edges = [e('s', 'sa')];
    const h = buildHierarchy(nodes, edges);
    const collapsed = defaultOutlineCollapsed(nodes, h);

    expect(collapsed.has('sa')).toBe(false);
  });

  it('returns a new Set', () => {
    const nodes = [n('s', 'session'), n('p', 'user_prompt')];
    const edges = [e('s', 'p')];
    const h = buildHierarchy(nodes, edges);
    const a = defaultOutlineCollapsed(nodes, h);
    const b = defaultOutlineCollapsed(nodes, h);

    expect(a).not.toBe(b);
  });
});

describe('outlineLabel', () => {
  it('session: uses node.name or falls back to "session"', () => {
    expect(outlineLabel(n('s', 'session', { name: 'my-session' }))).toEqual({
      primary: 'my-session',
      secondary: '',
    });
    expect(outlineLabel(n('s', 'session'))).toEqual({ primary: 'session', secondary: '' });
  });

  it('user_prompt: primary is "prompt", secondary is empty', () => {
    expect(outlineLabel(n('p', 'user_prompt'))).toEqual({ primary: 'prompt', secondary: '' });
  });

  it('assistant_turn: primary is "assistant", secondary is attrs.model', () => {
    expect(
      outlineLabel(n('t', 'assistant_turn', { attrs: { model: 'claude-3-5-sonnet' } })),
    ).toEqual({ primary: 'assistant', secondary: 'claude-3-5-sonnet' });
  });

  it('assistant_turn: falls back to attrs.model_id when model absent', () => {
    expect(
      outlineLabel(n('t', 'assistant_turn', { attrs: { model_id: 'claude-opus-4' } })),
    ).toEqual({ primary: 'assistant', secondary: 'claude-opus-4' });
  });

  it('assistant_turn: secondary is empty string when neither attrs key present', () => {
    expect(outlineLabel(n('t', 'assistant_turn'))).toEqual({ primary: 'assistant', secondary: '' });
  });

  it('tool_call: uses node.name or falls back to "tool"', () => {
    expect(outlineLabel(n('tc', 'tool_call', { name: 'bash' }))).toEqual({
      primary: 'bash',
      secondary: '',
    });
    expect(outlineLabel(n('tc', 'tool_call'))).toEqual({ primary: 'tool', secondary: '' });
  });

  it('mcp_call: uses node.name or falls back to "mcp"', () => {
    expect(outlineLabel(n('mc', 'mcp_call', { name: 'playwright' }))).toEqual({
      primary: 'playwright',
      secondary: '',
    });
    expect(outlineLabel(n('mc', 'mcp_call'))).toEqual({ primary: 'mcp', secondary: '' });
  });

  it('skill: uses node.name or falls back to "skill"', () => {
    expect(outlineLabel(n('sk', 'skill', { name: 'verify' }))).toEqual({
      primary: 'verify',
      secondary: '',
    });
    expect(outlineLabel(n('sk', 'skill'))).toEqual({ primary: 'skill', secondary: '' });
  });

  it('subagent: primary uses node.name when present', () => {
    expect(
      outlineLabel(n('sa', 'subagent', { name: 'Review PR1 reparent', subagent_type: 'claude-code' })),
    ).toEqual({ primary: 'Review PR1 reparent', secondary: 'claude-code' });
  });

  it('subagent: primary falls back to "subagent" when name absent', () => {
    expect(outlineLabel(n('sa', 'subagent', { subagent_type: 'claude-code' }))).toEqual({
      primary: 'subagent',
      secondary: 'claude-code',
    });
    expect(
      outlineLabel(n('sa', 'subagent', { attrs: { subagent_type: 'general-purpose' } })),
    ).toEqual({ primary: 'subagent', secondary: 'general-purpose' });
    expect(outlineLabel(n('sa', 'subagent'))).toEqual({ primary: 'subagent', secondary: '' });
  });

  it('marker: uses node.name or falls back to "phase"', () => {
    expect(outlineLabel(n('m', 'marker', { name: 'init-phase' }))).toEqual({ primary: 'init-phase', secondary: '' });
    expect(outlineLabel(n('m', 'marker'))).toEqual({ primary: 'phase', secondary: '' });
  });

  it('default: primary is node.name or node.type, secondary is empty', () => {
    expect(outlineLabel(n('h', 'hook_event', { name: 'pre-tool' }))).toEqual({
      primary: 'pre-tool',
      secondary: '',
    });
    expect(outlineLabel(n('h', 'hook_event'))).toEqual({ primary: 'hook_event', secondary: '' });
  });
});

describe('isSystemPrompt', () => {
  it('returns false for a user_prompt with no attrs', () => {
    const nd = n('x', 'user_prompt');
    expect(isSystemPrompt(nd)).toBe(false);
  });
  it('returns false for a user_prompt with prompt_kind=human', () => {
    const nd = n('x', 'user_prompt', { attrs: { prompt_kind: 'human' } });
    expect(isSystemPrompt(nd)).toBe(false);
  });
  it('returns true for a user_prompt with prompt_kind=system', () => {
    const nd = n('x', 'user_prompt', { attrs: { prompt_kind: 'system' } });
    expect(isSystemPrompt(nd)).toBe(true);
  });
  it('returns false for a non-user_prompt node even with prompt_kind=system', () => {
    const nd = n('x', 'assistant_turn', { attrs: { prompt_kind: 'system' } });
    expect(isSystemPrompt(nd)).toBe(false);
  });
  it('returns false for a user_prompt with an unknown prompt_kind value', () => {
    const nd = n('x', 'user_prompt', { attrs: { prompt_kind: 'command' } });
    expect(isSystemPrompt(nd)).toBe(false);
  });
});
