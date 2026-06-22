# M5 — Embedded web UI design

**Status:** approved (autonomous run, mandate-delegated)
**Date:** 2026-06-22
**Milestone:** M5 (spec §9 "Embedded web UI", §18 "M5 — Web UI. Four views over the live feed.") — the FINAL roadmap milestone.
**Consumes:** the M3a SSE feed (`GET /v1/subscribe`), the daemon HTTP mux + `authed` wrapper, the discovery file (addr + token).

## 1. Goal

Ship a zero-install web UI, embedded in the `catacomb` binary via `go:embed` and
served by the daemon, that renders the live execution graph from the SSE feed.
Four views over one in-browser graph model fed by the snapshot-then-delta stream:

1. **Live graph / DAG** — nodes + edges, colored by type/status, updating live.
2. **Timeline / waterfall** — per-run swim-lanes with durations.
3. **Node inspector** — click a node → status, timing, tokens, cost, attrs,
   contributing sources.
4. **Run list / compare** — browse the run forest; filter; select a run to focus.

## 2. Scope & verification boundary

The **Go side** (asset embedding + serving + the browser-token SSE auth + the
`catacomb ui` command) is held to the full project bar: 100% line coverage under
`-race`, lint-clean, no Go comments, cross-platform. The **frontend** (HTML / CSS
/ vanilla JS + SVG, no third-party library) is static embedded data — it is NOT
Go, so it is outside the Go coverage/comment gates. The agent cannot visually verify a
rendered browser UI, so the frontend is **best-effort and operator-verified**
(same deferral class as Step 7). The Go serve path is smoke-tested (GET `/`
returns the embedded `index.html`; assets resolve; the SSE feed is reachable with
a query-param token).

## 3. Architecture

```
browser ──GET /?token=T──▶ daemon (open static handler) ──serves embed.FS web/──▶ index.html + app.js + style.css
   │                                                                                        │
   └──EventSource GET /v1/subscribe?token=T ──▶ daemon (token-gated) ──snapshot+delta SSE──▶ app.js applies deltas → in-memory graph → renders active view
```

- **Static assets are served openly** (no auth): the HTML/JS/CSS are not secrets;
  only the graph DATA (behind the SSE feed) is gated. This avoids a
  chicken-and-egg auth bootstrap (the page must load before it can present a
  token).
- **The token reaches the browser via the URL** (`/?token=T`), the standard
  local-dashboard pattern (Jupyter-style). `app.js` reads it from
  `window.location` and uses it as the `?token=` on the EventSource URL. The
  token-in-URL/server-log exposure is the documented, accepted cost on a
  loopback, same-user trust boundary (ADR-0013); the `catacomb ui` command (not a
  shared link) is the intended way to obtain the URL.

## 4. Go side

### 4.1 Browser-token SSE auth (the M3a-deferred item)

`EventSource` cannot set the `Authorization` header, so the SSE route must also
accept the token via a `token` query parameter. Add an auth helper that accepts
**either** the `Authorization: Bearer <t>` header **or** `?token=<t>`, both
constant-time-compared (`crypto/subtle`):

```go
func (d *Daemon) authedAllowQuery(token string, next http.HandlerFunc) http.HandlerFunc
```

Gate `GET /v1/subscribe` with `authedAllowQuery` (replacing the header-only
`authed`). The header path stays valid for programmatic clients (`catacomb watch`,
curl); the query path enables the browser. The other gated routes
(`/hook`,`/v1/traces`,`/v1/stream-json`) keep header-only `authed` (no browser
need). Document the query-param logging caveat.

### 4.2 Asset embedding + serving

- A `webui` package (or `daemon` sub-file) with `//go:embed web` → `embed.FS`
  holding `web/index.html`, `web/app.js`, `web/style.css` (no third-party
  library — see §5; the renderer is vanilla JS + SVG, so there is no vendored
  blob to embed or source).
- A handler serving the FS: `GET /` → `index.html`; `GET /ui/...` (or serve the
  embed root) → the asset by path, correct `Content-Type` (`http.FileServerFS`
  or `http.ServeFileFS` over a sub-FS). Open (no auth). A missing asset → 404.
- Register in `daemon.Handler`: `mux.Handle("GET /", <static handler>)` (the
  catch-all root) + the asset paths. Keep `/healthz`,`/metrics`, and the gated
  API routes ahead of the catch-all as needed (ServeMux longest-pattern wins, so
  explicit routes beat `/`).

### 4.3 `catacomb ui` command

`catacomb ui` reads the discovery file (addr + token), composes
`http://<addr>/?token=<token>`, and (a) prints it and (b) opens the default
browser. Seam the browser-opener (`var openBrowser = func(url) error {…}` using
`xdg-open`/`open`/`rundll32` per GOOS — but kept behind the seam so tests inject a
fake; the real opener is the only OS-specific bit and must stay cross-platform
build-clean) and the output writer, for 100% coverage without launching a browser.
Flags: `--no-open` (print only). Errors (missing discovery) surface clearly.

