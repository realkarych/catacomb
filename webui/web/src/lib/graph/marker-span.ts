import type { Edge } from '../types';

export interface SpanAggregate {
  memberCount: number;
  tokensIn: number;
  tokensOut: number;
  costUsd: number;
  durationMs: number;
}

export function markerSpanMembers(markerId: string, edges: Edge[]): string[] {
  return edges
    .filter((e) => e.type === 'marker_span' && e.src === markerId)
    .map((e) => e.dst);
}

export function markerSpanAggregate(
  memberIds: string[],
  nodesById: Record<string, { tokens_in?: number; tokens_out?: number; cost_usd?: number; duration_ms?: number }>,
): SpanAggregate {
  let tokensIn = 0;
  let tokensOut = 0;
  let costUsd = 0;
  let durationMs = 0;
  for (const id of memberIds) {
    const n = nodesById[id];
    if (!n) continue;
    tokensIn += n.tokens_in ?? 0;
    tokensOut += n.tokens_out ?? 0;
    costUsd += n.cost_usd ?? 0;
    durationMs += n.duration_ms ?? 0;
  }
  return { memberCount: memberIds.length, tokensIn, tokensOut, costUsd, durationMs };
}
