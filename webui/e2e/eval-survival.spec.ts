import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

// This regression test ensures that a session with NONE of the new eval fields
// (no step_key, no phase_key, no annotations, no repro) renders identically
// to before the feature was added.

const sessionHash = 'survival0001aaaasurvival0001aaaa';

const fakeSessions: SessionSummary[] = [{
  session: sessionHash,
  status: 'ok',
  started_at: '2026-06-20T10:00:00Z',
  duration_ms: 2100,
  tokens_in: 100,
  tokens_out: 50,
  cost_usd: 0.001,
  cost_source: 'reported',
  node_count: 3,
  tool_count: 1,
  error_count: 0,
  model_id: 'claude-opus-4-8',
  run_ids: ['run-sv'],
}];

// Classic session fixture — no new fields
const sseEvents: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-sv', type: 'session', name: 'Session Root', status: 'ok', t_start: '2026-06-20T10:00:00Z', rev: 1 } },
  { kind: 'node_upsert', rev: 2, node: { id: 'n-prompt', run_id: 'run-sv', type: 'user_prompt', name: 'user prompt', status: 'ok', t_start: '2026-06-20T10:00:01Z', rev: 2 } },
  { kind: 'edge_upsert', rev: 3, edge: { id: 'e1', run_id: 'run-sv', type: 'parent_child', src: 'n-session', dst: 'n-prompt', rev: 3 } },
  { kind: 'node_upsert', rev: 4, node: { id: 'n-turn', run_id: 'run-sv', type: 'assistant_turn', name: 'assistant turn', status: 'ok', t_start: '2026-06-20T10:00:02Z', tokens_in: 80, tokens_out: 40, cost_usd: 0.0009, rev: 4 } },
  { kind: 'edge_upsert', rev: 5, edge: { id: 'e2', run_id: 'run-sv', type: 'parent_child', src: 'n-prompt', dst: 'n-turn', rev: 5 } },
  { kind: 'node_upsert', rev: 6, node: { id: 'n-tool', run_id: 'run-sv', type: 'tool_call', name: 'Bash', status: 'ok', t_start: '2026-06-20T10:00:03Z', duration_ms: 307, rev: 6 } },
  { kind: 'edge_upsert', rev: 7, edge: { id: 'e3', run_id: 'run-sv', type: 'parent_child', src: 'n-turn', dst: 'n-tool', rev: 7 } },
];

async function routeBase(page: Page): Promise<void> {
  await page.route('/v1/sessions', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) })
  );
  await page.route('/v1/subscribe**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: sseEvents.map(e => `data: ${JSON.stringify(e)}\n\n`).join('') })
  );
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, (route: Route) =>
    route.fulfill({ status: 403 })
  );
}

test('session without new eval fields: outline renders all rows', async ({ page }) => {
  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  // Session root visible
  await expect(page.locator('.outline-row').filter({ hasText: 'session' }).first()).toBeVisible();
  // No repro section rendered
  await expect(page.locator('.repro-section')).toHaveCount(0);
});

test('session without new eval fields: no console errors', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', msg => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });

  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();
  await page.getByRole('button', { name: 'Expand all' }).click();
  await expect(page.locator('.outline-row').filter({ hasText: 'Bash' })).toBeVisible();

  expect(consoleErrors).toHaveLength(0);
});

test('session without new fields: tool node drawer shows no Step key, Annotations', async ({ page }) => {
  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();
  const toolRow = page.locator('.outline-row').filter({ hasText: 'Bash' });
  await toolRow.click();
  await expect(page.locator('.node-drawer--open')).toBeVisible();

  await page.locator('.advanced-summary').click();

  await expect(page.locator('.advanced-body')).not.toContainText('Step key');
  await expect(page.locator('.annotations-section')).toHaveCount(0);
});

test('session without new fields: no marker rows appear', async ({ page }) => {
  await routeBase(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();
  await page.getByRole('button', { name: 'Expand all' }).click();

  // No 'phase' label should appear
  await expect(page.locator('.outline-row').filter({ hasText: 'phase' })).toHaveCount(0);
  await expect(page.locator('.outline-row').filter({ hasText: 'init-phase' })).toHaveCount(0);
});
