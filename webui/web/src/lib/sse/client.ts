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
  factory?: (url: string) => EventSourceLike;
}

export interface Connection {
  close(): void;
}

export function buildSubscribeURL(session: string, token: string): string {
  return `/v1/subscribe?session=${encodeURIComponent(session)}&token=${encodeURIComponent(token)}`;
}

export function connect(opts: ConnectOptions): Connection {
  opts.onStatus?.('connecting');
  const factory = opts.factory ?? ((url: string) => new (globalThis as { EventSource: new (url: string) => EventSourceLike }).EventSource(url));
  const source = factory(buildSubscribeURL(opts.session, opts.token));
  source.onopen = () => opts.onStatus?.('open');
  source.onerror = () => opts.onStatus?.('error');
  source.onmessage = (ev: { data: string }) => {
    try {
      opts.onEvent(JSON.parse(ev.data) as SseEvent);
    } catch {
    }
  };
  return { close: () => source.close() };
}
