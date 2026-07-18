#!/usr/bin/env bash
# Scenario 90 — analysis-command coverage the round-2 audit flagged as having NO
# per-PR-gate coverage: these paths ran only in the LIVE (API) suite or as smoke.
# Every step here is offline/deterministic (rendered fixture transcripts + one tiny
# bench), zero API spend. Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_*
# exported; catacomb is on PATH.
#
#   (1) diff --json + scoping. Two transcripts differ only in B: one extra step INSIDE
#       the `core` phase (StepExtraInCore @10:00:04) and one OUTSIDE it, after core end
#       (StepExtraOutside @10:00:07). Unscoped `diff A B --json` must report BOTH extras
#       under `added`; `diff A B --phase core` must scope both sides to the core window
#       [core.start, core.end) and report ONLY the in-phase extra — the out-of-phase step
#       drops out, so the phase flag demonstrably NARROWS the diff. Non-vacuity: the same
#       binary on identical input (`diff A A`) reports zero added/removed/changed, so the
#       delta is real, not an artifact of the diff always emitting something.
#
#   (2) subgraph --from/--to (range mode). The audit found only `subgraph --phase` was
#       ever driven. Over a fixture with markers alpha(@01)/beta(@03)/gamma(@05) and a
#       step after each, `subgraph --from alpha --to gamma --json` scopes to the half-open
#       window [alpha.start, gamma.start): the in-range steps RangeStepA/RangeStepB are
#       present and the post-gamma RangeStepC is absent. Non-vacuity/narrowing: the FULL
#       graph (the step-3 export snapshot of the same session) carries RangeStepC and more
#       nodes, so the range subgraph is a strict, non-empty subset of the whole.
#
#   (3) standalone export. `catacomb export <transcript> --to jsonl --out <file>` is the
#       export command path, distinct from `replay --export-jsonl`. Over the range fixture
#       it must emit a JSONL graph snapshot whose step nodes carry `step_key` and typed
#       `tool_call` nodes (RangeStepA/B/C), plus the `run` record — proving the export
#       carries the graph contract, not just node ids.
#
#   (4) standalone verify over production-shaped evidence. The `verify` command was only
#       re-run over the sql/ws/import baskets. A tiny 2-variant x 2-rep basket with a
#       verify hook (verify_token.py scores out/result.csv into verifier.pass, the
#       composite scenario's artifact shape) is benched, then
#       `catacomb verify <basket> --runs-dir <runs>` runs as a SEPARATE offline
#       re-verification step: it must re-score every cell (mode flips bench -> offline,
#       scores byte-identical, verifier.pass present) — the verify path exercised over the
#       newer artifact/annotation evidence shape. `--label variant=baseline` then narrows
#       the re-verify to only the baseline cells (2/4 "ok" lines vs. 4/4 unfiltered),
#       mirroring the live sql basket's 15-total/5-filtered `--label` coverage.
set -euo pipefail
w="$WORK/analysis"; rm -rf "$w"; mkdir -p "$w"

echo "== prod.90 diff: --json reports the real delta; --phase core narrows it =="
sed 's/__SESSION_ID__/prod90-diff-a/g' "$PROD/fixtures/90-diff-a.jsonl.tmpl" > "$w/a.jsonl"
sed 's/__SESSION_ID__/prod90-diff-b/g' "$PROD/fixtures/90-diff-b.jsonl.tmpl" > "$w/b.jsonl"
run_json 0 "$w/diff-unscoped.json" "diff A B --json (unscoped)" -- \
  catacomb diff "$w/a.jsonl" "$w/b.jsonl" --json
run_json 0 "$w/diff-scoped.json" "diff A B --phase core --json (scoped)" -- \
  catacomb diff "$w/a.jsonl" "$w/b.jsonl" --phase core --json
run_json 0 "$w/diff-ident.json" "diff A A --json (identity, non-vacuity)" -- \
  catacomb diff "$w/a.jsonl" "$w/a.jsonl" --json
rc=0; python3 - "$w/diff-unscoped.json" "$w/diff-scoped.json" "$w/diff-ident.json" <<'PY' || rc=$?
import json, sys
uns = json.load(open(sys.argv[1]))
sco = json.load(open(sys.argv[2]))
idn = json.load(open(sys.argv[3]))
errs = []
uns_added = sorted(s["tool"] for s in uns["added"])
if uns_added != ["StepExtraInCore", "StepExtraOutside"]:
    errs.append(f"unscoped added tools {uns_added} want both extras")
