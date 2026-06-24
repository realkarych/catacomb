import type { Node } from './types';

export interface FilterState {
  query: string;
  statuses: string[];
  types: string[];
  models: string[];
  hasError: boolean;
}

export function emptyFilter(): FilterState {
  return { query: '', statuses: [], types: [], models: [], hasError: false };
}

export function isActive(f: FilterState): boolean {
  return (
    f.query.trim().length > 0 ||
    f.statuses.length > 0 ||
    f.types.length > 0 ||
    f.models.length > 0 ||
    f.hasError
  );
}

export function filterNodes(nodes: Node[], f: FilterState): Node[] {
  if (!isActive(f)) return nodes;

  const q = f.query.trim().toLowerCase();

  return nodes.filter((n) => {
    if (f.hasError && n.status !== 'error') return false;
    if (f.statuses.length > 0 && !f.statuses.includes(n.status ?? '')) return false;
    if (f.types.length > 0 && !f.types.includes(n.type)) return false;
    if (f.models.length > 0) {
      const model = (n.attrs?.['model_id'] ?? n.attrs?.['model'] ?? '') as string;
      if (!f.models.includes(model)) return false;
    }
    if (q) {
      const haystack = (n.name ?? n.type ?? n.id).toLowerCase();
      if (!haystack.includes(q)) return false;
    }
    return true;
  });
}
