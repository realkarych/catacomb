import { test, expect } from '@playwright/test';

test('dark color scheme uses dark --bg token', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.goto('/?token=test');
  const bg = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--bg').trim(),
  );
  expect(bg).toBeTruthy();
  const lightBg = await page.evaluate(() => {
    const tmp = document.createElement('div');
    tmp.style.setProperty('--test', 'oklch(0.96 0.008 82)');
    document.body.appendChild(tmp);
    const v = getComputedStyle(tmp).getPropertyValue('--test').trim();
    document.body.removeChild(tmp);
    return v;
  });
  expect(bg).not.toBe(lightBg);
});

test('light color scheme uses light --bg token', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.route('/v1/sessions', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.goto('/?token=test');
  const darkBg = await page.evaluate(() => {
    const tmp = document.createElement('div');
    tmp.style.setProperty('--test', 'oklch(0.16 0.008 70)');
    document.body.appendChild(tmp);
    const v = getComputedStyle(tmp).getPropertyValue('--test').trim();
    document.body.removeChild(tmp);
    return v;
  });
  const lightBg = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--bg').trim(),
  );
  expect(lightBg).toBeTruthy();
  expect(lightBg).not.toBe(darkBg);
});
