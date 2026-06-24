# Task G Verification Report — Sessions List + Hash Routing

## Status: PASS

All 8 verification steps passed with zero issues.

---

## Modules and Components Built

### Pure logic (fully unit-tested)

- `/Users/karych/src/catacomb/webui/web/src/lib/router.ts`
  - `Route` discriminated union (`kind: 'list' | 'session' | 'session-node'`)
  - `parseHash(hash: string): Route` — parses `window.location.hash` into a `Route`
  - `toHash(route: Route): string` — serialises a `Route` back to a hash string
  - Test file: `router.test.ts` — 100% coverage

- `/Users/karych/src/catacomb/webui/web/src/lib/sessions-sort.ts`
  - `filterSessions(sessions, query)` — case-insensitive partial match on hash + model_id
  - `sortSessions(sessions, key, dir)` — sorts by any `SortKey` field (`started_at`, `cost_usd`, `duration_ms`, `tokens_in`, `tokens_out`, `node_count`, `error_count`)
  - Test file: `sessions-sort.test.ts` — 100% coverage

### Svelte components

- `/Users/karych/src/catacomb/webui/web/src/components/StatusPill.svelte`
  - Single `status` prop; maps to CSS custom property `--ok / --running / --error / --blocked / --pending`

- `/Users/karych/src/catacomb/webui/web/src/components/SessionRow.svelte`
  - Columns: short hash (mono), `StatusPill`, duration, tokens in/out, cost + `cost_source` provenance badge, tool_count, error_count, model_id
  - Keyboard-accessible (tabindex + Enter/Space navigation)

- `/Users/karych/src/catacomb/webui/web/src/components/SessionsList.svelte`
  - Searchable + sortable table; loading / empty / error states
  - Search input accepts partial hash or model name; pasting full hash + Enter routes directly
  - Clicking a row navigates to `#/s/{hash}`

- `/Users/karych/src/catacomb/webui/web/src/components/SessionView.svelte`
  - Placeholder session view; see "What h/i need to know" section below

- `/Users/karych/src/catacomb/webui/web/src/App.svelte`
  - `hashchange` listener calls `parseHash`; routes between `SessionsList`, `SessionView`, and `SessionView` with nodeId
  - SSE `session` parameter updated from store when in session/session-node route

---

## Vitest Coverage Results

```
Test Files  8 passed (8)
Tests       167 passed (167)

Statements : 100% (202/202)
Branches   : 100% (163/163)
Functions  : 100% (32/32)
Lines      : 100% (154/154)
```

All gated files pass at 100%: router.ts, sessions-sort.ts, reducer/**, selectors.ts, format/**, pricing/**, sse/client.ts, api.ts.

---

## E2E Tests Added

File: `/Users/karych/src/catacomb/webui/e2e/sessions.spec.ts` — 7 tests

1. `app loads and shows sessions list` — `.sessions-list` visible, 3 rows rendered
2. `sessions list shows formatted cost and tokens` — `$0.02`, `1,500`, `3,200` visible
3. `search narrows the session list` — model name filter leaves 1 row
4. `search by partial hash narrows list` — partial hash filter narrows correctly
5. `clicking a row navigates to session view` — `#/s/{hash}` route triggered
6. `session view has back button that returns to list` — `.back-btn` click returns to `.sessions-list`
7. `deep-link to #/s/{hash} renders session view directly` — direct navigation to session hash renders view

All 7 tests use hermetic Playwright mocking of `/v1/sessions` — no live server required.

Existing file: `/Users/karych/src/catacomb/webui/e2e/shell.spec.ts` — 2 tests (unchanged, still passing)

Total e2e: **9 tests, 9 passed**.

---

## Build and Dist Result

`npm run build` succeeded in 141ms. `git diff --exit-code webui/dist` exited 0 — no dist drift; the committed artifact matches the rebuild.

---

## Typecheck and Go Results

- `svelte-check`: 341 files, 0 errors, 0 warnings
- `make cover`: 100% — file/package/total thresholds all satisfied
- `golangci-lint run ./...`: 0 issues
- `GOOS=windows go build ./...`: no errors

---

## Minor Findings from Implementation Reviews

- `toHash` was briefly imported but unused in `App.svelte` after a refactor; fixed in commit `ac59962`
- `noUncheckedIndexedAccess` TS strict flag required non-null assertions in `sessions-sort.test.ts`; added in commit `7190db9`
- No pre-existing lint issues; the 0-issue lint result is clean

---

## What Tasks h/i Need to Know About SessionView

The `SessionView.svelte` shell is at:
`/Users/karych/src/catacomb/webui/web/src/components/SessionView.svelte`

**Props accepted:**
```ts
interface Props {
  hash: string;
  nodeId?: string;
}
```

**CSS anchors:**
- Root element: `.session-view`
- Back button: `.back-btn` (already wired to `toHash({ kind: 'list' })`)
- Graph placeholder area: `.graph-placeholder` (currently shows "Graph coming soon" copy)

**To wire in the graph canvas (task h):**
Replace the `.graph-placeholder` div contents with `<GraphCanvas {hash} {nodeId} />`.
The outer `.graph-placeholder` div is `flex: 1` and fills the remaining height, so GraphCanvas will
receive the full remaining viewport height automatically.

**Deep-link support (task h/i):**
When `nodeId` is set on mount (i.e., the user navigated directly to `#/s/{hash}/n/{nodeId}`), the
graph canvas should call `selectNode(nodeId)` from `stores.svelte.ts` to pre-select that node.
This call is NOT currently wired in `SessionView` — task h/i must add it.
The `selectNode` function signature (from `stores.svelte.ts`) is expected to accept a `string` node ID.

**Route shape (from router.ts):**
```ts
{ kind: 'session', hash: string }
{ kind: 'session-node', hash: string, nodeId: string }
```
Both routes render `SessionView`; App.svelte passes `nodeId` only for the `session-node` kind.
