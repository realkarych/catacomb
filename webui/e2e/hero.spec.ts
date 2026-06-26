import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'abc123def456abc1abc123def456abc1';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'ok',
    started_at: '2024-06-01T10:00:00Z',
    duration_ms: 4200,
    tokens_in: 512,
    tokens_out: 128,
    cost_usd: 0.0031,
    cost_source: 'reported',
    node_count: 3,
    tool_count: 2,
    error_count: 0,
    model_id: 'claude-opus-4-5',
    run_ids: ['run-hero'],
  },
];

const sseEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 1,
    node: {
      id: 'node-session-hero',
      run_id: 'run-hero',
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
      id: 'node-assistant-hero',
      run_id: 'run-hero',
      type: 'assistant_turn',
      name: 'Assistant Turn',
      status: 'ok',
      duration_ms: 4200,
      tokens_in: 512,
      tokens_out: 128,
      cost_usd: 0.0031,
      attrs: {
        cost_source: 'reported',
        model: 'claude-opus-4-5',
      },
      rev: 2,
    },
  },
  {
    kind: 'node_upsert',
    rev: 3,
    node: {
      id: 'node-tool-hero',
      run_id: 'run-hero',
      type: 'tool_call',
      name: 'BashTool',
      status: 'ok',
      tokens_in: 50,
      tokens_out: 80,
      rev: 3,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 4,
    edge: {
      id: 'edge-hero-1',
      run_id: 'run-hero',
      type: 'parent_child',
      src: 'node-session-hero',
      dst: 'node-assistant-hero',
      rev: 4,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 5,
    edge: {
      id: 'edge-hero-2',
      run_id: 'run-hero',
      type: 'parent_child',
      src: 'node-assistant-hero',
      dst: 'node-tool-hero',
      rev: 5,
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
      body: JSON.stringify(fakeSessions),
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

test('hero flow: list → session → node → drawer shows metrics', async ({ page }) => {
  await page.goto('/?token=test');

  await expect(page.locator('.sessions-list')).toBeVisible();
  await expect(page.locator('.session-row')).toHaveCount(1);
  await expect(page.locator('.session-row')).toContainText('abc123');

  await page.locator('.session-row').first().click();
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}`));
  await expect(page.locator('.session-view')).toBeVisible();

  await expect(page.locator('.outline-root')).toBeVisible();

  const assistantRow = page.locator('.outline-row').filter({ hasText: 'assistant' }).first();
  await assistantRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();

  await expect(drawer.locator('.meta-summary')).toContainText('4.2s');
  await expect(drawer.locator('.meta-summary')).toContainText('$0.0031');

  await drawer.locator('.advanced-summary').click();

  await expect(drawer.locator('.metric-row')).toHaveCount(5);

  await expect(drawer).toContainText('Duration');
  await expect(drawer).toContainText('4.2s');

  await expect(drawer).toContainText('Tokens in');
  await expect(drawer).toContainText('512');

  await expect(drawer).toContainText('Tokens out');
  await expect(drawer).toContainText('128');

  await expect(drawer).toContainText('Cost');
  await expect(drawer).toContainText('$0.0031');

  const provBadge = drawer.locator('.provenance-badge[data-provenance="reported"]');
  await expect(provBadge).toBeVisible();
  await expect(provBadge).toContainText('reported');

  await expect(drawer).toContainText('Model');
  await expect(drawer).toContainText('claude-opus-4-5');

  await page.keyboard.press('Escape');

  await expect(drawer).not.toBeVisible();
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}$`));
});

test('hero flow: clicking node without metrics shows dashes for unknown fields', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const sessionRow = page.locator('.outline-row').filter({ hasText: 'Session Root' }).first();
  await sessionRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();

  await drawer.locator('.advanced-summary').click();

  await expect(drawer).toContainText('Duration');
  await expect(drawer).toContainText('—');

  await expect(drawer).toContainText('Model');
});

test('hero flow: Escape closes drawer and clears selection', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const assistantRow = page.locator('.outline-row').filter({ hasText: 'assistant' }).first();
  await assistantRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();

  await page.keyboard.press('Escape');

  await expect(drawer).not.toBeVisible();
  await expect(page).not.toHaveURL(/\/n\//);
});

test('deep-link to #/s/{hash} opens session view directly', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.outline-root')).toBeVisible();
  await expect(page.locator('.session-view')).toContainText('abc123def456');
});

test('deep-link to #/s/{hash}/n/{nodeId} opens session view', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}/n/node-assistant-hero`);
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.node-drawer.node-drawer--open')).toBeVisible({ timeout: 8000 });
});
