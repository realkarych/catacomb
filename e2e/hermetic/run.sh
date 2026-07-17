#!/usr/bin/env bash
# Hermetic verifier-contract E2E — the SP1 verifier contract (waves A-D) exercised
# end-to-end against fully deterministic evidence, with zero API spend.
#
# A tiny SQLite task (agent.sh runs $SQL_QUERY -> out/result.csv) is benched across
# three variants x 5 reps (baseline, degraded, baseline2) and driven through the whole
# offline pipeline: inline bench verify + artifact capture, `catacomb verify` offline
# re-verification, and `catacomb regress` gating on the run-level `verifier.pass`
# annotation. Every axis except wall-clock duration is byte-identical across cells (the
# transcript is a fixed template; result.csv is a pure function of the query), so the
# A-vs-A control gates cleanly at DEFAULT thresholds — no band widening (unlike the live
# e2e/run.sh, which widens the continuous band to absorb real API latency drift).
#
# Assertion order (self-consistent; each step notes its dependency inline):
#   2 bench -> 3 evidence shape (verify.json mode "bench") -> 4 idempotent offline
#   verify (scores byte-identical, mode "offline") -> 5 degraded gate (exit 1) ->
#   6 A-vs-A control (exit 0) -> 7 operational-failure visibility (broken verifier) +
#   restore -> 8 --scores override -> 9 SP2 reliability/paired shape (re-reads the
#   step-5 report JSON) -> 10 SP3 env stamps (re-reads the bench-time meta.json) ->
#   11 SP3 pareto (re-records the step-5/6 comparisons into a fresh store) ->
#   12 SP4 audit (duration-pinned copy of the runs dir: dormancy at full defaults,
#   then a planted outlier; both exits must match) -> 13 SP4 pack (deterministic
#   stride bundle over the untouched runs dir) -> 14 SP4 scores round-trip (external
#   audit.clean score -> annotation finding) -> 15 SP5 judge agreement (gold labels
#   vs pooled baseline+degraded scores; kappa gate both directions incl. the exact
#   ties-pass boundary) -> 16 SP5 judge panel (three-judge fixture -> byte-exact
#   mean/vote streams; the vote stream gates through regress --scores).
#   Then CLI-wiring coverage: 17 replay determinism -> 18 baseline list/bundle
#   round-trip/rm -> 19 mcp stdio.
#   Then 20 SP-W workspace isolation + patch-free offline verify (its own basket +
#   runs dirs, nothing earlier touched). Then 21 import path: a step-2 session
#   ingested into a fresh evidence dir and driven through offline verify + regress
#   with zero agent spawn -> 22 ADR-0034 gate self-check (calibrate over fabricated
#   single-variant groups: clean A/A, seeded drift, <6-run insufficiency,
#   leave-one-out influence flip). Last, 23 production scenarios (prod/run.sh).
# Steps 5/6 need the clean offline scores produced by step 4. Step 7 points verify at
# /usr/bin/false: a failing verifier writes NO scores (so scores.jsonl survives intact),
# but stamps verify.json with an error and the broken config's hash — so it runs after
# the gates and is immediately followed by a clean re-verify that restores (and asserts)
# the offline state. Step 8 reads only scores.jsonl; step 9 re-reads the step-5 report
# JSON from disk. Steps 10/11 are SP3: step 10 re-reads meta.json exactly as bench
# wrote it (offline verify rewrites verify.json and scores.jsonl, never meta.json), and
# step 11 re-runs the step-5/6 comparisons with --record against a fresh store and
# asserts the pareto view over that history. Steps 12-14 are SP4: step 12 works on a
# COPY of the runs dir whose meta.json marker windows are pinned to one fixed value —
# wall-clock duration is the single audited axis bench cannot make deterministic (the
# first cell pays a ~10x warm-up premium, a legitimate far-out duration flag) — so the
# copy is value-identical on all five audited axes and the dormancy raw-key check runs
# at FULL defaults; the planted comparison then re-runs on the same copy plus one
# fabricated cell, and its exit must equal the clean one (audit never gates). Steps
# 13/14 read only the untouched runs dir (plant and pinning live in the copy alone).
# Steps 15/16 are SP5 and mutate no evidence: step 15 copies the baseline+degraded
# scores.jsonl files into a two-variant pool and runs the judge agreement calculator
# (python3 -m catacomb_judge off PYTHONPATH, like the verifier SDK) against fixture
# gold labels — the judge id is the verifier's own recorded tool provenance
# ("verify_sql", the SP1 seam read back); step 16 aggregates a fixture three-judge
# panel and feeds its --vote output through the built binary via regress --scores.
# Steps 17-19 are CLI wiring coverage (renumbered after the SP4/SP5 additions): step
# 17 replays the fixture transcript (fixed session id, so stdout must be byte-identical
# across two runs), step 18 does a baseline list/rm roundtrip on the step-11 trends
# store (so it must run after 11 and after the SP4/SP5 steps that read baselines) and,
# between list and rm, proves the baseline bundle hand-off: export twice
# (byte-identical), import into a fresh db + runs dir, identical regress verdicts
# there with no runs-dir warning, and a flipped-byte bundle refused with nothing
# landed; and
# step 19 drives the `catacomb mcp` stdio JSON-RPC server with a fixed 3-request script.
# Step 20 is SP-W and mutates none of the earlier evidence: a separate 3-rep workspace
# basket benched into fresh runs dirs proves per-cell dir isolation (the agent exits 7
# on a reused dir, 8 on a leaked CATACOMB_PATCH), the patch handover (captured artifact
# == patch bytes; rev + sha256 stamped at env.workspace, absent on the sql cells per
# step 10), teardown after every cell, --keep-workspaces, and a seeded workspace-cmd
# failure under --fail-fast (manifest note + exit code, teardown still firing). The
# step closes with an offline `catacomb verify` over the ws runs dir with fix.patch
# moved away — offline loading never resolves workspace.patch — asserting step-4-style
# idempotency (scores byte-identical, verify.json mode offline) before restoring it.
# Step 21 is the import path and mutates none of the earlier evidence: two step-2
# bench sessions (still under projects/hermetic, keyed by the session_id each
# meta.json recorded) are ingested by `catacomb import` into a fresh runs dir with
# no agent spawn, then driven through offline verify + regress. Because import runs
# no task cmd it captures no artifacts, so the sql verifier cannot re-verify
# imported evidence; verify_import.py is the artifact-free twin that reads the
# ingested session.jsonl from the evidence dir (the import contract: a verifier
# sees the transcript, not a live workdir), and basket-import.yaml is the sql basket
# with only that verify cmd swapped, so labels match the sql cells. Step 22 is the
# ADR-0034 gate self-check: `catacomb calibrate` over four fabricated
# single-variant groups (clean A/A, seeded second-half tokens_out drift, a <6-run
# insufficiency, and a 7-run leave-one-out influence flip), all derived from the
# fixture transcript template with pinned marker windows — no bench dependency, no
# earlier evidence touched. Step 23 runs the production scenarios (prod/run.sh).
#
# Environment:
#   CATACOMB_BIN   catacomb binary to drive (default: `catacomb` on PATH). Its dir is
#                  prepended to PATH so bare `catacomb` resolves.
#
# Run: make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

fatal() {
	printf 'hermetic-e2e: FATAL: %s\n' "$1" >&2
	exit 2
}

# --- required binaries --------------------------------------------------------
catacomb_bin="${CATACOMB_BIN:-catacomb}"
"$catacomb_bin" version >/dev/null 2>&1 ||
	fatal "catacomb is not runnable — install it, add it to PATH, or set CATACOMB_BIN"
catacomb_abs="$(command -v "$catacomb_bin" 2>/dev/null || true)"
[ -n "$catacomb_abs" ] || fatal "cannot resolve the catacomb binary path"
PATH="$(cd "$(dirname "$catacomb_abs")" && pwd):$PATH"
export PATH
command -v catacomb >/dev/null 2>&1 || fatal "catacomb must resolve on PATH"
command -v sqlite3 >/dev/null 2>&1 || fatal "sqlite3 not found on PATH"
command -v python3 >/dev/null 2>&1 || fatal "python3 not found on PATH"
[ -d "$repo/integrations/verifier/src" ] ||
	fatal "verifier SDK not found at integrations/verifier/src (PYTHONPATH source)"
[ -d "$repo/integrations/judge/src" ] ||
	fatal "judge SDK not found at integrations/judge/src (PYTHONPATH source)"

# --- assertion bookkeeping (conventions copied from e2e/run.sh) ---------------
failures=()
pass() { printf '  PASS  %s\n' "$1"; }
failrec() {
	printf '  FAIL  %s\n' "$1"
	failures+=("$1")
}
record() { # <rc> <label>
	if [ "$1" -eq 0 ]; then pass "$2"; else failrec "$2"; fi
}

# run a command and compare its exit code against the expected one
run_expect() { # <want> <label> -- cmd...
	local want="$1" label="$2"
	shift 2
	[ "${1:-}" = "--" ] && shift
	local rc=0
	"$@" || rc=$?
	if [ "$rc" -eq "$want" ]; then pass "$label (exit $rc)"; else failrec "$label (exit $rc, want $want)"; fi
}

# run a command capturing stdout to <out> and stderr to <out>.stderr, compare exit code
run_json() { # <want> <out> <label> -- cmd...
	local want="$1" out="$2" label="$3"
	shift 3
	[ "${1:-}" = "--" ] && shift
	local rc=0
	"$@" >"$out" 2>"$out.stderr" || rc=$?
	if [ "$rc" -eq "$want" ]; then
		pass "$label (exit $rc)"
	else
		failrec "$label (exit $rc, want $want; out: $out)"
		sed 's/^/        stderr: /' "$out.stderr" >&2 || true
	fi
}

