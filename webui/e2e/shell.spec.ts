import { test, expect } from '@playwright/test';

test('app shell loads and mounts the #app element', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveTitle('Catacomb');
  await expect(page.locator('#app')).toBeAttached();
});

test('wordmark is visible', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.wordmark')).toBeVisible();
  await expect(page.locator('.wordmark')).toHaveText('Catacomb');
});

test('connection pill is absent when idle (no active session)', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.conn-pill')).toHaveCount(0);
});

test('connection pill shows degraded state when error', async ({ page }) => {
  await page.addInitScript(() => {
    class ErrorEventSource {
      onopen: ((ev: unknown) => void) | null = null;
      onerror: ((ev: unknown) => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      constructor(_url: string) {
        setTimeout(() => {
          this.onerror?.({});
        }, 0);
      }
      close() {}
    }
    (globalThis as unknown as { EventSource: unknown }).EventSource = ErrorEventSource as unknown;
  });

  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });

  await page.goto('/?token=test#/s/aaaa0000aaaa0000aaaa0000aaaa0000');
  await expect(page.locator('.conn-pill[data-state="error"]')).toBeVisible({ timeout: 4000 });
  await expect(page.locator('.conn-pill')).toContainText('disconnected');
});
