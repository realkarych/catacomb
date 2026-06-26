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
