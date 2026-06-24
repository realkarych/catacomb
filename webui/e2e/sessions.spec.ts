import { test, expect } from '@playwright/test';
import type { SessionSummary } from '../web/src/lib/types';

const fakeSessions: SessionSummary[] = [
  {
    session: 'deadbeef0001deadbeef0001deadbeef',
    status: 'ok',
    started_at: '2024-06-01T10:00:00Z',
    duration_ms: 12500,
    tokens_in: 1500,
    tokens_out: 3200,
    cost_usd: 0.0234,
    cost_source: 'reported',
    node_count: 12,
    tool_count: 5,
    error_count: 0,
    model_id: 'claude-opus-4',
    run_ids: ['run1'],
  },
  {
    session: 'cafebabe0002cafebabe0002cafebabe',
    status: 'error',
    started_at: '2024-06-01T09:00:00Z',
    duration_ms: 3800,
    tokens_in: 800,
    tokens_out: 1100,
    cost_usd: 0.0089,
    cost_source: 'estimated',
    node_count: 6,
    tool_count: 2,
    error_count: 3,
    model_id: 'claude-sonnet-3-7',
    run_ids: ['run2'],
  },
  {
    session: 'abcd123400030000abcd123400030000',
    status: 'running',
    started_at: '2024-06-01T11:00:00Z',
    duration_ms: undefined,
    tokens_in: 200,
    tokens_out: 400,
    cost_usd: undefined,
    node_count: 3,
    tool_count: 1,
    error_count: 0,
    model_id: 'claude-haiku-3-5',
    run_ids: ['run3'],
  },
];

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(fakeSessions),
    });
  });
});

test('app loads and shows sessions list', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.sessions-list')).toBeVisible();
  await expect(page.locator('.session-row')).toHaveCount(3);
});

test('sessions list shows formatted cost and tokens', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.session-row')).toHaveCount(3);
  const list = page.locator('.sessions-list');
  await expect(list).toContainText('$0.02');
  await expect(list).toContainText('1,500');
  await expect(list).toContainText('3,200');
});

test('search narrows the session list', async ({ page }) => {
  await page.goto('/');
  const search = page.locator('.search-input');
  await search.fill('opus');
  await expect(page.locator('.session-row')).toHaveCount(1);
  await expect(page.locator('.session-row')).toContainText('deadbeef');
});

test('search by partial hash narrows list', async ({ page }) => {
  await page.goto('/');
  const search = page.locator('.search-input');
  await search.fill('cafebabe');
  await expect(page.locator('.session-row')).toHaveCount(1);
});

test('clicking a row navigates to session view', async ({ page }) => {
  await page.goto('/');
  await page.locator('.session-row').first().click();
  await expect(page).toHaveURL(/#\/s\//);
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.session-view')).toContainText('abcd1234');
});

test('session view has back button that returns to list', async ({ page }) => {
  await page.goto('/#/s/deadbeef0001deadbeef0001deadbeef');
  await expect(page.locator('.session-view')).toBeVisible();
  await page.locator('.back-btn').click();
  await expect(page.locator('.sessions-list')).toBeVisible();
});

test('deep-link to #/s/{hash} renders session view directly', async ({ page }) => {
  await page.goto('/#/s/cafebabe0002cafebabe0002cafebabe');
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.session-view')).toContainText('cafebabe0002');
});
