import { describe, it, expect } from 'vitest';
import { isOutcomeStatus, shouldShowStatus, statusColor, displayLabel, isSessionLive, sessionDisplayStatus, LIVE_WINDOW_MS } from './status';
import type { SessionSummary } from './types';

describe('isOutcomeStatus', () => {
  it('returns true for error', () => expect(isOutcomeStatus('error')).toBe(true));
  it('returns true for ok', () => expect(isOutcomeStatus('ok')).toBe(true));
  it('returns true for blocked', () => expect(isOutcomeStatus('blocked')).toBe(true));
  it('returns false for pending', () => expect(isOutcomeStatus('pending')).toBe(false));
  it('returns false for unknown', () => expect(isOutcomeStatus('unknown')).toBe(false));
  it('returns false for running', () => expect(isOutcomeStatus('running')).toBe(false));
  it('returns false for empty string', () => expect(isOutcomeStatus('')).toBe(false));
  it('returns false for undefined cast to string', () => expect(isOutcomeStatus('undefined')).toBe(false));
});

describe('shouldShowStatus', () => {
  it('shows error regardless of isLive', () => {
    expect(shouldShowStatus('error', false)).toBe(true);
    expect(shouldShowStatus('error', true)).toBe(true);
  });
  it('shows ok regardless of isLive', () => {
    expect(shouldShowStatus('ok', false)).toBe(true);
    expect(shouldShowStatus('ok', true)).toBe(true);
  });
  it('shows blocked regardless of isLive', () => {
    expect(shouldShowStatus('blocked', false)).toBe(true);
    expect(shouldShowStatus('blocked', true)).toBe(true);
  });
  it('shows running only when isLive=true', () => {
    expect(shouldShowStatus('running', true)).toBe(true);
    expect(shouldShowStatus('running', false)).toBe(false);
  });
  it('never shows pending', () => {
    expect(shouldShowStatus('pending', false)).toBe(false);
    expect(shouldShowStatus('pending', true)).toBe(false);
  });
  it('never shows unknown', () => {
    expect(shouldShowStatus('unknown', false)).toBe(false);
    expect(shouldShowStatus('unknown', true)).toBe(false);
  });
  it('never shows undefined status', () => {
    expect(shouldShowStatus(undefined, false)).toBe(false);
    expect(shouldShowStatus(undefined, true)).toBe(false);
  });
  it('never shows empty string status', () => {
    expect(shouldShowStatus('', false)).toBe(false);
    expect(shouldShowStatus('', true)).toBe(false);
  });
});

describe('statusColor', () => {
  it('returns error token for error', () => expect(statusColor('error')).toBe('var(--error)'));
  it('returns ok token for ok', () => expect(statusColor('ok')).toBe('var(--ok)'));
  it('returns blocked token for blocked', () => expect(statusColor('blocked')).toBe('var(--blocked)'));
  it('returns running token for running', () => expect(statusColor('running')).toBe('var(--running)'));
  it('returns transparent for unknown status', () => expect(statusColor('unknown')).toBe('transparent'));
  it('returns transparent for empty string', () => expect(statusColor('')).toBe('transparent'));
  it('returns transparent for pending', () => expect(statusColor('pending')).toBe('transparent'));
});

describe('displayLabel', () => {
  it('returns "error" for error', () => expect(displayLabel('error')).toBe('error'));
  it('returns "ok" for ok', () => expect(displayLabel('ok')).toBe('ok'));
  it('returns "blocked" for blocked', () => expect(displayLabel('blocked')).toBe('blocked'));
  it('returns "running" for running', () => expect(displayLabel('running')).toBe('running'));
  it('returns "pending" for pending', () => expect(displayLabel('pending')).toBe('pending'));
  it('returns "unknown" for unknown', () => expect(displayLabel('unknown')).toBe('unknown'));
  it('returns the raw value for an unrecognized status', () => expect(displayLabel('superseded')).toBe('superseded'));
});