## 5. Frontend (best-effort, operator-verified)

A single `index.html` shell + `app.js` + `style.css`, **vanilla JS + SVG, no
external library and no build step** (files are embedded as-is). Avoiding a
vendored graph library keeps the binary self-contained, the repo blob-free, and
the whole UI agent-authored + offline. (The foundational spec's "e.g. Cytoscape"
is a suggestion; a hand-rolled SVG renderer satisfies the live-graph view.)

- **SSE client:** `new EventSource('/v1/subscribe?token='+token)`; each `data:`
  line is a `GraphDelta` JSON (kind/rev/run_id/node/edge…). Apply to an in-memory
  `Map` of nodes + edges (rev-guarded: ignore a delta whose rev ≤ the stored
  rev), handling `node_upsert`/`node_status`/`node_merge`/`edge_upsert`/
  `edge_delete`/run-lifecycle. Auto-reconnect (EventSource does this; on reconnect
  the server resends a fresh snapshot).
- **Views (tabs):**
  1. **Graph:** an SVG renderer with a simple deterministic layered layout
     (BFS-depth columns from each run's session root; siblings stacked), node
     color by `type`, border/shape by `status`; live add/update/remove.
  2. **Timeline:** rows per run; bars from `t_start`→`t_end` (or running);
     simple CSS/SVG.
  3. **Inspector:** on node tap, a side panel with id/type/name/status/timing/
     tokens/cost/attrs/sources.
  4. **Runs:** a list of runs (derived from nodes' `run_id` + run-lifecycle
     deltas) with status + a filter box; selecting a run filters the graph.
- No external network, no third-party JS; works offline on loopback.

## 6. Decomposition

Single plan, executed on one branch (`feat/m5-webui`), 4 tasks:

1. **Browser-token SSE auth** (`authedAllowQuery`, gate `/v1/subscribe`) — Go,
   TDD, 100%.
2. **Embed + serve** (`//go:embed web`, static handler, route registration; ships
   a minimal real `index.html` so the serve path is testable end-to-end) — Go,
   TDD, 100%.
3. **`catacomb ui` command** (discovery → URL → open/print, seamed) — Go, TDD,
   100%.
4. **Frontend SPA** (`web/index.html` + `web/app.js` + `web/style.css`, the 4
   views, vanilla JS + SVG) — best-effort, operator-verified; a Go smoke test
   asserts `GET /` serves the real `index.html` and the asset routes resolve.

## 7. Constraints (inherited, binding — Go side)

Go 1.26 pure-Go (no cgo); **NO Go comments except `//go:build|//go:embed|
//go:generate`** (the `//go:embed` directive is explicitly allowed); **100% line
coverage under `-race`** for the Go code (the embedded frontend files are data,
not statements — they do not affect Go coverage; the serve handler + auth + cmd
ARE covered); golangci-lint v2 clean (gofumpt, goimports, govet shadow, forbidigo
bans `time.Sleep`, unparam, errcheck, bodyclose); **never `go mod tidy`** (M5 adds
NO Go deps and no third-party JS — the renderer is hand-rolled vanilla JS + SVG);
cross-platform
(`GOOS=windows go build ./...` clean — the browser-opener seam's OS branch must
build on all platforms; tests inject a fake opener); loopback + bearer trust
boundary preserved (static open, data gated, token via URL documented); commit per
task; never commit to master mid-plan.

## 8. Testing strategy (M5-specific, Go side)

- **Auth:** `authedAllowQuery` — header valid → pass; `?token=` valid → pass;
  both absent/wrong → 401; constant-time compare. The SSE route reachable with a
  query-param token (extend an SSE e2e test).
- **Serve:** `GET /` → 200 + the embedded `index.html` body + `text/html`
  content-type; an asset path → 200 + correct content-type; a missing asset →
  404; the static handler does not shadow `/healthz`/`/metrics`/the API routes.
- **`catacomb ui`:** reads a written discovery file → composes the right URL →
  calls the (faked) opener + prints; `--no-open` skips the opener; missing
  discovery → error. Browser-opener OS seam: the real function builds on all
  GOOS (a construct/var test), the logic is covered via the fake.
- **Frontend smoke:** `GET /` returns the real `index.html` containing the app
  mount point; `web/app.js` + `web/style.css` resolve with sane content-types.
- All Go tests `-race`; host + windows build.

## 9. Deferred → post-M5 / operator

- **Visual/interaction verification of the 4 views** — operator (the agent cannot
  drive a browser); same class as Step 7.
- **WebSocket transport** (M3c) — SSE suffices for the UI.
- **Run diff/compare** beyond single-run focus (the "compare two runs" depth) — a
  v2 enhancement; v1 ships run list + filter + focus.
- **Auth hardening** for the token-in-URL exposure (e.g. a one-time token
  exchanged for a cookie) — documented; loopback same-user makes it low-risk now.
- **Payload rendering** in the inspector (payloads are omitted from the wire,
  ADR-0020; only `payload_hash` shows).
