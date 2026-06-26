import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { SessionSummary } from '../web/src/lib/types';

const sessionHash = 'l1vebadge001l1vebadge001l1vebade';

function buildFakeSession(lastActivity: string): SessionSummary {
  return {
    session: sessionHash,
    status: 'running',
    started_at: '2026-06-20T10:00:00Z',
    last_activity: lastActivity,
    tokens_in: 10,
    tokens_out: 5,
    node_count: 1,
    tool_count: 0,
    error_count: 0,
    run_ids: ['run-live'],
  };
}

async function routeSession(page: Page, lastActivity: string): Promise<void> {
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([buildFakeSession(lastActivity)]),
    });
  });
  await page.route('/v1/subscribe**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: '',
    });
  });
}

test('running session with recent last_activity shows live badge', async ({ page }) => {
  const recentActivity = new Date(Date.now() - 30_000).toISOString();
  await routeSession(page, recentActivity);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-kpi')).toBeVisible();
  await expect(page.locator('.live-badge')).toBeVisible();
  await expect(page.locator('.live-badge')).toContainText('live');
});

test('running session with old last_activity (> 5 min) shows NO live badge', async ({ page }) => {
  const oldActivity = new Date(Date.now() - 6 * 60_000).toISOString();
  await routeSession(page, oldActivity);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-kpi')).toBeVisible();
  await expect(page.locator('.live-badge')).toHaveCount(0);
});
