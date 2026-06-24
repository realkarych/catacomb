# B-fe1 Report: Opt-in node content viewing in the drawer

## Files changed

- `webui/web/src/lib/types.ts` — added `RedactionFinding` and `PayloadView` interfaces
- `webui/web/src/lib/api.ts` — added `ForbiddenError` class + `fetchNodePayload(hash, nodeId, token, f)` (403→ForbiddenError, 404→NotFoundError, else throw)
- `webui/web/src/lib/api.test.ts` — extended with `fetchNodePayload` + `ForbiddenError` test blocks (200/403/404/500)
- `webui/web/src/lib/payload-view.ts` — new pure module: `PayloadState` type, `prettyJSON(value)`, `payloadState(view, forbidden)` → `'disabled'|'empty'|'redacted'|'ready'`
- `webui/web/src/lib/payload-view.test.ts` — 100% coverage of both helpers across all branches
- `webui/vitest.config.ts` — added `web/src/lib/payload-view.ts` to coverage include
- `webui/web/src/components/PayloadPanel.svelte` — new component: collapsed-by-default "Reveal content" toggle; states: loading (spinner), disabled (403→"Content viewing is off…"), not-found (404), ok (Input+Output pretty JSON + copy buttons), redacted (badge + count)
- `webui/web/src/components/NodeDrawer.svelte` — added `token` prop, imported+mounted `<PayloadPanel {hash} nodeId={node.id} {token} />` between metrics and Advanced sections
- `webui/web/src/components/SessionView.svelte` — added `token` prop, threaded to `<NodeDrawer {hash} {token} />`
- `webui/web/src/App.svelte` — passed `{token}` to both `<SessionView>` usages
- `webui/e2e/payload.spec.ts` — new Playwright spec (3 tests)
- `webui/dist/**` — rebuilt

## States handled

| State | Trigger | UI |
|---|---|---|
| collapsed (default) | initial | "Reveal content" button only; zero fetches |
| loading | after reveal click | quiet spinner with sr-only text |
| disabled | 403 ForbiddenError | "Content viewing is off. Start the daemon with `--allow-payload-access` to enable." |
| not-found | 404 NotFoundError | "No stored payload for this node." |
| error | other error | "Failed to load content." (dim error color) |
| ok | 200, no redactions | Input + Output sections, pretty JSON, Copy buttons each |
| redacted | 200, redacted=true | same as ok + "redacted" badge + "N secret(s) redacted" count |

## Gated coverage

- `npm run test`: 219 tests, **100% statements/branches/functions/lines**
- `make cover`: **100% Go coverage** (Go untouched — passes unchanged)
- `npm run typecheck`: 0 errors, 0 warnings
- `npm run build`: clean build, 286 kB JS bundle
- `git diff --exit-code webui/dist`: no drift (dist rebuilt and staged)

## e2e (Playwright)

`webui/e2e/payload.spec.ts` — 3 new tests, all pass (28/28 total pass):
1. `content panel: collapsed by default, no fetch until reveal` — asserts no request to `/nodes/**/payload` before toggle
2. `content panel: 200 redacted payload shows input/output + redacted badge + copy` — mocked 200 redacted PayloadView; asserts badge, count, 2 sections, copy buttons
3. `content panel: 403 shows disabled message with --allow-payload-access` — mocked 403; asserts message text, zero payload sections

## LIVE screenshots

Saved relative to repo root (`.playwright-mcp/` screenshots directory):

- `bfe1-with-flag-collapsed.png` — Bash node selected, drawer open, "Reveal content" button visible (daemon with `--allow-payload-access`, default collapsed)
- `bfe1-with-flag-revealed.png` — after reveal: Input `{"command":"ls"}`, Output `"a.txt\nb.txt"`, Copy buttons, no redacted badge (testdata has no secrets)
- `bfe1-without-flag-disabled-msg.png` — after reveal on default daemon (no flag): "Content viewing is off. Start the daemon with `--allow-payload-access` to enable."

Live verification used: `bin/catacomb` built from HEAD, replayed `cmd/catacomb/testdata/session.jsonl` via transcript tail, navigated to Bash tool node in the SPA.
