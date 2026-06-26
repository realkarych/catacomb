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
