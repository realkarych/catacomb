import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'f00dbeef0002f00dbeef0002f00dbeef';
const rootNodeId = 'node-kbtest-root';
const childNodeId = 'node-kbtest-child';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'ok',
    started_at: '2024-06-01T10:00:00Z',
    duration_ms: 5000,
    tokens_in: 500,
    tokens_out: 1000,
    cost_usd: 0.01,
    cost_source: 'reported',
    node_count: 2,
    tool_count: 0,
    error_count: 0,
    model_id: 'claude-opus-4',
    run_ids: ['run-kbtest'],
  },
];

const topologyEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 1,
    node: {
      id: rootNodeId,
      run_id: 'run-kbtest',
      type: 'session',
      name: 'Root Node',
      status: 'ok',
      rev: 1,
    },
  },
  {
    kind: 'node_upsert',
    rev: 2,
    node: {
      id: childNodeId,
      run_id: 'run-kbtest',
      type: 'user_prompt',
      name: 'Child Node',
      status: 'ok',
      rev: 2,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 3,
    edge: {
      id: 'edge-kbtest-1',
      run_id: 'run-kbtest',
      type: 'parent_child',
      src: rootNodeId,
      dst: childNodeId,
      rev: 3,
    },
  },
];

async function installFakeSse(page: Page): Promise<void> {
  await page.addInitScript(
    ([topology]) => {
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
          }, 0);
        }
        close() {}
      }
      (globalThis as unknown as { EventSource: unknown }).EventSource =
        FakeEventSource as unknown;
    },
    [topologyEvents] as const,
  );
}

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(fakeSessions),
    });
  });
  await page.route(`/v1/sessions/${sessionHash}/graph`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(topologyEvents),
    });
  });
  await installFakeSse(page);
});

test('ArrowRight from canvas with no selection selects root and updates URL hash', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  const canvas = page.locator('[role="application"][aria-label="Session graph"]');
  await canvas.focus();

  await page.keyboard.press('ArrowRight');

  await expect(page.locator('.graph-node--selected')).toHaveCount(1, { timeout: 3000 });
  await expect(page.locator('[role="button"][aria-current="true"]')).toBeVisible();

  const hash = await page.evaluate(() => window.location.hash);
  expect(hash).toContain(`/n/${rootNodeId}`);
});

test('ArrowRight CHAINING: two consecutive arrows with no re-focus traverse two different nodes', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  const canvas = page.locator('[role="application"][aria-label="Session graph"]');
  await canvas.focus();

  await page.keyboard.press('ArrowRight');
  await expect(page.locator('[role="button"][aria-current="true"]')).toHaveAttribute(
    'aria-label',
    /Root Node/,
    { timeout: 3000 },
  );
  const hashAfterFirst = await page.evaluate(() => window.location.hash);
  expect(hashAfterFirst).toContain(`/n/${rootNodeId}`);

  await page.keyboard.press('ArrowRight');

  await expect(page.locator('[role="button"][aria-current="true"]')).toHaveAttribute(
    'aria-label',
    /Child Node/,
    { timeout: 3000 },
  );

  const hashAfterSecond = await page.evaluate(() => window.location.hash);
  expect(hashAfterSecond).toContain(`/n/${childNodeId}`);
  expect(hashAfterSecond).not.toContain(`/n/${rootNodeId}`);

  await expect(page.locator('.node-drawer--open .drawer-title')).toContainText('Child Node');
});

test('ArrowRight again selects neighbor and drawer title, aria-current, URL hash all agree', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  const canvas = page.locator('[role="application"][aria-label="Session graph"]');
  await canvas.focus();

  await page.keyboard.press('ArrowRight');
  await expect(page.locator('[role="button"][aria-current="true"]')).toHaveAttribute(
    'aria-label',
    /Root Node/,
    { timeout: 3000 },
  );

  const canvas2 = page.locator('[role="application"][aria-label="Session graph"]');
  await canvas2.focus();
  await page.keyboard.press('ArrowRight');

  await expect(page.locator('[role="button"][aria-current="true"]')).toHaveAttribute(
    'aria-label',
    /Child Node/,
    { timeout: 3000 },
  );

  const hash = await page.evaluate(() => window.location.hash);
  expect(hash).toContain(`/n/${childNodeId}`);

  await expect(page.locator('.node-drawer--open .drawer-title')).toContainText('Child Node');
});

test('Enter opens drawer then Escape returns focus to node button (not body)', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  const rootNode = page.locator('.svelte-flow__node').filter({ hasText: 'Root Node' });
  const nodeBtn = rootNode.locator('.graph-node');
  await nodeBtn.click();
  await expect(page.locator('.node-drawer--open')).toBeVisible();

  await page.keyboard.press('Escape');

  await expect(page.locator('.node-drawer--open')).not.toBeVisible();

  await page.waitForTimeout(450);

  const activeInfo = await page.evaluate(() => {
    const el = document.activeElement;
    if (!el) return 'null';
    return `${el.tagName.toLowerCase()}|role=${el.getAttribute('role') ?? ''}`;
  });
  expect(activeInfo).not.toContain('body');
  expect(activeInfo).toContain('role=button');
});

test('typing arrow keys in the node-search box does not move graph selection', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  const canvas = page.locator('[role="application"][aria-label="Session graph"]');
  await canvas.focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('.graph-node--selected')).toHaveCount(1, { timeout: 3000 });

  const hashBefore = await page.evaluate(() => window.location.hash);
  expect(hashBefore).toContain(`/n/${rootNodeId}`);

  const searchBox = page.locator('input[aria-label="Search nodes by name or type"]');
  await searchBox.focus();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');

  const hashAfter = await page.evaluate(() => window.location.hash);
  expect(hashAfter).toContain(`/n/${rootNodeId}`);

  await expect(page.locator('.graph-node--selected')).toHaveCount(1);
  await expect(page.locator('[role="button"][aria-current="true"]')).toHaveAttribute(
    'aria-label',
    /Root Node/,
  );
});