if [s["tool"] for s in uns["removed"]] or [c["tool"] for c in uns["changed"]]:
    errs.append("unscoped removed/changed not empty (steps should align)")
if sorted(m["tool"] for m in uns["unchanged"]) != ["StepOne", "StepTail", "StepTwo"]:
    errs.append(f"unscoped unchanged {sorted(m['tool'] for m in uns['unchanged'])}")
sco_added = sorted(s["tool"] for s in sco["added"])
if sco_added != ["StepExtraInCore"]:
    errs.append(f"scoped added tools {sco_added} want only the in-phase extra")
if "StepExtraOutside" in sco_added:
    errs.append("phase scoping did NOT narrow: out-of-phase extra still in added")
if "StepTail" in [m["tool"] for m in sco["unchanged"]]:
    errs.append("scoped unchanged still holds out-of-phase StepTail")
for k in ("added", "removed", "changed"):
    if idn[k]:
        errs.append(f"identity diff non-empty {k}={idn[k]!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("diff: unscoped added=[InCore,Outside]; --phase core narrows to added=[InCore]; A-vs-A empty")
PY
record "$rc" "diff --json reports both extras; --phase core narrows added to the in-phase step; A-vs-A empty"

echo "== prod.90 diff: asymmetric per-side scoping (--a-phase/--b-from/--b-to) =="
# --b-from X --b-to X (same checkpoint on both range endpoints) collapses side B to a
# ZERO-WIDTH window: RangeWindow scopes [from.start, to.start) (subgraph/spec.go's
# RangeWindow: `end := to.Start`, never `to.End`), so identical from/to selectors always
# yield an empty side, regardless of --phase core's real (non-empty) duration. This is
# the SAME catacomb invocation + SAME python assertion as e2e/run.sh's live diff smoke
# (step g2) — the $0 mirror for that live-leg proof.
run_json 0 "$w/diff-asym.json" "diff A B --a-phase core --b-from core --b-to core --json (asymmetric per-side scoping)" -- \
  catacomb diff "$w/a.jsonl" "$w/b.jsonl" --a-phase core --b-from core --b-to core --json
rc=0; python3 - "$w/diff-scoped.json" "$w/diff-asym.json" <<'PY' || rc=$?
import json, sys
sym = json.load(open(sys.argv[1]))
asym = json.load(open(sys.argv[2]))
errs = []
if asym["added"] or asym["changed"] or asym["unchanged"]:
    errs.append("asymmetric diff (side B force-emptied by --b-from/--b-to core) should have no "
                f"added/changed/unchanged: added={len(asym['added'])} changed={len(asym['changed'])} "
                f"unchanged={len(asym['unchanged'])}")
sym_a_total = len(sym["removed"]) + len(sym["changed"]) + len(sym["unchanged"])
if len(asym["removed"]) != sym_a_total:
    errs.append(f"asymmetric removed={len(asym['removed'])} want {sym_a_total} (every side-A item the "
                "symmetric --phase core diff scoped; --b-from/--b-to core collapses side B to the empty "
                "zero-width range, so nothing can match)")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"asymmetric --a-phase core --b-from/--b-to core: side A matches the symmetric --phase core scoping "
      f"({sym_a_total} items), side B collapses to empty -> all {len(asym['removed'])} unmatched")
PY
record "$rc" "diff --a-phase/--b-from/--b-to: side A matches symmetric --phase scoping, side B correctly collapses to the empty range"

echo "== prod.90 export: standalone export emits a step_key'd, typed graph snapshot =="
sed 's/__SESSION_ID__/prod90-range/g' "$PROD/fixtures/90-range.jsonl.tmpl" > "$w/range.jsonl"
run_json 0 "$w/export.out" "export range fixture --to jsonl --out" -- \
  catacomb export "$w/range.jsonl" --to jsonl --out "$w/export.jsonl"
rc=0; python3 - "$w/export.jsonl" <<'PY' || rc=$?
import json, sys
kinds = {}
nodes = []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    o = json.loads(line)
    kinds[o["kind"]] = kinds.get(o["kind"], 0) + 1
    if o["kind"] == "node":
        nodes.append(o)
errs = []
if kinds.get("run", 0) != 1:
    errs.append(f"want exactly 1 run record, got {kinds.get('run', 0)}")
