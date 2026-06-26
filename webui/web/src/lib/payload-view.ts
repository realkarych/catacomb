import type { PayloadView } from './types';

export type PayloadState = 'disabled' | 'empty' | 'redacted' | 'ready';

export function prettyJSON(value: unknown): string {
  if (value === undefined) return '';
  return JSON.stringify(value, null, 2);
}

export function payloadState(view: PayloadView | null, forbidden: boolean): PayloadState {
  if (forbidden) return 'disabled';
  if (view === null) return 'empty';
  const hasInput = view.input !== undefined && view.input !== null;
  const hasOutput = view.output !== undefined && view.output !== null;
  if (!hasInput && !hasOutput) return 'empty';
  if (view.redacted) return 'redacted';
  return 'ready';
}

export function truncateAtNewline(
  text: string,
  limit: number,
): { shown: string; hasMore: boolean; remaining: string } {
  if (text.length <= limit) {
    return { shown: text, hasMore: false, remaining: '' };
  }
  const cutAt = text.lastIndexOf('\n', limit - 1);
  if (cutAt !== -1) {
    return { shown: text.slice(0, cutAt + 1), hasMore: true, remaining: text.slice(cutAt + 1) };
  }
  return { shown: text.slice(0, limit), hasMore: true, remaining: text.slice(limit) };
}

export function remainingLineCount(remaining: string): number {
  if (remaining === '') return 0;
  return remaining.split('\n').filter((s) => s !== '').length;
}
