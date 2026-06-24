import type { Node } from '../types';

export type CostProvenance = 'reported' | 'estimated' | 'unknown';

export function costProvenance(node: Pick<Node, 'attrs' | 'cost_usd'>): CostProvenance {
  if (node.cost_usd == null) return 'unknown';
  if (node.attrs?.['cost_source'] === 'reported') return 'reported';
  return 'estimated';
}
