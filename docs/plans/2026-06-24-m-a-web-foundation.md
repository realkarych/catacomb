# Milestone A — Web Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Scope:** the **web foundation** half of Milestone A (spec §5.5 + §5.6, i.e. spec sub-projects **(e)** and **(f)**). This plan stands up the Vite + Svelte 5 toolchain embedded in the Go binary, the CI gates, the dark design system, the pure tested reducer, and the normalized runes stores. It pins the reducer/store/SSE-client public API as the **contract** consumed by the separate "web-views" plan.

**Explicitly NOT in this plan** (they are the "web-views" plan, spec §5.7–§5.9): the Sessions-list landing, client routing/deep-links, the Svelte Flow graph engine, and the node-detail drawer. **Also NOT here** (spec §5.1–§5.4): all backend Go work (`?session=` filter, `GET /v1/sessions`, pricing, duration stamping, rev-ordered flush) — those land in a separate backend plan. This plan only touches the **one Go file** that moves the `//go:embed` target, plus its tests, and is otherwise pure frontend toolchain + pure-logic TS.

**Architecture.** Frontend sources live under `webui/web/` (Vite root); the production build emits to `webui/dist/`, which is **committed to git** and embedded via the existing `//go:embed` directive in `webui/webui.go` (target moves `web` → `dist`). The Go `webui.Handler()` (`fs.Sub` + `http.FileServerFS`) model is unchanged. The old vanilla UI (`webui/web/{index.html,app.js,style.css}`) is **deleted entirely**. Pure logic (`reducer`, `stores` helpers, `format`, `pricing`, `sse`) is framework-free TypeScript gated at **100% coverage via Vitest**; Svelte components are **not** line-coverage-gated; Playwright drives one e2e smoke of the app shell. CI gains a frontend job (typecheck + vitest-with-gate + playwright) and a check that the committed `dist/` is up to date.

**Tech stack (versions pinned from npm registry on 2026-06-24 — do not downgrade without re-checking).**

| Package | Version | Role |
|---|---|---|
| `vite` | `^8.1.0` | build + dev server |
| `svelte` | `^5.56.4` | UI framework (runes) |
| `@sveltejs/vite-plugin-svelte` | `^7.1.2` | Svelte ↔ Vite integration |
| `vitest` | `^4.1.9` | unit test runner |
| `@vitest/coverage-v8` | `^4.1.9` | coverage provider (v8) |
| `jsdom` | `^29.1.1` | DOM env for store/rune tests |
| `@playwright/test` | `^1.61.1` | e2e |
| `svelte-check` | `^4.7.0` | Svelte + TS typecheck |
| `typescript` | `^6.0.3` | tsc |

Node `24` in CI (matches the existing `lint-docs` job). Local toolchain verified on Node `v26.3.1` / npm `11.16.0`.