describe('isSessionLive', () => {

  function session(overrides: Partial<SessionSummary>): SessionSummary {
    return {
      session: 'abc',
      status: 'running',
      tokens_in: 0,
      tokens_out: 0,
      node_count: 0,
      tool_count: 0,
      error_count: 0,
      run_ids: [],
      ...overrides,
    };
  }

  it('returns true for running session with recent last_activity', () => {
    const nowMs = Date.now();
    const recentActivity = new Date(nowMs - LIVE_WINDOW_MS + 1000).toISOString();
    expect(isSessionLive(session({ last_activity: recentActivity }), nowMs)).toBe(true);
  });

  it('returns false for running session with old last_activity (> 5 min ago)', () => {
    const nowMs = Date.now();
    const oldActivity = new Date(nowMs - LIVE_WINDOW_MS - 1000).toISOString();
    expect(isSessionLive(session({ last_activity: oldActivity }), nowMs)).toBe(false);
  });

  it('returns false for running session with no last_activity', () => {
    expect(isSessionLive(session({ last_activity: undefined }), Date.now())).toBe(false);
  });

  it('returns false for non-running session', () => {
    const nowMs = Date.now();
    const recentActivity = new Date(nowMs - 1000).toISOString();
    expect(isSessionLive(session({ status: 'ok', last_activity: recentActivity }), nowMs)).toBe(false);
  });

  it('returns false for undefined session', () => {
    expect(isSessionLive(undefined, Date.now())).toBe(false);
  });

  it('returns false for unparseable last_activity', () => {
    expect(isSessionLive(session({ last_activity: 'not-a-date' }), Date.now())).toBe(false);
  });
});

describe('sessionDisplayStatus', () => {

  function session(overrides: Partial<SessionSummary>): SessionSummary {
    return {
      session: 'abc',
      status: 'running',
      tokens_in: 0,
      tokens_out: 0,
      node_count: 0,
      tool_count: 0,
      error_count: 0,
      run_ids: [],
      ...overrides,
    };
  }

  it('returns null for undefined session', () => {
    expect(sessionDisplayStatus(undefined, Date.now())).toBe(null);
  });

  it('returns "live" for running session with recent last_activity', () => {
    const nowMs = Date.now();
    const recentActivity = new Date(nowMs - LIVE_WINDOW_MS + 1000).toISOString();
    expect(sessionDisplayStatus(session({ last_activity: recentActivity }), nowMs)).toBe('live');
  });

  it('returns null for running session with old last_activity (> 5 min ago)', () => {
    const nowMs = Date.now();
    const oldActivity = new Date(nowMs - LIVE_WINDOW_MS - 1000).toISOString();
    expect(sessionDisplayStatus(session({ last_activity: oldActivity }), nowMs)).toBe(null);
  });

  it('returns null for running session with no last_activity', () => {
    expect(sessionDisplayStatus(session({ last_activity: undefined }), Date.now())).toBe(null);
  });

  it('returns "ok" for ok session regardless of last_activity', () => {
    const nowMs = Date.now();
    const oldActivity = new Date(nowMs - LIVE_WINDOW_MS - 1000).toISOString();
    expect(sessionDisplayStatus(session({ status: 'ok', last_activity: oldActivity }), nowMs)).toBe('ok');
  });

  it('returns "error" for error session', () => {
    expect(sessionDisplayStatus(session({ status: 'error' }), Date.now())).toBe('error');
  });

  it('returns "blocked" for blocked session', () => {
    expect(sessionDisplayStatus(session({ status: 'blocked' }), Date.now())).toBe('blocked');
  });

  it('returns null for pending session', () => {
    expect(sessionDisplayStatus(session({ status: 'pending' }), Date.now())).toBe(null);
  });

  it('returns null for unknown session', () => {
    expect(sessionDisplayStatus(session({ status: 'unknown' }), Date.now())).toBe(null);
  });

  it('returns null for empty status session', () => {
    expect(sessionDisplayStatus(session({ status: '' }), Date.now())).toBe(null);
  });
});
