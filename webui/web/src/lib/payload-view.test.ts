import { describe, it, expect } from 'vitest';
import { prettyJSON, payloadState } from './payload-view';
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
