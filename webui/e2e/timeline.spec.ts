import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'cccc1111cccc1111cccc1111cccc1111';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'ok',
    started_at: '2024-06-01T10:00:00Z',
    duration_ms: 4000,
    tokens_in: 1000,
    tokens_out: 2000,
    cost_usd: 0.01,
    cost_source: 'reported',
    node_count: 5,
    tool_count: 1,
    error_count: 0,
    model_id: 'claude-opus-4',
    run_ids: ['run-tl1'],
  },
];

const sseEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 0,
    node: {
      id: 'tl-session-1',
      run_id: 'run-tl1',
      type: 'session',
      name: 'Session',
      status: 'ok',
      t_start: '2024-06-01T09:00:00.000Z',
      t_end: '2024-06-01T10:00:03.000Z',
      rev: 0,
    },
  },
  {
    kind: 'node_upsert',
    rev: 4,
    node: {
      id: 'tl-mcp-1',
      run_id: 'run-tl1',
      type: 'mcp_call',
      name: 'mcp__playwright__browser_navigate',
      status: 'ok',
      t_start: '2024-06-01T10:00:02.500Z',
      t_end: '2024-06-01T10:00:02.900Z',
      duration_ms: 400,
      rev: 4,
    },
  },
  {
    kind: 'node_upsert',
    rev: 1,
    node: {
      id: 'tl-tool-1',
      run_id: 'run-tl1',
      type: 'tool_call',
      name: 'BashTool',
      status: 'ok',
      t_start: '2024-06-01T10:00:01.000Z',
      t_end: '2024-06-01T10:00:02.000Z',
      duration_ms: 1000,
      rev: 1,
    },
  },
  {
    kind: 'node_upsert',
    rev: 2,
    node: {
      id: 'tl-assistant-1',
      run_id: 'run-tl1',
      type: 'assistant_turn',
      name: 'Assistant',
      status: 'ok',
      t_start: '2024-06-01T10:00:00.000Z',
      t_end: '2024-06-01T10:00:03.000Z',
      duration_ms: 3000,
      rev: 2,
    },
  },
  {
    kind: 'node_upsert',
    rev: 3,
    node: {
      id: 'tl-user-1',
      run_id: 'run-tl1',
      type: 'user_prompt',
      name: 'User Prompt',
      status: 'ok',
      t_start: '2024-06-01T10:00:00.500Z',
      rev: 3,
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
      contentType: 'application/json',
      body: JSON.stringify(sseEvents),
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

test('timeline toggle appears and clicking it renders timeline bars', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();

  const timelineBtn = page.getByRole('button', { name: 'Timeline' });
  await expect(timelineBtn).toBeVisible();

  await timelineBtn.click();
  await expect(page.locator('.timeline-root')).toBeVisible();
  await expect(page.locator('.bar, .bar-marker').first()).toBeVisible();
});

test('clicking a timeline row opens node drawer', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();

  const timelineBtn = page.getByRole('button', { name: 'Timeline' });
  await expect(timelineBtn).toBeVisible();
  await timelineBtn.click();
  await expect(page.locator('.timeline-root')).toBeVisible();

  const bashRow = page.locator('.timeline-row', { hasText: 'BashTool' });
  await expect(bashRow).toBeVisible();
  await bashRow.click();

  await expect(page.locator('.node-drawer')).toBeVisible();
  await expect(page.locator('.node-drawer')).toContainText('BashTool');
});

test('untimed node renders with data-unknown marker', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();

  const timelineBtn = page.getByRole('button', { name: 'Timeline' });
  await expect(timelineBtn).toBeVisible();
  await timelineBtn.click();
  await expect(page.locator('.timeline-root')).toBeVisible();

  await expect(page.locator('[data-unknown="true"]').first()).toBeVisible();
});

test('anchors timeline to first activity, no leading dead space from session node', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();

  const timelineBtn = page.getByRole('button', { name: 'Timeline' });
  await timelineBtn.click();
  await expect(page.locator('.timeline-root')).toBeVisible();

  const assistantRow = page.locator('.timeline-row', { hasText: 'Assistant' });
  const bar = assistantRow.locator('.bar');
  await expect(bar).toBeVisible();
  const style = (await bar.getAttribute('style')) ?? '';
  const left = Number(/left:\s*([\d.]+)%/.exec(style)?.[1] ?? 'NaN');
  expect(left).toBeLessThan(5);
});

test('middle-truncates a long MCP label, keeping the distinguishing suffix', async ({ page }) => {
  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.session-view')).toBeVisible();

  const timelineBtn = page.getByRole('button', { name: 'Timeline' });
  await timelineBtn.click();
  await expect(page.locator('.timeline-root')).toBeVisible();

  const mcpRow = page.locator('.timeline-row', { hasText: 'navigate' });
  await expect(mcpRow).toBeVisible();
  const label = (await mcpRow.locator('.row-label').textContent())?.trim() ?? '';
  expect(label).toContain('…');
  expect(label.endsWith('navigate')).toBe(true);
  expect(label).not.toBe('mcp__playwright__browser_navigate');
});
