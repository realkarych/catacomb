import { describe, it, expect } from 'vitest';
import { emptyState, applyDelta } from './reducer';
import type { GraphState } from './reducer';
import type { SseEvent } from '../types';

function fold(deltas: SseEvent[]): GraphState {
  const s = emptyState();
  for (const d of deltas) applyDelta(s, d);
  return s;
}

function permutations<T>(xs: T[]): T[][] {
  if (xs.length <= 1) return [xs];
  const out: T[][] = [];
  xs.forEach((x, i) => {
    const rest = [...xs.slice(0, i), ...xs.slice(i + 1)];
    for (const p of permutations(rest)) out.push([x, ...p]);
  });
  return out;
}

describe('emptyState', () => {
  it('returns empty collections', () => {
    const s = emptyState();
    expect(s.nodes).toEqual({});
    expect(s.edges).toEqual({});
    expect(s.established).toEqual({});
    expect(s.tombstones).toEqual({});
  });
});

describe('node_upsert', () => {
  it('inserts a node and marks it established', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 1,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', rev: 1 },
    });
    expect(s.nodes['n1']).toBeDefined();
    expect(s.nodes['n1']?.name).toBe('Bash');
    expect(s.established['n1']).toBe(true);
  });

  it('drops a stale upsert (rev <= existing established rev)', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'First', rev: 5 },
    });
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 3,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Stale', rev: 3 },
    });
    expect(s.nodes['n1']?.name).toBe('First');
  });

  it('drops an equal-rev duplicate upsert', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'First', rev: 5 },
    });
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Dup', rev: 5 },
    });
    expect(s.nodes['n1']?.name).toBe('First');
  });

  it('replaces when rev is strictly greater than existing established rev', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 2,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Old', rev: 2 },
    });
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 7,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'New', rev: 7 },
    });
    expect(s.nodes['n1']?.name).toBe('New');
    expect(s.nodes['n1']?.rev).toBe(7);
  });

  it('is safe no-op when ev.node is missing', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'node_upsert', rev: 1 });
    expect(s.nodes).toEqual({});
  });
});

