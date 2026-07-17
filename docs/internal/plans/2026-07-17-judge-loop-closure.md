# Judge-loop closure — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** make a `pack` audit bundle's returned findings feed BOTH the `regress --scores` gate AND the `catacomb-judge` agreement/panel utilities, and document the full pack → external judge → calibrate/panel → regress loop as one end-to-end recipe. No new command, no judge runner (ADR-0027 non-goal), no ADR — this closes an existing seam within ADR-0027's sanctioned surface.

**The gap:** `catacomb-judge panel`/`agreement` require a `tool` provenance field (plus optional `tool_version`/`prompt_hash`) on each score line — `integrations/judge` skips lines missing `tool`. But `cmd/catacomb/pack.go`'s `INSTRUCTIONS.md` return contract tells the reviewer to emit only `{"key","value","run_id"}`, so a pack's findings can gate directly (`regress --scores` ignores extra fields) but cannot feed the panel/agreement calibration. Adding provenance to the pack contract is purely additive: `regress --scores` ignores `tool`/`tool_version`/`prompt_hash`; `catacomb-judge` consumes them.

**Tech stack:** Go stdlib (one embedded-string edit + its test); Markdown docs. No new deps, no gate-path contact.

## Global constraints

- **No comments in Go code**; TDD; 100% coverage (`make cover`); gofumpt; `make lint`; testify.
- **Additive only**: the `regress --scores` path and `catacomb-judge` behavior are unchanged; the pack INSTRUCTIONS gains provenance guidance; docs stitch existing sections. Do NOT modify scores parsing, the judge utilities, or the gate.
- Cross-check every claim against `integrations/judge/README.md` (the exact provenance fields: `tool`, `tool_version`, `prompt_hash`) and `cmd/catacomb/scores.go` (regress ignores unknown fields).
- Commit after every green task; branch `feat/judge-loop-closure` (based on this plan-doc branch).

---

### Task 1: pack INSTRUCTIONS — provenance-complete return contract

**Files:**

- Modify: `cmd/catacomb/pack.go` (the `INSTRUCTIONS.md` embedded template's "Returning findings" section)
- Modify: `cmd/catacomb/pack_test.go` (`TestPackManifestAndInstructions` — assert the new provenance guidance)

**Change:** extend the "Returning findings" section so the return contract instructs the reviewer to stamp each JSONL line with a `tool` provenance field (the judge identity — e.g. the model/prompt name), and optionally `tool_version` and `prompt_hash`, IN ADDITION to `key`/`value`/`run_id`. Explain the dual consumer in one or two sentences: the same file gates directly through `catacomb regress --scores`, and — because it carries `tool` provenance — also feeds `catacomb-judge agreement`/`panel` for judge calibration before it is trusted to gate (link/name `integrations/judge`). Update the example line to include `tool` (and optionally `tool_version`/`prompt_hash`), e.g.:

```json
{"key":"audit.clean","value":1,"run_id":"<run id>","tool":"<judge name>","tool_version":"<v>"}
```

Keep the existing `regress --scores` gate example. The prose must stay accurate: `tool` is what `catacomb-judge` uses as the judge identity; without it, panel/agreement skip the line.

- [ ] **Step 1: failing test** — extend `TestPackManifestAndInstructions` to assert the INSTRUCTIONS content now contains the provenance guidance (the string `tool`, the dual-consumer mention of `catacomb-judge` or `agreement`/`panel`, and the example line's `"tool"` key). RED against current template.
- [ ] **Step 2–4:** RED → edit the template string → GREEN. Verify no OTHER pack test pins the old exact INSTRUCTIONS bytes (grep; if a golden-exact assert exists, update it).
- [ ] **Step 5:** `make fmt && make cover && make lint` (100%). Confirm the gate path is untouched: `git diff master..HEAD -- regress/ aggregate/ integrations/` is empty. Commit `feat(pack): provenance-complete findings contract so audit packs feed catacomb-judge`.

### Task 2: end-to-end judge-loop docs

**Files:**

- Modify: `docs/guide/workflows.md` (the "Auditing cells" and "Calibrating a judge" sections)
- Modify (if it references the pack contract): `docs/guide/cli.md` (the `pack` section — note findings now carry `tool` provenance for judge reuse)

**Change:** stitch the two sections into one coherent loop with a single worked example that flows pack → external judge (stamping `tool`) → `catacomb-judge agreement --min-kappa` (calibrate) or `panel` (aggregate) → `regress --scores` (gate). Concretely:

- In "Auditing cells" step 3/4, show the reviewer emitting `tool`-stamped lines (matching the new INSTRUCTIONS), and add a sentence that because the findings carry provenance, the SAME file can first be calibrated/aggregated via `catacomb-judge` before gating — cross-link forward to "Calibrating a judge".
- In "Calibrating a judge", add an opening sentence tying it back: the scores it calibrates are exactly the provenance-stamped findings a `pack` audit (or any external judge) produces, so the loop is: pack the flagged cells → judge them (stamp `tool`) → calibrate the judge against a gold set (`agreement --min-kappa`) and/or aggregate a panel → gate the calibrated scores with `regress --scores`. Keep the existing κ>0.8 and panel material.
- Pitch framing (one sentence, honest): catacomb never runs a judge — it defines the provenance contract, calibrates any judge against measured human agreement before that judge may gate, and gates deterministically on the result, with zero data egress beyond the pack the operator ships. No competitor gates a judge on measured agreement first.

Every command cross-checked against the shipped CLI/`catacomb-judge` behavior. markdownlint + relative-link/anchor check clean. Do NOT invent flags — verify `agreement`/`panel`/`--min-kappa`/`--labels`/`--runs-dir` against `integrations/judge/README.md`.

- [ ] Implement; commit `docs: end-to-end judge loop (pack -> judge -> calibrate/panel -> scores gate)`.

### Task 3: final review + PR

- [ ] Final whole-branch review (named risks: the provenance addition is truly additive and doesn't break `regress --scores`; the docs commands all exist; no judge runner introduced; no ADR-0027 non-goal violated); fix wave if needed.
- [ ] PR `feat: judge-loop closure — provenance-complete pack contract + end-to-end docs`. Base: master (independent; no ADR branch needed — this is within ADR-0027).

## Deliberately out of scope

- Any judge runner / LLM invocation inside catacomb (ADR-0027 non-goal — permanent).
- Changing the scores schema, the gate, or the `catacomb-judge` utilities.
- A reference Python judge runner in `integrations/` (ADR-0027 rejected it; the `claude -p` one-liner is the reference invocation).