# --- step 1: workspace + fixtures ---------------------------------------------
# The basket template references goldens/verify/transcript under __WORK__ (the
# documented anti-gaming layout: verifier inputs live outside the cell workdir). agent.sh
# runs as `./agent.sh` from dir __WORK__/cellwork, so it is staged there; the verifier
# resolves catacomb_verifier from PYTHONPATH (the SDK is not installed).
work="$(mktemp -d)"
runs="$work/runs"
cp "$here/golden.csv" "$here/../verify_sql.py" "$here/transcript.jsonl.tmpl" "$work/"
sed "s|__WORK__|$work|g" "$here/basket.yaml.tmpl" >"$work/basket.yaml"
# broken twin: same basket name/hash inputs, verify swapped to a guaranteed-failing
# command. The `cmd:` line is the only one carrying verify_sql.py, so match on that.
sed "s|__WORK__|$work|g" "$here/basket.yaml.tmpl" |
	sed 's|cmd: \[.*verify_sql.py.*\]|cmd: ["/usr/bin/false"]|' >"$work/basket-broken.yaml"
sqlite3 "$work/e2e.db" <"$here/seed.sql"
mkdir -p "$work/cellwork" "$work/projects" "$runs"
cp "$here/agent.sh" "$work/cellwork/agent.sh"
chmod +x "$work/cellwork/agent.sh"
# SP-W fixtures (step 20): the ws basket renders next to fix.patch (patch paths
# resolve against the basket file's dir); agent_ws.sh stays at $work root so the
# workspace cmd can copy it into each fresh cell dir; setup/teardown logs
# accumulate under wslog/ across cells. The fail twin swaps the workspace cmd —
# the only line carrying CATACOMB_PATCH — for a guaranteed exit 3, keeping
# teardown, so step 20 can assert teardown still runs after a failed setup.
sed "s|__WORK__|$work|g" "$here/ws-basket.yaml.tmpl" >"$work/ws-basket.yaml"
sed 's|cmd: \[.*CATACOMB_PATCH.*\]|cmd: ["sh", "-c", "exit 3"]|' \
	"$work/ws-basket.yaml" >"$work/ws-basket-fail.yaml"
cp "$here/fix.patch" "$here/verify_ws.py" "$here/agent_ws.sh" "$work/"
chmod +x "$work/agent_ws.sh"
mkdir -p "$work/wslog"
export HERMETIC_DB="$work/e2e.db"
export HERMETIC_PROJECTS="$work/projects"
export HERMETIC_TDIR="$work"
export PYTHONPATH="$repo/integrations/verifier/src:$repo/integrations/judge/src${PYTHONPATH:+:$PYTHONPATH}"

# Run ids are deterministic: bench-<basket>-<task>-<variant>-r<rep>.
base1="bench-hermetic-sql-sql-baseline-r1"
base1_dir="$runs/$base1"

echo "== 2. bench hermetic basket (15 cells: 3 variants x 5 reps) =="
run_expect 0 "bench hermetic-sql basket" -- \
	catacomb bench "$work/basket.yaml" --projects-dir "$work/projects" --runs-dir "$runs" --manifest "$work/m.jsonl"

echo "== 3. evidence shape on a baseline cell (bench-time) =="
rc=0
python3 - "$base1_dir" <<'PY' || rc=$?
import json, os, sys

d = sys.argv[1]
errs = []
csv = os.path.join(d, "artifacts", "out", "result.csv")
if not os.path.isfile(csv):
    errs.append(f"missing captured artifact {csv}")
try:
    if "verifier.pass" not in open(os.path.join(d, "scores.jsonl")).read():
        errs.append("scores.jsonl lacks verifier.pass")
except OSError as e:
    errs.append(f"scores.jsonl unreadable: {e}")
try:
    v = json.load(open(os.path.join(d, "verify.json")))
    if v.get("mode") != "bench":
        errs.append(f"verify.json mode={v.get('mode')!r} want 'bench'")
    if v.get("error"):
        errs.append(f"verify.json error non-empty at bench time: {v.get('error')!r}")
except OSError as e:
    errs.append(f"verify.json unreadable: {e}")
try:
    arts = (json.load(open(os.path.join(d, "meta.json"))).get("artifacts") or [])
    if not arts:
        errs.append("meta.json artifacts array empty")
    for a in arts:
        if len(a.get("sha256", "")) != 64:
            errs.append(f"artifact {a.get('rel')!r} sha256 not 64 hex: {a.get('sha256')!r}")
except OSError as e:
    errs.append(f"meta.json unreadable: {e}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("evidence shape OK: artifacts/out/result.csv, scores(verifier.pass), verify.json mode=bench, meta artifacts+sha256")
PY
record "$rc" "evidence shape: result.csv + scores(verifier.pass) + verify.json mode=bench + meta artifacts/sha256"

echo "== 4. idempotent offline re-verify (scores byte-identical, mode -> offline) =="
snap="$work/snap"
mkdir -p "$snap"
for d in "$runs"/*/; do
	cp "$d/scores.jsonl" "$snap/$(basename "$d").scores"
done
run_expect 0 "catacomb verify (offline re-verify)" -- \
	catacomb verify "$work/basket.yaml" --runs-dir "$runs"
rc=0
for d in "$runs"/*/; do
	cmp -s "$d/scores.jsonl" "$snap/$(basename "$d").scores" || rc=1
done
record "$rc" "offline verify leaves every scores.jsonl byte-identical"
rc=0
python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]+"/verify.json"))["mode"]=="offline" else 1)' "$base1_dir" || rc=$?
record "$rc" "verify.json mode flips to offline after re-verify"

echo "== 5. seeded regression: baseline vs degraded must gate (exit 1) =="
run_json 1 "$work/regress-degraded.json" \
	"degraded gate (baseline vs degraded)" -- \
	catacomb regress --runs-dir "$runs" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=degraded --json
rc=0
python3 - "$work/regress-degraded.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "total" and f.get("metric") == "ann:verifier.pass"
    and f.get("verdict") == "regression"
]
if not hits:
    print("no total ann:verifier.pass regression; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print(f"decisive finding: ann:verifier.pass {hits[0].get('detail', '')} (regression)")
PY
record "$rc" "degraded gate attributed to ann:verifier.pass total regression"

echo "== 6. A-vs-A control: baseline vs baseline2 must NOT gate (exit 0) =="
# Hermetic determinism: tokens/cost/nodes/verifier.pass are exactly equal across cells;
# only wall-clock duration_ms varies. --metric-rel-delta 0.5 widens just the continuous
# band so CI-runner jitter (e2e/run.sh saw duration ~2x between identical batches) cannot
# false-gate this control; the seeded detection (step 5) is on the annotation-rate axis,
# which --metric-rel-delta does not touch.
run_json 0 "$work/regress-AvA.json" \
	"A-vs-A must NOT gate (baseline vs baseline2)" -- \
	catacomb regress --runs-dir "$runs" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=baseline2 \
	--metric-rel-delta 0.5 --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$work/regress-AvA.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions"

echo "== 7. operational-failure visibility: broken verifier surfaces the failure =="
# basket-broken.yaml keeps the basket name but points verify at /usr/bin/false. Its
# config hash differs from the recorded runs, so `catacomb verify` prints the hash
# warning to STDERR; the per-cell failure line ("verify <run-id>: error (...)") goes to
# STDOUT, so the run-id grep scans the combined output. A failing verifier writes no
# scores, so scores.jsonl survives — but verify.json is stamped with the error, which the
# clean re-verify below restores.
run_json 1 "$work/broken.out" \
	"broken verifier gate (verify basket-broken.yaml)" -- \
	catacomb verify "$work/basket-broken.yaml" --runs-dir "$runs"
rc=0
grep -qi 'hash differs' "$work/broken.out.stderr" || rc=1
record "$rc" "broken verify emits the basket-hash-mismatch warning on stderr"
rc=0
grep -q "$base1" "$work/broken.out" "$work/broken.out.stderr" || rc=1
record "$rc" "broken verify output mentions a run id ($base1)"
rc=0
python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]+"/verify.json")).get("error") else 1)' "$base1_dir" || rc=$?
record "$rc" "broken verify.json error is non-empty"
run_expect 0 "clean re-verify restores offline state" -- \
	catacomb verify "$work/basket.yaml" --runs-dir "$runs"
rc=0
python3 -c 'import json,sys; v=json.load(open(sys.argv[1]+"/verify.json")); sys.exit(0 if v["mode"]=="offline" and not v.get("error") else 1)' "$base1_dir" || rc=$?
record "$rc" "verify.json restored to mode=offline with no error"

echo "== 8. --scores override: flipping one baseline cell must NOT gate at defaults =="
# One external run-level score flips a single baseline cell's verifier.pass 1 -> 0 (4/5
# pass). Against baseline2 (5/5) that is an improvement, never a regression, and a lone
# flip of five is below the annotation-rate floor anyway (PV-6a). The override lands on
# an evidence-provided value, so regress prints the "overrode" warning to stderr.
printf '{"key":"verifier.pass","value":0,"run_id":"%s"}\n' "$base1" >"$work/override.scores"
run_json 0 "$work/regress-scores.json" \
	"A-vs-A with --scores must NOT gate" -- \
	catacomb regress --runs-dir "$runs" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=baseline2 \
	--scores "$work/override.scores" --json
rc=0
grep -qi 'overrode' "$work/regress-scores.json.stderr" || rc=1
record "$rc" "external --scores override reported on stderr (overrode)"

echo "== 9. SP2 reliability + paired disclosure on the degraded comparison =="
# Re-reads the step-5 report JSON. One task (sql) x 5 reps per side, baseline
# verifier.pass 5/5 vs degraded 0/5, so the pass^k mean curves are exactly all-1.0 vs
# all-0.0 at k_max=5. A single matched task can never fire the paired sign test
# (paired min 5 tasks), so every paired finding must say insufficient and the
# sensitivity block must disclose the smallest task count at which a unanimous shift
# would gate — the gate says out loud that it cannot fire, never silently impotent.
rc=0
python3 - "$work/regress-degraded.json" <<'PY' || rc=$?
import json, sys

