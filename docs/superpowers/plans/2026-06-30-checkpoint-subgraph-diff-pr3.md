# Checkpoint Subgraph Diff — PR3 (Web UI phase pickers) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Let users scope the web diff view per side to a checkpoint phase, populated from each session's marker nodes — consuming the `/v1/diff` `aPhase`/`bPhase` params shipped in PR2.

**Architecture:** Pure frontend. `web/src/lib/api.ts` gains optional phase params on `fetchDiff` and a `fetchSessionPhases` helper (both unit-tested — `api.ts` is under the 100% coverage gate). `web/src/components/DiffView.svelte` gains a per-side phase `<select>` that loads a session's phases on selection and re-diffs on change. The committed `webui/dist` build output is regenerated.

**Tech Stack:** Svelte 5 (runes), TypeScript, Vite, Vitest (jsdom), Playwright. Frontend root is `webui/`; source under `webui/web/src/`. All commands run from `webui/`.

## Global Constraints

- **100% per-file coverage** on the vitest `coverage.include` set (which includes `web/src/lib/api.ts`). Enforced by `npm run test` (`vitest run --coverage`, `thresholds: { 100: true, perFile: true }`). Every new branch in `api.ts` must be tested. `.svelte` components are NOT in the coverage set.
- **Typecheck must pass:** `npm run typecheck` (svelte-check). Use Svelte 5 runes (`$state`/`$effect`/`$props`/`$derived`), matching existing components.
- **`webui/dist` is committed and must be current:** after any source change run `npm run build` and commit `dist/`; CI fails if `git status --porcelain dist` is non-empty.
- **Work in the worktree** `/Users/karych/src/catacomb/.claude/worktrees/checkpoint-phase3-webui` (branch `worktree-checkpoint-phase3-webui`). Never the shared checkout.
- Backward compatibility: `fetchDiff` with no phase opts produces the byte-identical URL it does today; existing callers/tests keep working (update call sites that pass `f` positionally — see Task 1).
- No new runtime dependencies.

## File Structure

| File | Responsibility |
| --- | --- |
| `web/src/lib/api.ts` (modify) | `fetchDiff` phase opts; new `fetchSessionPhases`. |
| `web/src/lib/api.test.ts` (modify) | Tests for the above (keep `api.ts` at 100%). |
| `web/src/components/DiffView.svelte` (modify) | Per-side phase `<select>`; load phases on session select; re-diff on phase change. |
| `webui/dist/**` (regenerate) | Rebuilt bundle, committed. |

---

### Task 1: `api.ts` — `fetchDiff` phase opts + `fetchSessionPhases`

**Files:**

- Modify: `web/src/lib/api.ts`, `web/src/lib/api.test.ts`

**Interfaces:**

- Produces:
  - `fetchDiff(a, b, token, opts?: { aPhase?: string; bPhase?: string }, f?)` — appends `&aPhase=`/`&bPhase=` only when set; throws `NotFoundError` on 404, a plain error mentioning "phase" on 400.
  - `fetchSessionPhases(hash, token, f?): Promise<string[]>` — fetches the session graph and returns the de-duplicated `name`s of `type === 'marker'` nodes, in first-seen order.

- [ ] **Step 1: Write the failing tests**

In `web/src/lib/api.test.ts`, first UPDATE the two existing `fetchDiff` calls that pass `f` positionally to insert an empty opts object (signature gains `opts` before `f`):

```ts
    const result = await fetchDiff('hash-a', 'hash-b', 'mytoken', {}, f);
```

```ts
    await fetchDiff('a/1', 'b/2', 'tok/x', {}, f);
```

Then append new tests:

```ts
describe('fetchDiff phase params', () => {
  const sampleResult: DiffResult = { added: [], removed: [], changed: [], unchanged: [] };

  it('appends aPhase and bPhase when set', async () => {
    const f = mockFetch(200, sampleResult);
    await fetchDiff('a', 'b', 'tok', { aPhase: 'plan', bPhase: 'impl,1' }, f);
    expect(f).toHaveBeenCalledWith('/v1/diff?a=a&b=b&token=tok&aPhase=plan&bPhase=impl%2C1');
  });

  it('omits phase params when not set', async () => {
    const f = mockFetch(200, sampleResult);
    await fetchDiff('a', 'b', 'tok', {}, f);
    expect(f).toHaveBeenCalledWith('/v1/diff?a=a&b=b&token=tok');
  });

  it('throws on 400 (invalid/absent phase)', async () => {
    const f = mockFetch(400, null);
    await expect(fetchDiff('a', 'b', 'tok', { aPhase: 'ghost' }, f)).rejects.toThrow(/phase/);
  });
});

describe('fetchSessionPhases', () => {
  function nodeEvent(type: string, name?: string): SseEvent {
    return { kind: 'node_upsert', rev: 1, node: { id: type + (name ?? ''), run_id: 'r', type, name, rev: 1 } };
  }

  it('returns de-duplicated marker names in order', async () => {
    const events: SseEvent[] = [
      nodeEvent('tool_call', 'Bash'),
      nodeEvent('marker', 'plan'),
      nodeEvent('marker', 'impl'),
      nodeEvent('marker', 'plan'),
      nodeEvent('marker'),
      { kind: 'edge_upsert', rev: 1 },
    ];
    const f = mockFetch(200, events);
    const result = await fetchSessionPhases('h', 'tok', f);
    expect(result).toEqual(['plan', 'impl']);
  });

  it('propagates NotFoundError on 404', async () => {
    const f = mockFetch(404, null);
    await expect(fetchSessionPhases('h', 'tok', f)).rejects.toBeInstanceOf(NotFoundError);
  });
});
```