if kinds.get("edge", 0) < 1:
    errs.append("no edge records in the snapshot")
tools = sorted(n.get("name", "") for n in nodes if n.get("type") == "tool_call")
if tools != ["RangeStepA", "RangeStepB", "RangeStepC"]:
    errs.append(f"tool_call node names {tools} want all three RangeStep*")
keyed = {n.get("name", "") for n in nodes if n.get("type") == "tool_call" and n.get("step_key")}
if keyed != {"RangeStepA", "RangeStepB", "RangeStepC"}:
    errs.append(f"tool_call nodes carrying step_key {sorted(keyed)} want all three")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"export: {len(nodes)} node lines, kinds={kinds}, all 3 tool_call nodes typed and step_key'd")
PY
record "$rc" "export --to jsonl carries typed tool_call nodes + step_key + run record"

echo "== prod.90 subgraph: --from/--to range mode is the in-range subset, narrower than full =="
run_json 0 "$w/sub-range.json" "subgraph range --from alpha --to gamma --json" -- \
  catacomb subgraph "$w/range.jsonl" --from alpha --to gamma --json
rc=0; python3 - "$w/sub-range.json" "$w/export.jsonl" <<'PY' || rc=$?
import json, sys
sub = json.load(open(sys.argv[1]))["nodes"]
full = [json.loads(l) for l in open(sys.argv[2]) if l.strip() and json.loads(l)["kind"] == "node"]
sub_tools = sorted(n.get("name", "") for n in sub if n.get("type") == "tool_call")
full_tools = sorted(n.get("name", "") for n in full if n.get("type") == "tool_call")
errs = []
if sub_tools != ["RangeStepA", "RangeStepB"]:
    errs.append(f"range tool_call names {sub_tools} want [RangeStepA, RangeStepB]")
if "RangeStepC" in sub_tools:
    errs.append("post-gamma RangeStepC leaked into the range subgraph")
if "RangeStepC" not in full_tools:
    errs.append("non-vacuity broken: RangeStepC absent from the FULL export too")
if not (0 < len(sub) < len(full)):
    errs.append(f"range subgraph not a strict non-empty subset: {len(sub)} vs full {len(full)}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"subgraph range: {len(sub)} nodes (RangeStepA+B, no C) < full {len(full)} nodes (has C)")
PY
record "$rc" "subgraph --from/--to returns the in-range subset (RangeStepA+B, no C), narrower than full"

echo "== prod.90 subgraph: --from X --to X (same checkpoint) is a well-formed EMPTY range, narrower than --phase X =="
# Same RangeWindow collapse as the diff mirror above: --from gamma --to gamma resolves
# BOTH endpoints to gamma's own start (subgraph/spec.go's RangeWindow uses `to.Start`,
# never `to.End`), producing a zero-width, empty window — NOT the same window as
# `--phase gamma` (which spans gamma's real start-to-end duration). gamma carries an
# explicit end mark in the range fixture (unlike alpha/beta, which stay open to the
# session's own end), so its phase window is a clean, non-degenerate contrast. This is
# the SAME catacomb invocation + SAME python assertion as e2e/run.sh's live subgraph
# smoke (step g2).
run_json 0 "$w/sub-degenerate.json" "subgraph range --from gamma --to gamma --json (zero-width range)" -- \
  catacomb subgraph "$w/range.jsonl" --from gamma --to gamma --json
run_json 0 "$w/sub-phase-gamma.json" "subgraph --phase gamma --json (reference window)" -- \
  catacomb subgraph "$w/range.jsonl" --phase gamma --json
rc=0; python3 - "$w/sub-degenerate.json" "$w/sub-phase-gamma.json" <<'PY' || rc=$?
import json, sys
deg = json.load(open(sys.argv[1])).get("nodes") or []
phase = json.load(open(sys.argv[2])).get("nodes") or []
errs = []
if len(deg) != 0:
    errs.append(f"--from gamma --to gamma returned {len(deg)} nodes, want 0 (zero-width range: from.start == to.start)")
if len(phase) == 0:
    errs.append("--phase gamma returned 0 nodes on the same fixture (should be non-empty)")
if not (len(deg) < len(phase)):
    errs.append(f"--from/--to gamma ({len(deg)}) not strictly narrower than --phase gamma ({len(phase)})")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"subgraph --from gamma --to gamma: {len(deg)} nodes (zero-width), strictly narrower than --phase gamma's {len(phase)} nodes")
