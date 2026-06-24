import type { Node } from './types';

export interface TimelineRow {
  id: string;
  label: string;
  offsetFrac: number;
  widthFrac: number;
  unknownDuration: boolean;
  durationMs?: number;
  type: string;
  status?: string;
}

export interface TimelineModel {
  rows: TimelineRow[];
  spanMs: number;
  startMs: number;
}

export function buildTimeline(nodes: Node[]): TimelineModel {
  const timed = nodes.filter((n) => n.t_start !== undefined);
  if (timed.length === 0) {
    return { rows: [], spanMs: 0, startMs: 0 };
  }

  const startMs = Math.min(...timed.map((n) => new Date(n.t_start!).getTime()));
  const endMs = Math.max(
    ...timed.map((n) => {
      const tStartMs = new Date(n.t_start!).getTime();
      if (n.t_end) return new Date(n.t_end).getTime();
      if (n.duration_ms && n.duration_ms > 0) return tStartMs + n.duration_ms;
      return tStartMs;
    })
  );
  const spanMs = Math.max(endMs - startMs, 1);

  const rows: TimelineRow[] = timed.map((n) => {
    const tStartMs = new Date(n.t_start!).getTime();
    const offsetFrac = (tStartMs - startMs) / spanMs;

    let widthFrac: number;
    let unknownDuration: boolean;
    let durationMs: number | undefined;
    if (n.duration_ms !== undefined && n.duration_ms > 0) {
      widthFrac = Math.min(Math.max(n.duration_ms / spanMs, 0.005), 1);
      unknownDuration = false;
      durationMs = n.duration_ms;
    } else {
      widthFrac = 0.005;
      unknownDuration = true;
    }

    return {
      id: n.id,
      label: n.name ?? n.type,
      offsetFrac,
      widthFrac,
      unknownDuration,
      durationMs,
      type: n.type,
      status: n.status,
    };
  });

  rows.sort((a, b) => {
    if (a.offsetFrac !== b.offsetFrac) return a.offsetFrac - b.offsetFrac;
    return a.id.localeCompare(b.id);
  });

  return { rows, spanMs, startMs };
}
