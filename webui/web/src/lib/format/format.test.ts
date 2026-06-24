import { describe, it, expect } from 'vitest';
import { formatDuration, formatTokens, formatCost, shortHash, formatDate } from './format';

describe('formatDuration', () => {
  it('returns em dash when undefined', () => {
    expect(formatDuration(undefined)).toBe('—');
  });

  it('returns ms for 0', () => {
    expect(formatDuration(0)).toBe('0ms');
  });

  it('returns ms for values < 1000', () => {
    expect(formatDuration(820)).toBe('820ms');
  });

  it('returns ms for value exactly 999', () => {
    expect(formatDuration(999)).toBe('999ms');
  });

  it('returns seconds with 1 decimal for 1000', () => {
    expect(formatDuration(1000)).toBe('1.0s');
  });

  it('returns seconds with 1 decimal for 1400', () => {
    expect(formatDuration(1400)).toBe('1.4s');
  });

  it('returns seconds for values just under 60000', () => {
    expect(formatDuration(59999)).toBe('60.0s');
  });

  it('returns minutes and seconds for exactly 60000', () => {
    expect(formatDuration(60000)).toBe('1m 00s');
  });

  it('returns minutes and seconds for 123000', () => {
    expect(formatDuration(123000)).toBe('2m 03s');
  });

  it('returns minutes and padded seconds for values just under 3600000', () => {
    expect(formatDuration(3599000)).toBe('59m 59s');
  });

  it('returns hours and minutes for exactly 3600000', () => {
    expect(formatDuration(3600000)).toBe('1h 00m');
  });

  it('returns hours and minutes for 3840000', () => {
    expect(formatDuration(3840000)).toBe('1h 04m');
  });

  it('returns hours and padded minutes for large values', () => {
    expect(formatDuration(7260000)).toBe('2h 01m');
  });
});

describe('formatTokens', () => {
  it('returns em dash when undefined', () => {
    expect(formatTokens(undefined)).toBe('—');
  });

  it('returns 0 for zero', () => {
    expect(formatTokens(0)).toBe('0');
  });

  it('returns formatted string for values below 1000', () => {
    expect(formatTokens(999)).toBe('999');
  });

  it('returns thousands-separated string for 1000', () => {
    expect(formatTokens(1000)).toBe('1,000');
  });

  it('returns thousands-separated string for 9999', () => {
    expect(formatTokens(9999)).toBe('9,999');
  });

  it('returns compact form for 10000', () => {
    expect(formatTokens(10000)).toBe('10.0k');
  });

  it('returns compact form for 12345', () => {
    expect(formatTokens(12345)).toBe('12.3k');
  });

  it('returns compact form for 123456', () => {
    expect(formatTokens(123456)).toBe('123.5k');
  });
});

describe('formatCost', () => {
  it('returns em dash when undefined', () => {
    expect(formatCost(undefined)).toBe('—');
  });

  it('returns $0.00 for zero', () => {
    expect(formatCost(0)).toBe('$0.00');
  });

  it('returns 4 decimal places for values below 0.01', () => {
    expect(formatCost(0.0012)).toBe('$0.0012');
  });

  it('returns 2 decimal places for 0.0123 (>= 0.01)', () => {
    expect(formatCost(0.0123)).toBe('$0.01');
  });

  it('returns 2 decimal places for values >= 0.01', () => {
    expect(formatCost(0.12)).toBe('$0.12');
  });

  it('returns 2 decimal places for values >= 1', () => {
    expect(formatCost(1.23)).toBe('$1.23');
  });

  it('returns 4 decimals for exactly 0.009', () => {
    expect(formatCost(0.009)).toBe('$0.0090');
  });
});

describe('shortHash', () => {
  it('returns em dash when undefined', () => {
    expect(shortHash(undefined)).toBe('—');
  });

  it('returns em dash for empty string', () => {
    expect(shortHash('')).toBe('—');
  });

  it('returns first 8 chars by default', () => {
    expect(shortHash('sha-1234abcdef')).toBe('sha-1234');
  });

  it('returns full string if shorter than default n', () => {
    expect(shortHash('abc')).toBe('abc');
  });

  it('respects custom n', () => {
    expect(shortHash('sha-1234abcdef', 4)).toBe('sha-');
  });

  it('returns full string when custom n exceeds length', () => {
    expect(shortHash('abc', 10)).toBe('abc');
  });
});

describe('formatDate', () => {
  it('returns em dash when undefined', () => {
    expect(formatDate(undefined)).toBe('—');
  });

  it('returns em dash for invalid date string', () => {
    expect(formatDate('not-a-date')).toBe('—');
  });

  it('returns a formatted date string for a valid ISO timestamp', () => {
    const result = formatDate('2026-06-20T10:00:01Z');
    expect(result).not.toBe('—');
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });
});
