import type { SseEvent } from '../types';

export interface EventSourceLike {
  onopen: ((this: unknown, ev: unknown) => void) | null;
  onerror: ((this: unknown, ev: unknown) => void) | null;
  onmessage: ((this: unknown, ev: { data: string }) => void) | null;
  close(): void;
}

export interface ConnectOptions {
  session: string;
  token: string;
  onEvent: (ev: SseEvent) => void;
  onStatus?: (s: 'connecting' | 'open' | 'error') => void;
  onParseError?: (raw: string, err: unknown) => void;
  getLastRev?: () => number;
  factory?: (url: string) => EventSourceLike;
}

export interface Connection {
  close(): void;
}

export function buildSubscribeURL(session: string, token: string, since?: number): string {
  const base = `/v1/subscribe?session=${encodeURIComponent(session)}&token=${encodeURIComponent(token)}`;
  return since != null && since > 0 ? `${base}&since=${since}` : base;
}

export function extractRev(ev: SseEvent): number {
  return typeof ev.rev === 'number' && ev.rev > 0 ? ev.rev : 0;
}

export function nextLastSeenRev(current: number, ev: SseEvent): number {
  const r = extractRev(ev);
  return r > current ? r : current;
}

export function connect(opts: ConnectOptions): Connection {
  let closed = false;
  const factory = opts.factory ?? ((url: string) => new (globalThis as { EventSource: new (url: string) => EventSourceLike }).EventSource(url));

  opts.onStatus?.('connecting');
  const since = opts.getLastRev?.() ?? 0;
  const source = factory(buildSubscribeURL(opts.session, opts.token, since > 0 ? since : undefined));

  source.onopen = () => {
    opts.onStatus?.('open');
  };

  source.onerror = () => {
    opts.onStatus?.('error');
  };

  source.onmessage = (ev: { data: string }) => {
    try {
      const parsed = JSON.parse(ev.data) as SseEvent;
      opts.onEvent(parsed);
    } catch (err) {
      opts.onParseError?.(ev.data, err);
    }
  };

  return {
    close: () => {
      if (!closed) {
        closed = true;
        source.close();
      }
    },
  };
}
