import { describe, it, expect } from 'vitest';
import { buildTimeline, timelineLabel } from './timeline';
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

  it('populates durationMs for timed rows and leaves it undefined for unknown-duration rows', () => {
    const nodes = [
      makeNode({
        id: 'timed',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 1500,
      }),
      makeNode({
        id: 'untimed',
        type: 'user_prompt',
        t_start: '2024-06-01T10:00:00.000Z',
      }),
    ];
    const result = buildTimeline(nodes);
    const timedRow = getRow(result.rows, 'timed');
    const untimedRow = getRow(result.rows, 'untimed');
    expect(timedRow.durationMs).toBe(1500);
    expect(untimedRow.durationMs).toBeUndefined();
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

  it('anchors span start to earliest non-session activity, removing leading dead space', () => {
    const nodes = [
      makeNode({
        id: 'sess',
        type: 'session',
        t_start: '2024-06-01T09:00:00.000Z',
        t_end: '2024-06-01T10:00:20.000Z',
      }),
      makeNode({
        id: 'tool',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:10.000Z',
        t_end: '2024-06-01T10:00:11.000Z',
        duration_ms: 1000,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.startMs).toBe(new Date('2024-06-01T10:00:10.000Z').getTime());
    expect(getRow(result.rows, 'tool').offsetFrac).toBe(0);
    expect(getRow(result.rows, 'sess').offsetFrac).toBe(0);
  });

  it('clamps offsetFrac into [0, 1]', () => {
    const nodes = [
      makeNode({
        id: 'sess',
        type: 'session',
        t_start: '2024-06-01T09:00:00.000Z',
        t_end: '2024-06-01T10:00:05.000Z',
      }),
      makeNode({
        id: 'a',
        type: 'tool_call',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 5000,
      }),
    ];
    const result = buildTimeline(nodes);
    for (const row of result.rows) {
      expect(row.offsetFrac).toBeGreaterThanOrEqual(0);
      expect(row.offsetFrac).toBeLessThanOrEqual(1);
    }
  });

  it('falls back to overall min t_start when only session nodes are timed', () => {
    const nodes = [
      makeNode({
        id: 'sess',
        type: 'session',
        t_start: '2024-06-01T10:00:00.000Z',
        duration_ms: 5000,
      }),
    ];
    const result = buildTimeline(nodes);
    expect(result.startMs).toBe(new Date('2024-06-01T10:00:00.000Z').getTime());
    const row = getRow(result.rows, 'sess');
    expect(row.offsetFrac).toBe(0);
    expect(row.widthFrac).toBe(1);
  });
});

describe('timelineLabel', () => {
  it('returns short labels unchanged', () => {
    expect(timelineLabel('BashTool')).toBe('BashTool');
  });

  it('returns a label exactly at the max length unchanged', () => {
    const s = 'x'.repeat(24);
    expect(timelineLabel(s)).toBe(s);
  });

  it('middle-truncates a long MCP label, keeping head and tail', () => {
    const result = timelineLabel('mcp__playwright__browser_navigate');
    expect(result).toContain('…');
    expect(result.startsWith('mcp__')).toBe(true);
    expect(result.endsWith('navigate')).toBe(true);
    expect(result.length).toBe(24);
  });

  it('truncates a label one char over the boundary', () => {
    const result = timelineLabel('a'.repeat(25));
    expect(result.length).toBe(24);
    expect(result).toContain('…');
    expect(result.startsWith('a')).toBe(true);
    expect(result.endsWith('a')).toBe(true);
  });

  it('honors a custom max length', () => {
    expect(timelineLabel('abcdefghij', 5)).toBe('ab…ij');
  });
});
