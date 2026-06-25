import { describe, it, expect } from 'vitest';
import { buildOutlineHierarchy } from './outline-tree';
import type { Node, Edge } from '../types';

function n(id: string, type: string, extras: Partial<Node> = {}): Node {
  return { id, run_id: 'r1', type, rev: 1, ...extras };
}
function e(src: string, dst: string, runId = 'r1'): Edge {
  return { id: `${src}->${dst}`, run_id: runId, type: 'parent_child', src, dst, rev: 1 };
}

describe('buildOutlineHierarchy', () => {
  it('empty input → empty hierarchy', () => {
    const h = buildOutlineHierarchy([], []);
    expect(h.roots).toEqual([]);
    expect(h.orphans).toEqual([]);
    expect(h.childrenOf('x')).toEqual([]);
    expect(h.parentOf('x')).toBeUndefined();
    expect(h.ancestorsOf('x')).toEqual([]);
    expect(h.descendantsOf('x')).toEqual([]);
  });

  it('keeps real edges: tool stays under its turn', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:01Z' }),
      n('tc', 'tool_call', { t_start: '2026-06-20T10:00:02Z' }),
    ];
    const edges = [e('s', 'p'), e('t', 'tc')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('tc')).toBe('t');
    expect(h.childrenOf('t')).toEqual(['tc']);
  });

  it('synthesizes a parentless turn under the preceding prompt', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:05Z' }),
    ];
    const edges = [e('s', 'p')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBe('p');
    expect(h.parentOf('p')).toBe('s');
    expect(h.childrenOf('p')).toEqual(['t']);
    expect(h.roots).toEqual(['s']);
  });

  it('turn before the first prompt is parented to the session', () => {
    const nodes = [
      n('s', 'session'),
      n('t', 'assistant_turn', { t_start: '2026-06-20T09:59:00Z' }),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
    ];
    const edges = [e('s', 'p')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBe('s');
    expect(new Set(h.childrenOf('s'))).toEqual(new Set(['p', 't']));
  });

  it('chooses the latest prompt at or before the turn key', () => {
    const nodes = [
      n('s', 'session'),
      n('p1', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('p2', 'user_prompt', { t_start: '2026-06-20T11:00:00Z' }),
      n('tA', 'assistant_turn', { t_start: '2026-06-20T10:30:00Z' }),
      n('tB', 'assistant_turn', { t_start: '2026-06-20T11:30:00Z' }),
    ];
    const edges = [e('s', 'p1'), e('s', 'p2')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('tA')).toBe('p1');
    expect(h.parentOf('tB')).toBe('p2');
  });

  it('equal keys: turn at exactly the prompt key attaches to that prompt', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:00Z' }),
    ];
    const edges = [e('s', 'p')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBe('p');
  });

  it('falls back to id ordering when t_start is absent (ULIDs sort chronologically)', () => {
    const nodes = [
      n('s', 'session'),
      n('id01', 'user_prompt'),
      n('id03', 'user_prompt'),
      n('id02', 'assistant_turn'),
      n('id04', 'assistant_turn'),
    ];
    const edges = [e('s', 'id01'), e('s', 'id03')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('id02')).toBe('id01');
    expect(h.parentOf('id04')).toBe('id03');
  });

  it('key is a plain string compare: an id-keyed prompt after the turn key does not qualify', () => {
    const nodes = [
      n('s', 'session'),
      n('zzz', 'user_prompt'),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:00Z' }),
    ];
    const edges = [e('s', 'zzz')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBe('s');
  });

  it('tie-break by id when two prompts share the same t_start', () => {
    const nodes = [
      n('s', 'session'),
      n('pB', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('pA', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:00Z' }),
    ];
    const edges = [e('s', 'pA'), e('s', 'pB')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBe('pB');
  });

  it('parentless user_prompt is parented to its session', () => {
    const nodes = [n('s', 'session'), n('p', 'user_prompt')];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.parentOf('p')).toBe('s');
    expect(h.childrenOf('s')).toEqual(['p']);
  });

  it('other parentless node (marker) is parented to its session', () => {
    const nodes = [n('s', 'session'), n('m', 'marker')];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.parentOf('m')).toBe('s');
    expect(h.orphans).toEqual([]);
    expect(h.roots).toEqual(['s']);
  });

  it('parentless tool with no turn is parented to its session', () => {
    const nodes = [n('s', 'session'), n('tc', 'tool_call')];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.parentOf('tc')).toBe('s');
  });

  it('the session node is the root', () => {
    const nodes = [n('s', 'session'), n('p', 'user_prompt')];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.roots).toEqual(['s']);
    expect(h.parentOf('s')).toBeUndefined();
  });

  it('multiple sessions: each is a root with its own subtree, scoped by run_id', () => {
    const nodes = [
      n('sA', 'session', { run_id: 'rA' }),
      n('pA', 'user_prompt', { run_id: 'rA', t_start: '2026-06-20T10:00:00Z' }),
      n('tA', 'assistant_turn', { run_id: 'rA', t_start: '2026-06-20T10:00:05Z' }),
      n('sB', 'session', { run_id: 'rB' }),
      n('pB', 'user_prompt', { run_id: 'rB', t_start: '2026-06-20T11:00:00Z' }),
      n('tB', 'assistant_turn', { run_id: 'rB', t_start: '2026-06-20T11:00:05Z' }),
    ];
    const edges = [e('sA', 'pA', 'rA'), e('sB', 'pB', 'rB')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.roots).toEqual(['sA', 'sB']);
    expect(h.parentOf('tA')).toBe('pA');
    expect(h.parentOf('tB')).toBe('pB');
    expect(h.parentOf('pA')).toBe('sA');
    expect(h.parentOf('pB')).toBe('sB');
  });

  it('no session node: falls back to the real hierarchy roots', () => {
    const nodes = [
      n('p', 'user_prompt'),
      n('t', 'assistant_turn'),
      n('tc', 'tool_call'),
    ];
    const edges = [e('p', 't'), e('t', 'tc')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('p')).toBeUndefined();
    expect(h.roots).toEqual(['p']);
    expect(h.childrenOf('p')).toEqual(['t']);
    expect(h.childrenOf('t')).toEqual(['tc']);
  });

  it('no session for a run: parentless node in that run stays parentless', () => {
    const nodes = [
      n('sA', 'session', { run_id: 'rA' }),
      n('pA', 'user_prompt', { run_id: 'rA' }),
      n('m', 'marker', { run_id: 'rB' }),
    ];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.parentOf('m')).toBeUndefined();
    expect(h.parentOf('pA')).toBe('sA');
    expect(h.orphans).toContain('m');
    expect(h.roots).toEqual(['sA']);
  });

  it('picks the lexicographically smallest session id when a run has several sessions', () => {
    const nodes = [
      n('sM', 'session', { run_id: 'r1' }),
      n('sA', 'session', { run_id: 'r1' }),
      n('sZ', 'session', { run_id: 'r1' }),
      n('m', 'marker', { run_id: 'r1' }),
    ];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.parentOf('m')).toBe('sA');
  });

  it('parentless turn in a run with no prompts falls back to the session', () => {
    const nodes = [
      n('s', 'session'),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:00Z' }),
    ];
    const h = buildOutlineHierarchy(nodes, []);

    expect(h.parentOf('t')).toBe('s');
  });

  it('cycle guard skips a synthesized parent that is the node\'s own descendant', () => {
    const nodes = [n('s', 'session'), n('m', 'marker')];
    const edges = [e('m', 's')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('s')).toBe('m');
    expect(h.parentOf('m')).toBeUndefined();
    expect(h.roots).toEqual(['m']);
    const anc = h.ancestorsOf('s');
    expect(new Set(anc).size).toBe(anc.length);
  });

  it('descendantsOf is cycle-safe against cyclic real edges', () => {
    const nodes = [n('a', 'marker'), n('b', 'marker'), n('c', 'marker')];
    const edges = [e('a', 'b'), e('b', 'c'), e('c', 'a')];
    const h = buildOutlineHierarchy(nodes, edges);
    const desc = h.descendantsOf('a');

    expect(new Set(desc).size).toBe(desc.length);
    expect(desc).toContain('b');
    expect(desc).toContain('c');
  });

  it('children are ordered by chronological key then id', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t2', 'assistant_turn', { t_start: '2026-06-20T10:00:20Z' }),
      n('t1', 'assistant_turn', { t_start: '2026-06-20T10:00:10Z' }),
      n('t3', 'assistant_turn', { t_start: '2026-06-20T10:00:30Z' }),
    ];
    const edges = [e('s', 'p')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.childrenOf('p')).toEqual(['t1', 't2', 't3']);
  });

  it('children with equal keys order by id', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('tC', 'assistant_turn', { t_start: '2026-06-20T10:00:10Z' }),
      n('tA', 'assistant_turn', { t_start: '2026-06-20T10:00:10Z' }),
      n('tB', 'assistant_turn', { t_start: '2026-06-20T10:00:10Z' }),
    ];
    const edges = [e('s', 'p')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.childrenOf('p')).toEqual(['tA', 'tB', 'tC']);
  });

  it('ancestorsOf walks nearest-first through the synthesized chain', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:05Z' }),
      n('tc', 'tool_call', { t_start: '2026-06-20T10:00:06Z' }),
    ];
    const edges = [e('s', 'p'), e('t', 'tc')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.ancestorsOf('tc')).toEqual(['t', 'p', 's']);
  });

  it('descendantsOf returns the full synthesized subtree in pre-order', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:05Z' }),
      n('tc', 'tool_call', { t_start: '2026-06-20T10:00:06Z' }),
    ];
    const edges = [e('s', 'p'), e('t', 'tc')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.descendantsOf('s')).toEqual(['p', 't', 'tc']);
  });

  it('cycle-safe: never parents a node to one of its own descendants', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt'),
      n('t', 'assistant_turn'),
    ];
    const edges = [e('p', 's'), e('s', 'p')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBeDefined();
    const anc = h.ancestorsOf('t');
    expect(new Set(anc).size).toBe(anc.length);
    const desc = h.descendantsOf('t');
    expect(new Set(desc).size).toBe(desc.length);
  });

  it('does not re-parent a node that already has a real parent', () => {
    const nodes = [
      n('s', 'session'),
      n('p', 'user_prompt', { t_start: '2026-06-20T10:00:00Z' }),
      n('t', 'assistant_turn', { t_start: '2026-06-20T10:00:05Z' }),
    ];
    const edges = [e('s', 'p'), e('s', 't')];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.parentOf('t')).toBe('s');
  });

  it('real-shape session with two prompts and interleaved parentless turns', () => {
    const nodes = [
      n('S', 'session', { t_start: '2026-06-20T10:00:00Z' }),
      n('P1', 'user_prompt', { t_start: '2026-06-20T10:00:01Z' }),
      n('T1', 'assistant_turn', { t_start: '2026-06-20T10:00:02Z' }),
      n('TC1', 'tool_call', { t_start: '2026-06-20T10:00:03Z' }),
      n('T2', 'assistant_turn', { t_start: '2026-06-20T10:00:04Z' }),
      n('TC2', 'tool_call', { t_start: '2026-06-20T10:00:05Z' }),
      n('P2', 'user_prompt', { t_start: '2026-06-20T10:00:06Z' }),
      n('T3', 'assistant_turn', { t_start: '2026-06-20T10:00:07Z' }),
      n('TC3', 'tool_call', { t_start: '2026-06-20T10:00:08Z' }),
      n('T4', 'assistant_turn', { t_start: '2026-06-20T10:00:09Z' }),
    ];
    const edges = [
      e('S', 'P1'),
      e('S', 'P2'),
      e('T1', 'TC1'),
      e('T2', 'TC2'),
      e('T3', 'TC3'),
    ];
    const h = buildOutlineHierarchy(nodes, edges);

    expect(h.roots).toEqual(['S']);
    expect(h.parentOf('P1')).toBe('S');
    expect(h.parentOf('P2')).toBe('S');
    expect(h.parentOf('T1')).toBe('P1');
    expect(h.parentOf('T2')).toBe('P1');
    expect(h.parentOf('T3')).toBe('P2');
    expect(h.parentOf('T4')).toBe('P2');
    expect(h.parentOf('TC1')).toBe('T1');
    expect(h.parentOf('TC2')).toBe('T2');
    expect(h.parentOf('TC3')).toBe('T3');
    expect(h.childrenOf('P1')).toEqual(['T1', 'T2']);
    expect(h.childrenOf('P2')).toEqual(['T3', 'T4']);
    expect(h.childrenOf('T1')).toEqual(['TC1']);
    expect(h.descendantsOf('S')).toEqual([
      'P1', 'T1', 'TC1', 'T2', 'TC2', 'P2', 'T3', 'TC3', 'T4',
    ]);
    expect(h.orphans).toEqual([]);
  });
});
