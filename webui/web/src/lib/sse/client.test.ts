import { describe, it, expect, vi } from 'vitest';
import { buildSubscribeURL, connect } from './client';
import type { EventSourceLike } from './client';

class FakeEventSource implements EventSourceLike {
  onopen: ((this: unknown, ev: unknown) => void) | null = null;
  onerror: ((this: unknown, ev: unknown) => void) | null = null;
  onmessage: ((this: unknown, ev: { data: string }) => void) | null = null;
  closed = false;

  close(): void {
    this.closed = true;
  }

  triggerOpen(): void {
    this.onopen?.call(this, {});
  }

  triggerError(): void {
    this.onerror?.call(this, {});
  }

  triggerMessage(data: string): void {
    this.onmessage?.call(this, { data });
  }
}

describe('buildSubscribeURL', () => {
  it('builds basic URL', () => {
    expect(buildSubscribeURL('session1', 'tok123')).toBe(
      '/v1/subscribe?session=session1&token=tok123',
    );
  });

  it('encodes special chars in session', () => {
    expect(buildSubscribeURL('session/with spaces', 'tok')).toContain(
      'session=session%2Fwith%20spaces',
    );
  });

  it('encodes special chars in token', () => {
    expect(buildSubscribeURL('s', 'tok=with&special')).toContain(
      'token=tok%3Dwith%26special',
    );
  });
});

describe('connect default factory', () => {
  it('uses globalThis.EventSource when no factory is provided', () => {
    const fake = new FakeEventSource();
    class FakeEventSourceClass {
      onopen: ((this: unknown, ev: unknown) => void) | null = null;
      onerror: ((this: unknown, ev: unknown) => void) | null = null;
      onmessage: ((this: unknown, ev: { data: string }) => void) | null = null;
      close = () => fake.close();
    }
    const original = (globalThis as Record<string, unknown>)['EventSource'];
    (globalThis as Record<string, unknown>)['EventSource'] = FakeEventSourceClass;
    try {
      const onEvent = vi.fn();
      const conn = connect({ session: 's', token: 't', onEvent });
      conn.close();
      expect(fake.closed).toBe(true);
    } finally {
      (globalThis as Record<string, unknown>)['EventSource'] = original;
    }
  });
});

describe('connect', () => {
  it('calls onStatus connecting immediately', () => {
    const fake = new FakeEventSource();
    const onStatus = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      onStatus,
      factory: () => fake,
    });
    expect(onStatus).toHaveBeenCalledWith('connecting');
  });

  it('calls onStatus open when onopen fires', () => {
    const fake = new FakeEventSource();
    const onStatus = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      onStatus,
      factory: () => fake,
    });
    fake.triggerOpen();
    expect(onStatus).toHaveBeenCalledWith('open');
  });

  it('calls onStatus error when onerror fires', () => {
    const fake = new FakeEventSource();
    const onStatus = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      onStatus,
      factory: () => fake,
    });
    fake.triggerError();
    expect(onStatus).toHaveBeenCalledWith('error');
  });

  it('calls onEvent with parsed JSON on valid message', () => {
    const fake = new FakeEventSource();
    const onEvent = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent,
      factory: () => fake,
    });
    fake.triggerMessage('{"kind":"node_upsert","rev":1}');
    expect(onEvent).toHaveBeenCalledWith({ kind: 'node_upsert', rev: 1 });
  });

  it('does not throw on malformed JSON', () => {
    const fake = new FakeEventSource();
    const onEvent = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent,
      factory: () => fake,
    });
    expect(() => fake.triggerMessage('not-json')).not.toThrow();
    expect(onEvent).not.toHaveBeenCalled();
  });

  it('close calls source.close', () => {
    const fake = new FakeEventSource();
    const conn = connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      factory: () => fake,
    });
    conn.close();
    expect(fake.closed).toBe(true);
  });

  it('works without optional onStatus', () => {
    const fake = new FakeEventSource();
    expect(() => {
      connect({
        session: 's',
        token: 't',
        onEvent: vi.fn(),
        factory: () => fake,
      });
      fake.triggerOpen();
      fake.triggerError();
    }).not.toThrow();
  });
});