Add `fetchSessionPhases` to the import list at the top of `api.test.ts`.

- [ ] **Step 2: Run to verify it fails**

Run (from `webui/`): `npm run test -- --run web/src/lib/api.test.ts`
Expected: FAIL — `fetchSessionPhases` undefined; `fetchDiff` signature mismatch / missing phase params.

- [ ] **Step 3: Implement in `api.ts`**

Replace `fetchDiff` and add `fetchSessionPhases`:

```ts
export async function fetchDiff(
  a: string,
  b: string,
  token: string,
  opts: { aPhase?: string; bPhase?: string } = {},
  f = fetch,
): Promise<DiffResult> {
  let url = `/v1/diff?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}&token=${encodeURIComponent(token)}`;
  if (opts.aPhase) url += `&aPhase=${encodeURIComponent(opts.aPhase)}`;
  if (opts.bPhase) url += `&bPhase=${encodeURIComponent(opts.bPhase)}`;
  const res = await f(url);
  if (res.status === 404) throw new NotFoundError('session not found');
  if (res.status === 400) throw new Error('invalid or missing phase selector');
  if (!res.ok) throw new Error(`fetchDiff failed: ${res.status}`);
  return res.json() as Promise<DiffResult>;
}

export async function fetchSessionPhases(hash: string, token: string, f = fetch): Promise<string[]> {
  const events = await fetchSessionGraph(hash, token, f);
  const names: string[] = [];
  const seen = new Set<string>();
  for (const ev of events) {
    const n = ev.node;
    if (!n || n.type !== 'marker' || !n.name) continue;
    if (seen.has(n.name)) continue;
    seen.add(n.name);
    names.push(n.name);
  }
  return names;
}
```

- [ ] **Step 4: Run tests + coverage**

Run: `npm run test -- --run web/src/lib/api.test.ts` then `npm run test` (full coverage gate).
Expected: PASS; `api.ts` at 100% (all new branches — aPhase set/unset, bPhase set/unset, 400, and the `fetchSessionPhases` skip/dupe/push branches — are exercised).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api.ts web/src/lib/api.test.ts
git commit -m "feat(webui): fetchDiff phase params + fetchSessionPhases

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `DiffView.svelte` — per-side phase pickers

**Files:**

- Modify: `web/src/components/DiffView.svelte`

**Interfaces:**

- Consumes: `fetchSessionPhases`, `fetchDiff` (Task 1).
- Produces: when a session is selected, a phase `<select>` for that side (only if the session has ≥1 phase); selecting a phase re-runs the diff scoped to it.

- [ ] **Step 1: Add phase state + loaders to the `<script>`**

After the existing `let diffError` state, add:

```ts
  let phasesA: string[] = $state([]);
  let phasesB: string[] = $state([]);
  let selPhaseA: string = $state('');
  let selPhaseB: string = $state('');

  $effect(() => {
    const h = selA;
    selPhaseA = '';
    phasesA = [];
    if (!h) return;
    let cancelled = false;
    fetchSessionPhases(h, token)
      .then((p) => { if (!cancelled) phasesA = p; })
      .catch(() => { if (!cancelled) phasesA = []; });
    return () => { cancelled = true; };
  });

  $effect(() => {
    const h = selB;
    selPhaseB = '';
    phasesB = [];
    if (!h) return;
    let cancelled = false;
    fetchSessionPhases(h, token)
      .then((p) => { if (!cancelled) phasesB = p; })
      .catch(() => { if (!cancelled) phasesB = []; });
    return () => { cancelled = true; };
  });
```

Update the import line to include `fetchSessionPhases`:

```ts
  import { fetchSessions, fetchDiff, fetchSessionPhases } from '../lib/api';
```