> Sources for versions/config patterns: [Vite 7 release](https://vite.dev/blog/announcing-vite7), [@sveltejs/vite-plugin-svelte npm](https://www.npmjs.com/package/@sveltejs/vite-plugin-svelte?activeTab=versions), [Svelte 5 runes / `.svelte.ts` docs](https://svelte.dev/docs/svelte/svelte-js-files), [Svelte testing docs](https://svelte.dev/docs/svelte/testing), [Vitest coverage config](https://vitest.dev/config/#coverage), [Playwright webServer config](https://playwright.dev/docs/test-webserver), [npm `view <pkg> version`].

---

## Global Constraints

- **Frontend deps via the JS toolchain ONLY.** Never `go mod tidy` for them; they never enter the Go module graph. The embedded `dist/` is **data**, not Go. (Spec §4.2, §10 Q4.)
- **Go serve path stays green and 100%.** The only Go change is `webui/webui.go`'s embed target (`web` → `dist`) + adjusting `webui/webui_test.go` expectations; the daemon still serves `/` as `text/html` from the built `dist/index.html`. (Spec §5.5.)
- **No Go comments** except `//go:build|//go:embed|//go:generate` — only `//go:embed` appears here; `internal/codepolicy` parses every hand-written `.go` file. The committed `dist/` and all JS/TS are **skipped** by codepolicy (non-Go) and **excluded** from the Go coverage gate (not Go statements). (Spec §4.2.)
- **Pure-logic JS = 100% coverage** via Vitest (`reducer`, `stores` helpers, `format`, `pricing`, `sse`). **Components are NOT line-coverage-gated.** (Spec §4.1 item 3, §10 Q4.)
- **Determinism contract mirrored:** the TS reducer proves *same deltas, any order → same state* with a permutation test, mirroring the Go reducer's `permute(...)` tests in `reduce/reduce_test.go`. (Spec §4.2, §5.6.)
- **`GOOS=windows go build ./...` stays clean.** (Spec §4.2.)
- **Commit per task; never commit to `master`.** Branch first (`git checkout -b feat/m-a-web-foundation`). Squash-merge via PR. No `--no-verify`. (AGENTS.md Workflow.)
- **Dark theme only.** Light theme is Milestone D. (Spec §5.5.)
- **`.gitignore` currently ignores `/dist/`** (root-anchored). The committed build dir is `webui/dist/` (NOT root `/dist/`), so the existing rule does **not** match it — but Task e1 adds an explicit guard to keep it tracked and to prevent a future `webui/dist` rule from sneaking in. Verify with `git check-ignore webui/dist/index.html` returning nothing.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `webui/web/index.html` | **Delete → recreate** | OLD vanilla shell deleted; recreated as the Vite entry HTML (module `<script>`, favicon, title) |
| `webui/web/app.js` | **Delete** | OLD vanilla renderer — gone |
| `webui/web/style.css` | **Delete → recreate** | OLD styles deleted; recreated as the design-token + base-shell stylesheet |
| `webui/package.json` | Create | deps, scripts (`dev`/`build`/`check`/`test`/`test:e2e`) |
| `webui/package-lock.json` | Create (generated) | committed lockfile |
| `webui/.gitignore` | Create | ignore `node_modules/`, Playwright artifacts; **do not** ignore `dist/` |
| `webui/vite.config.ts` | Create | Vite root=`web`, outDir=`../dist`, Svelte plugin, base `./` |
| `webui/svelte.config.js` | Create | `vitePreprocess({ script: true })` |
| `webui/tsconfig.json` | Create | strict TS, Svelte types, bundler resolution |
| `webui/vitest.config.ts` | Create | jsdom env, **100% coverage gate scoped to pure-logic globs** |
| `webui/playwright.config.ts` | Create | `webServer` = vite preview of `dist/`, one chromium project |
| `webui/src/main.ts` | Create | mounts root `App.svelte` |
| `webui/src/App.svelte` | Create | base app shell/layout (no feature views) |
| `webui/src/lib/format/format.ts` | Create | pure formatters (duration, tokens, cost, hash) — gated |
| `webui/src/lib/format/format.test.ts` | Create | 100% unit tests |
| `webui/src/lib/pricing/provenance.ts` | Create | pure `cost_source` provenance helper — gated |
| `webui/src/lib/pricing/provenance.test.ts` | Create | 100% unit tests |
| `webui/src/lib/types.ts` | Create | pinned TS types for the backend wire contract |
| `webui/src/lib/reducer/reducer.ts` | Create | pure `applyDelta` + `GraphState` — gated |
| `webui/src/lib/reducer/reducer.test.ts` | Create | 100% unit tests incl. permutation/determinism |
| `webui/src/lib/stores/selectors.ts` | Create | pure store-logic helpers (`sessionGraphFrom`, `upsertById`, …) — gated |
| `webui/src/lib/stores/selectors.test.ts` | Create | 100% unit tests |
| `webui/src/lib/stores/stores.svelte.ts` | Create | thin runes wiring (`$state` maps, `selectNode`, `sessionGraph`) — NOT gated |
| `webui/src/lib/sse/client.ts` | Create | `connect({session, token, onEvent})` SSE client — gated logic + thin EventSource seam |
| `webui/src/lib/sse/client.test.ts` | Create | 100% unit tests (fake EventSource) |
| `webui/e2e/shell.spec.ts` | Create | Playwright smoke: shell loads + connects |
| `webui/dist/**` | Create (generated, committed) | built SPA embedded by Go |
| `webui/webui.go` | Modify | `//go:embed web` → `//go:embed dist`; `fs.Sub(assets, "web")` → `"dist"` |
| `webui/webui_test.go` | Modify | assert served `index.html` is the built shell (token-bootstrap), keep route/Content-Type tests green |
| `.github/workflows/ci.yml` | Modify | add `frontend` job (install → check → vitest+gate → build → dist-drift check → playwright) |
| `Makefile` | Modify | add `web-install`/`web-build`/`web-test`/`web-e2e` targets |
| `.gitignore` | Modify | add `webui/node_modules/`, `webui/test-results/`, `webui/playwright-report/`; note `webui/dist/` stays tracked |

---

## Task e1: Scaffold Vite + Svelte 5, move `go:embed` to `dist/`, delete the old UI

**Files:**

- Create: `webui/package.json`, `webui/.gitignore`, `webui/vite.config.ts`, `webui/svelte.config.js`, `webui/tsconfig.json`, `webui/src/main.ts`, `webui/src/App.svelte`, `webui/src/vite-env.d.ts`
- Delete: `webui/web/app.js`; replace contents of `webui/web/index.html` and `webui/web/style.css` (the OLD vanilla files) per below
- Create (generated, committed): `webui/dist/**`, `webui/package-lock.json`
- Modify: `webui/webui.go`, `webui/webui_test.go`, `.gitignore`, `Makefile`

**Interfaces:**

- Produces: a buildable Vite project rooted at `webui/web/` emitting to `webui/dist/`
- Produces: `webui.Handler()` now serving the built `dist/` (signature unchanged: `func Handler() http.Handler`)
- Consumes: nothing new in Go; the daemon mux wiring in `daemon/server.go` (`mux.Handle("GET /", webui.Handler())`) is untouched

**Notes for implementer:** The `//go:embed dist` directive requires `webui/dist/` to exist and be non-empty **at Go build time** — so the Vite build MUST run and be committed before the Go tests run. The directive is the only allowed comment in the file. Vite's `root` is `web/` and `build.outDir` is `../dist` (relative to root) with `emptyOutDir: true`; `base: './'` makes asset URLs relative so the embed serves correctly from `/`. Do NOT use absolute `/assets/...` base — keep it relative so a future SPA deep-link mount still resolves assets (see "Gaps" at the end re: deep-link fallback).

- [ ] **Step 1: Create the branch**

```bash
cd /Users/karych/src/catacomb && git checkout -b feat/m-a-web-foundation
```

- [ ] **Step 2: Write `webui/package.json`**

`/Users/karych/src/catacomb/webui/package.json`:

```json
{
  "name": "catacomb-webui",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "check": "svelte-check --tsconfig ./tsconfig.json",
    "test": "vitest run",
    "test:watch": "vitest",
    "test:e2e": "playwright test"
  },
  "devDependencies": {
    "@playwright/test": "^1.61.1",
    "@sveltejs/vite-plugin-svelte": "^7.1.2",
    "@vitest/coverage-v8": "^4.1.9",
    "jsdom": "^29.1.1",
    "svelte": "^5.56.4",
    "svelte-check": "^4.7.0",
    "typescript": "^6.0.3",
    "vite": "^8.1.0",
    "vitest": "^4.1.9"
  }
}
```

- [ ] **Step 3: Write `webui/.gitignore`**

`/Users/karych/src/catacomb/webui/.gitignore`:

```gitignore
node_modules/
test-results/
playwright-report/
.svelte-kit/
*.tsbuildinfo
```

(Note: `dist/` is intentionally **absent** — it is committed.)

- [ ] **Step 4: Write `webui/vite.config.ts`**

`/Users/karych/src/catacomb/webui/vite.config.ts`:

```ts
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  root: 'web',
  base: './',
  plugins: [svelte()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    target: 'es2022',
  },
});
```

- [ ] **Step 5: Write `webui/svelte.config.js`**

`/Users/karych/src/catacomb/webui/svelte.config.js`:

```js
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess({ script: true }),
};
```

- [ ] **Step 6: Write `webui/tsconfig.json`**

`/Users/karych/src/catacomb/webui/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "verbatimModuleSyntax": true,
    "isolatedModules": true,
    "skipLibCheck": true,
    "types": ["svelte", "vite/client"],
    "resolveJsonModule": true
  },
  "include": ["src/**/*.ts", "src/**/*.svelte", "web/**/*.ts", "e2e/**/*.ts", "*.config.ts"]
}
```

- [ ] **Step 7: Write `webui/src/vite-env.d.ts`**

`/Users/karych/src/catacomb/webui/src/vite-env.d.ts`:

```ts
/// <reference types="svelte" />
/// <reference types="vite/client" />
```

- [ ] **Step 8: Delete the old vanilla UI files; create the Vite entry HTML**

```bash
cd /Users/karych/src/catacomb && git rm webui/web/app.js
```

Overwrite `/Users/karych/src/catacomb/webui/web/index.html` with the Vite entry (favicon + capitalized title per spec §5.5; the SVG favicon is data-URI inline so there is no extra asset 404):

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Catacomb</title>
    <link
      rel="icon"
      href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='6' fill='%230b0d12'/%3E%3Cpath d='M8 22V10l8 6 8-6v12' fill='none' stroke='%237aa2ff' stroke-width='2.5' stroke-linejoin='round' stroke-linecap='round'/%3E%3C/svg%3E"
    />
    <meta name="color-scheme" content="dark" />
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

(The `/src/main.ts` path is the Vite dev/build convention; Vite rewrites it to the hashed bundle in `dist/`. Note `src/` is a sibling of `web/`, resolved by Vite because the script is module-resolved from the project, not the web root.)

> If the implementer prefers `src/` under `web/` to keep Vite's root self-contained, that is acceptable — but then **all** `src/...` paths in this plan shift to `web/src/...` and `tsconfig` `include`/Vitest globs must follow. Pick one and keep it consistent. This plan assumes `webui/src/` (sibling of `web/`) and `script src="/src/main.ts"`.

- [ ] **Step 9: Write `webui/src/main.ts`**

`/Users/karych/src/catacomb/webui/src/main.ts`:

```ts
import { mount } from 'svelte';
import App from './App.svelte';
import './../web/style.css';

const target = document.getElementById('app');
if (!target) throw new Error('catacomb: #app mount point missing');

export default mount(App, { target });
```

- [ ] **Step 10: Write a minimal `webui/src/App.svelte` (full shell lands in e3)**

`/Users/karych/src/catacomb/webui/src/App.svelte`:

```svelte
<script lang="ts">
  import { connectionState } from './lib/stores/stores.svelte';
</script>

<div class="app-shell">
  <header class="topbar">
    <span class="wordmark">Catacomb</span>
    <span class="conn" data-state={connectionState.status}>{connectionState.status}</span>
  </header>
  <main class="content">
    <slot />
  </main>
</div>
```

> `connectionState` is defined in e1's stores stub (Step 11) and finalized in f2; `style.css` classes land in e3. This component stays a thin shell — **no Sessions list, no graph, no drawer** (those are web-views).

- [ ] **Step 11: Create a minimal stores stub so the shell compiles**

`/Users/karych/src/catacomb/webui/src/lib/stores/stores.svelte.ts` (minimal now; expanded in f2):

```ts
export const connectionState = $state<{ status: 'idle' | 'connecting' | 'open' | 'error' }>({
  status: 'idle',
});
```

- [ ] **Step 12: Recreate `webui/web/style.css` as a minimal placeholder (full design system in e3)**

Overwrite `/Users/karych/src/catacomb/webui/web/style.css`:

```css
:root {
  color-scheme: dark;
}
html,
body {
  margin: 0;
  background: #0b0d12;
  color: #e6e8ee;
  font-family: system-ui, sans-serif;
}
```

- [ ] **Step 13: Install deps and build (generates `dist/` + lockfile)**

```bash
cd /Users/karych/src/catacomb/webui && npm install && npm run build
```

Expected: `webui/dist/index.html` + `webui/dist/assets/*.js`/`*.css` produced; `webui/package-lock.json` created.

- [ ] **Step 14: Verify `webui/dist/` is NOT gitignored**

```bash
cd /Users/karych/src/catacomb && git check-ignore webui/dist/index.html; echo "exit=$?"
```

Expected: no output, `exit=1` (not ignored). If it IS ignored, add `!webui/dist/` to `.gitignore`.

- [ ] **Step 15: Point the Go embed at `dist/`**

Edit `/Users/karych/src/catacomb/webui/webui.go`:

```go
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var assets embed.FS

var subFn = fs.Sub

func Handler() http.Handler {
	sub, err := subFn(assets, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
	}
	return http.FileServerFS(sub)
}
```

- [ ] **Step 16: Update `webui/webui_test.go` expectations**

The old tests asserted the vanilla `app.js`/`style.css`/`id="app"` strings. Update the content assertions to match the built shell. Read the existing file first; then:

- Keep `TestHandlerServesIndexHTML` / `TestHandlerServesRootAsIndexHTML` (Content-Type `text/html`, 200) — these still hold for `dist/index.html`.
- Keep `TestHandlerMissingAsset404`.
- Replace any assertion of literal `app.js` / `style.css` / `id="app"` filenames with assertions that survive Vite hashing, e.g.:

```go
func TestHandlerIndexIsBuiltShell(t *testing.T) {
	srv := httptest.NewServer(webui.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `id="app"`)
	assert.Contains(t, string(body), "<title>Catacomb</title>")
	assert.Contains(t, string(body), "type=\"module\"")
}
```

(`id="app"` and `<title>Catacomb</title>` are stable across builds; the hashed `<script src>` is not, so assert on `type="module"` rather than a filename. If the old asset-Content-Type tests hit `/app.js` / `/style.css` by name, delete them — those files no longer exist; the hashed assets live under `/assets/`.)

- [ ] **Step 17: Update `.gitignore` (root)**

Add to `/Users/karych/src/catacomb/.gitignore` (the `webui/` JS artifacts; keep `webui/dist/` tracked):

```gitignore
# Frontend (webui) — node artifacts; dist/ is committed (embedded by go:embed)
webui/node_modules/
webui/test-results/
webui/playwright-report/
```

Leave the existing root `/dist/` rule as-is (it is root-anchored and does not match `webui/dist/`).

- [ ] **Step 18: Add Makefile frontend targets**

Append to `/Users/karych/src/catacomb/Makefile`:

```make
WEB := webui

.PHONY: web-install web-build web-test web-e2e web-check
web-install:
	cd $(WEB) && npm ci
web-check:
	cd $(WEB) && npm run check
web-build:
	cd $(WEB) && npm run build
web-test:
	cd $(WEB) && npm run test
web-e2e:
	cd $(WEB) && npx playwright install --with-deps chromium && npm run test:e2e
```

- [ ] **Step 19: Go tests + cross-platform build green**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./webui/ ./daemon/ -v 2>&1 | tail -20
cd /Users/karych/src/catacomb && GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
```

Expected: all PASS; clean cross-builds.

- [ ] **Step 20: Coverage on the touched Go package is 100%**

```bash
cd /Users/karych/src/catacomb && go test -race -coverprofile=/tmp/cov-webui.out ./webui/ && go tool cover -func=/tmp/cov-webui.out | grep -v '100.0%'
```

Expected: no `webui/webui.go` lines below 100%.

- [ ] **Step 21: Codepolicy (no-comments) still green**

```bash
cd /Users/karych/src/catacomb && go test ./internal/codepolicy/...
```

Expected: PASS (only `//go:embed` present in `webui.go`; `dist/` + JS/TS are non-Go and skipped).

- [ ] **Step 22: Commit**

```bash
cd /Users/karych/src/catacomb && git add -A && git commit -m "$(cat <<'EOF'
feat(webui): scaffold Vite + Svelte 5; move go:embed to dist/; delete vanilla UI

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task e2: Frontend CI + test harness (Vitest gate + Playwright + dist-drift check)

**Files:**

- Create: `webui/vitest.config.ts`, `webui/playwright.config.ts`, `webui/e2e/shell.spec.ts`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**

- Produces: `npm run test` (vitest, **fails if pure-logic globs aren't 100%**), `npm run test:e2e` (playwright)
- Produces: a CI `frontend` job: install → `check` → `test` (with gate) → `build` → **dist-drift check** → `test:e2e`
- Consumes: nothing from Go; runs entirely in the JS toolchain

**Notes for implementer:** The coverage gate is **scoped** — `coverage.include` lists only pure-logic globs, so components contribute nothing to the denominator and are not gated (spec §4.1/§10 Q4). With `thresholds.100: true` + `perFile: { 100: true }`, every *included* file must hit 100% on all four metrics. Vitest 4 only reports coverage for files matched by `include` AND loaded by a test; the `perFile` shortcut catches an included file that a test forgets to load (it reports 0% → fail). Playwright runs against `vite preview` of the **committed `dist/`** (not the dev server) so the e2e exercises exactly what ships. The dist-drift check rebuilds and fails if `git status --porcelain webui/dist` is dirty.

- [ ] **Step 1: Write `webui/vitest.config.ts`**

`/Users/karych/src/catacomb/webui/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte({ hot: false })],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,svelte.ts}'],
    exclude: ['e2e/**', 'node_modules/**'],
    coverage: {
      provider: 'v8',
      include: [
        'src/lib/reducer/**',
        'src/lib/stores/selectors.ts',
        'src/lib/format/**',
        'src/lib/pricing/**',
        'src/lib/sse/client.ts',
      ],
      exclude: [
        '**/*.test.ts',
        '**/*.spec.ts',
        'src/lib/stores/stores.svelte.ts',
        'src/**/*.svelte',
      ],
      thresholds: {
        100: true,
        perFile: { 100: true },
      },
      reporter: ['text', 'text-summary'],
    },
  },
});
```

> Note: `stores.svelte.ts` (rune wiring) is **excluded** from the gate; only `stores/selectors.ts` (pure logic) is included. This is the spec's "rune wiring is thin / not gated" split.

- [ ] **Step 2: Write `webui/playwright.config.ts`**

`/Users/karych/src/catacomb/webui/playwright.config.ts`:

```ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: 'e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run preview -- --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
```

> `vite preview` serves the committed `dist/`; the e2e therefore validates the embedded artifact. There is no live daemon in CI, so the smoke asserts the shell renders and the connection indicator reaches a known state (`connecting` then `error`/`idle` when no SSE backend) — it does NOT assert real data. Live-data e2e against a daemon is a web-views concern.

- [ ] **Step 3: Write the smoke e2e `webui/e2e/shell.spec.ts`**

`/Users/karych/src/catacomb/webui/e2e/shell.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test('app shell loads and shows the wordmark', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveTitle('Catacomb');
  await expect(page.locator('.wordmark')).toHaveText('Catacomb');
});

