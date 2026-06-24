import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'deadbeef0001deadbeef0001deadbeef';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'ok',
    started_at: '2024-06-01T10:00:00Z',
    duration_ms: 12500,
    tokens_in: 1500,
    tokens_out: 3200,
    cost_usd: 0.0234,
    cost_source: 'reported',
    node_count: 4,
    tool_count: 2,
    error_count: 0,
    model_id: 'claude-opus-4',
    run_ids: ['run1'],
  },
];

const sseEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 1,
    node: {
      id: 'node-session-1',
      run_id: 'run1',
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
      id: 'node-user-1',
      run_id: 'run1',
      type: 'user_prompt',
      name: 'User Prompt',
      status: 'ok',
      tokens_in: 100,
      tokens_out: 200,
      rev: 2,
    },
  },
  {
    kind: 'node_upsert',
    rev: 3,
    node: {
      id: 'node-tool-1',
      run_id: 'run1',
      type: 'tool_call',
      name: 'BashTool',
      status: 'ok',
      tokens_in: 50,
      tokens_out: 80,
      rev: 3,
    },
  },
  {
    kind: 'node_upsert',
    rev: 4,
    node: {
      id: 'node-orphan-1',
      run_id: 'run1',
      type: 'marker',
      name: 'Orphan Node',
      status: 'pending',
      rev: 4,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 5,
    edge: {
      id: 'edge-1',
      run_id: 'run1',
      type: 'parent_child',
      src: 'node-session-1',
      dst: 'node-user-1',
      rev: 5,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 6,
    edge: {
      id: 'edge-2',
      run_id: 'run1',
      type: 'sequence',
      src: 'node-user-1',
      dst: 'node-tool-1',
      rev: 6,
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

  await page.route(`/v1/sessions/${sessionHash}/graph`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: buildSseBody(sseEvents),
    });
  });

  await page.route('/v1/events**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: buildSseBody(sseEvents),
    });
  });
});

test('session view renders graph canvas with nodes', async ({ page }) => {
  await page.goto(`/#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.graph-canvas-root')).toBeVisible();
});

test('graph canvas is visible when navigating from session list', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.sessions-list')).toBeVisible();
  await page.locator('.session-row').first().click();
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.graph-canvas-root')).toBeVisible();
});

test('deep-link to session-node route renders session view', async ({ page }) => {
  await page.goto(`/#/s/${sessionHash}/n/node-user-1`);
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.graph-canvas-root')).toBeVisible();
  await expect(page.locator('.session-view')).toContainText('node-user-1');
});

test('empty session shows waiting state', async ({ page }) => {
  const emptyHash = 'aaaa0000aaaa0000aaaa0000aaaa0000';
  const emptySessions: SessionSummary[] = [
    {
      session: emptyHash,
      status: 'running',
      tokens_in: 0,
      tokens_out: 0,
      node_count: 0,
      tool_count: 0,
      error_count: 0,
      run_ids: ['run-empty'],
    },
  ];
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(emptySessions),
    });
  });
  await page.goto(`/#/s/${emptyHash}`);
  await expect(page.locator('.session-view')).toBeVisible();
  await expect(page.locator('.graph-canvas-root')).toBeVisible();
  await expect(page.locator('.empty-state-headline')).toContainText('Waiting');
});