describe('node_status', () => {
  it('seeds a partial node when no prior node exists', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: '', status: 'ok', t_end: '2026-01-01T00:00:00Z', duration_ms: 100, rev: 5 },
    });
    expect(s.nodes['n1']).toBeDefined();
    expect(s.nodes['n1']?.status).toBe('ok');
    expect(s.nodes['n1']?.t_end).toBe('2026-01-01T00:00:00Z');
    expect(s.nodes['n1']?.duration_ms).toBe(100);
    expect(s.nodes['n1']?.name).toBeUndefined();
    expect(s.established['n1']).toBeUndefined();
  });

  it('patches status/t_end/duration on existing established node', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 2,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', tokens_in: 10, rev: 2 },
    });
    applyDelta(s, {
      kind: 'node_status',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: '', status: 'done', t_end: '2026-01-01T00:00:01Z', duration_ms: 200, rev: 5 },
    });
    expect(s.nodes['n1']?.status).toBe('done');
    expect(s.nodes['n1']?.t_end).toBe('2026-01-01T00:00:01Z');
    expect(s.nodes['n1']?.duration_ms).toBe(200);
    expect(s.nodes['n1']?.name).toBe('Bash');
    expect(s.nodes['n1']?.tokens_in).toBe(10);
  });

  it('does not apply a stale status patch (rev < existing node rev)', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 10,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', status: 'running', rev: 10 },
    });
    applyDelta(s, {
      kind: 'node_status',
      rev: 3,
      node: { id: 'n1', run_id: 'r', type: '', status: 'stale_status', rev: 3 },
    });
    expect(s.nodes['n1']?.status).toBe('running');
  });

  it('does NOT mark established', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: '', status: 'ok', rev: 5 },
    });
    expect(s.established['n1']).toBeUndefined();
  });

  it('is safe no-op when ev.node is missing', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'node_status', rev: 1 });
    expect(s.nodes).toEqual({});
  });

  it('updates rev on seed node when newer status patch arrives', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 3,
      node: { id: 'n1', run_id: 'r', type: '', status: 'running', rev: 3 },
    });
    expect(s.nodes['n1']?.rev).toBe(3);
    applyDelta(s, {
      kind: 'node_status',
      rev: 6,
      node: { id: 'n1', run_id: 'r', type: '', status: 'done', t_end: '2026-01-01T00:00:05Z', duration_ms: 400, rev: 6 },
    });
    expect(s.nodes['n1']?.rev).toBe(6);
    expect(s.nodes['n1']?.status).toBe('done');
    expect(s.established['n1']).toBeUndefined();
  });

  it('node_status skipped when tombstoned by node_merge', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_merge',
      rev: 10,
      old_id: 'old',
      node: { id: 'new', run_id: 'r', type: 'tool_call', rev: 10 },
    });
    applyDelta(s, {
      kind: 'node_status',
      rev: 5,
      node: { id: 'old', run_id: 'r', type: '', status: 'stale', rev: 5 },
    });
    expect(s.nodes['old']).toBeUndefined();
  });

  it('seeds a partial node with only duration_ms (no status, no t_end)', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 2,
      node: { id: 'n1', run_id: 'r', type: '', duration_ms: 99, rev: 2 },
    });
    expect(s.nodes['n1']?.duration_ms).toBe(99);
    expect(s.nodes['n1']?.status).toBeUndefined();
    expect(s.nodes['n1']?.t_end).toBeUndefined();
  });

  it('patches only duration_ms on an established node (no status or t_end in patch)', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 3,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', status: 'running', rev: 3 },
    });
    applyDelta(s, {
      kind: 'node_status',
      rev: 8,
      node: { id: 'n1', run_id: 'r', type: '', duration_ms: 250, rev: 8 },
    });
    expect(s.nodes['n1']?.duration_ms).toBe(250);
    expect(s.nodes['n1']?.status).toBe('running');
    expect(s.nodes['n1']?.name).toBe('Bash');
  });
});

describe('clobber/rev fix', () => {
  it('node_upsert always overrides an earlier status-only seed regardless of rev', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: '', status: 'ok', rev: 5 },
    });
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 1,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', tokens_in: 10, rev: 1 },
    });
    expect(s.nodes['n1']?.type).toBe('tool_call');
    expect(s.nodes['n1']?.name).toBe('Bash');
    expect(s.nodes['n1']?.tokens_in).toBe(10);
    expect(s.established['n1']).toBe(true);
  });

  it('upsert after status preserves newer status fields from the seed when seed rev > upsert rev', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: '', status: 'done', t_end: '2026-01-01T00:00:01Z', duration_ms: 500, rev: 5 },
    });
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 1,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', rev: 1 },
    });
    expect(s.nodes['n1']?.name).toBe('Bash');
    expect(s.nodes['n1']?.status).toBe('done');
    expect(s.nodes['n1']?.t_end).toBe('2026-01-01T00:00:01Z');
    expect(s.nodes['n1']?.duration_ms).toBe(500);
  });

  it('upsert rev wins for its own status fields when upsert rev > seed rev', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_status',
      rev: 1,
      node: { id: 'n1', run_id: 'r', type: '', status: 'running', rev: 1 },
    });
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 5,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', status: 'complete', rev: 5 },
    });
    expect(s.nodes['n1']?.status).toBe('complete');
    expect(s.nodes['n1']?.name).toBe('Bash');
  });
});

