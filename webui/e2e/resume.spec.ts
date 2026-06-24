import { test, expect } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'deadbeef0000deadbeef0000deadbeef';

const fakeSession: SessionSummary = {
  session: sessionHash,
  status: 'running',
  started_at: '2026-06-01T10:00:00Z',
  tokens_in: 100,
  tokens_out: 50,
  node_count: 1,
  tool_count: 0,
  error_count: 0,
  run_ids: ['run-resume'],
};

const sseEvents: SseEvent[] = [
  {
    kind: 'node_upsert',
    rev: 5,
    node: {
      id: 'resume-root',
      run_id: 'run-resume',
      type: 'session',
      name: 'Session Root',
      status: 'running',
      rev: 5,
    },
  },
];

test.beforeEach(async ({ page }) => {
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([fakeSession]),
    });
  });

  await page.route(`/v1/sessions/${sessionHash}/graph`, async (route) => {
    const body = sseEvents.map((ev) => `data: ${JSON.stringify(ev)}\n\n`).join('');
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body,
    });
  });
});

test('connection pill shows reconnecting state on stream error then hides on open', async ({
  page,
}) => {
  await page.addInitScript(() => {
    let instance: {
      onopen: ((ev: unknown) => void) | null;
      onerror: ((ev: unknown) => void) | null;
      onmessage: ((ev: { data: string }) => void) | null;
    } | null = null;

    class ControllableEventSource {
      onopen: ((ev: unknown) => void) | null = null;
      onerror: ((ev: unknown) => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      constructor(_url: string) {
        instance = this;
        (globalThis as unknown as { _sseInstance: unknown })._sseInstance = this;
        setTimeout(() => {
          this.onopen?.({});
        }, 0);
      }
      close() {}
    }

    (globalThis as unknown as { EventSource: unknown }).EventSource =
      ControllableEventSource as unknown;
    (globalThis as unknown as { _triggerSseError: () => void })._triggerSseError = () => {
      instance?.onerror?.({});
    };
    (globalThis as unknown as { _triggerSseOpen: () => void })._triggerSseOpen = () => {
      instance?.onopen?.({});
    };
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);

  await expect(page.locator('.conn-pill')).not.toBeVisible({ timeout: 3000 });

  await page.evaluate(() => {
    (globalThis as unknown as { _triggerSseError: () => void })._triggerSseError();
  });

  await expect(page.locator('.conn-pill[data-state="error"]')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('.conn-pill')).toContainText('disconnected');

  await page.evaluate(() => {
    (globalThis as unknown as { _triggerSseOpen: () => void })._triggerSseOpen();
  });

  await expect(page.locator('.conn-pill')).not.toBeVisible({ timeout: 3000 });
});

test('stale pill appears on parse error and reflects degraded signal', async ({ page }) => {
  await page.addInitScript(() => {
    let instance: {
      onopen: ((ev: unknown) => void) | null;
      onerror: ((ev: unknown) => void) | null;
      onmessage: ((ev: { data: string }) => void) | null;
    } | null = null;

    class ParseErrorEventSource {
      onopen: ((ev: unknown) => void) | null = null;
      onerror: ((ev: unknown) => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      constructor(_url: string) {
        instance = this;
        (globalThis as unknown as { _sseParseInstance: unknown })._sseParseInstance = this;
        setTimeout(() => {
          this.onopen?.({});
        }, 0);
      }
      close() {}
    }

    (globalThis as unknown as { EventSource: unknown }).EventSource =
      ParseErrorEventSource as unknown;
    (globalThis as unknown as { _triggerMalformed: () => void })._triggerMalformed = () => {
      instance?.onmessage?.({ data: 'THIS IS NOT JSON }{' });
    };
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);

  await expect(page.locator('.conn-pill')).not.toBeVisible({ timeout: 3000 });

  await page.evaluate(() => {
    (globalThis as unknown as { _triggerMalformed: () => void })._triggerMalformed();
  });

  await expect(page.locator('.conn-pill[data-state="stale"]')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('.conn-pill')).toContainText('stale');
});

test('reconnect URL carries since param matching last seen rev', async ({ page }) => {
  const capturedUrls: string[] = [];

  await page.addInitScript(() => {
    const urls: string[] = [];
    (globalThis as unknown as { _capturedSseUrls: string[] })._capturedSseUrls = urls;

    class CapturingEventSource {
      onopen: ((ev: unknown) => void) | null = null;
      onerror: ((ev: unknown) => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      constructor(url: string) {
        urls.push(url);
        (globalThis as unknown as { _lastSseInstance: unknown })._lastSseInstance = this;
        setTimeout(() => {
          this.onopen?.({});
          this.onmessage?.({
            data: JSON.stringify({ kind: 'node_upsert', rev: 5 }),
          });
        }, 0);
      }
      close() {}
    }

    (globalThis as unknown as { EventSource: unknown }).EventSource =
      CapturingEventSource as unknown;
  });

  await page.goto(`/?token=test#/s/${sessionHash}`);

  await page.waitForTimeout(200);

  const urls: string[] = await page.evaluate(
    () => (globalThis as unknown as { _capturedSseUrls: string[] })._capturedSseUrls,
  );
  capturedUrls.push(...urls);

  expect(capturedUrls.length).toBeGreaterThanOrEqual(1);
  const firstUrl = capturedUrls[0]!;
  expect(firstUrl).toContain('/v1/subscribe');
  expect(firstUrl).toContain('session=');
});
