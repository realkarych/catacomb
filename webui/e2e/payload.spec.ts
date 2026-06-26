import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent, PayloadView } from '../web/src/lib/types';

const sessionHash = 'payload001payload001payload001pa';

const fakeSessions: SessionSummary[] = [
  {
    session: sessionHash,
    status: 'ok',
    started_at: '2024-06-01T10:00:00Z',
    duration_ms: 2100,
    tokens_in: 100,
    tokens_out: 50,
    cost_usd: 0.001,
    cost_source: 'reported',
    node_count: 2,
    tool_count: 1,
    error_count: 0,
    model_id: 'claude-opus-4-5',
    run_ids: ['run-payload'],
  },
];

const sseEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 1,
    node: {
      id: 'node-session-pl',
      run_id: 'run-payload',
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
      id: 'node-tool-pl',
      run_id: 'run-payload',
      type: 'tool_call',
      name: 'BashTool',
      status: 'ok',
      tokens_in: 50,
      tokens_out: 40,
      payload_hash: 'abc123payloadhash',
      rev: 2,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 3,
    edge: {
      id: 'edge-pl-1',
      run_id: 'run-payload',
      type: 'parent_child',
      src: 'node-session-pl',
      dst: 'node-tool-pl',
      rev: 3,
    },
  },
  {
    kind: 'node_upsert',
    rev: 4,
    node: {
      id: 'node-turn-pl',
      run_id: 'run-payload',
      type: 'assistant_turn',
      name: 'assistant turn',
      status: 'ok',
      payload_hash: 'turnhash',
      rev: 4,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 5,
    edge: {
      id: 'edge-pl-2',
      run_id: 'run-payload',
      type: 'parent_child',
      src: 'node-session-pl',
      dst: 'node-turn-pl',
      rev: 5,
    },
  },
  {
    kind: 'node_upsert',
    rev: 6,
    node: {
      id: 'node-prompt-pl',
      run_id: 'run-payload',
      type: 'user_prompt',
      name: 'user prompt',
      status: 'ok',
      payload_hash: 'prompthash',
      rev: 6,
    },
  },
  {
    kind: 'edge_upsert',
    rev: 7,
    edge: {
      id: 'edge-pl-3',
      run_id: 'run-payload',
      type: 'parent_child',
      src: 'node-session-pl',
      dst: 'node-prompt-pl',
      rev: 7,
    },
  },
];

const redactedPayload: PayloadView = {
  node_id: 'node-tool-pl',
  payload_hash: 'abc123payloadhash',
  input: { command: 'echo hello' },
  output: { result: 'hello' },
  redactions: [{ path: 'input.env.SECRET_KEY', reason: 'high-entropy secret' }],
  redacted: true,
};

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

  await page.route('/v1/subscribe**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: buildSseBody(sseEvents),
    });
  });
});

test('content panel: collapsed by default, no fetch until reveal', async ({ page }) => {
  let payloadFetched = false;

  await page.route(`/v1/sessions/${sessionHash}/nodes/node-tool-pl/payload`, async (route) => {
    payloadFetched = true;
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(redactedPayload) });
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const toolRow = page.locator('.outline-row').filter({ hasText: 'BashTool' });
  await toolRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();

  await expect(drawer.locator('.reveal-btn')).toBeVisible();
  await expect(drawer.locator('.payload-section')).toHaveCount(0);
  expect(payloadFetched).toBe(false);
});

test('content panel: 200 redacted payload shows input/output + redacted badge + copy', async ({ page }) => {
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(redactedPayload),
    });
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const toolRow = page.locator('.outline-row').filter({ hasText: 'BashTool' });
  await toolRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();

  await drawer.locator('.reveal-btn').click();

  await expect(drawer.locator('.redacted-badge')).toBeVisible();
  await expect(drawer.locator('.redacted-badge')).toContainText('redacted');
  await expect(drawer.locator('.redacted-count')).toContainText('1 secret redacted');

  await expect(drawer.locator('.payload-section')).toHaveCount(2);
  await expect(drawer.locator('.payload-section-label').first()).toContainText('Input');
  await expect(drawer.locator('.payload-section-label').last()).toContainText('Output');

  await expect(drawer.locator('.copy-btn[aria-label="Copy input content"]')).toBeVisible();
  await expect(drawer.locator('.copy-btn[aria-label="Copy output content"]')).toBeVisible();

  await expect(drawer.locator('.payload-content.mono').first()).toBeVisible();
  await expect(drawer.locator('.payload-text')).toHaveCount(0);
});

test('content panel: 403 shows disabled message with --allow-payload-access', async ({ page }) => {
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route) => {
    await route.fulfill({ status: 403, body: '' });
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const toolRow = page.locator('.outline-row').filter({ hasText: 'BashTool' });
  await toolRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();

  await drawer.locator('.reveal-btn').click();

  await expect(drawer.locator('.payload-msg')).toBeVisible();
  await expect(drawer.locator('.payload-msg')).toContainText('--allow-payload-access');
  await expect(drawer.locator('.payload-section')).toHaveCount(0);
});

test('content panel: user prompt renders input as text, not JSON', async ({ page }) => {
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route) => {
    const promptPayload: PayloadView = {
      node_id: 'node-prompt-pl',
      payload_hash: 'prompthash',
      input: 'What is the meaning of life?',
      redactions: [],
      redacted: false,
    };
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(promptPayload) });
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const promptRow = page.locator('.outline-row').filter({ hasText: 'prompt' }).first();
  await promptRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();
  await drawer.locator('.reveal-btn').click();

  await expect(drawer.locator('.payload-section-label')).toContainText('Prompt');
  await expect(drawer.locator('.payload-text')).toContainText('What is the meaning of life?');
  await expect(drawer.locator('.payload-content.mono')).toHaveCount(0);
});

test('content panel: assistant turn renders response as text, not JSON', async ({ page }) => {
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route) => {
    const turnPayload: PayloadView = {
      node_id: 'node-turn-pl',
      payload_hash: 'turnhash',
      output: 'Here is the **answer** to your question.',
      redactions: [],
      redacted: false,
    };
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(turnPayload) });
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);
  await expect(page.locator('.outline-root')).toBeVisible();

  const turnRow = page.locator('.outline-row').filter({ hasText: 'assistant' }).first();
  await turnRow.click();

  const drawer = page.locator('.node-drawer');
  await expect(drawer).toBeVisible();
  await drawer.locator('.reveal-btn').click();

  await expect(drawer.locator('.payload-section-label')).toContainText('Response');
  await expect(drawer.locator('.payload-text')).toContainText('Here is the **answer** to your question.');
  await expect(drawer.locator('.payload-content.mono')).toHaveCount(0);
});