- [ ] **Step 2: Pass the selected phases into the diff effect**

In the existing diff `$effect`, read the phase selections and pass them to `fetchDiff`. Replace the `fetchDiff(captA, captB, token)` call with:

```ts
    const captPhaseA = selPhaseA;
    const captPhaseB = selPhaseB;
    fetchDiff(captA, captB, token, {
      aPhase: captPhaseA || undefined,
      bPhase: captPhaseB || undefined,
    })
```

(Reading `selPhaseA`/`selPhaseB` inside the effect makes the diff re-run when a phase changes.)

- [ ] **Step 3: Add the phase selects to the toolbar markup**

Immediately after the Session A `<select>` block (the closing `</select>` for `diff-select-a`), add:

```svelte
    {#if phasesA.length > 0}
      <select class="diff-phase" aria-label="Phase A" bind:value={selPhaseA}>
        <option value="">— all phases —</option>
        {#each phasesA as p (p)}
          <option value={p}>{p}</option>
        {/each}
      </select>
    {/if}
```

And symmetrically after the Session B `<select>` block:

```svelte
    {#if phasesB.length > 0}
      <select class="diff-phase" aria-label="Phase B" bind:value={selPhaseB}>
        <option value="">— all phases —</option>
        {#each phasesB as p (p)}
          <option value={p}>{p}</option>
        {/each}
      </select>
    {/if}
```

- [ ] **Step 4: Style the phase select (reuse toolbar select styling)**

In the `<style>` block, add `.diff-phase` to the existing `.diff-toolbar select` rule so it inherits styling, e.g. change the selector to:

```css
  .diff-toolbar select,
  .diff-phase {
    padding: var(--s1) var(--s3);
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--text-sm);
    font-family: var(--font-ui);
    outline: none;
    min-width: 140px;
  }

  .diff-toolbar select:focus-visible,
  .diff-phase:focus-visible {
    border-color: var(--ring);
    box-shadow: 0 0 0 2px var(--ring);
  }
```

(Adjust `min-width` for `.diff-phase` to e.g. `120px` if desired — keep it visually consistent.)

- [ ] **Step 5: Typecheck**

Run (from `webui/`): `npm run typecheck`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/DiffView.svelte
git commit -m "feat(webui): per-side phase pickers in DiffView

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Rebuild `dist`, full frontend gate, e2e smoke

**Files:**

- Regenerate: `webui/dist/**`
- Possibly modify: `webui/e2e/diff.spec.ts` (only if the existing diff e2e breaks)

- [ ] **Step 1: Confirm the diff view degrades gracefully without a graph mock (review, not necessarily run)**

The existing `e2e/diff.spec.ts` mocks `/v1/sessions` and `/v1/diff` but NOT `/v1/sessions/{hash}/graph`. DiffView now calls `fetchSessionPhases` (→ the graph endpoint) when a session is selected; on a failed/un-mocked fetch it must degrade gracefully — the component's phase effects use `.catch(() => phasesA = [])`, so no phase select renders and the existing diff flow is unaffected. Confirm by reading Task 2's code that the catch is present.

Playwright e2e requires a chromium download (heavy) and is authoritatively run by CI. If you can run it locally, do: `npx playwright install --with-deps chromium && npm run test:e2e` (expect PASS). If the environment can't install browsers, SKIP and rely on CI — but you MUST still have confirmed the graceful-degradation catch above. Do NOT weaken any e2e assertion.

- [ ] **Step 2: Rebuild and stage `dist`**

Run (from `webui/`): `npm run build`
Then: `git add dist && git status --porcelain dist` — there SHOULD be staged changes (the bundle changed). Confirm `git diff --cached --stat dist` shows the rebuilt assets.

- [ ] **Step 3: Full frontend gate**

Run (from `webui/`): `npm run typecheck && npm run test && npm run build`
Then verify dist is current: `git status --porcelain dist` must be EMPTY after the final build + add (i.e. the committed dist matches a fresh build).
Expected: typecheck clean, `npm run test` 100% coverage, build clean, dist current.

- [ ] **Step 4: Commit**

```bash
git add webui/dist web/src/components/DiffView.svelte webui/e2e/diff.spec.ts 2>/dev/null; git add -A
git commit -m "build(webui): rebuild dist for phase pickers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final gate

- [ ] From `webui/`: `npm run typecheck && npm run test && npm run build && npm run test:e2e` all pass; `git status --porcelain dist` empty.
- [ ] From repo root: `npx --yes markdownlint-cli@0.49.0 '**/*.md' --ignore node_modules` exit 0 (the plan doc must lint clean).
- [ ] Go side untouched — no need to run Go tests, but `git status` should show only `webui/**` and `docs/**` changes.
