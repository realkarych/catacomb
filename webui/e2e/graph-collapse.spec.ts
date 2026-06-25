import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const hash = 'c0llapse0001c0llapse0001c0llap01';

const sessions: SessionSummary[] = [
  {
    session: hash,
    status: 'running',
    tokens_in: 100,
    tokens_out: 200,
    node_count: 7,
    tool_count: 3,
    error_count: 0,
    run_ids: ['run1'],
  },
];

const base: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 's', run_id: 'run1', type: 'session', name: 'Session', status: 'ok', rev: 1 } },
  { kind: 'node_upsert', rev: 2, node: { id: 'u', run_id: 'run1', type: 'user_prompt', name: 'Prompt', status: 'ok', rev: 2 } },
  { kind: 'node_upsert', rev: 3, node: { id: 'at', run_id: 'run1', type: 'assistant_turn', name: 'Turn', status: 'ok', tokens_in: 10, tokens_out: 20, rev: 3 } },
  { kind: 'node_upsert', rev: 4, node: { id: 't1', run_id: 'run1', type: 'tool_call', name: 'Bash', status: 'ok', tokens_in: 5, tokens_out: 7, rev: 4 } },
  { kind: 'node_upsert', rev: 5, node: { id: 't2', run_id: 'run1', type: 'tool_call', name: 'Read', status: 'ok', tokens_in: 3, tokens_out: 4, rev: 5 } },
  { kind: 'node_upsert', rev: 6, node: { id: 'sub', run_id: 'run1', type: 'subagent', name: 'Worker', status: 'ok', rev: 6 } },
  { kind: 'node_upsert', rev: 7, node: { id: 'subc', run_id: 'run1', type: 'tool_call', name: 'Grep', status: 'ok', rev: 7 } },
  { kind: 'edge_upsert', rev: 8, edge: { id: 'e1', run_id: 'run1', type: 'parent_child', src: 's', dst: 'u', rev: 8 } },
  { kind: 'edge_upsert', rev: 9, edge: { id: 'e2', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'at', rev: 9 } },
  { kind: 'edge_upsert', rev: 10, edge: { id: 'e3', run_id: 'run1', type: 'parent_child', src: 'at', dst: 't1', rev: 10 } },
  { kind: 'edge_upsert', rev: 11, edge: { id: 'e4', run_id: 'run1', type: 'parent_child', src: 'at', dst: 't2', rev: 11 } },
  { kind: 'edge_upsert', rev: 12, edge: { id: 'e5', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'sub', rev: 12 } },
  { kind: 'edge_upsert', rev: 13, edge: { id: 'e6', run_id: 'run1', type: 'parent_child', src: 'sub', dst: 'subc', rev: 13 } },
];

function node(page: import('@playwright/test').Page, id: string) {
  return page.locator(`.svelte-flow__node[data-id="${id}"]`);
}

async function installPushableSse(page: import('@playwright/test').Page): Promise<void> {
  await page.addInitScript(() => {
    const g = globalThis as unknown as {
      EventSource: unknown;
      __pushSse: (ev: unknown) => void;
    };
    let live: ((ev: { data: string }) => void) | null = null;
    class PushableEventSource {
      onopen: ((ev: unknown) => void) | null = null;
      onerror: ((ev: unknown) => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      constructor(_url: string) {
        setTimeout(() => {
          this.onopen?.({});
          live = (ev) => this.onmessage?.(ev);
        }, 0);
      }
      close() {
        live = null;
      }
    }
    g.EventSource = PushableEventSource as unknown;
    g.__pushSse = (ev: unknown) => {
      live?.({ data: JSON.stringify(ev) });
    };
  });
}

async function pushSse(page: import('@playwright/test').Page, events: SseEvent[]): Promise<void> {
  await page.evaluate((evs) => {
    const push = (globalThis as unknown as { __pushSse: (ev: unknown) => void }).__pushSse;
    for (const ev of evs) push(ev);
  }, events);
}

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', async (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sessions) }),
  );
  await page.route(`/v1/sessions/${hash}/graph`, async (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(base) }),
  );
  await page.route('/v1/subscribe**', async (route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }),
  );
});

test('default view shows the spine, not the leaves', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await expect(node(page, 's')).toBeVisible();
  await expect(node(page, 'u')).toBeVisible();
  await expect(node(page, 'at')).toBeVisible();
  await expect(node(page, 'sub')).toBeVisible();
  await expect(node(page, 't1')).toHaveCount(0);
  await expect(node(page, 't2')).toHaveCount(0);
  await expect(node(page, 'subc')).toHaveCount(0);
});

test('expanding a turn reveals its tool calls', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await node(page, 'at').locator('.graph-node-toggle').click();
  await expect(node(page, 't1')).toBeVisible();
  await expect(node(page, 't2')).toBeVisible();
});

test('collapsing a turn re-hides its tool calls', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await node(page, 'at').locator('.graph-node-toggle').click();
  await expect(node(page, 't1')).toBeVisible();
  await node(page, 'at').locator('.graph-node-toggle').click();
  await expect(node(page, 't1')).toHaveCount(0);
  await expect(node(page, 't2')).toHaveCount(0);
});

test('expanding a subagent reveals its subtree', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await node(page, 'sub').locator('.graph-node-toggle').click();
  await expect(node(page, 'subc')).toBeVisible();
});

test('collapsed turn shows an aggregate badge', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  const badge = node(page, 'at').locator('.graph-node-badge-stat');
  await expect(badge).toBeVisible();
  await expect(badge).toContainText('2 ·');
});

test('toggle is separate from body-click selection', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await node(page, 'at').locator('.graph-node-toggle').click();
  await expect(page).toHaveURL(new RegExp(`#/s/${hash}$`));
  await node(page, 'at').click();
  await expect(page).toHaveURL(new RegExp('/n/at$'));
});

