import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import type { SessionSummary, SseEvent } from '../web/src/lib/types';

const sessionHash = 'repro00001aaarepro00001aaarepro0';

const sseEvents: SseEvent[] = [
  { kind: 'node_upsert', rev: 1, node: { id: 'n-session', run_id: 'run-rp', type: 'session', name: 'repro test', status: 'ok', rev: 1 } },
];

const sessionsWithRepro: SessionSummary[] = [{
  session: sessionHash,
  status: 'ok',
  started_at: '2026-06-20T10:00:00Z',
  duration_ms: 1000,
  tokens_in: 10,
  tokens_out: 5,
  cost_usd: 0.0001,
  cost_source: 'reported',
  node_count: 1,
  tool_count: 0,
  error_count: 0,
  run_ids: ['run-rp'],
  repro: {
    claude_code_version: '1.2.3',
    catacomb_version: '0.9.0',
    cwd: '/home/user/project',
    prompts_hash: 'aabbccdd001122334455667788990011',
    skills_hash: 'bbccddee112233445566778899001122',
    subagents_hash: 'ccddeeff223344556677889900112233',
    catacomb_config_hash: 'ddeeff00334455667788990011223344',
  }
}];

const sessionsWithoutRepro: SessionSummary[] = [{
  session: sessionHash,
  status: 'ok',
  started_at: '2026-06-20T10:00:00Z',
  duration_ms: 1000,
  tokens_in: 10,
  tokens_out: 5,
  cost_usd: 0.0001,
  cost_source: 'reported',
  node_count: 1,
  tool_count: 0,
  error_count: 0,
  run_ids: ['run-rp'],
}];

async function routeRepro(page: Page, sessions: SessionSummary[]): Promise<void> {
  await page.route('/v1/sessions', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sessions) })
  );
  await page.route('/v1/subscribe**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: sseEvents.map(e => `data: ${JSON.stringify(e)}\n\n`).join('') })
  );
  await page.route(`/v1/sessions/${sessionHash}/nodes/**`, (route: Route) =>
    route.fulfill({ status: 403 })
  );
}

test('repro section shows version and fingerprint when session.repro present', async ({ page }) => {
  await routeRepro(page, sessionsWithRepro);
  await page.goto(`/?token=test#/s/${sessionHash}`);

  // The repro section should be visible
  await expect(page.locator('.repro-section')).toBeVisible();
  // Version shown
  await expect(page.locator('.repro-version')).toHaveText('1.2.3');
  // Fingerprint shown (first 6 of each of 4 hashes: aabbcc-bbccdd-ccddef-ddeeff wait — first 6 chars of each)
  // prompts: 'aabbcc', skills: 'bbccdd', subagents: 'ccddee', config: 'ddeeff'
  // Actually: aabbccdd -> first 6 = 'aabbcc', bbccddee -> 'bbccdd', ccddeeff -> 'ccddee', ddeeff00 -> 'ddeeff'
  await expect(page.locator('.repro-fp')).toContainText('aabbcc');
});

test('repro section is expandable and shows cwd and hashes', async ({ page }) => {
  await routeRepro(page, sessionsWithRepro);
  await page.goto(`/?token=test#/s/${sessionHash}`);

  await expect(page.locator('.repro-section')).toBeVisible();
  // Expand
  await page.locator('.repro-summary').click();
  // Should show cwd
  await expect(page.locator('.repro-body')).toContainText('/home/user/project');
  // Should show hash labels
  await expect(page.locator('.repro-body')).toContainText('prompts');
});

test('repro section absent when session.repro not present', async ({ page }) => {
  await routeRepro(page, sessionsWithoutRepro);
  await page.goto(`/?token=test#/s/${sessionHash}`);

  // Should not render the repro section at all
  await expect(page.locator('.repro-section')).toHaveCount(0);
});
