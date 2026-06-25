import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

// Regression coverage for the graph reactivity loop + node-click drawer bug.
//
// The earlier hero/graph specs used a SINGLE static `page.route` body that
// delivered every node exactly once, in order. That never exercised the
// data-only update path in GraphCanvas (same topology, fresh node identity),
// so it could not reproduce the `effect_update_depth_exceeded` loop that broke
// the live app.
//
// To reproduce faithfully we install a fake EventSource (the app's SSE client
// reads `globalThis.EventSource`). The fake lets the test push events in two
// flushes: the initial topology, then — after the graph has mounted — a
// re-upsert of an existing node at a higher rev. The second flush keeps the
// topology key constant while handing GraphCanvas a brand-new catNode object,
// which is exactly what tripped the self-retriggering `$effect`.

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

// A second flush that re-upserts an existing node at a higher rev. Same
// topology (ids unchanged) -> forces GraphCanvas's data-update branch.
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

// Installs a fake EventSource that emits `topologyEvents` on open, then a second
// `updateEvent` flush after a short delay — mirroring a live stream that updates
// an already-rendered node.
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

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', routeSessions);
  await installFakeSse(page);
});

function collectErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text());
  });
  page.on('pageerror', (err) => errors.push(String(err)));
  return errors;
}

test('graph view does not trigger effect_update_depth_exceeded on data updates', async ({
  page,
}) => {
  const errors = collectErrors(page);
  const loopErrors = () => errors.filter((e) => e.includes('effect_update_depth_exceeded'));

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await page.getByRole('button', { name: 'Graph', exact: true }).click();
  await expect(page.locator('.graph-canvas-root')).toBeVisible();

  // The loop trips while the graph is mounting and aborts node rendering, so
  // assert on the error directly (not just downstream symptoms). The canvas
  // mounts, the topology streams in, then a re-upsert arrives that exercises
  // the data-update path — the exact sequence that looped on the broken build.
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });
  expect(loopErrors(), `reactive-loop errors after topology:\n${loopErrors().join('\n')}`).toEqual(
    [],
  );

  // Let the second flush (the re-upsert) land and reactivity settle.
  await page.waitForTimeout(1000);
  expect(loopErrors(), `reactive-loop errors after update:\n${loopErrors().join('\n')}`).toEqual([]);

  const turn = page.locator('.svelte-flow__node').filter({ hasText: 'Assistant Turn' });
  await turn.click();
  await expect(page.locator('.node-drawer.node-drawer--open')).toBeVisible();
  await page.waitForTimeout(300);
  expect(loopErrors(), `reactive-loop errors after click:\n${loopErrors().join('\n')}`).toEqual([]);
});

test('clicking a graph node opens the drawer with metric values and lights the node', async ({
  page,
}) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await page.getByRole('button', { name: 'Graph', exact: true }).click();
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });
  await page.waitForTimeout(1000);

  const turn = page.locator('.svelte-flow__node').filter({ hasText: 'Assistant Turn' });
  await turn.click();

  // Route reflects the selection.
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/${turnId}`));

  // Drawer renders its metric header with real values.
  const drawer = page.locator('.node-drawer');
  await expect(drawer).toHaveClass(/node-drawer--open/);
  await expect(drawer.locator('.metric-row')).toHaveCount(5);
  await expect(drawer).toContainText('Tokens in');
  await expect(drawer).toContainText('10');
  await expect(drawer).toContainText('Tokens out');
  await expect(drawer).toContainText('5');
  await expect(drawer).toContainText('Cost');
  await expect(drawer.locator('.provenance-badge[data-provenance="estimated"]')).toBeVisible();
  await expect(drawer).toContainText('claude-opus-4-8');

  // The clicked node is lit, and exactly one node carries the selection.
  await expect(page.locator('.graph-node--selected')).toHaveCount(1);
  await expect(
    page.locator('.graph-node--selected').filter({ hasText: 'Assistant Turn' }),
  ).toBeVisible();

  // Escape clears selection: drawer closes, node un-lit, route back to session.
  await page.keyboard.press('Escape');
  await expect(drawer).not.toHaveClass(/node-drawer--open/);
  await expect(page.locator('.graph-node--selected')).toHaveCount(0);
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}$`));
});

test('deep-link #/s/{hash}/n/{id} opens the drawer and lights the node on load', async ({
  page,
}) => {
  await page.goto(`/?token=test#/s/${sessionHash}/n/${turnId}`);
  await page.getByRole('button', { name: 'Graph', exact: true }).click();
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toHaveClass(/node-drawer--open/);
  await expect(drawer).toContainText('Assistant Turn');
  await expect(drawer.locator('.metric-row')).toHaveCount(5);
  await expect(page.locator('.graph-node--selected')).toHaveCount(1);
});

test('clicking a visible on-screen node does NOT change the viewport transform', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await page.getByRole('button', { name: 'Graph', exact: true }).click();
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });
  await page.waitForTimeout(1000);

  const viewport = page.locator('.svelte-flow__viewport');
  const transformBefore = await viewport.evaluate((el) => getComputedStyle(el as HTMLElement).transform);

  const turn = page.locator('.svelte-flow__node').filter({ hasText: 'Assistant Turn' });
  await turn.click();

  await expect(page.locator('.node-drawer.node-drawer--open')).toBeVisible();
  await page.waitForTimeout(400);

  const transformAfter = await viewport.evaluate((el) => getComputedStyle(el as HTMLElement).transform);
  expect(transformAfter).toBe(transformBefore);
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
  await page.getByRole('button', { name: 'Graph', exact: true }).click();
  await expect(page.locator('.svelte-flow__node')).toHaveCount(2, { timeout: 8000 });

  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);

  const turn = page.locator('.svelte-flow__node').filter({ hasText: 'Assistant Turn' });
  await turn.click();
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/${turnId}`));
  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);

  const sessionNode = page.locator('.svelte-flow__node').filter({ hasText: 'Session Root' });
  await sessionNode.click();
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/session-root`));
  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);

  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}$`));

  constructionCount = await page.evaluate(() => (globalThis as unknown as { _sseCount: number })._sseCount ?? 0);
  expect(constructionCount).toBe(1);
});