test('connection indicator is present and reaches a settled state', async ({ page }) => {
  await page.goto('/');
  const conn = page.locator('.conn');
  await expect(conn).toBeVisible();
  await expect(conn).not.toHaveAttribute('data-state', 'idle', { timeout: 5000 });
});
```

> The second assertion proves the SSE client (f-tasks) actually fires on mount: with no backend it transitions `idle → connecting → error`, so `data-state` leaves `idle`. If the f-tasks land after e2 in execution order, gate this test with a TODO and enable it once `connect()` is wired in `App.svelte` (the wiring is part of f2/web-views; at minimum `connecting` is reached on mount). Keep the first test unconditional.

- [ ] **Step 4: Local sanity — vitest + playwright run**

```bash
cd /Users/karych/src/catacomb/webui && npm run test && npx playwright install --with-deps chromium && npm run test:e2e
```

Expected: vitest passes (note: with no `src/lib/**` logic files yet, coverage `include` matches nothing → vitest reports no coverage but does not fail; the gate becomes load-bearing once f-tasks add gated files). Playwright shell test passes.

- [ ] **Step 5: Add the `frontend` CI job**

Edit `/Users/karych/src/catacomb/.github/workflows/ci.yml`, adding a job (peer to `test`/`coverage`):

```yaml
  frontend:
    name: Frontend (vitest + playwright)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: webui
    steps:
      - uses: actions/checkout@v7.0.0
      - name: Set up Node.js
        uses: actions/setup-node@v6.4.0
        with:
          node-version: "24"
          cache: npm
          cache-dependency-path: webui/package-lock.json
      - name: Install
        run: npm ci
      - name: Typecheck
        run: npm run check
      - name: Unit tests + coverage gate
        run: npm run test -- --coverage
      - name: Build
        run: npm run build
      - name: Verify committed dist/ is up to date
        run: |
          if [ -n "$(git status --porcelain dist)" ]; then
            echo "::error::webui/dist is stale — run 'make web-build' and commit the result"
            git --no-pager diff --stat dist
            exit 1
          fi
      - name: Install Playwright browsers
        run: npx playwright install --with-deps chromium
      - name: E2E
        run: npm run test:e2e
```

> The dist-drift check runs `git status --porcelain dist` from `working-directory: webui`, so `dist` resolves to `webui/dist`. Vite build output must be **deterministic** across machines — it is, for a fixed dep set + lockfile; if hashes drift spuriously, pin `build.rollupOptions.output` filename patterns. Flag this in Self-Review if drift appears.

- [ ] **Step 6: Commit**

```bash
cd /Users/karych/src/catacomb && git add -A && git commit -m "$(cat <<'EOF'
ci(webui): vitest 100% logic gate + playwright smoke + dist-drift check

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task e3: Dark design system (OKLCH tokens, type scale, base shell)

**Files:**

- Modify: `webui/web/style.css` (full design system), `webui/src/App.svelte` (consume the shell classes)
- Rebuild + recommit `webui/dist/**`

**Interfaces:**

- Produces: a CSS design-token layer (`--bg/--surface/--border/--text/--accent` + a semantic node-type color ramp at even perceived brightness), Inter for UI + a mono face for ids/hashes/code, a type scale, and the base app shell/layout
- Consumes: the favicon + `<title>` already in `index.html` (e1)

**Notes for implementer:** OKLCH is well-supported in current browsers; keep lightness roughly constant across the node-type ramp so hues read at **even perceived brightness** (spec §5.5). Load Inter and the mono face **self-hosted** (a woff2 in `web/fonts/` referenced by `@font-face`) — do NOT pull from a CDN (the binary is offline-first; a CDN font would 404 behind a firewall and break the "not a prototype" bar). Mono is used **only** for ids/hashes/code (spec §5.5/§9). The node-type ramp must cover every `model.NodeType`: `session, user_prompt, assistant_turn, tool_call, subagent, mcp_call, hook_event, marker`. This is the visual floor; full chrome (illustrated states, connection pill, wordmark beyond minimum) is Milestone D.

- [ ] **Step 1: Add self-hosted fonts**

Place `Inter` (variable or 400/500/600) and a mono face (e.g. `JetBrains Mono` 400/500) woff2 files under `/Users/karych/src/catacomb/webui/web/fonts/`. Reference them via `@font-face` (see Step 2). These become committed assets embedded in `dist/`.

> Licensing: Inter (OFL) and JetBrains Mono (OFL) are redistributable; include them as binary assets. If the implementer cannot vendor fonts, fall back to `font-family: 'Inter', system-ui, sans-serif` / `ui-monospace, 'JetBrains Mono', monospace` so the stack degrades gracefully — but vendoring is preferred for the visual bar.

- [ ] **Step 2: Write the full `webui/web/style.css`**

Overwrite `/Users/karych/src/catacomb/webui/web/style.css`:

```css
@font-face {
  font-family: 'Inter';
  src: url('./fonts/Inter-Variable.woff2') format('woff2');
  font-weight: 100 900;
  font-display: swap;
}
@font-face {
  font-family: 'JetBrains Mono';
  src: url('./fonts/JetBrainsMono-Regular.woff2') format('woff2');
  font-weight: 400;
  font-display: swap;
}

:root {
  color-scheme: dark;

  /* base surfaces (OKLCH, dark) */
  --bg: oklch(16% 0.012 265);
  --surface: oklch(20% 0.014 265);
  --surface-2: oklch(24% 0.016 265);
  --border: oklch(30% 0.02 265);
  --text: oklch(92% 0.01 265);
  --text-muted: oklch(70% 0.015 265);
  --accent: oklch(72% 0.16 255);
  --accent-quiet: oklch(40% 0.08 255);
  --danger: oklch(64% 0.2 25);
  --ok: oklch(72% 0.17 150);
  --warn: oklch(80% 0.15 85);

  /* node-type ramp — even perceived brightness (L≈70%), hue-stepped */
  --node-session: oklch(70% 0.15 255);
  --node-user_prompt: oklch(70% 0.15 300);
  --node-assistant_turn: oklch(70% 0.15 150);
  --node-tool_call: oklch(70% 0.15 65);
  --node-subagent: oklch(70% 0.15 195);
  --node-mcp_call: oklch(70% 0.15 340);
  --node-hook_event: oklch(70% 0.15 95);
  --node-marker: oklch(70% 0.04 265);

  /* type scale (1.25 minor-third) */
  --font-ui: 'Inter', system-ui, -apple-system, sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, SFMono-Regular, monospace;
  --fs-xs: 0.75rem;
  --fs-sm: 0.875rem;
  --fs-base: 1rem;
  --fs-lg: 1.25rem;
  --fs-xl: 1.563rem;
  --fs-2xl: 1.953rem;
  --lh: 1.5;
  --radius: 8px;
  --space: 8px;
}

