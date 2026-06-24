import { describe, it, expect, vi } from 'vitest';
import { buildSubscribeURL, connect, extractRev, nextLastSeenRev } from './client';
import type { EventSourceLike, ConnectOptions } from './client';
import type { SseEvent } from '../types';

class FakeEventSource implements EventSourceLike {
  onopen: ((this: unknown, ev: unknown) => void) | null = null;
  onerror: ((this: unknown, ev: unknown) => void) | null = null;
  onmessage: ((this: unknown, ev: { data: string }) => void) | null = null;
  closed = false;
  url: string;

  constructor(url: string) {
    this.url = url;
  }

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
  it('builds basic URL without since', () => {
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

  it('appends since when > 0', () => {
    expect(buildSubscribeURL('s', 't', 42)).toBe(
      '/v1/subscribe?session=s&token=t&since=42',
    );
  });

  it('omits since when 0', () => {
    const url = buildSubscribeURL('s', 't', 0);
    expect(url).not.toContain('since');
  });

  it('omits since when undefined', () => {
    const url = buildSubscribeURL('s', 't', undefined);
    expect(url).not.toContain('since');
  });
});

describe('extractRev', () => {
  it('returns rev for a valid event', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: 7 };
    expect(extractRev(ev)).toBe(7);
  });

  it('returns 0 for rev <= 0', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: 0 };
    expect(extractRev(ev)).toBe(0);
  });

  it('returns 0 for negative rev', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: -1 };
    expect(extractRev(ev)).toBe(0);
  });
});

describe('nextLastSeenRev', () => {
  it('returns event rev when higher than current', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: 10 };
    expect(nextLastSeenRev(3, ev)).toBe(10);
  });

  it('returns current when event rev is lower', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: 2 };
    expect(nextLastSeenRev(5, ev)).toBe(5);
  });

  it('returns current when event rev equals current', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: 5 };
    expect(nextLastSeenRev(5, ev)).toBe(5);
  });

  it('returns 0 for zero rev and zero current', () => {
    const ev: SseEvent = { kind: 'node_upsert', rev: 0 };
    expect(nextLastSeenRev(0, ev)).toBe(0);
  });
});

describe('connect default factory', () => {
  it('uses globalThis.EventSource when no factory is provided', () => {
    const fake = new FakeEventSource('');
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
    let capturedUrl = '';
    const fake = new FakeEventSource('');
    const onStatus = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      onStatus,
      factory: (url) => { capturedUrl = url; return fake; },
    });
    expect(onStatus).toHaveBeenCalledWith('connecting');
    expect(capturedUrl).toContain('/v1/subscribe');
  });

  it('calls onStatus open when onopen fires', () => {
    const fake = new FakeEventSource('');
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
    const fake = new FakeEventSource('');
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
    const fake = new FakeEventSource('');
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

  it('calls onParseError on malformed JSON instead of swallowing silently', () => {
    const fake = new FakeEventSource('');
    const onEvent = vi.fn();
    const onParseError = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent,
      onParseError,
      factory: () => fake,
    });
    fake.triggerMessage('not-json');
    expect(onEvent).not.toHaveBeenCalled();
    expect(onParseError).toHaveBeenCalledOnce();
    const firstCall = onParseError.mock.calls[0]!;
    expect(firstCall[0]).toBe('not-json');
    expect(firstCall[1]).toBeInstanceOf(Error);
  });

  it('does not throw on malformed JSON even without onParseError', () => {
    const fake = new FakeEventSource('');
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
    const fake = new FakeEventSource('');
    const conn = connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      factory: () => fake,
    });
    conn.close();
    expect(fake.closed).toBe(true);
  });

  it('double close does not throw', () => {
    const fake = new FakeEventSource('');
    const conn = connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      factory: () => fake,
    });
    expect(() => {
      conn.close();
      conn.close();
    }).not.toThrow();
    expect(fake.closed).toBe(true);
  });

  it('works without optional onStatus', () => {
    const fake = new FakeEventSource('');
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

  it('passes since query param when getLastRev returns > 0', () => {
    let capturedUrl = '';
    const fake = new FakeEventSource('');
    connect({
      session: 'mysess',
      token: 'tok',
      onEvent: vi.fn(),
      getLastRev: () => 17,
      factory: (url) => { capturedUrl = url; return fake; },
    });
    expect(capturedUrl).toContain('since=17');
  });

  it('omits since query param when getLastRev returns 0', () => {
    let capturedUrl = '';
    const fake = new FakeEventSource('');
    connect({
      session: 'mysess',
      token: 'tok',
      onEvent: vi.fn(),
      getLastRev: () => 0,
      factory: (url) => { capturedUrl = url; return fake; },
    });
    expect(capturedUrl).not.toContain('since');
  });

  it('omits since query param when getLastRev is not provided', () => {
    let capturedUrl = '';
    const fake = new FakeEventSource('');
    connect({
      session: 'mysess',
      token: 'tok',
      onEvent: vi.fn(),
      factory: (url) => { capturedUrl = url; return fake; },
    });
    expect(capturedUrl).not.toContain('since');
  });

  it('multiple valid messages fire onEvent each time', () => {
    const fake = new FakeEventSource('');
    const onEvent = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent,
      factory: () => fake,
    });
    fake.triggerMessage('{"kind":"node_upsert","rev":1}');
    fake.triggerMessage('{"kind":"edge_upsert","rev":2}');
    expect(onEvent).toHaveBeenCalledTimes(2);
  });

  it('onParseError receives correct raw string for empty message', () => {
    const fake = new FakeEventSource('');
    const onParseError = vi.fn();
    connect({
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      onParseError,
      factory: () => fake,
    });
    fake.triggerMessage('');
    expect(onParseError).toHaveBeenCalledOnce();
    const emptyCall = onParseError.mock.calls[0]!;
    expect(emptyCall[0]).toBe('');
  });
});

describe('connect: ConnectOptions type completeness', () => {
  it('accepts all optional fields without error', () => {
    const fake = new FakeEventSource('');
    const opts: ConnectOptions = {
      session: 's',
      token: 't',
      onEvent: vi.fn(),
      onStatus: vi.fn(),
      onParseError: vi.fn(),
      getLastRev: () => 5,
      factory: () => fake,
    };
    expect(() => connect(opts)).not.toThrow();
  });
});