describe('node_merge', () => {
  it('deletes old_id and installs new node established', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 1,
      node: { id: 'old', run_id: 'r', type: 'tool_call', name: 'Orig', rev: 1 },
    });
    applyDelta(s, {
      kind: 'node_merge',
      rev: 3,
      old_id: 'old',
      node: { id: 'new', run_id: 'r', type: 'tool_call', name: 'Merged', rev: 3 },
    });
    expect(s.nodes['old']).toBeUndefined();
    expect(s.established['old']).toBeUndefined();
    expect(s.nodes['new']).toBeDefined();
    expect(s.nodes['new']?.name).toBe('Merged');
    expect(s.established['new']).toBe(true);
  });

  it('installs new node even when old_id is absent', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_merge',
      rev: 3,
      old_id: 'missing',
      node: { id: 'new', run_id: 'r', type: 'tool_call', name: 'Merged', rev: 3 },
    });
    expect(s.nodes['missing']).toBeUndefined();
    expect(s.nodes['new']).toBeDefined();
    expect(s.established['new']).toBe(true);
  });

  it('rewrites edges that reference old_id', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 1,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'old', dst: 'other', rev: 1 },
    });
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 2,
      edge: { id: 'e2', run_id: 'r', type: 'parent_child', src: 'other', dst: 'old', rev: 2 },
    });
    applyDelta(s, {
      kind: 'node_merge',
      rev: 4,
      old_id: 'old',
      node: { id: 'new', run_id: 'r', type: 'tool_call', rev: 4 },
    });
    expect(s.edges['e1']?.src).toBe('new');
    expect(s.edges['e2']?.dst).toBe('new');
  });

  it('is safe no-op when ev.node is missing', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'node_merge', rev: 1, old_id: 'x' });
    expect(s.nodes).toEqual({});
  });

  it('handles old_id === new id (self-merge, idempotent)', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_upsert',
      rev: 1,
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Orig', rev: 1 },
    });
    applyDelta(s, {
      kind: 'node_merge',
      rev: 4,
      old_id: 'n1',
      node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Self', rev: 4 },
    });
    expect(s.nodes['n1']?.name).toBe('Self');
    expect(s.established['n1']).toBe(true);
  });

  it('updates existing tombstone for old_id on repeated merge', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'node_merge',
      rev: 5,
      old_id: 'old',
      node: { id: 'new1', run_id: 'r', type: 'tool_call', rev: 5 },
    });
    applyDelta(s, {
      kind: 'node_merge',
      rev: 10,
      old_id: 'old',
      node: { id: 'new2', run_id: 'r', type: 'tool_call', rev: 10 },
    });
    expect(s.tombstones['node:old']).toBe(10);
    expect(s.nodes['old']).toBeUndefined();
  });
});

describe('edge_upsert', () => {
  it('inserts an edge', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 1,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'n1', dst: 'n2', rev: 1 },
    });
    expect(s.edges['e1']).toBeDefined();
    expect(s.edges['e1']?.src).toBe('n1');
  });

  it('drops a stale edge upsert (rev <= existing)', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 5,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 5 },
    });
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 3,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'c', dst: 'd', rev: 3 },
    });
    expect(s.edges['e1']?.src).toBe('a');
  });

  it('replaces when rev is strictly greater', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 2,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 2 },
    });
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 8,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'x', dst: 'y', rev: 8 },
    });
    expect(s.edges['e1']?.src).toBe('x');
  });

  it('tombstone blocks a stale upsert after edge_delete', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 3,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 3 },
    });
    applyDelta(s, {
      kind: 'edge_delete',
      rev: 6,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 6 },
    });
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 4,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'resurrected', dst: 'b', rev: 4 },
    });
    expect(s.edges['e1']).toBeUndefined();
  });

  it('is safe no-op when ev.edge is missing', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'edge_upsert', rev: 1 });
    expect(s.edges).toEqual({});
  });
});