rel = json.load(open(sys.argv[1])).get("reliability") or {}
errs = []
for group, want in (("baseline", 1.0), ("candidate", 0.0)):
    g = rel.get(group) or {}
    mean = g.get("mean") or []
    if g.get("k_max") != 5 or mean != [want] * 5:
        errs.append(f"{group}: k_max={g.get('k_max')!r} mean={mean!r} want five x {want}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("reliability: baseline mean all 1.0, candidate mean all 0.0 (k_max=5)")
PY
record "$rc" "reliability pass^1..^5: baseline mean all 1.0, candidate all 0.0"
rc=0
python3 - "$work/regress-degraded.json" <<'PY' || rc=$?
import json, sys

paired = [f for f in json.load(open(sys.argv[1])).get("findings", []) if f.get("scope") == "paired"]
errs = []
metrics = sorted(f.get("metric") for f in paired)
if metrics != ["cost_usd", "duration_ms", "tokens_in", "tokens_out"]:
    errs.append(f"paired metrics {metrics}")
for f in paired:
    if f.get("verdict") != "insufficient" or f.get("detail") != "matched 1 task below paired min 5":
        errs.append(f"{f.get('metric')}: verdict={f.get('verdict')!r} detail={f.get('detail')!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("paired: all 4 metrics insufficient (matched 1 task below paired min 5)")
PY
record "$rc" "paired sign test reports insufficient at n_tasks=1"
rc=0
python3 -c 'import json,sys; p=(json.load(open(sys.argv[1])).get("sensitivity") or {}).get("paired") or {}; sys.exit(0 if p.get("reachable") is False and p.get("min_tasks") == 5 else 1)' "$work/regress-degraded.json" || rc=$?
record "$rc" "sensitivity discloses the paired gate needs k>=5 tasks"

echo "== 10. SP3 env stamps in meta.json (bench-time provenance) =="
# Env stamps are written once at bench time (step 2); the offline re-verifies in steps
# 4 and 7 rewrite verify.json and scores.jsonl but never meta.json, so this also proves
# the stamps survive the whole verify cycle. The fixture pins the assertions exactly:
# agent.sh seds ONE generated uuid into both the transcript's sessionId and the
# stream-json session_id, and every assistant turn in transcript.jsonl.tmpl carries
# model "claude-opus-4-8" — so model_id MUST be present with that value; the template
# has no top-level "version" field, so claude_code_version MUST be absent. The sql
# task has no workspace block either, so the SP-W stamp must be absent too (the
# step-20 positive's negative: env carries no "workspace" key at all).
rc=0
python3 - "$base1_dir" <<'PY' || rc=$?
import json, sys

env = json.load(open(sys.argv[1] + "/meta.json")).get("env") or {}
res = env.get("resources") or {}
errs = []
if not env.get("catacomb_version"):
    errs.append(f"catacomb_version empty: {env.get('catacomb_version')!r}")
for k in ("os", "arch"):
    if not res.get(k):
        errs.append(f"resources.{k} empty: {res.get(k)!r}")
if not isinstance(res.get("cpus"), int) or res["cpus"] < 1:
    errs.append(f"resources.cpus not an int >= 1: {res.get('cpus')!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"env: catacomb_version={env['catacomb_version']} resources={res['os']}/{res['arch']} cpus={res['cpus']}")
PY
record "$rc" "meta.json env: catacomb_version + resources (os/arch non-empty, cpus >= 1)"
rc=0
python3 - "$base1_dir" <<'PY' || rc=$?
import json, sys

env = json.load(open(sys.argv[1] + "/meta.json")).get("env") or {}
errs = []
if env.get("model_id") != "claude-opus-4-8":
    errs.append(f"model_id={env.get('model_id')!r} want 'claude-opus-4-8' (fixture assistant turns)")
if "claude_code_version" in env:
    errs.append(f"claude_code_version present ({env['claude_code_version']!r}) but the fixture transcript has no version field")
if "workspace" in env:
    errs.append(f"workspace stamp present ({env['workspace']!r}) but the sql task has no workspace block")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("env: model_id=claude-opus-4-8 (fixture model), claude_code_version + workspace absent")
PY
record "$rc" "meta.json env: model_id pinned to the fixture model, claude_code_version + workspace absent"

echo "== 11. SP3 pareto: record history, read the accuracy-vs-cost table =="
# A fresh store pins the baseline variant, then the step-6 A-vs-A control and the step-5
# degraded gate are re-run with --record. Hermetic cost is exactly 0.0 on every cell
# (agent.sh reports total_cost_usd 0.0 and the fixture transcript carries no usage), so
# the three pareto points differ only on accuracy: the degraded point (equal cost,
# strictly worse accuracy) must be dominated, while the baseline point and the A-vs-A
# point are equal on BOTH axes and must both stay non-dominated — equal points never
# dominate each other. Hermetic runs always carry cost + verifier, so every point must
# have both axes (the no-axis note path is untestable here by construction).
run_expect 0 "baseline set hermetic-trends (fresh store)" -- \
	catacomb baseline set hermetic-trends --label basket=hermetic-sql,variant=baseline \
	--db "$work/trends.db" --runs-dir "$runs"
run_json 0 "$work/record-AvA.json" \
	"record A-vs-A comparison (exit 0)" -- \
	catacomb regress --runs-dir "$runs" --db "$work/trends.db" \
	--baseline name:hermetic-trends \
	--candidate label:basket=hermetic-sql,variant=baseline2 \
	--metric-rel-delta 0.5 --record --json
run_json 1 "$work/record-degraded.json" \
	"record degraded comparison (still gates, exit 1)" -- \
	catacomb regress --runs-dir "$runs" --db "$work/trends.db" \
	--baseline name:hermetic-trends \
	--candidate label:basket=hermetic-sql,variant=degraded \
	--record --json
run_json 0 "$work/pareto.json" \
	"trends --pareto --json over the recorded history" -- \
	catacomb trends hermetic-trends --pareto --json --db "$work/trends.db"
rc=0
python3 - "$work/pareto.json" <<'PY' || rc=$?
import json, sys

points = json.load(open(sys.argv[1]))["points"]
errs = []

def one(what, pred):
    hits = [p for p in points if pred(p)]
    if len(hits) != 1:
        errs.append(f"want exactly 1 {what} point, got {len(hits)}")
        return {}
    return hits[0]

base = one("baseline", lambda p: p.get("source") == "baseline")
ava = one("A-vs-A", lambda p: "variant=baseline2" in p.get("candidate", ""))
deg = one("degraded", lambda p: "variant=degraded" in p.get("candidate", ""))
for what, p, dom, acc in (("baseline", base, False, 1.0), ("A-vs-A", ava, False, 1.0), ("degraded", deg, True, 0.0)):
    if not p:
        continue
    if p.get("dominated") is not dom:
        errs.append(f"{what}: dominated={p.get('dominated')!r} want {dom}")
    if p.get("accuracy") != acc or p.get("cost_usd") != 0.0:
        errs.append(f"{what}: accuracy={p.get('accuracy')!r} cost_usd={p.get('cost_usd')!r} want {acc}/0.0")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("pareto: degraded dominated (equal cost, worse accuracy); baseline + A-vs-A equal on both axes, both non-dominated")
PY
record "$rc" "pareto marks only the degraded point dominated (equal points both stay)"
rc=0
python3 - "$work/pareto.json" <<'PY' || rc=$?
import json, sys

points = json.load(open(sys.argv[1]))["points"]
errs = []
if len(points) != 3:
    errs.append(f"want 3 points (baseline + 2 records), got {len(points)}")
for p in points:
    tag = p.get("candidate") or p.get("source")
    if "accuracy" not in p or "cost_usd" not in p:
        errs.append(f"{tag}: lacks an axis, keys={sorted(p)}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("pareto: 3 points, every point carries both axes (accuracy + cost_usd)")
PY
record "$rc" "pareto lists baseline + 2 records, both axes on every point"

echo "== 12. SP4 audit: dormant on A-vs-A, planted outlier flagged without gating =="
# The audit screens per-cell duration_ms/cost_usd/tokens_in/tokens_out/turns against
# the group median. Four of those are byte-determined by the fixed transcript template
# (cost/tokens sums 0.0, turns 5); wall-clock duration is not — the first bench cell
# pays a ~10x warm-up premium, a legitimate far-out flag at the default audit band. So
# this step works on a copy of the runs dir with every meta.json marker window pinned
# to the template's own mark window (4000 ms): evidence value-identical on ALL five
# audited axes, letting both comparisons below run at FULL default thresholds. The
# clean A-vs-A over that copy must carry no "audit" key at all (raw-key dormancy).
runs_plant="$work/runs-plant"
cp -R "$runs" "$runs_plant"
python3 - "$runs_plant" <<'PY'
import glob, json, os, sys

for path in glob.glob(os.path.join(sys.argv[1], "*", "meta.json")):
    meta = json.load(open(path))
    meta["marker_start"] = "2026-06-20T10:00:03Z"
    meta["marker_end"] = "2026-06-20T10:00:07Z"
    json.dump(meta, open(path, "w"))
PY
run_json 0 "$work/regress-dormant.json" \
	"A-vs-A on duration-pinned evidence at full defaults (exit 0)" -- \
	catacomb regress --runs-dir "$runs_plant" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=baseline2 --json
rc=0
python3 -c 'import json,sys; sys.exit(0 if "audit" not in json.load(open(sys.argv[1])) else 1)' "$work/regress-dormant.json" || rc=$?
record "$rc" "A-vs-A report JSON carries no audit key (dormancy)"
# Planted outlier: the same copy gains one fabricated baseline2 cell built from the
# same transcript template, with usage injected into every assistant turn (5 x 1200 =
# 6000 tokens_out; every real cell sums 0). The plant's model is renamed to one the
# pricer cannot price, so cost_usd stays 0.0 like the group and tokens_out is the only
# axis that moves; meta is the pinned baseline2-r1 meta (duration 4000 ms like all),
# and turns stay 5 (same template). The gate compares group medians (5 zeros + 1 plant
# -> median 0), so this comparison must keep the clean run's exit 0 above: audit is
# non-gating end-to-end.
plant="bench-hermetic-sql-sql-baseline2-r6"
plant_dir="$runs_plant/$plant"
mkdir "$plant_dir"
plant_sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
sed -e "s/__SESSION_ID__/$plant_sid/g" \
	-e 's|"model":"claude-opus-4-8","content":|"model":"hermetic-audit-plant","usage":{"output_tokens":1200},"content":|g' \
	"$work/transcript.jsonl.tmpl" >"$plant_dir/session.jsonl"
python3 - "$runs_plant/bench-hermetic-sql-sql-baseline2-r1/meta.json" "$plant_dir/meta.json" "$plant" "$plant_sid" <<'PY'
import json, sys

meta = json.load(open(sys.argv[1]))
meta["run_id"] = sys.argv[3]
meta["session_id"] = sys.argv[4]
meta["rep"] = 6
meta["labels"]["rep"] = "6"
json.dump(meta, open(sys.argv[2], "w"))
PY
printf '{"key":"verifier.pass","value":1,"run_id":"%s"}\n' "$plant" >"$plant_dir/scores.jsonl"
run_json 0 "$work/regress-plant.json" \
	"planted-outlier comparison keeps the clean exit (audit never gates)" -- \
	catacomb regress --runs-dir "$runs_plant" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=baseline2 --json
rc=0
python3 - "$work/regress-plant.json" "$plant" <<'PY' || rc=$?
import json, sys

audit = json.load(open(sys.argv[1])).get("audit")
want = {"run_id": sys.argv[2], "task": "sql", "metric": "tokens_out",
        "value": 6000, "median": 0, "band": 0}
errs = []
if audit is None:
    errs.append("audit block missing despite the planted outlier")
elif "baseline" in audit:
    errs.append(f"baseline flags present: {audit['baseline']!r}")
elif audit.get("candidate") != [want]:
    errs.append(f"candidate flags {audit.get('candidate')!r} want [{want!r}]")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"audit: candidate {sys.argv[2]} tokens_out 6000 vs median 0 (band 0), no other flag")
PY
record "$rc" "audit names exactly the planted run and tokens_out (value 6000, median 0)"

echo "== 13. SP4 pack: deterministic stride bundle over the sql basket =="
# Packs the UNTOUCHED runs dir (the plant lives only in the step-12 copy): 15 runs
# sorted by run id, default --sample 3 -> stride indices floor(i*15/3) = 0, 5, 10.
# The expected id list is recomputed from the rule over the evidence dir listing, so
# a sampling change breaks this assertion, not just the manifest shape.
pack_out="$work/pack"
run_json 0 "$work/pack.out" \
	"pack the sql basket (default sample of 3)" -- \
	catacomb pack label:basket=hermetic-sql --runs-dir "$runs" --out "$pack_out"
rc=0
grep -Fqx "packed 3 of 15 runs into $pack_out" "$work/pack.out" || rc=1
record "$rc" "pack stdout reports packed 3 of 15 runs"
rc=0
python3 - "$pack_out/pack.json" "$runs" <<'PY' || rc=$?
import json, os, sys

m = json.load(open(sys.argv[1]))
ids = sorted(os.listdir(sys.argv[2]))
want = [ids[i * len(ids) // 3] for i in range(3)]
errs = []
if len(ids) != 15:
    errs.append(f"runs dir holds {len(ids)} runs, want 15")
for field, wantv in (("selector", "label:basket=hermetic-sql"), ("runs_dir", sys.argv[2]),
                     ("sample_rule", "runid-stride"), ("requested", 3), ("runs", want)):
    if m.get(field) != wantv:
        errs.append(f"{field}={m.get(field)!r} want {wantv!r}")
created = m.get("created_at")
if not isinstance(created, str) or "T" not in created or not created.endswith("Z"):
    errs.append(f"created_at not a UTC RFC3339 stamp: {created!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"pack.json: runid-stride sample {want} out of {len(ids)} runs")
PY
record "$rc" "pack.json manifest fields + exact stride-sampled run list"
rc=0
python3 - "$pack_out" <<'PY' || rc=$?
import json, os, sys

out = sys.argv[1]
errs = []
for rid in json.load(open(os.path.join(out, "pack.json")))["runs"]:
    for name in ("meta.json", "session.jsonl"):
        path = os.path.join(out, rid, name)
        if not os.path.isfile(path):
            errs.append(f"missing {path}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("bundle: meta.json + session.jsonl present for every sampled run")
PY
record "$rc" "bundle carries meta.json + session.jsonl per sampled run"
rc=0
[ -s "$pack_out/INSTRUCTIONS.md" ] || rc=1
record "$rc" "INSTRUCTIONS.md present in the bundle"

echo "== 14. SP4 round-trip: external audit.clean score surfaces as a finding =="
# Closes the pack loop through the built binary: a hand-written scores line — the
# exact dialect INSTRUCTIONS.md asks an external auditor to return, tool provenance
# included (the gate ignores it; catacomb-judge consumes it) — lands on a packed
# run (baseline r1, index 0 of the step-13 sample) via --scores, and --annotation
# gates it. Only one baseline run carries the key, so the deterministic outcome is the
# annotation-absent-in-candidate insufficient finding; exit stays 0 (nothing gates).
printf '{"key":"audit.clean","value":1,"run_id":"%s","tool":"hermetic-auditor"}\n' "$base1" >"$work/audit-clean.scores"
run_json 0 "$work/regress-audit.json" \
	"A-vs-A with the returned audit.clean score (exit 0)" -- \
	catacomb regress --runs-dir "$runs" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=baseline2 \
	--metric-rel-delta 0.5 --scores "$work/audit-clean.scores" \
	--annotation audit.clean:higher-better --json
rc=0
python3 - "$work/regress-audit.json" <<'PY' || rc=$?
import json, sys

hits = [
    f for f in json.load(open(sys.argv[1])).get("findings", [])
    if f.get("metric") == "ann:audit.clean"
]
errs = []
if len(hits) != 1:
    errs.append(f"want exactly 1 ann:audit.clean finding, got {len(hits)}")
for f in hits:
    got = (f.get("scope"), f.get("verdict"), f.get("detail"))
    if got != ("total", "insufficient", "annotation absent in candidate"):
        errs.append(f"scope/verdict/detail {got!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("round-trip: ann:audit.clean total finding (insufficient, absent in candidate)")
PY
record "$rc" "returned score surfaces as the ann:audit.clean annotation finding"

echo "== 15. SP5 judge agreement: gold labels vs pooled verifier scores =="
# The judge utilities read only scores.jsonl, so a two-variant pool is staged by
# copying the 10 baseline+degraded scores files (baseline2 stays out: 10 labels,
# 10 matching scores, zero unmatched on either side). Every persisted line carries
# the verifier's own provenance (tool "verify_sql") plus the run_id the binary
# injected at persist time — the SP1 seam read back — so "verify_sql" is the judge
# id the report must group under. Perfect agreement first: baseline runs pass
# (label 1), degraded fail (label 0), so every metric is exactly 1.0 and the
# --min-kappa 0.8 gate holds. One flipped label (degraded-r1 -> 1) gives p_o 0.9
# and p_e 0.5, so kappa is (0.9-0.5)/(1-0.5) = exactly the float 0.8 — which
# PASSES the strict-< gate at 0.8, the documented ties-pass boundary, pinned here
# end-to-end. Two flipped labels (also baseline-r1 -> 0) rebalance the marginals
# (p_o 0.8, p_e 0.5), kappa drops to (0.8-0.5)/0.5 = 0.6000000000000001 exactly,
# and the gate fails with exit 1.
runs_judge="$work/runs-judge"
mkdir "$runs_judge"
for d in "$runs"/bench-hermetic-sql-sql-baseline-r? "$runs"/bench-hermetic-sql-sql-degraded-r?; do
	mkdir "$runs_judge/$(basename "$d")"
	cp "$d/scores.jsonl" "$runs_judge/$(basename "$d")/scores.jsonl"
done
for rep in 1 2 3 4 5; do
	printf '{"run_id":"bench-hermetic-sql-sql-baseline-r%d","key":"verifier.pass","label":1}\n' "$rep"
	printf '{"run_id":"bench-hermetic-sql-sql-degraded-r%d","key":"verifier.pass","label":0}\n' "$rep"
done >"$work/judge-labels.jsonl"
run_json 0 "$work/judge-agree.json" \
	"judge agreement over the pooled evidence (--min-kappa 0.8, exit 0)" -- \
	python3 -m catacomb_judge agreement --labels "$work/judge-labels.jsonl" \
	--runs-dir "$runs_judge" --key verifier.pass --json --min-kappa 0.8
rc=0
python3 - "$work/judge-agree.json" <<'PY' || rc=$?
import json, sys

got = json.load(open(sys.argv[1]))
want = {
    "keys": [
        {
            "key": "verifier.pass",
            "unmatched_labels": 0,
            "unmatched_scores": 0,
            "judges": [
                {"tool": "verify_sql", "n": 10,
                 "spearman": 1.0, "kappa": 1.0, "tpr": 1.0, "tnr": 1.0}
            ],
            "overall": {"n": 10, "spearman": 1.0, "kappa": 1.0,
                        "tpr": 1.0, "tnr": 1.0},
        }
    ]
}
if got != want:
    print("agreement JSON mismatch:", file=sys.stderr)
    print("  got:  " + json.dumps(got, sort_keys=True), file=sys.stderr)
    print("  want: " + json.dumps(want, sort_keys=True), file=sys.stderr)
    sys.exit(1)
print("agreement: judge verify_sql n=10, spearman/kappa/tpr/tnr all exactly 1.0")
PY
record "$rc" "agreement JSON exact: judge verify_sql (provenance-grouped), perfect metrics"
sed 's|"bench-hermetic-sql-sql-degraded-r1","key":"verifier.pass","label":0|"bench-hermetic-sql-sql-degraded-r1","key":"verifier.pass","label":1|' \
	"$work/judge-labels.jsonl" >"$work/judge-labels-flip1.jsonl"
sed 's|"bench-hermetic-sql-sql-baseline-r1","key":"verifier.pass","label":1|"bench-hermetic-sql-sql-baseline-r1","key":"verifier.pass","label":0|' \
	"$work/judge-labels-flip1.jsonl" >"$work/judge-labels-flip2.jsonl"
run_json 0 "$work/judge-agree-flip1.json" \
	"one flipped label: kappa exactly 0.8 passes --min-kappa 0.8 (strict <)" -- \
	python3 -m catacomb_judge agreement --labels "$work/judge-labels-flip1.jsonl" \
	--runs-dir "$runs_judge" --key verifier.pass --json --min-kappa 0.8
rc=0
python3 - "$work/judge-agree-flip1.json" <<'PY' || rc=$?
import json, math, sys

key = json.load(open(sys.argv[1]))["keys"][0]
judge = key["judges"][0]
errs = []
if judge.get("tool") != "verify_sql" or judge.get("n") != 10:
    errs.append(f"judge row tool/n: {judge.get('tool')!r}/{judge.get('n')!r}")
want = {
    "kappa": 0.8,
    "spearman": 50 / math.sqrt(3750),
    "tpr": 5 / 6,
    "tnr": 1.0,
}
for name, wantv in sorted(want.items()):
    for row, where in ((judge, "judge"), (key["overall"], "overall")):
        if row.get(name) != wantv:
            errs.append(f"{where} {name}={row.get(name)!r} want {wantv!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"flip1: kappa exactly 0.8 (p_o 0.9, p_e 0.5), "
      f"spearman {judge['spearman']:.6f} = 50/sqrt(3750), tpr 5/6, tnr 1.0")
PY
record "$rc" "one-flip metrics pinned: kappa 0.8 exact, spearman 50/sqrt(3750), tpr 5/6"
run_json 1 "$work/judge-agree-flip2.json" \
	"two flipped labels: kappa fails --min-kappa 0.8 (exit 1)" -- \
	python3 -m catacomb_judge agreement --labels "$work/judge-labels-flip2.jsonl" \
	--runs-dir "$runs_judge" --key verifier.pass --json --min-kappa 0.8
rc=0
grep -Fq 'verifier.pass/verify_sql: kappa 0.600 < 0.8' "$work/judge-agree-flip2.json.stderr" || rc=1
record "$rc" "gate failure names the judge: verifier.pass/verify_sql kappa 0.600 < 0.8"
rc=0
python3 -c 'import json,sys; k=json.load(open(sys.argv[1]))["keys"][0]; sys.exit(0 if k["judges"][0]["kappa"] == 0.6000000000000001 and k["overall"]["kappa"] == 0.6000000000000001 else 1)' "$work/judge-agree-flip2.json" || rc=$?
record "$rc" "two-flip kappa pinned to the exact float 0.6000000000000001"

echo "== 16. SP5 judge panel: three-judge fixture -> mean/vote bytes -> regress =="
# Three fixture judges score judge.grounded on every baseline and degraded run.
# The values are dyadic — exact under any summation order and interpreter — and
# chosen so the tool-sorted baseline mean is exactly 1, pinning the
# int-preservation rule in the output bytes, while the degraded mean is 0.25;
# --vote binarizes at 0.5 into 1 (bits 1,1,1) vs 0 (bits 0,0,1). Group order in
# the output is sorted by (run_id, key), so the goldens list baseline-r1..r5 then
# degraded-r1..r5. The vote stream then closes the loop through the BUILT binary:
# regress --scores consumes it verbatim (tool/tool_version/panel_size ride along
# as tolerated provenance) and gates judge.grounded as a binary rate, 5/5 -> 0/5.
for judge in judge-a judge-b judge-c; do
	case "$judge" in
	judge-a) bval=0.5 dval=0 ;;
	judge-b) bval=1 dval=0.25 ;;
	judge-c) bval=1.5 dval=0.5 ;;
	esac
	for rep in 1 2 3 4 5; do
		printf '{"key":"judge.grounded","value":%s,"run_id":"bench-hermetic-sql-sql-baseline-r%d","tool":"%s"}\n' "$bval" "$rep" "$judge"
		printf '{"key":"judge.grounded","value":%s,"run_id":"bench-hermetic-sql-sql-degraded-r%d","tool":"%s"}\n' "$dval" "$rep" "$judge"
	done >"$work/$judge.jsonl"
done
for rep in 1 2 3 4 5; do
	printf '{"key":"judge.grounded","value":1,"run_id":"bench-hermetic-sql-sql-baseline-r%d","tool":"panel","tool_version":"0.1.0","panel_size":3}\n' "$rep"
done >"$work/panel-mean.golden"
for rep in 1 2 3 4 5; do
	printf '{"key":"judge.grounded","value":0.25,"run_id":"bench-hermetic-sql-sql-degraded-r%d","tool":"panel","tool_version":"0.1.0","panel_size":3}\n' "$rep"
done >>"$work/panel-mean.golden"
for rep in 1 2 3 4 5; do
	printf '{"key":"judge.grounded","value":1,"run_id":"bench-hermetic-sql-sql-baseline-r%d","tool":"panel","tool_version":"0.1.0","panel_size":3}\n' "$rep"
done >"$work/panel-vote.golden"
for rep in 1 2 3 4 5; do
	printf '{"key":"judge.grounded","value":0,"run_id":"bench-hermetic-sql-sql-degraded-r%d","tool":"panel","tool_version":"0.1.0","panel_size":3}\n' "$rep"
done >>"$work/panel-vote.golden"
run_json 0 "$work/panel-mean.out" \
	"panel mean over the three-judge fixture" -- \
	python3 -m catacomb_judge panel "$work/judge-a.jsonl" "$work/judge-b.jsonl" \
	"$work/judge-c.jsonl" --out "$work/panel-mean.jsonl"
rc=0
cmp -s "$work/panel-mean.jsonl" "$work/panel-mean.golden" || rc=1
record "$rc" "mean output byte-exact (baseline mean 1 int-preserved, degraded 0.25)"
run_json 0 "$work/panel-vote.out" \
	"panel --vote over the three-judge fixture" -- \
	python3 -m catacomb_judge panel "$work/judge-a.jsonl" "$work/judge-b.jsonl" \
	"$work/judge-c.jsonl" --vote --out "$work/panel-vote.jsonl"
rc=0
cmp -s "$work/panel-vote.jsonl" "$work/panel-vote.golden" || rc=1
record "$rc" "vote output byte-exact (baseline 1, degraded 0)"
run_json 1 "$work/regress-panel.json" \
	"regress consumes the panel vote stream (baseline vs degraded, exit 1)" -- \
	catacomb regress --runs-dir "$runs" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=degraded \
	--scores "$work/panel-vote.jsonl" --annotation judge.grounded:higher-better --json
rc=0
python3 - "$work/regress-panel.json" <<'PY' || rc=$?
import json, sys

hits = [
    f for f in json.load(open(sys.argv[1])).get("findings", [])
    if f.get("metric") == "ann:judge.grounded"
]
errs = []
if len(hits) != 1:
    errs.append(f"want exactly 1 ann:judge.grounded finding, got {len(hits)}")
for f in hits:
    got = (f.get("scope"), f.get("verdict"), f.get("detail"))
    if got != ("total", "regression", "ones 5/5 -> 0/5"):
        errs.append(f"scope/verdict/detail {got!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("panel loop closed: ann:judge.grounded total regression (ones 5/5 -> 0/5)")
PY
record "$rc" "panel vote gates as ann:judge.grounded total regression through the built binary"

echo "== 17. replay: deterministic graph summary over the fixture transcript =="
# The reducer input is fully pinned: a fixed session id is sed'd into the fixture
# template (agent.sh generates a fresh uuid per cell; here determinism matters more
# than uniqueness), so two replays of the same file must print byte-identical
# stdout. Drift warnings go to stderr and are deliberately not compared.
sed 's/__SESSION_ID__/hermetic-replay/g' "$work/transcript.jsonl.tmpl" >"$work/replay.jsonl"
run_json 0 "$work/replay1.out" "replay fixture transcript (1st run)" -- \
	catacomb replay "$work/replay.jsonl"
run_json 0 "$work/replay2.out" "replay fixture transcript (2nd run)" -- \
	catacomb replay "$work/replay.jsonl"
rc=0
grep -Eq 'replayed .+ -> [1-9][0-9]* nodes, [0-9]+ edges' "$work/replay1.out" || rc=1
record "$rc" "replay prints a non-empty summary (>= 1 node)"
rc=0
cmp -s "$work/replay1.out" "$work/replay2.out" || rc=1
record "$rc" "replay stdout byte-identical across two runs"

echo "== 18. baseline list -> bundle round-trip -> rm on the step-11 trends store =="
# Reuses $work/trends.db, where step 11 pinned the hermetic-trends baseline over the
# 5 baseline-variant cells. list --json must show exactly that baseline; then the
# bundle round-trip proves the CI hand-off story before rm tears the source down:
# export twice (byte-identical bundles), import into a FRESH db + runs dir (the
# ephemeral-runner simulation; import rewrites the baseline's RunsDir to the fresh
# dir, so regress there must NOT warn about a recorded runs-dir), copy the candidate
# cells over (the runner benches its own candidate; here the recorded cells stand in),
# and re-run the step-11 comparisons against the imported baseline — same verdicts,
# same finding set. A one-byte flip in a bundle copy must be refused (exit 2) with
# nothing landed. Finally rm deletes the source baseline, the follow-up list must
# come back empty (the store encodes an empty baselines table as JSON null, so the
# assertion accepts null or []), and the imported copy must survive the source rm.
run_json 0 "$work/baseline-list.json" "baseline list --json (trends store)" -- \
	catacomb baseline list --db "$work/trends.db" --json
rc=0
python3 - "$work/baseline-list.json" <<'PY' || rc=$?
import json, sys

bs = json.load(open(sys.argv[1])) or []
errs = []
if len(bs) != 1:
    errs.append(f"want exactly 1 baseline, got {len(bs)}")
b = bs[0] if bs else {}
if b.get("name") != "hermetic-trends":
    errs.append(f"name={b.get('name')!r} want 'hermetic-trends'")
if len(b.get("run_ids") or []) != 5:
    errs.append(f"run_ids={b.get('run_ids')!r} want the 5 baseline cells")
if b.get("selector") != {"basket": "hermetic-sql", "variant": "baseline"}:
    errs.append(f"selector={b.get('selector')!r} want basket=hermetic-sql,variant=baseline")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("baseline list: hermetic-trends, 5 runs, selector basket=hermetic-sql,variant=baseline")
PY
record "$rc" "baseline list shows hermetic-trends (5 runs, recorded selector)"
bundle_dir="$work/bundle"
fresh="$work/fresh"
mkdir -p "$bundle_dir" "$fresh"
run_expect 0 "baseline export hermetic-trends (bundle 1)" -- \
	catacomb baseline export hermetic-trends --db "$work/trends.db" \
	--runs-dir "$runs" --out "$bundle_dir/golden.tar.gz"
run_expect 0 "baseline export hermetic-trends (bundle 2)" -- \
	catacomb baseline export hermetic-trends --db "$work/trends.db" \
	--runs-dir "$runs" --out "$bundle_dir/golden2.tar.gz"
rc=0
cmp -s "$bundle_dir/golden.tar.gz" "$bundle_dir/golden2.tar.gz" || rc=1
record "$rc" "two exports of the same baseline are byte-identical"
run_json 0 "$work/bundle-import.out" "baseline import into a fresh db + runs dir" -- \
	catacomb baseline import "$bundle_dir/golden.tar.gz" \
	--db "$fresh/trends.db" --runs-dir "$fresh/runs"
rc=0
grep -q "imported baseline hermetic-trends: 5 runs" "$work/bundle-import.out" || rc=1
record "$rc" "import lands the 5 pinned baseline cells"
cp -R "$runs"/bench-hermetic-sql-sql-degraded-r? \
	"$runs"/bench-hermetic-sql-sql-baseline2-r? "$fresh/runs/"
run_json 1 "$work/fresh-degraded.json" \
	"imported baseline still gates degraded (exit 1)" -- \
	catacomb regress --runs-dir "$fresh/runs" --db "$fresh/trends.db" \
	--baseline name:hermetic-trends \
	--candidate label:basket=hermetic-sql,variant=degraded --json
run_json 0 "$work/fresh-AvA.json" \
	"imported baseline keeps A-vs-A clean (exit 0)" -- \
	catacomb regress --runs-dir "$fresh/runs" --db "$fresh/trends.db" \
	--baseline name:hermetic-trends \
	--candidate label:basket=hermetic-sql,variant=baseline2 \
	--metric-rel-delta 0.5 --json
rc=0
! grep -q "recorded runs-dir" "$work/fresh-degraded.json.stderr" "$work/fresh-AvA.json.stderr" || rc=1
record "$rc" "no runs-dir mismatch warning (import rewrote RunsDir to the fresh dir)"
rc=0
python3 - "$work/record-degraded.json" "$work/fresh-degraded.json" \
	"$work/record-AvA.json" "$work/fresh-AvA.json" <<'PY' || rc=$?
import json, sys

def proj(p):
    r = json.load(open(p))
    return (r.get("overall_verdict"), r.get("regressions"),
            sorted((f.get("scope"), f.get("metric"), f.get("verdict"))
                   for f in r.get("findings", [])))

errs = []
for what, src_p, fresh_p in (("degraded", sys.argv[1], sys.argv[2]),
                             ("A-vs-A", sys.argv[3], sys.argv[4])):
    src, fresh = proj(src_p), proj(fresh_p)
    if src != fresh:
        errs.append(f"{what}: source {src} != fresh {fresh}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("verdict parity: fresh-import reports match the step-11 source reports")
PY
record "$rc" "fresh-import verdicts + findings match the source-store reports"
cp "$bundle_dir/golden.tar.gz" "$bundle_dir/tampered.tar.gz"
python3 - "$bundle_dir/tampered.tar.gz" <<'PY'
import sys

with open(sys.argv[1], "r+b") as f:
    f.seek(0, 2)
    mid = f.tell() // 2
    f.seek(mid)
    b = f.read(1)[0]
    f.seek(mid)
    f.write(bytes([b ^ 0xFF]))
PY
run_expect 2 "tampered bundle refused (one flipped byte, exit 2)" -- \
	catacomb baseline import "$bundle_dir/tampered.tar.gz" \
	--db "$fresh/tamper.db" --runs-dir "$fresh/tamper-runs"
rc=0
if [ -e "$fresh/tamper.db" ]; then
	tamper_rows="$(sqlite3 "$fresh/tamper.db" 'SELECT count(*) FROM baselines;' 2>/dev/null || echo 0)"
	[ "$tamper_rows" = "0" ] || rc=1
fi
[ -z "$(find "$fresh/tamper-runs" -mindepth 1 2>/dev/null)" ] || rc=1
record "$rc" "tampered import lands no baseline row and no runs"
run_expect 0 "baseline rm hermetic-trends" -- \
	catacomb baseline rm hermetic-trends --db "$work/trends.db"
run_json 0 "$work/baseline-list2.json" "baseline list --json after rm" -- \
	catacomb baseline list --db "$work/trends.db" --json
rc=0
python3 -c 'import json,sys; sys.exit(0 if not json.load(open(sys.argv[1])) else 1)' "$work/baseline-list2.json" || rc=$?
record "$rc" "baseline list no longer shows hermetic-trends after rm"
run_json 0 "$work/baseline-list-fresh.json" "baseline list --json (fresh store, after source rm)" -- \
	catacomb baseline list --db "$fresh/trends.db" --json
rc=0
python3 -c 'import json,sys; bs=json.load(open(sys.argv[1])) or []; sys.exit(0 if [b.get("name") for b in bs]==["hermetic-trends"] else 1)' "$work/baseline-list-fresh.json" || rc=$?
record "$rc" "imported baseline survives the source-store rm"

echo "== 19. mcp stdio session: initialize, tools/list, tools/call mark =="
# The server answers one newline-delimited JSON-RPC response per request and exits 0
# on stdin EOF, so a fixed 3-request script is fully deterministic (Go sorts map keys
# when marshaling). ids must round-trip in order, every response must carry a result
# and no error, tools/list must expose the mark tool, and the mark call must ack.
printf '%s\n' \
	'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"hermetic-e2e","version":"0"}}}' \
	'{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
	'{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mark","arguments":{"name":"impl","boundary":"start"}}}' \
	>"$work/mcp-requests.jsonl"
run_json 0 "$work/mcp.out" "mcp stdio session (3 requests, exit on EOF)" -- \
	catacomb mcp <"$work/mcp-requests.jsonl"
rc=0
python3 - "$work/mcp.out" <<'PY' || rc=$?
import json, sys

lines = [l for l in open(sys.argv[1]).read().splitlines() if l.strip()]
errs = []
if len(lines) != 3:
    errs.append(f"want exactly 3 response lines, got {len(lines)}")
resps = []
for i, l in enumerate(lines):
    try:
        resps.append(json.loads(l))
    except ValueError as e:
        errs.append(f"line {i + 1} is not JSON: {e}")
for want_id, r in zip((1, 2, 3), resps):
    if r.get("jsonrpc") != "2.0" or r.get("id") != want_id:
        errs.append(f"id {want_id}: jsonrpc={r.get('jsonrpc')!r} id={r.get('id')!r}")
    if "error" in r or "result" not in r:
        errs.append(f"id {want_id}: error={r.get('error')!r}, result present: {'result' in r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("mcp framing: 3 responses, ids 1..3 round-trip, result present, no error")
PY
record "$rc" "mcp responses well-formed (3 replies, matching ids, result, no error)"
rc=0
python3 - "$work/mcp.out" <<'PY' || rc=$?
import json, sys

lines = [l for l in open(sys.argv[1]).read().splitlines() if l.strip()]
init, tools = (json.loads(l).get("result") or {} for l in lines[:2])
errs = []
if init.get("protocolVersion") != "2025-06-18":
    errs.append(f"protocolVersion={init.get('protocolVersion')!r} want the client's 2025-06-18")
if (init.get("serverInfo") or {}).get("name") != "catacomb":
    errs.append(f"serverInfo={init.get('serverInfo')!r} want name 'catacomb'")
if "tools" not in (init.get("capabilities") or {}):
    errs.append(f"capabilities={init.get('capabilities')!r} lack tools")
names = [t.get("name") for t in tools.get("tools") or []]
if names != ["mark"]:
    errs.append(f"tools/list names={names!r} want exactly ['mark']")
mark = (tools.get("tools") or [{}])[0]
if sorted((mark.get("inputSchema") or {}).get("required") or []) != ["boundary", "name"]:
    errs.append(f"mark inputSchema required={mark.get('inputSchema')!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("mcp: initialize echoes protocol 2025-06-18 (serverInfo catacomb), tools/list exposes mark")
PY
record "$rc" "mcp initialize + tools/list: serverInfo catacomb, mark tool exposed"
rc=0
python3 - "$work/mcp.out" <<'PY' || rc=$?
import json, sys

lines = [l for l in open(sys.argv[1]).read().splitlines() if l.strip()]
res = json.loads(lines[2]).get("result") or {}
content = res.get("content") or [{}]
errs = []
if res.get("isError") is not False:
    errs.append(f"isError={res.get('isError')!r} want False")
if content[0].get("type") != "text" or content[0].get("text") != "marked start impl":
    errs.append(f"content={content!r} want text 'marked start impl'")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("mcp: tools/call mark acked 'marked start impl' with isError false")
PY
record "$rc" "mcp tools/call mark acks (marked start impl, isError false)"

echo "== 20. SP-W workspace isolation: fresh dirs, patch handover, stamps, teardown =="
# A separate 3-rep basket (ws-basket.yaml) proves the workspace lifecycle without
# touching the sql evidence. The workspace cmd copies the agent into the fresh
# cell dir and materializes the patch handed over via CATACOMB_PATCH (visible to
# the workspace cmd ONLY) as applied.txt; the agent exits 7 if `marker` already
# exists in its cwd and 8 if CATACOMB_PATCH leaked into its env — with a fresh
# dir per cell neither guard ever trips (against shared-dir behavior rep 2 would
# die with 7), so the manifest must hold exactly three exit-0 cells. Teardown
# appends one line per cell to wslog/teardown.log (fresh context after every
# cell), and default cleanup leaves the --workspaces-dir base empty.
wsruns="$work/wsruns"
wsroot="$work/wsroot"
mkdir -p "$wsroot"
run_expect 0 "ws bench (3 reps, fresh dir each)" -- \
	catacomb bench "$work/ws-basket.yaml" --projects-dir "$work/projects" \
	--runs-dir "$wsruns" --manifest "$work/ws.manifest.jsonl" --workspaces-dir "$wsroot"
rc=0
[ "$(grep -c '"exit_code":0' "$work/ws.manifest.jsonl")" -eq 3 ] || rc=1
! grep -qE '"exit_code":[78]' "$work/ws.manifest.jsonl" || rc=1
record "$rc" "isolation guards never tripped (3 exit-0 cells; no reused dir, no leaked patch var)"
rc=0
python3 -c 'import json,sys; e=json.loads(open(sys.argv[1]).readline()); sys.exit(0 if e["key"]=="verifier.pass" and e["value"]==1 else 1)' \
	"$wsruns/bench-hermetic-ws-ws-only-r1/scores.jsonl" || rc=$?
record "$rc" "verify_ws scored the handover through the SDK (verifier.pass 1 on r1)"
rc=0
[ "$(grep -c torn "$work/wslog/teardown.log")" -eq 3 ] || rc=1
record "$rc" "teardown ran per cell (3 lines)"
rc=0
[ -z "$(ls -A "$wsroot")" ] || rc=1
record "$rc" "workspace dirs removed after teardown"
wsmeta="$wsruns/bench-hermetic-ws-ws-only-r1/meta.json"
wantsha="$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$work/fix.patch")"
rc=0
python3 - "$wsmeta" "$wantsha" <<'PY' || rc=$?
import json, sys

ws = (json.load(open(sys.argv[1])).get("env") or {}).get("workspace") or {}
errs = []
if ws.get("rev") != "seed-r1":
    errs.append(f"rev={ws.get('rev')!r} want 'seed-r1'")
if ws.get("patch_sha256") != sys.argv[2]:
    errs.append(f"patch_sha256={ws.get('patch_sha256')!r} want {sys.argv[2]}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"workspace stamp: rev=seed-r1, patch_sha256={sys.argv[2][:12]}... (fix.patch bytes)")
PY
record "$rc" "meta.json stamps workspace rev + patch sha256"
rc=0
cmp -s "$wsruns/bench-hermetic-ws-ws-only-r1/artifacts/applied.txt" "$work/fix.patch" || rc=1
record "$rc" "patch handover: captured applied.txt == fix.patch bytes"
run_expect 0 "ws bench --keep-workspaces" -- \
	catacomb bench "$work/ws-basket.yaml" --projects-dir "$work/projects" \
	--runs-dir "$work/wsruns2" --manifest "$work/ws2.manifest.jsonl" \
	--workspaces-dir "$wsroot" --keep-workspaces
rc=0
[ -n "$(ls -A "$wsroot")" ] || rc=1
[ "$(grep -c torn "$work/wslog/teardown.log")" -eq 6 ] || rc=1
record "$rc" "--keep-workspaces keeps dirs, teardown still ran (6 lines)"
# Seeded workspace failure: the fail twin's workspace cmd is `exit 3`, so under
# --fail-fast the process must exit 1 with the fail-fast error on stderr, the
# sole manifest entry must record the workspace exit code with the "workspace
# failed" note, and cleanup must still run: teardown fires a 7th torn line and
# the workspace dir is removed even though the cmd never succeeded.
wsrootfail="$work/wsroot-fail"
mkdir -p "$wsrootfail"
run_json 1 "$work/ws-fail.out" \
	"workspace cmd failure gates under --fail-fast (exit 1)" -- \
	catacomb bench "$work/ws-basket-fail.yaml" --projects-dir "$work/projects" \
	--runs-dir "$work/wsruns3" --manifest "$work/ws3.manifest.jsonl" \
	--workspaces-dir "$wsrootfail" --fail-fast
rc=0
grep -q 'fail-fast' "$work/ws-fail.out.stderr" || rc=1
record "$rc" "fail-fast error names the flag on stderr"
rc=0
grep -q '"exit_code":3' "$work/ws3.manifest.jsonl" || rc=1
grep -q '"note":"workspace failed"' "$work/ws3.manifest.jsonl" || rc=1
record "$rc" "manifest records the workspace exit code + 'workspace failed' note"
rc=0
[ "$(grep -c torn "$work/wslog/teardown.log")" -eq 7 ] || rc=1
[ -z "$(ls -A "$wsrootfail")" ] || rc=1
record "$rc" "teardown + removal still run when the workspace cmd fails"
# Patch-free offline verify (last: the --keep-workspaces and seeded-failure benches
# above still load fix.patch). Offline verification replays verifiers over captured
# evidence and never touches the patch, so `catacomb verify` loads the basket without
# resolving workspace.patch — with fix.patch moved away the pass must still succeed,
# leave every ws scores.jsonl byte-identical (step-4-style idempotency), and flip
# verify.json to mode offline. The patch is restored afterward for cleanliness.
wssnap="$work/ws-snap"
mkdir -p "$wssnap"
for d in "$wsruns"/*/; do
	cp "$d/scores.jsonl" "$wssnap/$(basename "$d").scores"
done
mv "$work/fix.patch" "$work/fix.patch.gone"
run_expect 0 "offline verify over ws runs with the patch file gone" -- \
	catacomb verify "$work/ws-basket.yaml" --runs-dir "$wsruns"
rc=0
for d in "$wsruns"/*/; do
	cmp -s "$d/scores.jsonl" "$wssnap/$(basename "$d").scores" || rc=1
done
record "$rc" "patch-free offline verify leaves every ws scores.jsonl byte-identical"
rc=0
python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]+"/verify.json"))["mode"]=="offline" else 1)' \
	"$wsruns/bench-hermetic-ws-ws-only-r1" || rc=$?
record "$rc" "ws verify.json mode flips to offline after the patch-free re-verify"
mv "$work/fix.patch.gone" "$work/fix.patch"

echo "== 21. import path: ingest a recorded session -> offline verify -> regress (no agent spawn) =="
# `catacomb import` ingests an already-finished transcript into a bench-cell-shaped
# evidence dir so verify/regress run over it unchanged with zero agent spawn. It
# reuses the step-2 bench transcripts (still under projects/hermetic, keyed by the
# session_id bench recorded in each meta.json) — nothing is re-run, no claude is
# invoked — and writes into a FRESH runs dir, disturbing no earlier evidence. import
# runs no task cmd so it captures no artifacts; the sql verifier reads out/result.csv
# and therefore cannot re-verify imported evidence, so basket-import.yaml swaps only
# the verify cmd (name/tasks/variants unchanged, so labels match the sql cells) for
# verify_import.py, the artifact-free twin that reads the ingested session.jsonl
# straight from the evidence dir. Two hermetic sessions are imported under variants
# baseline/degraded (rep 1 each); both derive from the same fixed transcript template
# so every audited axis is byte-identical across the pair, and regress produces a
# verdict while gating nothing (exit 0, zero regressions).
importruns="$work/importruns"
cp "$here/verify_import.py" "$work/"
sed 's|verify_sql.py|verify_import.py|' "$work/basket.yaml" >"$work/basket-import.yaml"
imp_sid_baseline="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["session_id"])' "$base1_dir/meta.json")"
imp_sid_degraded="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["session_id"])' "$runs/bench-hermetic-sql-sql-degraded-r1/meta.json")"
run_expect 0 "import baseline session into fresh evidence" -- \
	catacomb import "$work/basket-import.yaml" --task sql --variant baseline \
	--session-id "$imp_sid_baseline" --projects-dir "$work/projects" --runs-dir "$importruns"
run_expect 0 "import degraded session into fresh evidence" -- \
	catacomb import "$work/basket-import.yaml" --task sql --variant degraded \
	--session-id "$imp_sid_degraded" --projects-dir "$work/projects" --runs-dir "$importruns"
rc=0
for v in baseline degraded; do
	d="$importruns/import-hermetic-sql-sql-$v-r1"
	if [ ! -f "$d/meta.json" ] || [ ! -f "$d/session.jsonl" ]; then rc=1; fi
done
record "$rc" "import wrote a bench-cell evidence dir (meta.json + session.jsonl) per variant"
run_expect 0 "catacomb verify over imported evidence (offline, no agent)" -- \
	catacomb verify "$work/basket-import.yaml" --runs-dir "$importruns"
imp_base="$importruns/import-hermetic-sql-sql-baseline-r1"
rc=0
python3 - "$imp_base" <<'PY' || rc=$?
import json, os, sys

d = sys.argv[1]
errs = []
try:
    if "verifier.pass" not in open(os.path.join(d, "scores.jsonl")).read():
        errs.append("scores.jsonl lacks verifier.pass")
except OSError as e:
    errs.append(f"scores.jsonl unreadable: {e}")
try:
    v = json.load(open(os.path.join(d, "verify.json")))
    if v.get("mode") != "offline":
        errs.append(f"verify.json mode={v.get('mode')!r} want 'offline'")
    if v.get("error"):
        errs.append(f"verify.json error non-empty: {v.get('error')!r}")
except OSError as e:
    errs.append(f"verify.json unreadable: {e}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("imported evidence re-verified: scores(verifier.pass), verify.json mode=offline, no error")
PY
record "$rc" "offline verify over imported evidence: verifier.pass scored, verify.json mode=offline no error"
run_json 0 "$work/regress-import.json" \
	"regress over imported evidence produces a verdict (exit 0)" -- \
	catacomb regress --runs-dir "$importruns" \
	--baseline label:basket=hermetic-sql,variant=baseline \
	--candidate label:basket=hermetic-sql,variant=degraded --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r.get("overall_verdict") and r.get("regressions")==0 else 1)' "$work/regress-import.json" || rc=$?
record "$rc" "regress over imported cells yields an overall verdict with zero regressions"

echo "== 22. gate self-check: calibrate over one variant's fabricated evidence =="
# ADR-0034: `catacomb calibrate` audits ONE variant's recorded runs on the shipped
# verdict path — a time-ordered A/A half-split (drift detector) plus leave-one-out
# influence — and exits 0 on every rendered self-check (2 only on operational
# errors; it never gates). Four groups are fabricated from the fixture transcript
# template in bench-evidence shape (labels incl. task, one uuid session per run,
# marker window pinned to the template's own 4000 ms mark window so duration_ms is
# byte-identical), mirroring the step-12 plant: drifted runs get usage injected
# into every assistant turn (5 x 1200 = 6000 tokens_out) under a model the pricer
# cannot price, so tokens_out is the single moving axis. Because every group spans
# one task, the paired tier always reports insufficient below paired-min-tasks 5 —
# a clean A/A therefore reads split verdict "insufficient" (honest tier
# disclosure, nothing gates, no drift findings), never a fabricated "ok"; seeded
# drift reads "regression" with drift findings naming the metric. The influence
# group (3 clean + 4 drifted) flips on exactly the first-half clean runs: dropping
# one leaves a mixed [clean,clean,drift] baseline whose IQR band (1.5 x 6000)
# swallows the drifted half, so the verdict decays regression -> insufficient,
# while dropping a drifted run changes nothing (3 clean vs 3 drifted still gates).
calruns="$work/calruns"
mkdir "$calruns"
cat >"$work/cal-seed-meta.json" <<'JSON'
{"run_id":"seed","task":"sql","variant":"seed","rep":0,"session_id":"seed","labels":{"basket":"hermetic-cal","task":"sql","variant":"seed","rep":"0"},"exit_code":0,"basket_hash":"hermetic-cal","marker_name":"task:sql","marker_start":"2026-06-20T10:00:03Z","marker_end":"2026-06-20T10:00:07Z","finished_at":"2026-06-20T10:00:08Z"}
JSON
cal_stage() { # <run-id> <variant> <rep> <drift 0|1>
	local rid="$1" variant="$2" rep="$3" drift="$4"
	local dir="$calruns/$rid"
	mkdir "$dir"
	local sid
	sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
	if [ "$drift" -eq 0 ]; then
		sed "s/__SESSION_ID__/$sid/g" "$work/transcript.jsonl.tmpl" >"$dir/session.jsonl"
	else
		sed -e "s/__SESSION_ID__/$sid/g" \
			-e 's|"model":"claude-opus-4-8","content":|"model":"hermetic-calibrate-drift","usage":{"output_tokens":1200},"content":|g' \
			"$work/transcript.jsonl.tmpl" >"$dir/session.jsonl"
	fi
	python3 - "$work/cal-seed-meta.json" "$dir/meta.json" "$rid" "$sid" "$variant" "$rep" <<'PY'
import json, sys

meta = json.load(open(sys.argv[1]))
meta["run_id"] = sys.argv[3]
meta["session_id"] = sys.argv[4]
meta["variant"] = sys.argv[5]
meta["rep"] = int(sys.argv[6])
meta["labels"]["variant"] = sys.argv[5]
meta["labels"]["rep"] = sys.argv[6]
json.dump(meta, open(sys.argv[2], "w"))
PY
}
for i in 0 1 2 3 4 5; do cal_stage "cal-aa-r$i" cal-aa "$i" 0; done
for i in 0 1 2; do cal_stage "cal-drift-r$i" cal-drift "$i" 0; done
for i in 3 4 5; do cal_stage "cal-drift-r$i" cal-drift "$i" 1; done
for i in 0 1 2 3; do cal_stage "cal-short-r$i" cal-short "$i" 0; done
for i in 0 1 2; do cal_stage "cal-il-r$i" cal-il "$i" 0; done
for i in 3 4 5 6; do cal_stage "cal-il-r$i" cal-il "$i" 1; done
run_json 0 "$work/calibrate-aa.json" \
	"calibrate clean 6-run group (exit 0)" -- \
	catacomb calibrate --runs-dir "$calruns" --group label:variant=cal-aa --format json
rc=0
python3 - "$work/calibrate-aa.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
errs = []
for field, want in (("runs", 6), ("min_support", 3), ("sufficient", True)):
    if rep.get(field) != want:
        errs.append(f"{field}={rep.get(field)!r} want {want!r}")
if rep.get("run_ids") != [f"cal-aa-r{i}" for i in range(6)]:
    errs.append(f"run_ids={rep.get('run_ids')!r} want cal-aa-r0..r5 in time order")
if (rep.get("thresholds") or {}).get("min_support") != 3:
    errs.append(f"thresholds={rep.get('thresholds')!r} want an echo with min_support 3")
split = rep.get("split") or {}
for field, want in (("first_n", 3), ("second_n", 3), ("verdict", "insufficient")):
    if split.get(field) != want:
        errs.append(f"split.{field}={split.get(field)!r} want {want!r}")
if split.get("notes") != ["matched 1 task below paired min 5"]:
    errs.append(f"split.notes={split.get('notes')!r} want the paired-tier insufficiency detail")
if split.get("drift"):
    errs.append(f"clean group reports drift: {split['drift']!r}")
inf = rep.get("influence") or {}
if inf.get("evaluated") is not False or inf.get("detail") != "leave-one-out needs k>=7 runs (have 6)":
    errs.append(f"influence={inf!r} want unevaluated with the needs-k>=7 detail")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("clean A/A: sufficient, 3v3 split verdict insufficient (single-task paired tier), no drift")
PY
record "$rc" "clean 6-run group: sufficient, 3v3 split, no drift, influence needs k>=7"
run_json 0 "$work/calibrate-drift.json" \
	"calibrate seeded second-half drift (exit 0: calibrate never gates)" -- \
	catacomb calibrate --runs-dir "$calruns" --group label:variant=cal-drift --format json
rc=0
python3 - "$work/calibrate-drift.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
split = rep.get("split") or {}
drift = split.get("drift") or []
errs = []
if rep.get("runs") != 6 or rep.get("sufficient") is not True:
    errs.append(f"runs={rep.get('runs')!r} sufficient={rep.get('sufficient')!r} want 6/True")
if split.get("verdict") != "regression":
    errs.append(f"split.verdict={split.get('verdict')!r} want 'regression'")
want_total = {"scope": "total", "metric": "tokens_out", "verdict": "regression",
              "baseline": 0, "candidate": 6000}
if not drift:
    errs.append("drift findings empty despite the seeded tokens_out shift")
elif drift[0] != want_total:
    errs.append(f"drift[0]={drift[0]!r} want {want_total!r}")
for d in drift:
    if d.get("metric") != "tokens_out":
        errs.append(f"drift finding on {d.get('metric')!r} (scope {d.get('scope')!r}) — only tokens_out moved")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"drift: A/A regression, {len(drift)} finding(s) all tokens_out, total 0 -> 6000")
PY
record "$rc" "seeded drift: A/A regression, drift findings name tokens_out (total 0 -> 6000)"
run_json 0 "$work/calibrate-short.json" \
	"calibrate 4-run group stays honest (exit 0)" -- \
	catacomb calibrate --runs-dir "$calruns" --group label:variant=cal-short --format json
rc=0
python3 - "$work/calibrate-short.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
errs = []
if rep.get("runs") != 4 or rep.get("sufficient") is not False:
    errs.append(f"runs={rep.get('runs')!r} sufficient={rep.get('sufficient')!r} want 4/False")
if rep.get("detail") != "self-check needs k>=6 runs (have 4)":
    errs.append(f"detail={rep.get('detail')!r} want the needed-k line")
for key in ("split", "influence"):
    if key in rep:
        errs.append(f"{key} present on an insufficient report: {rep[key]!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("short group: sufficient false, detail names k>=6, no split/influence blocks")
PY
record "$rc" "4-run group: sufficient false with the needed-k detail, no split emitted"
run_json 0 "$work/calibrate-il.json" \
	"calibrate 7-run influence group (exit 0)" -- \
	catacomb calibrate --runs-dir "$calruns" --group label:variant=cal-il --format json
rc=0
python3 - "$work/calibrate-il.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
split = rep.get("split") or {}
inf = rep.get("influence") or {}
errs = []
if rep.get("runs") != 7 or (split.get("first_n"), split.get("second_n")) != (3, 4):
    errs.append(f"runs={rep.get('runs')!r} split={split.get('first_n')!r}v{split.get('second_n')!r} want 7 as 3v4")
if split.get("verdict") != "regression":
    errs.append(f"split.verdict={split.get('verdict')!r} want 'regression'")
if inf.get("evaluated") is not True:
    errs.append(f"influence.evaluated={inf.get('evaluated')!r} want True")
want = [{"dropped_index": i, "run_id": f"cal-il-r{i}", "from": "regression", "to": "insufficient"} for i in (0, 1, 2)]
if inf.get("flipping_runs") != want:
    errs.append(f"flipping_runs={inf.get('flipping_runs')!r} want {want!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("influence: dropping any first-half clean run flips regression -> insufficient (3 flips)")
PY
record "$rc" "influence: flipping_runs pins exactly the three first-half clean runs"
run_json 2 "$work/calibrate-none.out" \
	"calibrate unknown group is operational (exit 2)" -- \
	catacomb calibrate --runs-dir "$calruns" --group label:variant=cal-none --format json
rc=0
grep -Fq 'calibrate selector "label:variant=cal-none": selector matched no runs' \
	"$work/calibrate-none.out.stderr" || rc=1
record "$rc" "unknown group error carries the calibrate verb prefix"

echo "== 23. production scenarios (subagent/skill/mcp) =="
prod_rc=0
WORK="$work/prod" bash "$here/prod/run.sh" || prod_rc=$?
record "$prod_rc" "hermetic production scenarios (prod/run.sh) all pass"

echo "== summary =="
if [ "${#failures[@]}" -eq 0 ]; then
	echo "HERMETIC E2E: PASS — all assertions held (workspace: $work)"
	exit 0
fi
echo "HERMETIC E2E: FAIL — ${#failures[@]} assertion(s) failed:"
for f in "${failures[@]}"; do
	echo "  - $f"
done
echo "workspace kept for debugging: $work"
exit 1
