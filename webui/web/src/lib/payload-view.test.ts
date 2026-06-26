import { describe, it, expect } from 'vitest';
import { prettyJSON, payloadState, truncateAtNewline, remainingLineCount } from './payload-view';
import type { PayloadView } from './types';

describe('prettyJSON', () => {
  it('returns empty string for undefined', () => {
    expect(prettyJSON(undefined)).toBe('');
  });

  it('returns JSON for null', () => {
    expect(prettyJSON(null)).toBe('null');
  });

  it('returns JSON for string', () => {
    expect(prettyJSON('hello')).toBe('"hello"');
  });

  it('returns pretty JSON for object', () => {
    expect(prettyJSON({ a: 1 })).toBe('{\n  "a": 1\n}');
  });
});

describe('payloadState', () => {
  const baseView: PayloadView = {
    node_id: 'n1',
    redactions: [],
    redacted: false,
  };

  it('returns disabled when forbidden is true', () => {
    expect(payloadState(null, true)).toBe('disabled');
  });

  it('returns disabled when forbidden is true even with a view', () => {
    const view: PayloadView = { ...baseView, input: { cmd: 'ls' } };
    expect(payloadState(view, true)).toBe('disabled');
  });

  it('returns empty when view is null', () => {
    expect(payloadState(null, false)).toBe('empty');
  });

  it('returns empty when view has no input or output', () => {
    expect(payloadState(baseView, false)).toBe('empty');
  });

  it('returns empty when view input and output are null', () => {
    const view: PayloadView = { ...baseView, input: null, output: null };
    expect(payloadState(view, false)).toBe('empty');
  });

  it('returns redacted when view.redacted is true', () => {
    const view: PayloadView = { ...baseView, input: { cmd: 'ls' }, redacted: true };
    expect(payloadState(view, false)).toBe('redacted');
  });

  it('returns ready when view has content and is not redacted', () => {
    const view: PayloadView = { ...baseView, input: { cmd: 'ls' } };
    expect(payloadState(view, false)).toBe('ready');
  });

  it('returns ready when view has only output', () => {
    const view: PayloadView = { ...baseView, output: { result: 'ok' } };
    expect(payloadState(view, false)).toBe('ready');
  });
});

describe('truncateAtNewline', () => {
  it('short text within limit returns full text with hasMore false', () => {
    const result = truncateAtNewline('hello', 10);
    expect(result).toEqual({ shown: 'hello', hasMore: false, remaining: '' });
  });

  it('exact limit match returns full text with hasMore false', () => {
    const text = 'abcde';
    const result = truncateAtNewline(text, 5);
    expect(result).toEqual({ shown: text, hasMore: false, remaining: '' });
  });

  it('text longer than limit with newline before limit cuts at last newline', () => {
    const text = 'line1\nline2\nline3';
    const result = truncateAtNewline(text, 10);
    expect(result.hasMore).toBe(true);
    expect(result.shown).toBe('line1\n');
    expect(result.remaining).toBe('line2\nline3');
  });

  it('hasMore is true when text is longer than limit', () => {
    const result = truncateAtNewline('abcdefgh', 4);
    expect(result.hasMore).toBe(true);
  });

  it('remaining contains the rest after the cut', () => {
    const text = 'aa\nbb\ncc';
    const result = truncateAtNewline(text, 5);
    expect(result.shown).toBe('aa\n');
    expect(result.remaining).toBe('bb\ncc');
  });

  it('no newline before limit hard cuts at limit', () => {
    const text = 'abcdefghij';
    const result = truncateAtNewline(text, 4);
    expect(result).toEqual({ shown: 'abcd', hasMore: true, remaining: 'efghij' });
  });
});

describe('remainingLineCount', () => {
  it('empty remaining returns 0', () => {
    expect(remainingLineCount('')).toBe(0);
  });

  it('single line returns 1', () => {
    expect(remainingLineCount('hello')).toBe(1);
  });

  it('multiple lines returns correct count', () => {
    expect(remainingLineCount('line1\nline2\nline3')).toBe(3);
  });

  it('trailing newline does not add phantom line', () => {
    expect(remainingLineCount('line1\nline2\n')).toBe(2);
  });
});
