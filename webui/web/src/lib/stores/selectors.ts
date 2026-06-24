import type { Node, Edge } from '../types';
import type { GraphState } from '../reducer/reducer';

export function upsertById<T extends { id: string }>(map: Record<string, T>, item: T): Record<string, T> {
  return { ...map, [item.id]: item };
}

export function removeById<T>(map: Record<string, T>, id: string): Record<string, T> {
  const result = { ...map };
  delete result[id];
  return result;
}

export function sessionGraphFrom(
  state: GraphState,
  runIds: ReadonlySet<string>,
): { nodes: Node[]; edges: Edge[] } {
  if (runIds.size === 0) return { nodes: [], edges: [] };

  const nodes = Object.values(state.nodes)
    .filter((n) => runIds.has(n.run_id))
    .sort((a, b) => a.id.localeCompare(b.id));

  const nodeIdSet = new Set(nodes.map((n) => n.id));

  const edges = Object.values(state.edges)
    .filter((e) => nodeIdSet.has(e.src) && nodeIdSet.has(e.dst))
    .sort((a, b) => a.id.localeCompare(b.id));

  return { nodes, edges };
}
