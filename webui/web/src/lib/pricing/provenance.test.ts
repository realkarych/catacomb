import { describe, it, expect } from 'vitest';
import { costProvenance } from './provenance';

describe('costProvenance', () => {
  it('returns reported when cost_source is reported', () => {
    expect(costProvenance({ cost_usd: 1.5, attrs: { cost_source: 'reported' } })).toBe('reported');
  });

  it('returns estimated when cost_source is estimated', () => {
    expect(costProvenance({ cost_usd: 1.5, attrs: { cost_source: 'estimated' } })).toBe('estimated');
  });

  it('returns estimated when cost_usd is defined but no cost_source', () => {
    expect(costProvenance({ cost_usd: 1.5, attrs: {} })).toBe('estimated');
  });

  it('returns estimated when cost_usd is defined and attrs is absent', () => {
    expect(costProvenance({ cost_usd: 1.5 })).toBe('estimated');
  });

  it('returns unknown when cost_usd is undefined', () => {
    expect(costProvenance({ attrs: { cost_source: 'reported' } })).toBe('unknown');
  });

  it('returns unknown when cost_usd is null-ish (no cost_usd field)', () => {
    expect(costProvenance({})).toBe('unknown');
  });
});
