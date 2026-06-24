import { describe, it, expect } from 'vitest';
import { buildTimeline } from './timeline';
import type { Node } from './types';

function makeNode(overrides: Partial<Node> & { id: string; type: string }): Node {
  return {
    run_id: 'run1',
    rev: 1,
    ...overrides,
  };
}

function getRow(rows: ReturnType<typeof buildTimeline>['rows'], id: string) {
  const row = rows.find((r) => r.id === id);
  expect(row).toBeDefined();
  return row!;
}

describe('buildTimeline', () => {
  it('returns empty model for empty array', () => {
    const result = buildTimeline([]);
    expect(result).toEqual({ rows: [], spanMs: 0, startMs: 0 });
  });

  it('returns empty model when all nodes lack t_start', () => {
    const nodes = [
      makeNode({ id: 'a', type: 'session' }),
      makeNode({ id: 'b', type: 'tool_call' }),
    ];
    const result = buildTimeline(nodes);
    expect(result).toEqual({ rows: [], spanMs: 0, startMs: 0 });
  });

  it('handles single node with t_start and duration_ms', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'tool_call',
        name: 'MyTool',
        t_start: '2024-06-01T10:00:00.000Z',
        t_end: '2024-06-01T10:00:01.000Z',
        duration_ms: 1000,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.rows).toHaveLength(1);
    const row = getRow(result.rows, 'a');
    expect(row.offsetFrac).toBe(0);
    expect(row.widthFrac).toBe(1);
    expect(row.unknownDuration).toBe(false);
    expect(row.label).toBe('MyTool');
    expect(result.spanMs).toBe(1000);
  });

  it('handles single node with t_start but no duration_ms', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'user_prompt',
        t_start: '2024-06-01T10:00:00.000Z',
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.rows).toHaveLength(1);
    const row = getRow(result.rows, 'a');
    expect(row.unknownDuration).toBe(true);
    expect(row.widthFrac).toBe(0.005);
    expect(row.offsetFrac).toBe(0);
  });

  it('computes proportional widths for two nodes', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 1000,
      }),
      makeNode({
        id: 'b',
        type: 'assistant_turn',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 3000,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.spanMs).toBe(3000);
    const rowA = getRow(result.rows, 'a');
    const rowB = getRow(result.rows, 'b');
    expect(rowA.widthFrac).toBeCloseTo(1000 / 3000);
    expect(rowB.widthFrac).toBe(1);
  });

  it('gives tiny marker widthFrac for unknown-duration node', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'user_prompt',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 1000,
      }),
      makeNode({
        id: 'b',
        type: 'marker',
        t_start: '2024-06-01T10:00:00.500Z',
      }),
    ];
    const result = buildTimeline(nodes);
    const markerRow = getRow(result.rows, 'b');
    expect(markerRow.widthFrac).toBe(0.005);
    expect(markerRow.unknownDuration).toBe(true);
  });

  it('hides nodes without timing data', () => {
    const nodes = [
      makeNode({
        id: 'timed',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 500,
      }),
      makeNode({ id: 'untimed', type: 'session' }),
    ];
    const result = buildTimeline(nodes);
    expect(result.rows).toHaveLength(1);
    expect(result.rows[0]?.id).toBe('timed');
  });

  it('clamps tiny duration to at least 0.005 widthFrac', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 10000,
      }),
      makeNode({
        id: 'b',
        type: 'hook_event',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 1,
      }),
    ];
    const result = buildTimeline(nodes);
    const smallRow = getRow(result.rows, 'b');
    expect(smallRow.widthFrac).toBe(0.005);
    expect(smallRow.unknownDuration).toBe(false);
  });

  it('clamps huge duration to widthFrac of 1', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'assistant_turn',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 99999999,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.rows[0]?.widthFrac).toBe(1);
  });

  it('sorts rows by offsetFrac then id', () => {
    const nodes = [
      makeNode({
        id: 'c',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:02.000Z',
        duration_ms: 100,
      }),
      makeNode({
        id: 'b',
        type: 'user_prompt',
        t_start: '2024-06-01T10:00:01.000Z',
        duration_ms: 100,
      }),
      makeNode({
        id: 'a2',
        type: 'assistant_turn',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 100,
      }),
      makeNode({
        id: 'a1',
        type: 'session',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 100,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.rows.map((r) => r.id)).toEqual(['a1', 'a2', 'b', 'c']);
  });

  it('treats zero duration_ms as unknownDuration', () => {
    const nodes = [
      makeNode({
        id: 'a',
        type: 'marker',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 0,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.rows[0]?.unknownDuration).toBe(true);
    expect(result.rows[0]?.widthFrac).toBe(0.005);
  });

  it('uses name as label and falls back to type', () => {
    const nodes = [
      makeNode({
        id: 'named',
        type: 'tool_call',
        name: 'BashTool',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 100,
      }),
      makeNode({
        id: 'unnamed',
        type: 'user_prompt',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 100,
      }),
    ];
    const result = buildTimeline(nodes);
    const namedRow = getRow(result.rows, 'named');
    const unnamedRow = getRow(result.rows, 'unnamed');
    expect(namedRow.label).toBe('BashTool');
    expect(unnamedRow.label).toBe('user_prompt');
  });
});
