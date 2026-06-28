import type { SessionSummary } from './types';

export const LIVE_WINDOW_MS = 5 * 60_000;

const OUTCOME_STATUSES = new Set(['error', 'ok', 'blocked']);

export function isOutcomeStatus(s: string): boolean {
	return OUTCOME_STATUSES.has(s);
}

export function shouldShowStatus(status: string | undefined, isLive: boolean): boolean {
	if (status === undefined || status === '') return false;
	if (isOutcomeStatus(status)) return true;
	if (status === 'running' && isLive) return true;
	return false;
}

export function statusColor(status: string): string {
	if (status === 'error') return 'var(--error)';
	if (status === 'ok') return 'var(--ok)';
	if (status === 'blocked') return 'var(--blocked)';
	if (status === 'running') return 'var(--running)';
	return 'transparent';
}

const STATUS_LABELS: Record<string, string> = {
	error: 'error',
	ok: 'ok',
	blocked: 'blocked',
	running: 'running',
	pending: 'pending',
	unknown: 'unknown',
};

export function displayLabel(status: string): string {
	return STATUS_LABELS[status] ?? status;
}

export function isSessionLive(session: SessionSummary | undefined, nowMs: number): boolean {
	if (!session || session.status !== 'running' || !session.last_activity) return false;
	return nowMs - Date.parse(session.last_activity) < LIVE_WINDOW_MS;
}

export function sessionDisplayStatus(session: SessionSummary | undefined, nowMs: number): string | null {
	if (!session) return null;
	const live = isSessionLive(session, nowMs);
	if (!shouldShowStatus(session.status, live)) return null;
	return session.status === 'running' ? 'live' : session.status;
}

export function sessionElapsedMs(session: SessionSummary, nowMs: number): number | undefined {
	if (session.duration_ms !== undefined) return session.duration_ms;
	if (session.started_at) return nowMs - Date.parse(session.started_at);
	return undefined;
}