PY
record "$rc" "subgraph --from/--to (same checkpoint) is well-formed and empty, strictly narrower than --phase"

echo "== prod.90 verify: bench a verify-hook basket, then standalone offline re-verify =="
v="$w/verify"; mkdir -p "$v/cellwork" "$v/runs"
export PYTHONPATH="$REPO/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$v|g" "$PROD/fixtures/90-verify.basket.yaml.tmpl" > "$v/basket.yaml"
HERMETIC_PROJECTS="$v/projects" run_json 0 "$v/bench.out" "bench prod-analysis-verify basket (verify hook)" -- \
  catacomb bench "$v/basket.yaml" --projects-dir "$v/projects" --runs-dir "$v/runs" --manifest "$v/m.jsonl"
rid="bench-prod-analysis-verify-tok-baseline-r1"
rc=0; python3 - "$v/runs/$rid" <<'PY' || rc=$?
import json, sys
d = sys.argv[1]
errs = []
vj = json.load(open(d + "/verify.json"))
if vj.get("mode") != "bench":
    errs.append(f"bench-time verify.json mode={vj.get('mode')!r} want 'bench'")
if vj.get("error"):
    errs.append(f"bench-time verify.json error non-empty: {vj.get('error')!r}")
sc = [json.loads(x) for x in open(d + "/scores.jsonl") if x.strip()]
hit = [s for s in sc if s.get("key") == "verifier.pass" and s.get("value") == 1]
if not hit:
    errs.append(f"scores.jsonl lacks a passing verifier.pass: {sc!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("bench-time: verify.json mode=bench, scores verifier.pass=1 (verify_token)")
PY
record "$rc" "bench-time evidence: verify.json mode=bench + scores verifier.pass=1"
snap="$v/snap"; mkdir -p "$snap"
for d in "$v/runs"/*/; do cp "$d/scores.jsonl" "$snap/$(basename "$d").scores"; done
run_json 0 "$v/verify.out" "standalone catacomb verify (offline re-verify)" -- \
  catacomb verify "$v/basket.yaml" --runs-dir "$v/runs"
rc=0
for d in "$v/runs"/*/; do cmp -s "$d/scores.jsonl" "$snap/$(basename "$d").scores" || rc=1; done
record "$rc" "offline verify leaves every scores.jsonl byte-identical (idempotent re-score)"
rc=0; python3 - "$v/runs/$rid" "$v/verify.out" <<'PY' || rc=$?
import json, sys
d, out = sys.argv[1], sys.argv[2]
errs = []
vj = json.load(open(d + "/verify.json"))
if vj.get("mode") != "offline":
    errs.append(f"post-verify verify.json mode={vj.get('mode')!r} want 'offline'")
if vj.get("error"):
    errs.append(f"post-verify verify.json error non-empty: {vj.get('error')!r}")
sc = [json.loads(x) for x in open(d + "/scores.jsonl") if x.strip()]
if not [s for s in sc if s.get("key") == "verifier.pass" and s.get("value") == 1]:
    errs.append(f"post-verify scores lack passing verifier.pass: {sc!r}")
oktext = open(out).read()
if "verify %s: ok" % "bench-prod-analysis-verify-tok-baseline-r1" not in oktext:
    errs.append(f"verify stdout missing the ok line: {oktext!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("standalone verify: mode flips bench -> offline, verifier.pass=1 re-scored, per-cell 'ok'")
PY
record "$rc" "standalone verify re-scores offline: verify.json mode->offline, verifier.pass=1, per-cell ok"

echo "== prod.90 verify --label: narrows re-verification to the matching variant only =="
rc=0
ok_all=$(grep -c ': ok$' "$v/verify.out" || true)
[ "$ok_all" -eq 4 ] || rc=1
record "$rc" "unfiltered verify: $ok_all/4 cells re-verified ok (baseline+other x 2 reps)"
run_json 0 "$v/verify-label.out" "standalone catacomb verify --label variant=baseline (filtered re-verify)" -- \
  catacomb verify "$v/basket.yaml" --runs-dir "$v/runs" --label variant=baseline
rc=0
ok_baseline=$(grep -c ': ok$' "$v/verify-label.out" || true)
[ "$ok_baseline" -eq 2 ] || rc=1
record "$rc" "verify --label variant=baseline: $ok_baseline/2 cells re-verified ok (other variant excluded)"
