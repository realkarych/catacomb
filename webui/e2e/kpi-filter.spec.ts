import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'aabbccdd1122aabbccdd1122aabbccdd';

const fakeSession: SessionSummary = {
  session: sessionHash,
  status: 'ok',
  started_at: '2024-06-01T10:00:00Z',
  duration_ms: 8500,
  tokens_in: 1200,
  tokens_out: 2500,
  cost_usd: 0.0456,
  cost_source: 'estimated',
  node_count: 3,
  tool_count: 1,
  error_count: 1,
  model_id: 'claude-sonnet-4',
  run_ids: ['run-kpi'],
};

const sseEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 1,
    node: {
      id: 'n-session',
      run_id: 'run-kpi',
      type: 'session',
      name: 'Session Root',
      status: 'ok',
      rev: 1,
    },
  },
  {
    kind: 'node_upsert',
    rev: 2,
    node: {
      id: 'n-prompt',
      run_id: 'run-kpi',
      type: 'user_prompt',
      name: 'User Prompt',
      status: 'ok',
      rev: 2,
    },
  },
  {
    kind: 'node_upsert',
    rev: 3,
    node: {
      id: 'n-tool',
      run_id: 'run-kpi',
      type: 'tool_call',
      name: 'BashTool',
      status: 'error',
      rev: 3,
    },
  },
];

function buildSseBody(events: SseEvent[]): string {
  return events.map((ev) => `data: ${JSON.stringify(ev)}\n\n`).join('');
}

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([fakeSession]),
    });
  });

  await page.route('/v1/events**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: buildSseBody(sseEvents),
    });
  });

  await page.route('/v1/subscribe**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: buildSseBody(sseEvents),
    });
  });
});

test('KPI header shows cost, tokens, duration, and model', async ({ page }) => {
  await page.goto(`/#/s/${sessionHash}`);
  await expect(page.locator('.session-kpi')).toBeVisible();

  const kpi = page.locator('.session-kpi');
  await expect(kpi).toContainText('$0.05');
  await expect(kpi).toContainText('est');
  await expect(kpi).toContainText('1,200');
  await expect(kpi).toContainText('2,500');
  await expect(kpi).toContainText('8.5s');
  await expect(kpi).toContainText('claude-sonnet-4');
  await expect(kpi).toContainText('3 nodes');
  await expect(kpi).toContainText('1 tools');
});

test('error chip is visible and styled when error_count > 0', async ({ page }) => {
  await page.goto(`/#/s/${sessionHash}`);
  const errChip = page.locator('.kpi-chip--error');
  await expect(errChip).toBeVisible();
  await expect(errChip).toContainText('1 err');

  const color = await errChip.evaluate((el) => getComputedStyle(el).color);
  expect(color).not.toBe('');
});

test('filter bar is visible with search input and chips', async ({ page }) => {
  await page.goto(`/#/s/${sessionHash}`);
  await expect(page.locator('.filter-bar')).toBeVisible();
  await expect(page.locator('.filter-search')).toBeVisible();
});

test('typing in filter search shows N of M count', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.locator('.filter-search').fill('BashTool');

  await expect(page.locator('.filter-count')).toBeVisible();
  await expect(page.locator('.filter-count')).toContainText('of');
});

test('clearing filter removes count indicator', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.locator('.filter-search').fill('BashTool');
  await expect(page.locator('.filter-count')).toBeVisible();

  await page.locator('.filter-reset').click();
  await expect(page.locator('.filter-count')).not.toBeVisible();
});

test('has errors chip is shown when session has error_count > 0', async ({ page }) => {
  await page.goto(`/#/s/${sessionHash}`);
  await expect(page.locator('.filter-chip--err')).toBeVisible();
  await expect(page.locator('.filter-chip--err')).toContainText('has errors');
});

test('status filter chips appear for node statuses present in graph', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const chips = page.locator('.filter-group').first().locator('.filter-chip');
  await expect(chips.first()).toBeVisible({ timeout: 8000 });
});

test('type filter chips appear for node types present in graph', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const groups = page.locator('.filter-group');
  await expect(groups.first()).toBeVisible({ timeout: 8000 });
  const count = await groups.count();
  expect(count).toBeGreaterThanOrEqual(1);
});

test('type filter chips show human-readable labels with no underscores', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const typeGroup = page.locator('.filter-group').last();
  await expect(typeGroup.locator('.filter-chip').first()).toBeVisible({ timeout: 8000 });

  const labels = await typeGroup.locator('.filter-chip').allTextContents();
  expect(labels.length).toBeGreaterThan(0);
  for (const label of labels) {
    expect(label).not.toMatch(/_/);
  }
  expect(labels).toContain('user prompt');
});

test('status filter chips show human-readable labels', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const statusGroup = page.locator('.filter-group').first();
  await expect(statusGroup.locator('.filter-chip').first()).toBeVisible({ timeout: 8000 });

  const labels = await statusGroup.locator('.filter-chip').allTextContents();
  expect(labels.length).toBeGreaterThan(0);
  for (const label of labels) {
    expect(label).not.toMatch(/_/);
  }
});
