import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent, PayloadView } from '../web/src/lib/types';

const sessionHash = 'out11ne0001out11ne0001out11ne000';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'ok',
    started_at: '2026-06-20T10:00:00Z',
    duration_ms: 2100,
    tokens_in: 100,
    tokens_out: 50,
    cost_usd: 0.001,
    cost_source: 'reported',
    node_count: 4,
    tool_count: 1,
    error_count: 0,
    model_id: 'claude-opus-4-8',
    run_ids: ['run-out'],
  },
];

const sseEvents: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-out', type: 'session', name: 'Session Root', status: 'ok', t_start: '2026-06-20T10:00:00Z', rev: 1 } },
  { kind: 'node_upsert', rev: 2, node: { id: 'n-prompt', run_id: 'run-out', type: 'user_prompt', name: 'user prompt', status: 'ok', t_start: '2026-06-20T10:00:01Z', payload_hash: 'ph-prompt', rev: 2 } },
  { kind: 'edge_upsert', rev: 3, edge: { id: 'e1', run_id: 'run-out', type: 'parent_child', src: 'n-session', dst: 'n-prompt', rev: 3 } },
  { kind: 'node_upsert', rev: 4, node: { id: 'n-turn', run_id: 'run-out', type: 'assistant_turn', name: 'assistant turn', status: 'ok', t_start: '2026-06-20T10:00:02Z', tokens_in: 80, tokens_out: 40, cost_usd: 0.0009, payload_hash: 'ph-turn', rev: 4 } },
  { kind: 'edge_upsert', rev: 5, edge: { id: 'e2', run_id: 'run-out', type: 'parent_child', src: 'n-prompt', dst: 'n-turn', rev: 5 } },
  { kind: 'node_upsert', rev: 6, node: { id: 'n-tool', run_id: 'run-out', type: 'tool_call', name: 'BashTool', status: 'ok', t_start: '2026-06-20T10:00:03Z', tokens_in: 10, tokens_out: 5, rev: 6 } },
  { kind: 'edge_upsert', rev: 7, edge: { id: 'e3', run_id: 'run-out', type: 'parent_child', src: 'n-turn', dst: 'n-tool', rev: 7 } },
];

const promptPayload: PayloadView = {
  node_id: 'n-prompt',
  payload_hash: 'ph-prompt',
  input: 'Refactor the parser to stream tokens\nand keep the second line hidden',
  redactions: [],
  redacted: false,
};

function buildSseBody(events: SseEvent[]): string {
  return events.map((ev) => `data: ${JSON.stringify(ev)}\n\n`).join('');
}

async function routeBase(page: Page): Promise<void> {
  await page.route('/v1/sessions', async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) }),
  );
  await page.route('/v1/subscribe**', async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: buildSseBody(sseEvents) }),
  );
}

test('outline is the default view and renders the indented tree', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);

  await expect(page.getByRole('button', { name: 'Outline', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('.outline-root')).toBeVisible();
  await expect(page.locator('[role="tree"]')).toBeVisible();

  await expect(page.locator('.outline-row')).not.toHaveCount(0);
  await expect(page.locator('.outline-row').filter({ hasText: 'session' }).first()).toBeVisible();

  const promptRow = page.locator('.outline-row').filter({ hasText: 'prompt' }).first();
  await expect(promptRow).toBeVisible();
  await expect(promptRow).toHaveAttribute('aria-level', '2');
  await expect(page.locator('.outline-row').filter({ hasText: 'assistant' })).toHaveCount(0);

  await promptRow.locator('.outline-chevron').click();
  await expect(page.locator('.outline-row').filter({ hasText: 'assistant' }).first()).toHaveAttribute('aria-level', '3');
});

test('chevron toggles children and does not select the row', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  // Non-root collapsible nodes start collapsed, so the prompt's subtree is hidden.
  const promptRow = page.locator('.outline-row').filter({ hasText: 'prompt' }).first();
  await expect(promptRow).toBeVisible();
  await expect(promptRow.locator('.outline-chevron')).toHaveText('▸');
  await expect(page.locator('.outline-row').filter({ hasText: 'assistant' })).toHaveCount(0);

  await promptRow.locator('.outline-chevron').click();
  await expect(page).not.toHaveURL(/\/n\//);
  const turnRow = page.locator('.outline-row').filter({ hasText: 'assistant' }).first();
  await expect(turnRow).toBeVisible();

  await turnRow.locator('.outline-chevron').click();
  await expect(page.locator('.outline-row').filter({ hasText: 'BashTool' })).toBeVisible();

  await promptRow.locator('.outline-chevron').click();
  await expect(page.locator('.outline-row').filter({ hasText: 'assistant' })).toHaveCount(0);
});

