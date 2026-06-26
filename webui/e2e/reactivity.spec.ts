import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'f00dface0001f00dface0001f00dface';
const turnId = 'turn-msg-1';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'running',
    started_at: '2026-06-20T10:00:01Z',
    tokens_in: 10,
    tokens_out: 5,
    cost_usd: 0.000175,
    cost_source: 'estimated',
    node_count: 2,
    tool_count: 1,
    error_count: 0,
    model_id: 'claude-opus-4-8',
    run_ids: ['run-x'],
  },
];

const topologyEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 1,
    node: { id: 'session-root', run_id: 'run-x', type: 'session', name: 'Session Root', status: 'ok', rev: 1 },
  },
  {
    kind: 'node_upsert',
    rev: 2,
    node: {
      id: turnId,
      run_id: 'run-x',
      type: 'assistant_turn',
      name: 'Assistant Turn',
      status: 'running',
      duration_ms: 4200,
      tokens_in: 10,
      tokens_out: 5,
      cost_usd: 0.000175,
      attrs: { cost_source: 'estimated', model: 'claude-opus-4-8' },
      rev: 2,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 3,
    edge: { id: 'edge-root-turn', run_id: 'run-x', type: 'parent_child', src: 'session-root', dst: turnId, rev: 3 },
  },
];

const updateEvent: SseEvent = {
  kind: 'node_upsert',
  rev: 10,
  node: {
    id: turnId,
    run_id: 'run-x',
    type: 'assistant_turn',
    name: 'Assistant Turn',
    status: 'ok',
    duration_ms: 4200,
    tokens_in: 10,
    tokens_out: 5,
    cost_usd: 0.000175,
    attrs: { cost_source: 'estimated', model: 'claude-opus-4-8' },
    rev: 10,
  },
};

async function installFakeSse(page: Page): Promise<void> {
  await page.addInitScript(
    ([topology, update]) => {
      class FakeEventSource {
        onopen: ((ev: unknown) => void) | null = null;
        onerror: ((ev: unknown) => void) | null = null;
        onmessage: ((ev: { data: string }) => void) | null = null;
        url: string;
        constructor(url: string) {
          this.url = url;
          setTimeout(() => {
            this.onopen?.({});
            for (const ev of topology as unknown[]) {
              this.onmessage?.({ data: JSON.stringify(ev) });
            }
            setTimeout(() => {
              this.onmessage?.({ data: JSON.stringify(update) });
            }, 400);
          }, 0);
        }
        close() {}
      }
      (globalThis as unknown as { EventSource: unknown }).EventSource =
        FakeEventSource as unknown;
    },
    [topologyEvents, updateEvent] as const,
  );
}

async function routeSessions(route: Route): Promise<void> {
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) });
}

function collectErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text());
  });
  page.on('pageerror', (err) => errors.push(String(err)));
  return errors;
}

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', routeSessions);
  await installFakeSse(page);
});

test('outline does not trigger effect_update_depth_exceeded on data updates', async ({ page }) => {
  const errors = collectErrors(page);
  const loopErrors = () => errors.filter((e) => e.includes('effect_update_depth_exceeded'));

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();
  await expect(page.locator('.outline-row').filter({ hasText: 'Session Root' })).toBeVisible();

  expect(loopErrors(), `reactive-loop errors after topology:\n${loopErrors().join('\n')}`).toEqual([]);

  await page.waitForTimeout(1000);
  expect(loopErrors(), `reactive-loop errors after update:\n${loopErrors().join('\n')}`).toEqual([]);

  await page.locator('.outline-row').filter({ hasText: 'assistant' }).first().click();
  await expect(page.locator('.node-drawer.node-drawer--open')).toBeVisible();
  expect(loopErrors(), `reactive-loop errors after click:\n${loopErrors().join('\n')}`).toEqual([]);
});

test('clicking an outline row opens the drawer with metric values', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.locator('.outline-row').filter({ hasText: 'assistant' }).first().click();

  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/${turnId}`));

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toHaveClass(/node-drawer--open/);
  await drawer.locator('.advanced-summary').click();
  await expect(drawer.locator('.metric-row')).toHaveCount(5);
  await expect(drawer).toContainText('Tokens in');
  await expect(drawer).toContainText('10');
  await expect(drawer).toContainText('Tokens out');
  await expect(drawer).toContainText('5');
  await expect(drawer).toContainText('Cost');
  await expect(drawer.locator('.provenance-badge[data-provenance="estimated"]')).toBeVisible();
  await expect(drawer).toContainText('claude-opus-4-8');

  await page.keyboard.press('Escape');
  await expect(drawer).not.toHaveClass(/node-drawer--open/);
  await expect(page).toHaveURL(new RegExp(`/s/${sessionHash}$`));
});

test('deep-link #/s/{hash}/n/{id} opens the drawer on load', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}/n/${turnId}`);

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toHaveClass(/node-drawer--open/, { timeout: 8000 });
  await expect(drawer).toContainText('Assistant Turn');
  await drawer.locator('.advanced-summary').click();
  await expect(drawer.locator('.metric-row')).toHaveCount(5);
});

test('running session shows a live badge in the session header', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const badge = page.locator('.live-badge');
  await expect(badge).toBeVisible();
  await expect(badge).toContainText('live');
  await expect(badge.locator('.live-dot')).toBeVisible();
});

test('node navigation within a session does not reconnect SSE', async ({ page }) => {
  let constructionCount = 0;
  await page.addInitScript(
    ([topology]) => {
      let count = 0;
      class CountingEventSource {
        onopen: ((ev: unknown) => void) | null = null;
        onerror: ((ev: unknown) => void) | null = null;
        onmessage: ((ev: { data: string }) => void) | null = null;
        constructor(_url: string) {
          count += 1;
          (globalThis as unknown as { _sseCount: number })._sseCount = count;
          setTimeout(() => {
            this.onopen?.({});
            for (const ev of topology as unknown[]) {
              this.onmessage?.({ data: JSON.stringify(ev) });
            }
          }, 0);
        }
        close() {}
      }
      (globalThis as unknown as { EventSource: unknown }).EventSource = CountingEventSource as unknown;
    },
    [topologyEvents] as const,
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();
  await expect(page.locator('.outline-row').filter({ hasText: 'Session Root' })).toBeVisible();

  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);

  await page.locator('.outline-row').filter({ hasText: 'assistant' }).first().click();
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/${turnId}`));
  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);

  await page.locator('.outline-row').filter({ hasText: 'Session Root' }).click();
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/session-root`));
  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);

  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}$`));

  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);
});
