import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'evalfields0001evalfields0001aaaa';

const fakeSessions: SessionSummary[] = [{
  session: sessionHash,
  status: 'ok',
  started_at: '2026-06-20T10:00:00Z',
  duration_ms: 3000,
  tokens_in: 100,
  tokens_out: 50,
  cost_usd: 0.001,
  cost_source: 'reported',
  node_count: 2,
  tool_count: 1,
  error_count: 0,
  run_ids: ['run-ef'],
}];

const stepKeyHash = 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4';

const sseEventsWithFields: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-ef', type: 'session', name: 'eval fields session', status: 'ok', rev: 1 } },
  {
    kind: 'node_upsert', rev: 2,
    node: {
      id: 'n-tool',
      run_id: 'run-ef',
      type: 'tool_call',
      name: 'Bash',
      status: 'ok',
      step_key: stepKeyHash,
      step_key_method: 'hash_v1',
      annotations: { score: 0.95, label: 'correct' },
      rev: 2
    }
  },
  { kind: 'edge_upsert', rev: 3, edge: { id: 'e1', run_id: 'run-ef', type: 'parent_child', src: 'n-session', dst: 'n-tool', rev: 3 } },
];

const sseEventsPlain: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-ef', type: 'session', name: 'plain session', status: 'ok', rev: 1 } },
  { kind: 'node_upsert', rev: 2, node: { id: 'n-tool-plain', run_id: 'run-ef', type: 'tool_call', name: 'Bash', status: 'ok', rev: 2 } },
  { kind: 'edge_upsert', rev: 3, edge: { id: 'e1', run_id: 'run-ef', type: 'parent_child', src: 'n-session', dst: 'n-tool-plain', rev: 3 } },
];

async function routeWithFields(page: Page): Promise<void> {
  await page.route('/v1/sessions', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) })
  );
  await page.route('/v1/subscribe**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: sseEventsWithFields.map(e => `data: ${JSON.stringify(e)}\n\n`).join('') })
  );
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, (route: Route) =>
    route.fulfill({ status: 403 })
  );
}

async function routePlain(page: Page): Promise<void> {
  await page.route('/v1/sessions', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeSessions) })
  );
  await page.route('/v1/subscribe**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: sseEventsPlain.map(e => `data: ${JSON.stringify(e)}\n\n`).join('') })
  );
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, (route: Route) =>
    route.fulfill({ status: 403 })
  );
}

test('step_key and step_key_method shown in drawer when present', async ({ page }) => {
  await routeWithFields(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();

  const toolRow = page.locator('.outline-row').filter({ hasText: 'Bash' });
  await toolRow.click();
  await expect(page.locator('.node-drawer--open')).toBeVisible();

  // Expand Advanced section
  await page.locator('.advanced-summary').click();

  // Step key row should be present
  await expect(page.locator('.advanced-body')).toContainText('Step key');
  // The step_key is truncated to 16 chars by shortHash
  await expect(page.locator('.advanced-body')).toContainText(stepKeyHash.slice(0, 16));
  // Method shown as faint qualifier
  await expect(page.locator('.advanced-body')).toContainText('hash_v1');
});

test('annotations shown in drawer when present', async ({ page }) => {
  await routeWithFields(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();

  const toolRow = page.locator('.outline-row').filter({ hasText: 'Bash' });
  await toolRow.click();
  await expect(page.locator('.node-drawer--open')).toBeVisible();

  // Expand Advanced section
  await page.locator('.advanced-summary').click();

  // Annotations section should be present
  await expect(page.locator('.annotations-section')).toBeVisible();
  await expect(page.locator('.annotations-section')).toContainText('Annotations');
  // Keys sorted: 'label' before 'score'
  await expect(page.locator('.annotations-list .annotation-item').first().locator('.annotation-key')).toHaveText('label');
  await expect(page.locator('.annotations-list')).toContainText('correct');
  await expect(page.locator('.annotations-list')).toContainText('score');
  await expect(page.locator('.annotations-list')).toContainText('0.95');
});

test('step_key and annotations absent when node does not have them', async ({ page }) => {
  await routePlain(page);
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  await page.getByRole('button', { name: 'Expand all' }).click();

  const toolRow = page.locator('.outline-row').filter({ hasText: 'Bash' });
  await toolRow.click();
  await expect(page.locator('.node-drawer--open')).toBeVisible();

  // Expand Advanced section
  await page.locator('.advanced-summary').click();

  // Step key should NOT be present
  await expect(page.locator('.advanced-body')).not.toContainText('Step key');
  // Annotations section should NOT be present
  await expect(page.locator('.annotations-section')).toHaveCount(0);
});
