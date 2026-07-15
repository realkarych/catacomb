# PV-3: Deletion Wave I — Viewers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the display layer per ADR-0026 §3: `webui` (Go embed + Svelte app + e2e), `tui`, and the `observe`/`ui`/`watch` commands. Humans watch runs in the vendor substrate (Phoenix) from now on. `v0-platform-final` is already tagged on the pre-PV-3 master.

**Architecture:** Pure deletion plus seam cleanup. Only two production import points exist (verified): `cmd/catacomb/observe.go` → `tui`, and `daemon/server.go:26,73` → `webui.Handler()` mounted at `GET /`. The daemon keeps every API surface (hooks, stream-json, SSE, gRPC, sessions, mark) until PV-4 — this wave removes only the human-facing HTML surface and its toolchain.

**Tech Stack:** deletions only; no new dependencies.

## Global Constraints

- No comments in Go; 100% file/package/total coverage must HOLD after deletion (`make cover`); TDD where behavior changes (the `GET /` seam); golangci-lint clean after removing its `tui/client\.go` exclusion (.golangci.yml:60).
- Every intermediate commit builds and passes the full suite (deletion waves must never leave master broken mid-sequence).
- CI markdownlint runs over ALL md files — every doc reference to deleted surfaces must go in the same PR.
- Facts (verified): root registrations at cmd/catacomb/root.go:55-58 (wrapped in an `observe(...)` helper — inspect and remove/keep it based on remaining uses); publish.yml has no npm/webui steps; dependabot has no npm ecosystem; .dockerignore:12-13 lists webui artifacts; Makefile:62-70 has `WEB := webui` targets; CI frontend job at .github/workflows/ci.yml:66-98.

---

### Task 1: delete the TUI and its commands

**Files:** delete `tui/` (whole package), `cmd/catacomb/observe.go`, `observe_test.go`, `ui.go`, `ui_test.go`, `watch.go`, `watch_test.go`; modify `cmd/catacomb/root.go` (drop the three AddCommand lines :55-58 and the `observe(...)` wrapper if it has no remaining uses); modify `.golangci.yml` (drop the `tui/client\.go` exclusion).

**Contract:** `catacomb observe|ui|watch` are gone from `--help`; a root-command test asserting the remaining command set (or the absence of the three) is updated/added; full suite + `make cover` (100%) + lint green. Note: `newUICmd`/`newWatchCmd` may share helpers with surviving commands (e.g. browser-open, daemon client wiring) — move any shared helper still used elsewhere rather than deleting it; report what moved.

---

### Task 2: delete the web UI and its toolchain

**Files:** delete `webui/` entirely (webui.go + webui_test.go + web/ + e2e/ + dist/ + package.json etc.); modify `daemon/server.go` (drop import :26 and the `mux.Handle("GET /", webui.Handler())` route :73 — TDD: adjust/extend server tests so `GET /` now returns 404 and the API routes still serve); modify `Makefile` (drop `WEB` targets :62-70 and any aggregate target referencing them); modify `.github/workflows/ci.yml` (drop the `frontend` job :66-98); modify `.dockerignore` (:12-13).

**Contract:** `go build ./... && go test ./...` green with webui gone; daemon server tests cover the 404 seam; `make cover` 100%; lint clean; `git grep -l webui` over tracked files returns only docs (fixed in Task 3) and historical plan/review/ADR docs (which are immutable history — leave them).

---

### Task 3: docs scrub + gates

**Files:** delete `docs/guide/ui.md`; modify `docs/guide/README.md` (index), `getting-started.md`, `configuration.md`, `cli.md`, `workflows.md`, `README.md` (root) — remove/replace every instruction referencing `observe`/`ui`/`watch`/web UI; add one pointer in `workflows.md`'s daemonless section: watching runs live is delegated to the vendor substrate (Phoenix plugin), per ADR-0026 §2.

**Contract:** documented commands = actual commands (verify against `catacomb --help` output); `npx --yes markdownlint-cli@0.49.0 'docs/**/*.md' '*.md' --config .markdownlint.json` clean; full gates (`go build ./... && go test ./...`, `make cover`, `golangci-lint run --timeout=5m`) green. Immutable history docs (docs/adr/, docs/reviews/, docs/plans/, docs/specs/, docs/superpowers/) are NOT scrubbed.

---

## Self-review checklist

- No production Go file references `tui` or `webui` after Tasks 1–2 (`git grep` proof in reports).
- Coverage stays 100% — deleting covered code cannot drop it, but the `GET /` seam change and root.go edits need their branches covered.
- README badges/screenshots referencing the web UI removed (check README for asset links).
