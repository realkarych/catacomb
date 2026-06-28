import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'marker0001aaaa0001marker0001aaaa';

const fakeSessions: SessionSummary[] = [{
  session: sessionHash,
  status: 'ok',
  started_at: '2026-06-20T10:00:00Z',
  duration_ms: 5000,
  tokens_in: 200,
  tokens_out: 100,
  cost_usd: 0.002,
  cost_source: 'reported',
  node_count: 3,
  tool_count: 0,
  error_count: 0,
  run_ids: ['run-mk'],
}];

const sseEvents: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-mk', type: 'session', name: 'marker test session', status: 'ok', t_start: '2026-06-20T10:00:00Z', rev: 1 } },
  { kind: 'node_upsert', rev: 2, node: { id: 'n-prompt', run_id: 'run-mk', type: 'user_prompt', name: 'user prompt', status: 'ok', t_start: '2026-06-20T10:00:01Z', tokens_in: 50, tokens_out: 30, cost_usd: 0.001, duration_ms: 500, rev: 2 } },
  { kind: 'node_upsert', rev: 3, node: { id: 'n-marker', run_id: 'run-mk', type: 'marker', name: 'init-phase', status: 'ok', t_start: '2026-06-20T10:00:00Z', t_end: '2026-06-20T10:00:05Z', duration_ms: 5000, phase_key: 'abcdef1234567890abcdef1234567890', rev: 3 } },
  { kind: 'edge_upsert', rev: 4, edge: { id: 'e-sess-prompt', run_id: 'run-mk', type: 'parent_child', src: 'n-session', dst: 'n-prompt', rev: 4 } },
  { kind: 'edge_upsert', rev: 5, edge: { id: 'e-sess-marker', run_id: 'run-mk', type: 'parent_child', src: 'n-session', dst: 'n-marker', rev: 5 } },
  { kind: 'edge_upsert', rev: 6, edge: { id: 'e-span', run_id: 'run-mk', type: 'marker_span', src: 'n-marker', dst: 'n-prompt', rev: 6 } },
];

async function routeBase(page: Page): Promise<void> {
  await page.route('/v1/sessions', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) })
  );
  await page.route('/v1/subscribe**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: sseEvents.map(e => `data: ${JSON.stringify(e)}\n\n`).join('') })
  );
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, (route: Route) =>
    route.fulfill({ status: 403, body: 'no payload' })
  );
}

test('marker row renders with phase name label (not "marker")', async ({ page }) => {
  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const markerRow = page.locator('.outline-row').filter({ hasText: 'init-phase' });
  await expect(markerRow).toBeVisible();
  await expect(markerRow.locator('.outline-primary')).toHaveText('init-phase');
});

test('marker row has no status dot', async ({ page }) => {
  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const markerRow = page.locator('.outline-row').filter({ hasText: 'init-phase' });
  await expect(markerRow).toBeVisible();
  await expect(markerRow.locator('.outline-dot')).toHaveCount(0);
});

test('marker row falls back to "phase" when name absent', async ({ page }) => {
  const noNameEvents: SseEvent[] = [
    { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-mk', type: 'session', name: 'session', status: 'ok', rev: 1 } },
    { kind: 'node_upsert', rev: 2, node: { id: 'n-marker-noname', run_id: 'run-mk', type: 'marker', status: 'ok', rev: 2 } },
    { kind: 'edge_upsert', rev: 3, edge: { id: 'e1', run_id: 'run-mk', type: 'parent_child', src: 'n-session', dst: 'n-marker-noname', rev: 3 } },
  ];
  await page.route('/v1/sessions', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) })
  );
  await page.route('/v1/subscribe**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: noNameEvents.map(e => `data: ${JSON.stringify(e)}\n\n`).join('') })
  );
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, (route: Route) =>
    route.fulfill({ status: 403 })
  );

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();
  const markerRow = page.locator('.outline-row').filter({ hasText: 'phase' }).first();
  await expect(markerRow).toBeVisible();
  // Should not say "marker"
  const primaryText = await markerRow.locator('.outline-primary').textContent();
  expect(primaryText).toBe('phase');
});

test('marker drawer shows phase_key and span count when present', async ({ page }) => {
  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();

  const markerRow = page.locator('.outline-row').filter({ hasText: 'init-phase' });
  await markerRow.click();
  await expect(page.locator('.node-drawer--open')).toBeVisible();

  // Expand the Advanced section
  await page.locator('.advanced-summary').click();

  // Phase key should be shown
  await expect(page.locator('.advanced-body')).toContainText('Phase key');
  await expect(page.locator('.advanced-body')).toContainText('abcdef123456'); // first 16 chars shown

  // Span should show member count
  await expect(page.locator('.advanced-body')).toContainText('Span');
  await expect(page.locator('.advanced-body')).toContainText('1 nodes');
});

test('tree structure intact with marker + marker_span edges — no console errors', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', msg => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });

  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  // Both session children (prompt + marker) should be visible
  await page.getByRole('button', { name: 'Expand all' }).click();
  await expect(page.locator('.outline-row').filter({ hasText: 'init-phase' })).toBeVisible();
  await expect(page.locator('.outline-row').filter({ hasText: 'prompt' })).toBeVisible();

  expect(consoleErrors).toHaveLength(0);
});
