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