*,
*::before,
*::after {
  box-sizing: border-box;
}
html,
body {
  margin: 0;
  height: 100%;
}
body {
  background: var(--bg);
  color: var(--text);
  font-family: var(--font-ui);
  font-size: var(--fs-base);
  line-height: var(--lh);
  -webkit-font-smoothing: antialiased;
}
code,
kbd,
samp,
.mono,
[data-mono] {
  font-family: var(--font-mono);
}

#app {
  height: 100%;
}
.app-shell {
  display: flex;
  flex-direction: column;
  height: 100vh;
}
.topbar {
  display: flex;
  align-items: center;
  gap: calc(var(--space) * 1.5);
  height: 48px;
  padding: 0 calc(var(--space) * 2);
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}
.wordmark {
  font-weight: 600;
  letter-spacing: 0.02em;
}
.conn {
  margin-left: auto;
  font-size: var(--fs-xs);
  color: var(--text-muted);
  text-transform: lowercase;
}
.conn[data-state='open'] {
  color: var(--ok);
}
.conn[data-state='connecting'] {
  color: var(--warn);
}
.conn[data-state='error'] {
  color: var(--danger);
}
.content {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
```

- [ ] **Step 3: Confirm `App.svelte` consumes the shell (already done in e1)**

`App.svelte` from e1 already references `.app-shell/.topbar/.wordmark/.conn`; no change needed unless the implementer added markup. Keep it a thin shell.

- [ ] **Step 4: Rebuild + verify dist updated**

```bash
cd /Users/karych/src/catacomb/webui && npm run build && cd /Users/karych/src/catacomb && git status --porcelain webui/dist
```

Expected: `dist/` shows modified files (the new CSS + fonts hashed in).

- [ ] **Step 5: Go serve tests still green (built shell unchanged structurally)**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./webui/ ./daemon/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/karych/src/catacomb && git add -A && git commit -m "$(cat <<'EOF'
feat(webui): dark OKLCH design system — tokens, type scale, base shell, fonts

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task f1: Pure reducer `reducer.ts` (clobber/rev fix + determinism)

**Files:**

- Create: `webui/src/lib/types.ts`, `webui/src/lib/reducer/reducer.ts`, `webui/src/lib/reducer/reducer.test.ts`
- Rebuild + recommit `webui/dist/**` (the bundle now includes reducer code if imported by the shell; if not yet imported it won't change `dist` — that's fine)

**Interfaces (PINNED — this is the web-views contract):**

```ts
// types.ts
export type CostSource = 'reported' | 'estimated';

export interface SseEvent {
  kind: string;
  rev: number;
  run_id?: string;
  execution_id?: string;
  node?: Node;
  edge?: Edge;
  old_id?: string;
  new_id?: string;
}

export interface Node {
  id: string;
  run_id: string;
  type: string;
  parent_id?: string;
  agent_id?: string;
  parent_agent_id?: string;
  subagent_type?: string;
  name?: string;
  status?: string;
  t_start?: string;
  t_end?: string;
  duration_ms?: number;
  tokens_in?: number;
  tokens_out?: number;
  cost_usd?: number;
  attrs?: Record<string, unknown>;
  payload_hash?: string;
  sources?: { source: string; obs_id: string; observed_at: string }[];
  tier?: string;
  rev: number;
}

export interface Edge {
  id: string;
  run_id: string;
  type: string;
  src: string;
  dst: string;
  attrs?: Record<string, unknown>;
  rev: number;
}

// reducer.ts
export interface GraphState {
  nodes: Record<string, Node>;
  edges: Record<string, Edge>;
}
export function emptyState(): GraphState;
export function applyDelta(state: GraphState, ev: SseEvent): void;
```

**Notes for implementer (the §5.6 clobber/rev fix — read carefully):**

- `applyDelta` mutates `state` in place (returns `void`) — that is the pinned signature; the runes store will wrap a `$state` GraphState and call it.
- **`node_upsert`**: rev-guarded **only against a prior full upsert**. Mark upserted nodes with an internal flag distinguishing "established by upsert" from "status-only seed". A `node_upsert` ALWAYS replaces a status-only seed regardless of rev; against another upsert it applies only if `ev.node.rev > existing.rev` (strictly greater; equal is a dup → drop, matching the server's monotonic-rev flush).
- **`node_status`**: a **field-wise patch** of `status`, `t_end`, `duration_ms` (and any future status-only fields) that **NEVER establishes identity** and is **ALWAYS overwritten by a later `node_upsert`**. If the node does not exist yet, seed a status-only partial (id + the status fields, marked as a seed). If it exists, merge the status fields (latest by rev wins for the status patch itself), but do not touch name/type/tokens/attrs.
- **`node_merge`**: delete `old_id` if present, set the new node under `ev.node.id` (full node, established).
- **`edge_upsert`**: rev-guarded (`ev.edge.rev > existing.rev`).
- **`edge_delete`**: delete `ev.edge.id` if present.
- **Unknown / non-graph kinds** (`run_started`, `session_ended`, `run_ended`, anything else) → **no-op** (forward-compatible; keeps determinism — the backend bus emits these but the graph state ignores them).
- Track the "established vs seed" bit **out of band** so it never leaks into the public `Node` shape. Recommended: a parallel `Set<string>` of established ids is messy to thread through `void` mutation, so instead store it as a non-enumerable / underscored marker on the in-state node and strip it in the store read path — OR keep `GraphState` honest and put the marker in a side map on the state object:

```ts
export interface GraphState {
  nodes: Record<string, Node>;
  edges: Record<string, Edge>;
  established: Record<string, true>; // ids whose node came from node_upsert/node_merge
}
```

This plan pins `GraphState` to include `established` (it is part of the contract; web-views reads `nodes`/`edges` and ignores `established`). The determinism test treats `established` as part of the canonical state.

- **Determinism:** the test MUST mirror `reduce/reduce_test.go`'s `permute(...)`: build a fixed delta sequence covering all five kinds (status-before-upsert, upsert-before-status, merge, edge upsert/delete, dup-rev drop), enumerate all permutations, fold each through a fresh `emptyState()`, and assert every permutation yields a **deep-equal** `GraphState`. Use a small N (≤6 deltas → 720 perms) to stay fast.

- [ ] **Step 1: Write `webui/src/lib/types.ts`** (exact shape above)

- [ ] **Step 2: Write failing tests `webui/src/lib/reducer/reducer.test.ts`**

Cover, at minimum:

1. `node_upsert` inserts a node and marks it established.
2. `node_upsert` with `rev <= existing.rev` (existing established) is dropped.
3. `node_upsert` with `rev > existing.rev` replaces.
4. `node_status` before any upsert seeds a status-only partial (status/t_end/duration set, no name/type), NOT marked established.
5. **The clobber fix:** `node_status(rev=5)` then `node_upsert(rev=1)` → the upsert wins (name/type/tokens present), even though `1 < 5`. (This is the headline regression.)
6. `node_status` after `node_upsert` merges status/t_end/duration only; name/type/tokens preserved.
7. `node_status` newer-rev vs older-rev ordering on the status fields themselves.
8. `node_merge` deletes `old_id`, installs new node established.
9. `node_merge` when `old_id` absent still installs the new node.
10. `edge_upsert` insert + rev-guard drop + rev-bump replace.
11. `edge_delete` removes; `edge_delete` for a missing id is a no-op.
12. Unknown kind (`run_started`) is a no-op.
13. Missing `ev.node` / `ev.edge` for the relevant kind is a safe no-op (defensive).
14. **Determinism/permutation:** all permutations of a fixed 5–6 delta set → deep-equal `GraphState`.

```ts
import { describe, it, expect } from 'vitest';
import { emptyState, applyDelta, type GraphState } from './reducer';
import type { SseEvent } from '../types';

function fold(deltas: SseEvent[]): GraphState {
  const s = emptyState();
  for (const d of deltas) applyDelta(s, d);
  return s;
}

function permutations<T>(xs: T[]): T[][] {
  if (xs.length <= 1) return [xs];
  const out: T[][] = [];
  xs.forEach((x, i) => {
    const rest = [...xs.slice(0, i), ...xs.slice(i + 1)];
    for (const p of permutations(rest)) out.push([x, ...p]);
  });
  return out;
}

describe('applyDelta — clobber/rev fix', () => {
  it('node_upsert always overrides an earlier status-only seed regardless of rev', () => {
    const s = emptyState();
    applyDelta(s, { kind: 'node_status', rev: 5, node: { id: 'n1', run_id: 'r', type: '', status: 'ok', rev: 5 } });
    applyDelta(s, { kind: 'node_upsert', rev: 1, node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', tokens_in: 10, rev: 1 } });
    expect(s.nodes.n1.type).toBe('tool_call');
    expect(s.nodes.n1.name).toBe('Bash');
    expect(s.nodes.n1.tokens_in).toBe(10);
    expect(s.established.n1).toBe(true);
  });
});

describe('applyDelta — determinism', () => {
  it('any permutation of the same deltas yields the same state', () => {
    const deltas: SseEvent[] = [
      { kind: 'node_status', rev: 5, node: { id: 'n1', run_id: 'r', type: '', status: 'ok', t_end: '2026-01-01T00:00:01Z', rev: 5 } },
      { kind: 'node_upsert', rev: 2, node: { id: 'n1', run_id: 'r', type: 'tool_call', name: 'Bash', rev: 2 } },
      { kind: 'node_upsert', rev: 3, node: { id: 'n2', run_id: 'r', type: 'mcp_call', name: 'fetch', rev: 3 } },
      { kind: 'edge_upsert', rev: 4, edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'n1', dst: 'n2', rev: 4 } },
      { kind: 'edge_delete', rev: 6, edge: { id: 'e1', run_id: 'r', type: 'parent_child', src: 'n1', dst: 'n2', rev: 6 } },
    ];
    const perms = permutations(deltas);
    const want = fold(perms[0]);
    for (const p of perms) expect(fold(p)).toEqual(want);
  });
});
```

> Note the determinism set deliberately ends with `edge_delete` of `e1`, so the canonical end-state has no `e1` regardless of whether the delete is folded before or after the upsert — `applyDelta` must make delete-then-upsert and upsert-then-delete converge. **This forces a design decision:** an `edge_delete` must be **terminal for that edge id within the set** OR rev-gated so a lower-rev upsert cannot resurrect it. Pin it as: `edge_delete` removes the edge AND records a tombstone rev; a later `edge_upsert` with `rev <=` the tombstone is dropped. Apply the same tombstone discipline to nodes if a `node`-delete kind is ever added (none today). Document the tombstone in `GraphState` (`tombstones: Record<string, number>`), include it in the pinned shape, and cover it in tests. If the implementer finds the tombstone over-engineered for A's actual delta stream, the simpler alternative is to pick a determinism set that does not mix delete+upsert of the same id — but the tombstone is the honest mirror of the Go reducer and is recommended.

- [ ] **Step 3: Run tests — expect FAIL** (`reducer.ts` not yet written)

```bash
cd /Users/karych/src/catacomb/webui && npm run test -- src/lib/reducer
```

- [ ] **Step 4: Implement `webui/src/lib/reducer/reducer.ts`** to satisfy the tests, honoring every rule above.

- [ ] **Step 5: Run tests + coverage gate green**

```bash
cd /Users/karych/src/catacomb/webui && npm run test -- --coverage src/lib/reducer
```

Expected: PASS; `reducer.ts` at 100% lines/functions/branches/statements.

- [ ] **Step 6: Typecheck**

```bash
cd /Users/karych/src/catacomb/webui && npm run check
```

- [ ] **Step 7: Commit**

```bash
cd /Users/karych/src/catacomb && git add -A && git commit -m "$(cat <<'EOF'
feat(webui): pure reducer with clobber/rev fix + determinism (100% covered)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task f2: Normalized stores (runes) + pure selector helpers + SSE client + format/pricing

**Files:**

- Create: `webui/src/lib/format/format.ts` (+ `.test.ts`)
- Create: `webui/src/lib/pricing/provenance.ts` (+ `.test.ts`)
- Create: `webui/src/lib/stores/selectors.ts` (+ `.test.ts`)
- Create: `webui/src/lib/sse/client.ts` (+ `.test.ts`)
- Rewrite: `webui/src/lib/stores/stores.svelte.ts` (thin runes wiring over the pure logic)
- Wire `connect(...)` into `App.svelte` on mount (so the e2e connection-indicator assertion passes)
- Rebuild + recommit `webui/dist/**`

**Interfaces (PINNED — the web-views contract):**

```ts
// format/format.ts — pure, gated
export function formatDuration(ms: number | undefined): string;   // 1234 -> "1.2s"; undefined -> "—"
export function formatTokens(n: number | undefined): string;      // 12345 -> "12,345"; undefined -> "—"
export function formatCost(usd: number | undefined): string;      // 0.0123 -> "$0.0123"; undefined -> "—"
export function shortHash(h: string | undefined, n?: number): string; // sha-... -> "sha-12ab…" ; undefined -> "—"

// pricing/provenance.ts — pure, gated
export type CostProvenance = 'reported' | 'estimated' | 'unknown';
export function costProvenance(node: Pick<Node, 'attrs' | 'cost_usd'>): CostProvenance;
// reads attrs["cost_source"] === "reported"|"estimated"; if cost_usd == null -> "unknown"

// stores/selectors.ts — pure, gated (no runes)
export function upsertById<T extends { id: string }>(map: Record<string, T>, item: T): Record<string, T>;
export function removeById<T>(map: Record<string, T>, id: string): Record<string, T>;
export function sessionGraphFrom(
  state: GraphState,
  runIds: ReadonlySet<string>,
): { nodes: Node[]; edges: Edge[] };
// returns nodes/edges whose run_id ∈ runIds, deterministically sorted by id

// sse/client.ts — gated logic + thin EventSource seam
export interface ConnectOptions {
  session: string;
  token: string;
  onEvent: (ev: SseEvent) => void;
  onStatus?: (s: 'connecting' | 'open' | 'error') => void;
  factory?: (url: string) => EventSourceLike; // injection seam for tests; defaults to window.EventSource
}
export interface EventSourceLike {
  onopen: ((this: unknown, ev: unknown) => void) | null;
  onerror: ((this: unknown, ev: unknown) => void) | null;
  onmessage: ((this: unknown, ev: { data: string }) => void) | null;
  close(): void;
}
export interface Connection { close(): void; }
export function buildSubscribeURL(session: string, token: string): string;
export function connect(opts: ConnectOptions): Connection;

// stores/stores.svelte.ts — thin runes wiring, NOT gated
export const nodesById: Record<string, Node>;        // $state
export const edgesById: Record<string, Edge>;        // $state
export const sessionsById: Record<string, SessionSummary>; // $state
export const selectedNodeId: { value: string | null };     // $state wrapper
export const connectionState: { status: 'idle' | 'connecting' | 'open' | 'error' }; // $state
export function selectNode(id: string | null): void;
export function sessionGraph(hash: string): { nodes: Node[]; edges: Edge[] }; // $derived-backed read
```

> `SessionSummary` (for `sessionsById`) is pinned in `types.ts` to match `GET /v1/sessions` exactly:

```ts
export interface SessionSummary {
  session: string;
  status: string;
  started_at?: string;
  ended_at?: string;
  duration_ms?: number;
  tokens_in?: number;
  tokens_out?: number;
  cost_usd?: number;
  cost_source?: CostSource;
  node_count: number;
  tool_count: number;
  error_count: number;
  model_id?: string;
  run_ids: string[];
}
```

**Notes for implementer:** Svelte runes (`$state`/`$derived`) compile **only** in files whose name contains `.svelte` — hence `stores.svelte.ts`. ALL testable logic stays in plain `.ts` (`selectors.ts`, `sse/client.ts`, `format.ts`, `provenance.ts`) so the 100% gate runs in plain Vitest/node without the runes runtime. `stores.svelte.ts` is **thin**: it holds the `$state` maps, calls the pure helpers, and exposes `sessionGraph(hash)` via a `$derived` that delegates to `sessionGraphFrom(...)`. Keep zero branching in `.svelte.ts` (no `if`/loops beyond what runes need) so "not gated" stays honest. The `sse/client.ts` `connect` uses an injectable `factory` defaulting to `(url) => new EventSource(url)`; tests pass a fake `EventSourceLike` and drive `onopen/onmessage/onerror` synchronously — **no `EventSource` in jsdom, no real network, no `time.Sleep`-equivalent**.

- [ ] **Step 1: Write failing tests for `format.ts`, `provenance.ts`, `selectors.ts`, `sse/client.ts`** (one `.test.ts` each), covering every branch:
  - `format`: defined + `undefined`/`null` (`"—"`) for each; sub-second vs seconds vs minutes duration; thousands separators; cost rounding; hash truncation + custom `n`.
  - `provenance`: `reported`, `estimated`, missing `cost_source` with cost present (→ `estimated`? pin: missing source but `cost_usd != null` → `'estimated'`; `cost_usd == null` → `'unknown'`), `unknown` when no cost.
  - `selectors`: `upsertById` adds/replaces returning a new map; `removeById` removes/absent-noop; `sessionGraphFrom` filters by `run_ids` set and sorts deterministically; empty set → empty arrays.
  - `sse/client`: `buildSubscribeURL` encodes `session` + `token` into `/v1/subscribe?session=…&token=…`; `connect` calls `factory` with that URL, routes `onmessage` JSON → `onEvent`, ignores malformed JSON (no throw), reports `connecting`→`open`→`error` via `onStatus`, and `close()` calls the source's `close()`.

- [ ] **Step 2: Run — expect FAIL.**

```bash
cd /Users/karych/src/catacomb/webui && npm run test -- src/lib
```

- [ ] **Step 3: Implement the four pure modules** to green.

- [ ] **Step 4: Write the thin `stores.svelte.ts`** wiring `$state` + delegating to the pure helpers; expose the pinned API (incl. `connectionState`, `selectNode`, `sessionGraph`).

- [ ] **Step 5: Wire `connect(...)` into `App.svelte`** on mount via `$effect`, updating `connectionState.status` through `onStatus` and feeding `onEvent` into `applyDelta` over the runes `GraphState`. (Session/token come from the URL query for now — full routing is web-views; for the shell, default `session=''` is acceptable so the indicator still leaves `idle`.)

- [ ] **Step 6: Coverage gate green for all included logic**

```bash
cd /Users/karych/src/catacomb/webui && npm run test -- --coverage
```

Expected: PASS; `reducer.ts`, `stores/selectors.ts`, `format/**`, `pricing/**`, `sse/client.ts` all 100%. `stores.svelte.ts` and `*.svelte` excluded.

- [ ] **Step 7: Typecheck + build + e2e**

```bash
cd /Users/karych/src/catacomb/webui && npm run check && npm run build && npm run test:e2e
```

Expected: typecheck clean; the e2e connection-indicator test now passes (mount fires `connect`, indicator leaves `idle`).

- [ ] **Step 8: Go serve tests + dist drift**

```bash
cd /Users/karych/src/catacomb && go test -race -count=1 ./webui/ ./daemon/ && git status --porcelain webui/dist
```

Expected: Go PASS; `dist/` shows the rebuilt bundle (stage it).

- [ ] **Step 9: Commit**

```bash
cd /Users/karych/src/catacomb && git add -A && git commit -m "$(cat <<'EOF'
feat(webui): normalized runes stores + SSE client + format/pricing helpers (100% logic)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review

### Spec Coverage Table

| Spec Section | Requirement | Covered by |
|---|---|---|
| §5.5 / §10 Q4 | Vite + Svelte 5 under `webui/web/`, build to `webui/dist/`, `dist/` committed | e1 |
| §5.5 | `//go:embed` target `web` → `dist`; `fs.Sub`/`FileServerFS` unchanged; serve path green & 100% | e1 (Steps 15–20) |
| §4.2 / §9 | Delete OLD vanilla `web/{index.html,app.js,style.css}` (clean rebuild) | e1 (Step 8) |
| §5.5 / §9 | Capitalized `<title>` + single embedded (inline SVG) favicon | e1 (Step 8) |
| §5.5 / §10 Q4 | Vitest 100% gate scoped to pure-logic globs; components not gated | e2 (vitest.config) + f1/f2 |
| §5.5 | Playwright config + one smoke e2e (shell loads, connects) | e2 |
| §5.5 / §10 Q4 | CI job: typecheck + vitest(+gate) + playwright + committed-`dist/` drift check | e2 (ci.yml) |
| §4.2 | Frontend deps via JS toolchain only; never `go mod tidy`; not in Go module graph | Global Constraints; e1 |
| §5.5 | OKLCH tokens (`--bg/--surface/--border/--text/--accent`) + even-brightness node ramp | e3 |
| §5.5 | Inter for UI, mono ONLY for ids/hashes/code; type scale; base shell | e3 |
| §5.5 | Dark theme only (light = D) | e3 (`color-scheme: dark`, no light tokens) |
| §5.6 | Pure framework-free `reducer.ts` `applyDelta(state, ev)` for the five kinds | f1 |
| §5.6 | `node_status` field-wise, never establishes identity, always overwritten by later `node_upsert` | f1 (Steps 2.5, 2.6) |
| §5.6 | `node_upsert` rev-guarded only against other upserts | f1 (Steps 2.2, 2.3) |
| §5.6 / §4.2 | Vitest property/permutation test: same deltas any order → same state (mirrors Go) | f1 (Step 2.14) |
| §5.6 | Normalized `byId` stores: `nodesById`, `edgesById`, `selectedNodeId`; rune wiring thin | f2 |
| (plan task f2) | `sessionsById` + derived `sessionGraph(hash)`; pure helpers 100%, runes thin | f2 |
| §5.2 (consume) | `attrs["cost_source"]` provenance helper for the UI badge | f2 (pricing/provenance) |
| data contracts | `SseEvent`/`Node`/`Edge`/`SessionSummary` pinned as TS types matching backend | f1 (types.ts) + f2 (SessionSummary) |
| PIN | `connect({session, token, onEvent})` SSE client module | f2 (sse/client.ts) |
| §4.2 | `GOOS=windows go build ./...` clean; no Go comments but `//go:embed`; commit per task | e1 (Steps 19, 21–22); each task |

### Placeholder Scan

No TODO/TBD left as load-bearing. Two explicitly-flagged decision points are pinned with a recommended default and a stated alternative: (1) `src/` location (`webui/src/` vs `web/src/`) — pinned to `webui/src/`; (2) edge tombstone for delete+upsert determinism — pinned ON with a simpler fallback noted. The e2e connection-indicator test has a stated ordering caveat (enable once `connect()` is wired in f2) but the shell-loads assertion is unconditional.

### Type Consistency

- TS `Node`/`Edge` mirror `model/model.go` JSON tags exactly (incl. `parent_id?`, `agent_id?`, `parent_agent_id?`, `subagent_type?`, `payload_hash?`, `sources[]`, `tier?`, `rev`). `Payload`/`annotations` are intentionally omitted (SSE strips `payload`; annotations aren't on the A wire surface).
- `SseEvent` mirrors `daemon/sse.go`'s `sseEvent` (`kind, rev, run_id?, execution_id?, node?, edge?, old_id?, new_id?`).
- `SessionSummary` mirrors the spec's `GET /v1/sessions` field list exactly.
- The reducer's five kinds match `cdc` constants (`node_upsert/edge_upsert/node_status/node_merge/edge_delete`); `run_started/session_ended/run_ended` are explicitly no-op'd (forward-compatible).
- `webui.Handler()` signature unchanged (`func Handler() http.Handler`); only the embed target + `fs.Sub` arg change.

### Determinism / Coverage / No-Sleep Check

- Reducer determinism proven by full-permutation fold (mirrors `reduce/reduce_test.go` `permute`).
- 100% gate is per-file (`perFile: {100:true}`) on the included logic globs; an included-but-unloaded file fails at 0%, so coverage can't silently rot.
- No timers/sleeps in tests: the SSE client is driven through an injected `EventSourceLike` fake synchronously; no real `EventSource`, no network, no `time.Sleep` equivalent.
- Go side: only `webui.go` changes; `go test ./webui ./daemon` + `internal/codepolicy` + cross-compile all gate it.
