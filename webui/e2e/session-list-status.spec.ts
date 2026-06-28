import { test, expect } from '@playwright/test';
import type { SessionSummary } from '../web/src/lib/types';

const staleRunningHash = 'aaaa00010002aaaa00010002aaaa0001';
const okHash = 'bbbb00010002bbbb00010002bbbb0001';
const liveRunningHash = 'cccc00010002cccc00010002cccc0001';

const fakeSessions: SessionSummary[] = [
  {
    session: staleRunningHash,
    status: 'running',
    started_at: '2024-06-01T10:00:00Z',
    last_activity: new Date(Date.now() - 24 * 60 * 60_000).toISOString(),
    tokens_in: 100,
    tokens_out: 200,
    node_count: 1,
    tool_count: 0,
    error_count: 0,
    model_id: 'claude-opus-4',
    run_ids: ['run-stale'],
  },
  {
    session: okHash,
    status: 'ok',
    started_at: '2024-06-01T09:00:00Z',
    duration_ms: 5000,
    tokens_in: 300,
    tokens_out: 400,
    node_count: 2,
    tool_count: 1,
    error_count: 0,
    model_id: 'claude-sonnet-4',
    run_ids: ['run-ok'],
  },
  {
    session: liveRunningHash,
    status: 'running',
    started_at: '2024-06-01T11:00:00Z',
    last_activity: new Date(Date.now() - 5_000).toISOString(),
    tokens_in: 500,
    tokens_out: 600,
    node_count: 3,
    tool_count: 2,
    error_count: 0,
    model_id: 'claude-haiku-3-5',
    run_ids: ['run-live'],
  },
];

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(fakeSessions),
    });
  });
});

test('sessions list gates status pills by liveness', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.session-row')).toHaveCount(3);

  const staleRow = page.locator(`.session-row[aria-label*="${staleRunningHash.slice(0, 12)}"]`);
  const okRow = page.locator(`.session-row[aria-label*="${okHash.slice(0, 12)}"]`);
  const liveRow = page.locator(`.session-row[aria-label*="${liveRunningHash.slice(0, 12)}"]`);

  await expect(staleRow.locator('.cell-status .status-pill')).toHaveCount(0);

  await expect(okRow.locator('.cell-status .status-pill')).toHaveCount(1);
  await expect(okRow.locator('.cell-status .status-pill')).toContainText('finished');

  await expect(liveRow.locator('.cell-status .status-pill')).toHaveCount(1);
  await expect(liveRow.locator('.cell-status .status-pill')).toContainText('live');
});
