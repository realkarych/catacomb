import { describe, it, expect } from 'vitest';
import { badgeStatLine, badgeStatusColor } from './badge';
import type { Aggregate } from './types';

function agg(extra: Partial<Aggregate> = {}): Aggregate {
  return {
    count: 0,
    tokensIn: 0,
    tokensOut: 0,
    costUsd: 0,
    status: 'ok',
    hasError: false,
    ...extra,
  };
}

describe('badgeStatLine', () => {
  it('formats count, tokens and cost with existing helpers', () => {
    expect(badgeStatLine(agg({ count: 3, tokensIn: 1500, tokensOut: 12000, costUsd: 0.0234 }))).toBe(
      '3 · 1,500→12.0k · $0.02',
    );
  });

  it('renders zeros explicitly', () => {
    expect(badgeStatLine(agg())).toBe('0 · 0→0 · $0.00');
  });

  it('uses 4-decimal cost for sub-cent totals', () => {
    expect(badgeStatLine(agg({ count: 1, tokensIn: 5, tokensOut: 7, costUsd: 0.0005 }))).toBe(
      '1 · 5→7 · $0.0005',
    );
  });
});

describe('badgeStatusColor', () => {
  it('maps each status to its CSS variable', () => {
    expect(badgeStatusColor('error')).toBe('var(--error)');
    expect(badgeStatusColor('running')).toBe('var(--running)');
    expect(badgeStatusColor('ok')).toBe('var(--ok)');
  });
});
