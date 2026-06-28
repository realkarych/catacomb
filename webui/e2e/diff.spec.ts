import { test, expect } from '@playwright/test';
import type { SessionSummary, DiffResult } from '../web/src/lib/types';

const sessionA: SessionSummary = {
  session: 'aaaa000000000000aaaa000000000000',
  status: 'ok',
  tokens_in: 100,
  tokens_out: 200,
  node_count: 5,
  tool_count: 3,
  error_count: 0,
  run_ids: ['run-a'],
};

const sessionB: SessionSummary = {
  session: 'bbbb000000000000bbbb000000000000',
  status: 'ok',
  tokens_in: 150,
  tokens_out: 250,
  node_count: 6,
  tool_count: 4,
  error_count: 0,
  run_ids: ['run-b'],
};

const fakeDiffResult: DiffResult = {
  added: [{ type: 'tool_call', tool: 'bash', step_key: 'k1', content_key: 'c1' }],
  removed: [],
  changed: [],
  unchanged: [{ type: 'tool_call', tool: 'ls', a_step_key: 'k2', b_step_key: 'k2', a_content_key: 'c2', b_content_key: 'c2', tier: 'step_key' }],
};

const identicalDiffResult: DiffResult = {
  added: [],
  removed: [],
  changed: [],
  unchanged: [{ type: 'tool_call', tool: 'ls', a_step_key: 'k2', b_step_key: 'k2', a_content_key: 'c2', b_content_key: 'c2', tier: 'step_key' }],
};

test('compare link navigates to diff view', async ({ page }) => {
  await page.route('/v1/sessions', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([sessionA, sessionB]) });
  });
  await page.goto('/');
  await expect(page.locator('.compare-link')).toBeVisible();
  await page.locator('.compare-link').click();
  await expect(page).toHaveURL(/#\/diff/);
  await expect(page.locator('.diff-view')).toBeVisible();
});

test('deep link to diff renders session selects', async ({ page }) => {
  await page.route('/v1/sessions', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([sessionA, sessionB]) });
  });
  await page.route('/v1/diff*', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeDiffResult) });
  });
  await page.goto(`/#/diff/${sessionA.session}/${sessionB.session}`);
  await expect(page.locator('.diff-view')).toBeVisible();
  await expect(page.locator('#diff-select-a')).toBeVisible();
  await expect(page.locator('#diff-select-b')).toBeVisible();
});

test('identical sessions shows calm identical message', async ({ page }) => {
  await page.route('/v1/sessions', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([sessionA, sessionB]) });
  });
  await page.route('/v1/diff*', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(identicalDiffResult) });
  });
  await page.goto(`/#/diff/${sessionA.session}/${sessionB.session}`);
  await expect(page.locator('.diff-identical')).toBeVisible();
  await expect(page.locator('.diff-identical')).toContainText('identical');
});

test('non-identical diff shows added count', async ({ page }) => {
  await page.route('/v1/sessions', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([sessionA, sessionB]) });
  });
  await page.route('/v1/diff*', async route => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(fakeDiffResult) });
  });
  await page.goto(`/#/diff/${sessionA.session}/${sessionB.session}`);
  await expect(page.locator('.diff-view')).toBeVisible();
  await expect(page.locator('.diff-view')).toContainText('1');
});
