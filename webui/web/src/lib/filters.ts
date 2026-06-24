import type { Node, Edge } from './types';

export interface FilterState {
  query: string;
  statuses: string[];
  types: string[];
  hasError: boolean;
}

export function emptyFilter(): FilterState {
  return { query: '', statuses: [], types: [], hasError: false };
}

export function isActive(f: FilterState): boolean {
  return (
    f.query.trim().length > 0 ||
    f.statuses.length > 0 ||
    f.types.length > 0 ||
    f.hasError
  );
}

export function dimmedEdgeIds(edges: Edge[], matchingNodeIds: Set<string>): Set<string> {
  const dimmed = new Set<string>();
  for (const e of edges) {
    if (!matchingNodeIds.has(e.src) || !matchingNodeIds.has(e.dst)) {
      dimmed.add(e.id);
    }
  }
  return dimmed;
}

export function filterNodes(nodes: Node[], f: FilterState): Node[] {
  if (!isActive(f)) return nodes;

  const q = f.query.trim().toLowerCase();

  return nodes.filter((n) => {
    if (f.hasError && n.status !== 'error') return false;
    if (f.statuses.length > 0 && !f.statuses.includes(n.status ?? '')) return false;
    if (f.types.length > 0 && !f.types.includes(n.type)) return false;
    if (q) {
      const haystack = (n.name ?? n.type ?? n.id).toLowerCase();
      if (!haystack.includes(q)) return false;
    }
    return true;
  });
}