test('collapse all hides everything below the roots; expand all reveals leaves', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await page.getByRole('button', { name: 'Expand all' }).click();
  await expect(node(page, 't1')).toBeVisible();
  await expect(node(page, 'subc')).toBeVisible();
  await page.getByRole('button', { name: 'Collapse all' }).click();
  await expect(node(page, 's')).toBeVisible();
  await expect(node(page, 'u')).toHaveCount(0);
});

test('a collapsed subagent keeps its spine edge', async ({ page }) => {
  await page.goto(`/?token=test#/s/${hash}`);
  await expect(node(page, 'sub')).toBeVisible();
  await expect(page.locator('.svelte-flow__edge')).not.toHaveCount(0);
});

test('a turn streamed in live arrives collapsed per default policy', async ({ page }) => {
  await installPushableSse(page);
  await page.goto(`/?token=test#/s/${hash}`);
  await expect(node(page, 'at')).toBeVisible();

  await pushSse(page, [
    { kind: 'node_upsert', rev: 20, node: { id: 'at2', run_id: 'run1', type: 'assistant_turn', name: 'Turn2', status: 'running', tokens_in: 11, tokens_out: 22, rev: 20 } },
    { kind: 'node_upsert', rev: 21, node: { id: 't4', run_id: 'run1', type: 'tool_call', name: 'Edit', status: 'ok', tokens_in: 1, tokens_out: 2, rev: 21 } },
    { kind: 'edge_upsert', rev: 22, edge: { id: 'e8', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'at2', rev: 22 } },
    { kind: 'edge_upsert', rev: 23, edge: { id: 'e9', run_id: 'run1', type: 'parent_child', src: 'at2', dst: 't4', rev: 23 } },
  ]);

  await expect(node(page, 'at2')).toBeVisible();
  await expect(node(page, 'at2').locator('.graph-node-toggle')).toHaveText('▸');
  await expect(node(page, 't4')).toHaveCount(0);
});

test('a turn whose child edge lags still ends up collapsed once children arrive', async ({ page }) => {
  await installPushableSse(page);
  await page.goto(`/?token=test#/s/${hash}`);
  await expect(node(page, 'at')).toBeVisible();

  await pushSse(page, [
    { kind: 'node_upsert', rev: 20, node: { id: 'at2', run_id: 'run1', type: 'assistant_turn', name: 'Turn2', status: 'running', rev: 20 } },
    { kind: 'edge_upsert', rev: 21, edge: { id: 'e8', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'at2', rev: 21 } },
  ]);
  await expect(node(page, 'at2')).toBeVisible();
  await expect(node(page, 'at2').locator('.graph-node-toggle')).toHaveCount(0);

  await pushSse(page, [
    { kind: 'node_upsert', rev: 22, node: { id: 't4', run_id: 'run1', type: 'tool_call', name: 'Edit', status: 'ok', rev: 22 } },
    { kind: 'edge_upsert', rev: 23, edge: { id: 'e9', run_id: 'run1', type: 'parent_child', src: 'at2', dst: 't4', rev: 23 } },
  ]);
  await expect(node(page, 'at2').locator('.graph-node-toggle')).toHaveText('▸');
  await expect(node(page, 't4')).toHaveCount(0);
});

test('a user-expanded turn stays expanded across a later live delta', async ({ page }) => {
  await installPushableSse(page);
  await page.goto(`/?token=test#/s/${hash}`);
  await node(page, 'at').locator('.graph-node-toggle').click();
  await expect(node(page, 't1')).toBeVisible();
  await expect(node(page, 'at').locator('.graph-node-toggle')).toHaveText('▾');

  await pushSse(page, [
    { kind: 'node_upsert', rev: 20, node: { id: 'at2', run_id: 'run1', type: 'assistant_turn', name: 'Turn2', status: 'ok', rev: 20 } },
    { kind: 'edge_upsert', rev: 21, edge: { id: 'e8', run_id: 'run1', type: 'parent_child', src: 'u', dst: 'at2', rev: 21 } },
    { kind: 'node_upsert', rev: 22, node: { id: 't4', run_id: 'run1', type: 'tool_call', name: 'Edit', status: 'ok', rev: 22 } },
    { kind: 'edge_upsert', rev: 23, edge: { id: 'e9', run_id: 'run1', type: 'parent_child', src: 'at2', dst: 't4', rev: 23 } },
  ]);

  await expect(node(page, 'at2')).toBeVisible();
  await expect(node(page, 'at2').locator('.graph-node-toggle')).toHaveText('▸');
  await expect(node(page, 'at')).toBeVisible();
  await expect(node(page, 'at').locator('.graph-node-toggle')).toHaveText('▾');
  await expect(node(page, 't1')).toBeVisible();
});

test('a live node under a collapsed parent bumps the badge without appearing', async ({ page }) => {
  const withLate: SseEvent[] = [
    ...base,
    { kind: 'node_upsert', rev: 14, node: { id: 't3', run_id: 'run1', type: 'tool_call', name: 'Late', status: 'ok', tokens_in: 100, tokens_out: 100, rev: 14 } },
    { kind: 'edge_upsert', rev: 15, edge: { id: 'e7', run_id: 'run1', type: 'parent_child', src: 'at', dst: 't3', rev: 15 } },
  ];
  await page.route(`/v1/sessions/${hash}/graph`, async (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(withLate) }),
  );
  await page.goto(`/?token=test#/s/${hash}`);
  await expect(node(page, 'at')).toBeVisible();
  await expect(node(page, 't3')).toHaveCount(0);
  await expect(node(page, 'at').locator('.graph-node-badge-stat')).toContainText('3 ·');
});
