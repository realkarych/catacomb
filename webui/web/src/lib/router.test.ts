import { describe, it, expect } from 'vitest';
import { parseHash, toHash } from './router';
import type { Route } from './router';

describe('parseHash', () => {
  it('empty string → list', () => {
    expect(parseHash('')).toEqual({ kind: 'list' });
  });

  it('bare # → list', () => {
    expect(parseHash('#')).toEqual({ kind: 'list' });
  });

  it('#/ → list', () => {
    expect(parseHash('#/')).toEqual({ kind: 'list' });
  });

  it('unknown path → list', () => {
    expect(parseHash('#/settings')).toEqual({ kind: 'list' });
    expect(parseHash('#/foo/bar/baz')).toEqual({ kind: 'list' });
  });

  it('#/s/{hash} → session', () => {
    expect(parseHash('#/s/abc123')).toEqual({ kind: 'session', hash: 'abc123' });
  });

  it('#/s/{hash} with special chars → decoded', () => {
    expect(parseHash('#/s/abc%3A123')).toEqual({ kind: 'session', hash: 'abc:123' });
  });

  it('#/s/{hash}/n/{nodeId} → session-node', () => {
    expect(parseHash('#/s/abc123/n/node456')).toEqual({
      kind: 'session-node',
      hash: 'abc123',
      nodeId: 'node456',
    });
  });

  it('#/s/{hash}/n/{nodeId} with encoded chars → decoded', () => {
    expect(parseHash('#/s/abc%2F123/n/node%3A456')).toEqual({
      kind: 'session-node',
      hash: 'abc/123',
      nodeId: 'node:456',
    });
  });

  it('node path with extra segments falls back to list', () => {
    expect(parseHash('#/s/abc123/n/node456/extra')).toEqual({ kind: 'list' });
  });
});

describe('toHash', () => {
  it('list → "#/"', () => {
    expect(toHash({ kind: 'list' })).toBe('#/');
  });

  it('session → "#/s/{hash}"', () => {
    expect(toHash({ kind: 'session', hash: 'abc123' })).toBe('#/s/abc123');
  });

  it('session encodes special chars', () => {
    expect(toHash({ kind: 'session', hash: 'abc:123' })).toBe('#/s/abc%3A123');
  });

  it('session-node → "#/s/{hash}/n/{nodeId}"', () => {
    expect(toHash({ kind: 'session-node', hash: 'abc123', nodeId: 'node456' })).toBe(
      '#/s/abc123/n/node456'
    );
  });

  it('session-node encodes special chars', () => {
    expect(
      toHash({ kind: 'session-node', hash: 'abc/123', nodeId: 'node:456' })
    ).toBe('#/s/abc%2F123/n/node%3A456');
  });
});

describe('round-trip', () => {
  it('list round-trips', () => {
    const r: Route = { kind: 'list' };
    expect(parseHash(toHash(r))).toEqual(r);
  });

  it('session round-trips', () => {
    const r: Route = { kind: 'session', hash: 'deadbeef1234' };
    expect(parseHash(toHash(r))).toEqual(r);
  });

  it('session-node round-trips', () => {
    const r: Route = { kind: 'session-node', hash: 'deadbeef', nodeId: 'node:42 space' };
    expect(parseHash(toHash(r))).toEqual(r);
  });
});
