import type { DiffResult, DiffDeltas } from './types';

export function diffCounts(r: DiffResult): { added: number; removed: number; changed: number; unchanged: number } {
  return {
    added: r.added.length,
    removed: r.removed.length,
    changed: r.changed.length,
    unchanged: r.unchanged.length,
  };
}

export function isEmptyDiff(r: DiffResult): boolean {
  return r.added.length === 0 && r.removed.length === 0 && r.changed.length === 0;
}

export function changedFields(d: DiffDeltas): string[] {
  const fieldOrder: (keyof DiffDeltas)[] = ['args', 'status', 'cost_usd', 'duration_ms', 'tokens_in', 'tokens_out'];
  return fieldOrder.filter((f) => d[f] !== undefined);
}