describe('edge_delete', () => {
  it('removes an existing edge', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_upsert',
      rev: 1,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 1 },
    });
    applyDelta(s, {
      kind: 'edge_delete',
      rev: 2,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 2 },
    });
    expect(s.edges['e1']).toBeUndefined();
    expect(s.tombstones['e1']).toBe(2);
  });

  it('is a no-op for a missing edge id but still records tombstone', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_delete',
      rev: 5,
      edge: { id: 'missing', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 5 },
    });
    expect(s.edges['missing']).toBeUndefined();
    expect(s.tombstones['missing']).toBe(5);
  });

  it('tombstone keeps the max rev across repeated deletes', () => {
    const s = emptyState();
    applyDelta(s, {
      kind: 'edge_delete',
      rev: 3,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 3 },
    });
    applyDelta(s, {
      kind: 'edge_delete',
      rev: 7,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 7 },
    });
    applyDelta(s, {
      kind: 'edge_delete',
      rev: 5,
      edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'a', dst: 'b', rev: 5 },
    });
    expect(s.tombstones['e1']).toBe(7);
  });

  it('is safe no-op when ev.edge is missing', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'edge_delete', rev: 1 });
    expect(s.edges).toEqual({});
    expect(s.tombstones).toEqual({});
  });
});

describe('unknown and lifecycle kinds', () => {
  it('run_started is a no-op', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'run_started', rev: 1, run_id: 'r' });
    expect(s.nodes).toEqual({});
    expect(s.edges).toEqual({});
  });

  it('run_ended is a no-op', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'run_ended', rev: 2, run_id: 'r' });
    expect(s.nodes).toEqual({});
  });

  it('session_ended is a no-op', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'session_ended', rev: 3 });
    expect(s.nodes).toEqual({});
  });

  it('completely unknown kind is a no-op', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'future_kind_v99', rev: 99 });
    expect(s.nodes).toEqual({});
    expect(s.edges).toEqual({});
  });
});

describe('determinism — permutation property test', () => {
  it('any permutation of the same deltas yields the same state', () => {
    const deltas: SseEvent[] = [
      {
        kind: 'node_status',
        rev: 5,
        node: { id: 'n1', run_id: 'r', type: '', status: 'ok', t_end: '2026-01-01T00:00:01Z', rev: 5 },
      },
      {
        kind: 'node_upsert',
        rev: 2,
        node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', rev: 2 },
      },
      {
        kind: 'node_upsert',
        rev: 3,
        node: { id: 'n2', run_id: 'r', type: 'mcp_call', name: 'fetch', rev: 3 },
      },
      {
        kind: 'edge_upsert',
        rev: 4,
        edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'n1', dst: 'n2', rev: 4 },
      },
      {
        kind: 'edge_delete',
        rev: 6,
        edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'n1', dst: 'n2', rev: 6 },
      },
    ];
    const perms = permutations(deltas);
    expect(perms.length).toBe(120);
    const want = fold(perms[0]!);
    for (const p of perms) {
      expect(fold(p)).toEqual(want);
    }
  });

  it('merge + upsert + delete permutations all converge (6-delta set, 720 perms)', () => {
    const deltas: SseEvent[] = [
      {
        kind: 'node_upsert',
        rev: 1,
        node: { id: 'old', run_id: 'r', type: 'tool_call', name: 'OldName', rev: 1 },
      },
      {
        kind: 'node_status',
        rev: 7,
        node: { id: 'old', run_id: 'r', type: '', status: 'done', t_end: '2026-01-02T00:00:00Z', duration_ms: 300, rev: 7 },
      },
      {
        kind: 'node_merge',
        rev: 8,
        old_id: 'old',
        node: { id: 'new', run_id: 'r', type: 'tool_call', name: 'NewName', status: 'done', rev: 8 },
      },
      {
        kind: 'edge_upsert',
        rev: 2,
        edge: { id: 'e2', run_id: 'r', type: 'parent_child', src: 'new', dst: 'n3', rev: 2 },
      },
      {
        kind: 'edge_delete',
        rev: 9,
        edge: { id: 'e2', run_id: 'r', type: 'parent_child', src: 'new', dst: 'n3', rev: 9 },
      },
      {
        kind: 'node_upsert',
        rev: 4,
        node: { id: 'n3', run_id: 'r', type: 'mcp_call', name: 'Fetch', tokens_in: 50, rev: 4 },
      },
    ];
    const perms = permutations(deltas);
    expect(perms.length).toBe(720);
    const want = fold(perms[0]!);
    for (const p of perms) {
      expect(fold(p)).toEqual(want);
    }
  });
});
