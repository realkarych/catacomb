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
#   11 SP3 pareto (re-records the step-5/6 comparisons into a fresh store).
# Steps 5/6 need the clean offline scores produced by step 4. Step 7 points verify at
# /usr/bin/false: a failing verifier writes NO scores (so scores.jsonl survives intact),
# but stamps verify.json with an error and the broken config's hash — so it runs after
# the gates and is immediately followed by a clean re-verify that restores (and asserts)
# the offline state. Step 8 reads only scores.jsonl; step 9 re-reads the step-5 report
# JSON from disk. Steps 10/11 are SP3: step 10 re-reads meta.json exactly as bench
# wrote it (offline verify rewrites verify.json and scores.jsonl, never meta.json), and
# step 11 re-runs the step-5/6 comparisons with --record against a fresh store and
# asserts the pareto view over that history.
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
export HERMETIC_DB="$work/e2e.db"
export HERMETIC_PROJECTS="$work/projects"
export HERMETIC_TDIR="$work"
export PYTHONPATH="$repo/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"

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
# has no top-level "version" field, so claude_code_version MUST be absent.
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
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("env: model_id=claude-opus-4-8 (fixture model), claude_code_version absent")
PY
record "$rc" "meta.json env: model_id pinned to the fixture model, claude_code_version absent"

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
