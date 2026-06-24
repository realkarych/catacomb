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