test('clicking a row body opens the drawer and selects it', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const promptRow = page.locator('.outline-row').filter({ hasText: 'prompt' }).first();
  await promptRow.click();

  await expect(page).toHaveURL(new RegExp(`#/s/${sessionHash}/n/n-prompt`));
  await expect(page.locator('.node-drawer--open')).toBeVisible();
  await expect(promptRow).toHaveAttribute('data-selected', 'true');
});

test('conversation rows show a redacted-endpoint snippet', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const promptRow = page.locator('.outline-row').filter({ hasText: 'prompt' }).first();
  await expect(promptRow.locator('.outline-snippet')).toHaveText('Refactor the parser to stream tokens', { timeout: 5000 });
  await expect(page.locator('.outline-snippet').filter({ hasText: 'second line hidden' })).toHaveCount(0);
});

test('gated payload (403) yields no snippet but keeps the cheap label', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 403, body: 'payload access disabled' }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await expect(page.locator('.outline-row').filter({ hasText: 'prompt' }).first()).toBeVisible();
  await page.waitForTimeout(500);
  await expect(page.locator('.outline-snippet')).toHaveCount(0);
});

test('text-less conversation nodes never fetch a payload (no 404s)', async ({ page }) => {
  const textlessEvents: SseEvent[] = [
    { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-out', type: 'session', name: 'Session Root', status: 'ok', rev: 1 } },
    { kind: 'node_upsert', rev: 2, node: { id: 'n-prompt', run_id: 'run-out', type: 'user_prompt', name: 'user prompt', status: 'ok', payload_hash: 'ph-prompt', rev: 2 } },
    { kind: 'edge_upsert', rev: 3, edge: { id: 'e1', run_id: 'run-out', type: 'parent_child', src: 'n-session', dst: 'n-prompt', rev: 3 } },
    { kind: 'node_upsert', rev: 4, node: { id: 'n-turn-empty', run_id: 'run-out', type: 'assistant_turn', name: 'assistant turn', status: 'ok', rev: 4 } },
    { kind: 'edge_upsert', rev: 5, edge: { id: 'e2', run_id: 'run-out', type: 'parent_child', src: 'n-session', dst: 'n-turn-empty', rev: 5 } },
  ];

  await page.route('/v1/sessions', async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) }),
  );
  await page.route('/v1/subscribe**', async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: buildSseBody(textlessEvents) }),
  );

  const payloadPaths: string[] = [];
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) => {
    payloadPaths.push(new URL(route.request().url()).pathname);
    if (route.request().url().includes('n-prompt')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) });
    } else {
      await route.fulfill({ status: 404, body: 'payload not found' });
    }
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();
  await expect(page.locator('.outline-row').filter({ hasText: 'assistant' }).first()).toBeVisible();
  await page.waitForTimeout(500);

  expect(payloadPaths.some((p) => p.includes('n-turn-empty'))).toBe(false);
  expect(payloadPaths.some((p) => p.includes('n-prompt'))).toBe(true);
});

test('collapsed parent row shows an aggregate badge with a status dot', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  // The prompt row is collapsed-with-children by default, so it shows the aggregate badge.
  const promptRow = page.locator('.outline-row').filter({ hasText: 'prompt' }).first();
  await expect(promptRow.locator('.outline-stat-text')).toContainText('·');
  await expect(promptRow.locator('.outline-dot')).toBeVisible();
});

test('keyboard ArrowDown selects and Enter opens the drawer', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.locator('[role="tree"]').focus();
  await page.keyboard.press('ArrowDown');
  await expect(page).toHaveURL(/\/n\//);
  await expect(page.locator('.outline-row[data-selected="true"]')).toHaveCount(1);

  await page.keyboard.press('Enter');
  await expect(page.locator('.node-drawer--open')).toBeVisible();
});

test('collapse all and expand all toolbar buttons work', async ({ page }) => {
  await routeBase(page);
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) }),
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();
  await expect(page.locator('.outline-row').filter({ hasText: 'BashTool' })).toBeVisible();

  await page.getByRole('button', { name: 'Collapse all' }).click();
  await expect(page.locator('.outline-row').filter({ hasText: 'BashTool' })).toHaveCount(0);
  await expect(page.locator('.outline-row').filter({ hasText: 'session' }).first()).toBeVisible();
});
