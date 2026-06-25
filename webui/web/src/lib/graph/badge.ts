import type { Aggregate } from './types';
import { formatTokens, formatCost } from '../format/format';

export function badgeStatLine(agg: Aggregate): string {
  return `${agg.count} · ${formatTokens(agg.tokensIn)}→${formatTokens(agg.tokensOut)} · ${formatCost(agg.costUsd)}`;
}

export function badgeStatusColor(status: Aggregate['status']): string {
  if (status === 'error') return 'var(--error)';
  if (status === 'running') return 'var(--running)';
  return 'var(--ok)';
}
