#!/usr/bin/env bash
# E2E live gate — the PV-6b calibration methodology as a self-asserting driver.
#
# Runs three heterogeneous live `claude -p` baskets through `catacomb bench` and
# then exercises the full offline pipeline against the real evidence:
#   - every A-vs-A control must NOT gate (zero false positives), and
#   - a seeded checkpoint-presence regression, a seeded continuous (tokens_out)
#     regression, AND a seeded verifier-contract regression (a wrong SQL result
#     fails verification) MUST each gate, attributed to the swapped instruction.
# It also smoke-tests baseline pin/record/trends, diff/subgraph/export, and the
# external-scores path — all on the live evidence.
#
# See docs/reviews/2026-07-08-pv6b-live-calibration.md for the methodology.
#
# Cost: ~$1.7 of real API spend (60 bench cells; checkpoint + SQL tasks on sonnet).
#
# Environment:
#   CATACOMB_BIN    catacomb binary to drive with (default: `catacomb` on PATH).
#                   Its directory is also prepended to PATH so the in-run MCP
#                   server (mcp.json's `catacomb mcp`) resolves the same binary.
#   E2E_ARTIFACTS   directory for manifests + every regress --json (default:
#                   ./e2e-artifacts, resolved against the invocation cwd).
#   ANTHROPIC_API_KEY   Anthropic auth for `claude -p`; either this (API billing) or
#                       CLAUDE_CODE_OAUTH_TOKEN is required (checked by the caller/
#                       workflow). If both are set, ANTHROPIC_API_KEY takes precedence.
#   CLAUDE_CODE_OAUTH_TOKEN   Claude Pro/Max subscription auth for `claude -p`, an
#                       alternative to ANTHROPIC_API_KEY (generate: `claude setup-token`).
#
# The bench cells resolve `./presence.sh` / `./answer.sh` and `mcp.json` relative to
# the e2e directory, so this driver cd's into its own directory before invoking bench
# (the presence and continuous baskets declare `dir: .`). The SQL basket instead runs
# each cell in a fresh per-cell workspace whose setup cmd copies `./sql-live.sh` and
# `./verify_sql.py` from E2E_DIR (exported below). All other paths are absolute, so
# the cd does not affect them.
set -euo pipefail

e2e_dir="$(cd "$(dirname "$0")" && pwd)"

fatal() {
	printf 'e2e: FATAL: %s\n' "$1" >&2
	exit 2
}

# --- artifacts dir: resolve against the invocation cwd, keep absolute ---------
artifacts="${E2E_ARTIFACTS:-e2e-artifacts}"
mkdir -p "$artifacts" || fatal "cannot create artifacts dir: $artifacts"
artifacts="$(cd "$artifacts" && pwd)"

# --- required binaries --------------------------------------------------------
catacomb_bin="${CATACOMB_BIN:-catacomb}"
"$catacomb_bin" version >/dev/null 2>&1 ||
	fatal "catacomb is not runnable — install it, add it to PATH, or set CATACOMB_BIN"
catacomb_abs="$(command -v "$catacomb_bin" 2>/dev/null || true)"
[ -n "$catacomb_abs" ] || fatal "cannot resolve the catacomb binary path"
PATH="$(cd "$(dirname "$catacomb_abs")" && pwd):$PATH"
export PATH
command -v catacomb >/dev/null 2>&1 ||
	fatal "catacomb must resolve on PATH for the in-run MCP server (see mcp.json)"
command -v claude >/dev/null 2>&1 ||
	fatal "claude CLI not found on PATH (npm install -g @anthropic-ai/claude-code)"
command -v python3 >/dev/null 2>&1 || fatal "python3 not found on PATH"
command -v sqlite3 >/dev/null 2>&1 || fatal "sqlite3 not found on PATH (the SQL basket's agent and seed need it)"

# The SQL basket's verify hook imports catacomb_verifier; the SDK is not installed on
# runners, so it is sourced from the repo. Resolve the repo root from this driver's
# location and put the SDK on PYTHONPATH for the verify children bench spawns.
repo="$(cd "$e2e_dir/.." && pwd)"
[ -d "$repo/integrations/verifier/src" ] ||
	fatal "verifier SDK not found at integrations/verifier/src (PYTHONPATH source for the SQL verify hook)"
export PYTHONPATH="$repo/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"

# --- workspace ----------------------------------------------------------------
work="$(mktemp -d)"
runs1="$work/runs-presence"
runs2="$work/runs-continuous"
runs3="$work/runs-sql"
manifest1="$work/manifest-presence.jsonl"
manifest2="$work/manifest-continuous.jsonl"
manifest3="$work/manifest-sql.jsonl"
db="$work/e2e.db"
mkdir -p "$runs1" "$runs2" "$runs3"

# The SQL basket's agent reads a seeded database and its verifier reads the golden; both
# live here in the work dir, OUTSIDE every cell's per-cell workspace — the documented
# anti-gaming layout. The driver hands them to bench as ambient env: the wrapper reads
# SQL_DB, the verify hook reads GOLDEN. E2E_DIR anchors the basket's workspace.cmd
# (which inherits this driver's environment through catacomb): it copies sql-live.sh
# and verify_sql.py from here into each cell's fresh workspace.
sqldb="$work/sql.db"
sqlgolden="$work/sql-golden.csv"
sqlite3 "$sqldb" <"$e2e_dir/sql-seed.sql" || fatal "cannot seed the SQL basket database"
cp -f "$e2e_dir/sql-golden.csv" "$sqlgolden" || fatal "cannot stage the SQL basket golden"
export SQL_DB="$sqldb"
export GOLDEN="$sqlgolden"
export E2E_DIR="$e2e_dir"

# Pre-workspace runs created out/ here in e2e/ (the SQL cells then shared `dir: .`);
# cells now write out/ inside their own temp workspace, so nothing should land here.
# The trap cleanup stays as defensive hygiene against leftovers from older runs.
sqlout="$e2e_dir/out"

# shellcheck disable=SC2329  # invoked indirectly via the EXIT trap below
copy_artifacts() {
	cp -f "$manifest1" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest2" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest3" "$artifacts"/ 2>/dev/null || true
	rm -rf "$sqlout" 2>/dev/null || true
}
trap copy_artifacts EXIT

# --- assertion bookkeeping ----------------------------------------------------
failures=()
pass() { printf '  PASS  %s\n' "$1"; }
failrec() {
	printf '  FAIL  %s\n' "$1"
	failures+=("$1")
}
skip() { printf '  SKIP  %s\n' "$1"; }
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

# run a command capturing stdout to a JSON artifact, compare its exit code
run_json() { # <want> <outfile> <label> -- cmd...
	local want="$1" out="$2" label="$3"
	shift 3
	[ "${1:-}" = "--" ] && shift
	local rc=0
	"$@" >"$out" 2>"$out.stderr" || rc=$?
	if [ "$rc" -eq "$want" ]; then
		pass "$label (exit $rc)"
	else
		failrec "$label (exit $rc, want $want; json: $out)"
		sed 's/^/        stderr: /' "$out.stderr" >&2 || true
	fi
}

# overall_verdict from a saved regress --json (for informational log lines)
verdict_of() {
	python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("overall_verdict","?"))' "$1" 2>/dev/null || echo "?"
}

# A-vs-A continuous metrics use a WIDENED relative band. Sequential bench batches
# drift on API latency/cost/tokens (PV-6b: cost/duration are noisy regressors); the
# live calibration saw duration ~2.0x between identical batches, which sits on the
# edge of a 1.0 (=2x) band, so 2.0 (=3x) is used to absorb it with margin. Presence
# and error-rate stay at DEFAULT sensitivity (the moat), and the seeded regressions
# (steps d/f) are asserted at DEFAULT thresholds.
ava_metric_band="2.0"

cd "$e2e_dir"

echo "== a. bench presence basket (15 live claude -p cells) =="
run_expect 0 "bench presence basket" -- \
	catacomb bench basket-presence.yaml --runs-dir "$runs1" --manifest "$manifest1"

echo "== b. presence manifest assertions =="
rc=0
python3 - "$manifest1" <<'PY' || rc=$?
import json, os, sys

entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
errs = []
if len(entries) != 30:
    errs.append(f"expected 30 cells (2 tasks x 3 variants x 5 reps), got {len(entries)}")
for e in entries:
    rid = e.get("run_id", "?")
    if e.get("exit_code") != 0:
        errs.append(f"{rid}: exit_code={e.get('exit_code')} note={e.get('note','')}")
    if not e.get("session_id"):
        errs.append(f"{rid}: empty session_id note={e.get('note','')}")
    ev = e.get("evidence_dir", "")
    if not ev or not os.path.isdir(ev):
        errs.append(f"{rid}: evidence_dir missing on disk: {ev!r}")
present = {"baseline": 0, "degraded": 0, "baseline2": 0}
total = {"baseline": 0, "degraded": 0, "baseline2": 0}
for e in entries:
    if e.get("task") != "haiku":
        continue
    v = e.get("variant")
    if v in total:
        total[v] += 1
        if "verify" not in (e.get("missing_checkpoints") or []):
            present[v] += 1
for v in ("baseline", "baseline2"):
    if present[v] < 4:
        errs.append(
            f"verify present {present[v]}/{total[v]} on haiku/{v} (< 4/5, one "
            f"stochastic miss tolerated) — investigate model/instruction drift "
            f"before trusting the gate"
        )
if present["degraded"] != 0:
    errs.append(
        f"verify present {present['degraded']}/{total['degraded']} on haiku/degraded "
        f"(want 0/5) — the degraded instruction failed to suppress marking"
    )
if errs:
    print("presence manifest assertion failures:", file=sys.stderr)
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"presence manifest OK: {len(entries)} cells (haiku+echo), all exit 0, session "
      f"ids + evidence present; haiku verify present baseline={present['baseline']}/5 "
      f"baseline2={present['baseline2']}/5 degraded={present['degraded']}/5")
PY
record "$rc" "presence manifest: 30 cells/exit0/session/evidence; haiku verify baseline&baseline2 >=4/5, degraded 0/5"

echo "== c. presence A-vs-A control (baseline vs baseline2) must NOT gate =="
# (i) informational: DEFAULT thresholds. Continuous metrics can flag here on
#     inter-batch API latency/cost/token drift — logged, NOT asserted.
catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,task=haiku,variant=baseline \
	--candidate label:basket=e2e-presence,task=haiku,variant=baseline2 --json \
	>"$artifacts/regress-presence-AvA-default.json" 2>/dev/null || true
echo "  [info] presence A-vs-A @ default thresholds: overall_verdict=$(verdict_of "$artifacts/regress-presence-AvA-default.json") (informational; continuous drift not asserted)"
# (ii) HARD assertion — the moat: presence + error-rate stay at DEFAULT sensitivity
#      (a presence false positive must still fail the e2e); only the continuous band
#      is widened to absorb sequential-batch API latency/cost/token drift, which the
#      median/IQR band does not model. Seeded regressions (d/f) use DEFAULT thresholds.
echo "  [why] A-vs-A hard-asserted with presence/error-rate at DEFAULT sensitivity and continuous band WIDENED to --metric-rel-delta ${ava_metric_band}: calibration saw duration ~2x between identical batches (inter-batch latency), which a default band would false-flag; presence stays default so a real presence false positive still fails."
run_json 0 "$artifacts/regress-presence-AvA.json" \
	"presence A-vs-A must NOT gate (presence default; continuous band widened)" -- \
	catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,task=haiku,variant=baseline \
	--candidate label:basket=e2e-presence,task=haiku,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json

echo "== d. seeded presence regression (baseline vs degraded) must gate =="
run_json 1 "$artifacts/regress-presence-degraded.json" \
	"presence seeded regression (baseline vs degraded)" -- \
	catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,task=haiku,variant=baseline \
	--candidate label:basket=e2e-presence,task=haiku,variant=degraded --json
rc=0
python3 - "$artifacts/regress-presence-degraded.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "phase" and f.get("name") == "verify"
    and f.get("metric") == "presence" and f.get("verdict") == "regression"
]
if not hits:
    print("no phase 'verify' presence regression finding; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict")}, file=sys.stderr)
    sys.exit(1)
h = hits[0]
print(f"decisive finding: phase verify presence {h.get('baseline')} -> {h.get('candidate')} (regression)")
PY
record "$rc" "presence regression attributed to phase 'verify' presence drop"

echo "== d2. seeded STEP regression via echo task (Bash step presence 5/5 -> 0/5) =="
# Exit code is NOT asserted here. A clean flip leaves the degraded echo group with
# zero steps, so step coverage = matched/baseline = 0/1 < 0.7 (--coverage-floor) and
# regress downgrades the Bash presence regression to `notable` (exit 0, confirmed in
# regress/regress.go: rowFindings is called with active=!StepsTrusted, and
# applyDowngrade turns a step regression into notable). If a degraded cell leaks a
# Bash call, coverage = 1 and it stays `regression` (exit 1). Either way the --json
# carries a step-scope Bash presence finding with the drop — that is what we assert.
catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,task=echo,variant=baseline \
	--candidate label:basket=e2e-presence,task=echo,variant=degraded \
	--metric-rel-delta "$ava_metric_band" --json \
	>"$artifacts/regress-echo-degraded.json" 2>/dev/null || true
rc=0
python3 - "$artifacts/regress-echo-degraded.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "step" and f.get("metric") == "presence"
    and f.get("verdict") in ("regression", "notable")
    and f.get("candidate", 0) > f.get("baseline", 0)
]
if not hits:
    print("no step-scope presence regression/notable finding; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
h = hits[0]
print(f"decisive finding: step {h.get('name')!r} presence {h.get('verdict')} ({h.get('detail', '')})")
PY
record "$rc" "echo seeded step regression: Bash step presence drop at step scope (regression|notable)"

echo "== e. baseline pin + strict record + trends =="
run_expect 0 "baseline set e2e-presence-main" -- \
	catacomb baseline set e2e-presence-main \
	--label basket=e2e-presence,task=haiku,variant=baseline --runs-dir "$runs1" --db "$db"
run_json 1 "$artifacts/regress-presence-strict-record.json" \
	"strict+record name:e2e-presence-main vs degraded must gate" -- \
	catacomb regress --db "$db" --runs-dir "$runs1" \
	--baseline name:e2e-presence-main \
	--candidate label:basket=e2e-presence,task=haiku,variant=degraded --record --strict --json
rc=0
catacomb trends e2e-presence-main --db "$db" >"$artifacts/trends-presence.txt" 2>&1 || rc=$?
record "$rc" "trends e2e-presence-main exits 0"
if [ -s "$artifacts/trends-presence.txt" ]; then pass "trends output non-empty"; else failrec "trends output empty"; fi

echo "== f. bench continuous basket + continuous gate =="
run_expect 0 "bench continuous basket" -- \
	catacomb bench basket-continuous.yaml --runs-dir "$runs2" --manifest "$manifest2"
rc=0
python3 - "$manifest2" <<'PY' || rc=$?
import json, os, sys

entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
errs = []
if len(entries) != 15:
    errs.append(f"expected 15 cells, got {len(entries)}")
for e in entries:
    rid = e.get("run_id", "?")
    if e.get("exit_code") != 0:
        errs.append(f"{rid}: exit_code={e.get('exit_code')} note={e.get('note','')}")
    if not e.get("session_id"):
        errs.append(f"{rid}: empty session_id note={e.get('note','')}")
    ev = e.get("evidence_dir", "")
    if not ev or not os.path.isdir(ev):
        errs.append(f"{rid}: evidence_dir missing on disk: {ev!r}")
if errs:
    print("continuous manifest assertion failures:", file=sys.stderr)
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"continuous manifest OK: {len(entries)} cells, all exit 0, session ids + evidence present")
PY
record "$rc" "continuous manifest: 15 cells, all exit 0, session ids + evidence present"

echo "-- continuous A-vs-A control (baseline vs baseline2) must NOT gate --"
# (i) informational: DEFAULT thresholds (continuous drift may flag; NOT asserted).
catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=baseline2 --json \
	>"$artifacts/regress-continuous-AvA-default.json" 2>/dev/null || true
echo "  [info] continuous A-vs-A @ default thresholds: overall_verdict=$(verdict_of "$artifacts/regress-continuous-AvA-default.json") (informational; continuous drift not asserted)"
# (ii) HARD assertion: continuous band widened (same batch-drift rationale as step c).
run_json 0 "$artifacts/regress-continuous-AvA.json" \
	"continuous A-vs-A must NOT gate (continuous band widened)" -- \
	catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json
run_json 1 "$artifacts/regress-continuous-verbose.json" \
	"continuous seeded regression (baseline vs verbose)" -- \
	catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=verbose --json
rc=0
python3 - "$artifacts/regress-continuous-verbose.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("metric") == "tokens_out" and f.get("verdict") == "regression"
]
if not hits:
    print("no tokens_out regression finding; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "baseline", "candidate")}, file=sys.stderr)
    sys.exit(1)
h = hits[0]
print(f"decisive finding: tokens_out {h.get('baseline')} -> {h.get('candidate')} (regression, scope {h.get('scope')})")
PY
record "$rc" "continuous regression attributed to tokens_out growth"

echo "== g. artifact smokes on live evidence (diff/subgraph on haiku; export on echo) =="
# haiku pick (phase axis), no glob-order luck: among haiku baseline cells that bench
# verified (manifest missing_checkpoints empty), keep those whose verify phase ALSO
# resolves offline from the main session.jsonl. A cell that delegated the mark to a
# subagent verifies by NAME at bench time but splits the POSITIONAL phase key
# (ADR-0016), so `subgraph --phase verify` on its main transcript exits non-zero.
python3 - "$manifest1" >"$work/verified-haiku.txt" <<'PY'
import json, sys

for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    e = json.loads(line)
    if e.get("task") == "haiku" and e.get("variant") == "baseline" and not (e.get("missing_checkpoints") or []):
        ev = e.get("evidence_dir", "")
        if ev:
            print(ev)
PY
verified_dirs=()
resolved_dirs=()
while IFS= read -r d; do
	[ -n "$d" ] || continue
	verified_dirs+=("$d")
	if catacomb subgraph "$d/session.jsonl" --phase verify --json >/dev/null 2>&1; then
		resolved_dirs+=("$d")
	fi
done <"$work/verified-haiku.txt"

if [ "${#resolved_dirs[@]}" -ge 1 ]; then
	chosen="${resolved_dirs[0]}"
	if [ "${#resolved_dirs[@]}" -ge 2 ]; then
		d_a="${resolved_dirs[0]}"
		d_b="${resolved_dirs[1]}"
	elif [ "${#verified_dirs[@]}" -ge 2 ]; then
		d_a="${verified_dirs[0]}"
		d_b="${verified_dirs[1]}"
	else
		d_a=""
		d_b=""
	fi
	if [ -n "$d_a" ] && [ -n "$d_b" ]; then
		run_expect 0 "diff two bench-verified haiku sessions" -- \
			catacomb diff "$d_a/session.jsonl" "$d_b/session.jsonl"
	else
		failrec "need two bench-verified haiku cells for diff (verified=${#verified_dirs[@]}, resolved=${#resolved_dirs[@]})"
	fi
	run_json 0 "$artifacts/subgraph-verify.json" \
		"subgraph --phase verify --json (bench-verified, phase-resolving haiku cell)" -- \
		catacomb subgraph "$chosen/session.jsonl" --phase verify --json
	rc=0
	python3 - "$artifacts/subgraph-verify.json" <<'PY' || rc=$?
import json, sys

g = json.load(open(sys.argv[1]))
nodes = g.get("nodes") or []
if not nodes:
    print("subgraph verify phase resolved but has an empty nodes array", file=sys.stderr)
    sys.exit(1)
print(f"subgraph verify nodes: {len(nodes)}")
PY
	record "$rc" "subgraph verify nodes array non-empty (mark landed in the root session)"
else
	failrec "bench-time checkpoint verification and offline phase resolution diverged (positional phase-key split or evidence loss) — investigate (verified haiku baseline cells=${#verified_dirs[@]}, none resolved the verify phase offline)"
fi

# export (step axis): an echo baseline cell — the guaranteed Bash step_key node.
echo_base_dir="$(python3 - "$manifest1" <<'PY'
import json, sys

for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    e = json.loads(line)
    if e.get("task") == "echo" and e.get("variant") == "baseline":
        ev = e.get("evidence_dir", "")
        if ev:
            print(ev)
            break
PY
)"
if [ -n "$echo_base_dir" ]; then
	run_expect 0 "export echo baseline evidence dir to jsonl" -- \
		catacomb export "$echo_base_dir" --to jsonl --out "$work/export.jsonl"
	if [ -s "$work/export.jsonl" ]; then pass "export.jsonl non-empty"; else failrec "export.jsonl empty/missing"; fi
	if grep -q 'step_key' "$work/export.jsonl" 2>/dev/null; then
		pass "export.jsonl contains step_key"
	else
		failrec "export.jsonl has no step_key — the guaranteed Bash echo step is missing (did the echo agent skip Bash?)"
	fi
	cp -f "$work/export.jsonl" "$artifacts"/ 2>/dev/null || true
else
	failrec "no echo baseline evidence dir found for the export smoke"
fi

echo "== h. external-scores plumbing on live evidence =="
# The echo task's baseline and baseline2 cells each run a guaranteed `echo
# catacomb-e2e` Bash step (mark calls are consumed into markers, not step nodes), so
# a stable, cross-variant step-key-eligible node exists. A `regress` annotation
# finding is emitted only for a step present in BOTH groups, so we intersect step
# keys across all echo baseline and all echo baseline2 exports and PREFER the Bash
# step. Scoring that key on the baseline side only yields a one-sided (insufficient)
# annotation finding, which surfaces `e2e.quality` in --json while keeping the A-vs-A
# verdict at exit 0 (a two-sided equal score would be an `ok` step finding, which
# regress filters out of the report). The intersection fallback + SKIP remain for the
# (now unexpected) case of no shared step.
base_evid=()
while IFS= read -r d; do base_evid+=("$d"); done < <(
	find "$runs1" -maxdepth 1 -type d -name 'bench-e2e-presence-echo-baseline-r*' | sort
)
base2_evid=()
while IFS= read -r d; do base2_evid+=("$d"); done < <(
	find "$runs1" -maxdepth 1 -type d -name 'bench-e2e-presence-echo-baseline2-r*' | sort
)
if [ "${#base_evid[@]}" -ge 1 ] && [ "${#base2_evid[@]}" -ge 1 ]; then
	mkdir -p "$work/exp/base" "$work/exp/base2"
	for d in "${base_evid[@]}"; do
		catacomb export "$d" --to jsonl --out "$work/exp/base/$(basename "$d").jsonl" 2>/dev/null || true
	done
	for d in "${base2_evid[@]}"; do
		catacomb export "$d" --to jsonl --out "$work/exp/base2/$(basename "$d").jsonl" 2>/dev/null || true
	done
	sk_rc=0
	sk_out="$(python3 - "$work/exp/base" "$work/exp/base2" <<'PY'
import glob, json, os, sys

def scan(d):
    keys_by_run = {}
    bash_keys = set()
    for fp in glob.glob(os.path.join(d, "*.jsonl")):
        rid = os.path.basename(fp)[:-6]
        ks = set()
        for line in open(fp):
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except Exception:
                continue
            if r.get("kind") == "node" and r.get("step_key"):
                sk = r["step_key"]
                ks.add(sk)
                if str(r.get("name", "")).lower() == "bash":
                    bash_keys.add(sk)
        keys_by_run[rid] = ks
    return keys_by_run, bash_keys

b, b_bash = scan(sys.argv[1])
b2, b2_bash = scan(sys.argv[2])
ub = set().union(*b.values()) if b else set()
ub2 = set().union(*b2.values()) if b2 else set()
common = ub & ub2
if not common:
    sys.exit(3)
bash_common = sorted((b_bash & b2_bash) & common)
key = bash_common[0] if bash_common else sorted(common)[0]
rid = next((r for r, ks in b.items() if key in ks), "")
if not rid:
    sys.exit(3)
print(key)
print(rid)
PY
	)" || sk_rc=$?
	if [ "$sk_rc" -eq 0 ]; then
		step_key="$(printf '%s\n' "$sk_out" | sed -n 1p)"
		score_rid="$(printf '%s\n' "$sk_out" | sed -n 2p)"
		printf '{"step_key":"%s","key":"e2e.quality","value":1.0,"run_id":"%s"}\n' \
			"$step_key" "$score_rid" >"$work/scores.jsonl"
		cp -f "$work/scores.jsonl" "$artifacts"/ 2>/dev/null || true
		run_json 0 "$artifacts/regress-echo-scores.json" \
			"echo A-vs-A with --scores/--annotation must NOT gate" -- \
			catacomb regress --runs-dir "$runs1" \
			--baseline label:basket=e2e-presence,task=echo,variant=baseline \
			--candidate label:basket=e2e-presence,task=echo,variant=baseline2 \
			--scores "$work/scores.jsonl" --annotation e2e.quality \
			--metric-rel-delta "$ava_metric_band" --json
		if grep -q 'e2e.quality' "$artifacts/regress-echo-scores.json" 2>/dev/null; then
			pass "external score surfaced (e2e.quality present in report json)"
		else
			failrec "e2e.quality not present in report json (see $artifacts/regress-echo-scores.json)"
		fi
	elif [ "$sk_rc" -eq 3 ]; then
		skip "external-scores test: echo cells shared no step-key-eligible node to score this run"
	else
		failrec "external-scores test: step_key extraction errored (rc=$sk_rc)"
	fi
else
	failrec "no echo baseline/baseline2 evidence for the external-scores test"
fi

echo "== i. bench sql basket (15 live claude -p cells) — the verifier contract =="
run_expect 0 "bench sql basket" -- \
	catacomb bench basket-sql.yaml --runs-dir "$runs3" --manifest "$manifest3"

echo "== j. sql manifest + verifier.pass calibration assertions =="
# Verified in the manifest means the verify hook RAN cleanly, not that it passed — the
# pass/fail lands in each cell's scores.jsonl (verifier.pass 1/0). Baseline/baseline2
# must verify reliably (>=4/5, one stochastic miss tolerated) and degraded must fail
# entirely (0/5), or the seeded gate below is not a trustworthy signal.
rc=0
python3 - "$manifest3" <<'PY' || rc=$?
import json, os, sys

entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
errs = []
if len(entries) != 15:
    errs.append(f"expected 15 cells (1 task x 3 variants x 5 reps), got {len(entries)}")
passed = {"baseline": 0, "degraded": 0, "baseline2": 0}
total = {"baseline": 0, "degraded": 0, "baseline2": 0}
for e in entries:
    rid = e.get("run_id", "?")
    if e.get("exit_code") != 0:
        errs.append(f"{rid}: exit_code={e.get('exit_code')} note={e.get('note','')}")
    if not e.get("session_id"):
        errs.append(f"{rid}: empty session_id note={e.get('note','')}")
    ev = e.get("evidence_dir", "")
    if not ev or not os.path.isdir(ev):
        errs.append(f"{rid}: evidence_dir missing on disk: {ev!r}")
        continue
    if not e.get("verified"):
        errs.append(f"{rid}: verify hook did not run cleanly (verify_error={e.get('verify_error','')!r})")
    v = e.get("variant")
    if v not in total:
        continue
    total[v] += 1
    try:
        for line in open(os.path.join(ev, "scores.jsonl")):
            line = line.strip()
            if not line:
                continue
            s = json.loads(line)
            if s.get("key") == "verifier.pass" and s.get("value") == 1:
                passed[v] += 1
    except OSError as ex:
        errs.append(f"{rid}: scores.jsonl unreadable: {ex}")
for v in ("baseline", "baseline2"):
    if passed[v] < 4:
        errs.append(
            f"verifier.pass {passed[v]}/{total[v]} on {v} (< 4/5, one stochastic miss "
            f"tolerated) — investigate model/instruction drift before trusting the gate"
        )
if passed["degraded"] != 0:
    errs.append(
        f"verifier.pass {passed['degraded']}/{total['degraded']} on degraded (want 0/5) "
        f"— the all-orders instruction failed to produce a wrong result-set"
    )
if errs:
    print("sql manifest assertion failures:", file=sys.stderr)
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"sql manifest OK: {len(entries)} cells, all exit 0, session ids + evidence + "
      f"verify ran; verifier.pass baseline={passed['baseline']}/5 "
      f"baseline2={passed['baseline2']}/5 degraded={passed['degraded']}/5")
PY
record "$rc" "sql manifest: 15 cells/exit0/session/evidence/verify-ran; verifier.pass baseline&baseline2 >=4/5, degraded 0/5"

echo "== k. sql seeded regression (baseline vs degraded) must gate on verifier.pass =="
run_json 1 "$artifacts/regress-sql-degraded.json" \
	"sql seeded regression (baseline vs degraded)" -- \
	catacomb regress --runs-dir "$runs3" \
	--baseline label:basket=e2e-sql,variant=baseline \
	--candidate label:basket=e2e-sql,variant=degraded --json
rc=0
python3 - "$artifacts/regress-sql-degraded.json" <<'PY' || rc=$?
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
record "$rc" "sql degraded gate attributed to ann:verifier.pass total regression"

echo "== l. sql A-vs-A control (baseline vs baseline2) must NOT gate =="
# verifier.pass is equal across the identical variants (both ~5/5), so the annotation
# axis never gates; only the continuous metrics can drift on live API latency/cost/token
# jitter, so the continuous band is WIDENED (same rationale as steps c/f). A one-cell
# pass-rate wobble stays under the annotation-rate floor at k=5.
run_json 0 "$artifacts/regress-sql-AvA.json" \
	"sql A-vs-A must NOT gate (continuous band widened)" -- \
	catacomb regress --runs-dir "$runs3" \
	--baseline label:basket=e2e-sql,variant=baseline \
	--candidate label:basket=e2e-sql,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$artifacts/regress-sql-AvA.json" || rc=$?
record "$rc" "sql A-vs-A reports zero regressions"

echo "== m. cost report =="
python3 - "$manifest1" "$manifest2" "$manifest3" "$artifacts/cost.txt" <<'PY'
import json, sys

total = 0.0
for p in sys.argv[1:4]:
    try:
        for line in open(p):
            line = line.strip()
            if not line:
                continue
            c = json.loads(line).get("cost_usd")
            if isinstance(c, (int, float)):
                total += c
    except FileNotFoundError:
        pass
open(sys.argv[4], "w").write(f"total live spend: ${total:.2f}\n")
print(f"total live spend: ${total:.2f}")
PY

echo "== summary =="
if [ "${#failures[@]}" -eq 0 ]; then
	echo "E2E LIVE GATE: PASS — all assertions held (artifacts in $artifacts)"
	exit 0
fi
echo "E2E LIVE GATE: FAIL — ${#failures[@]} assertion(s) failed:"
for f in "${failures[@]}"; do
	echo "  - $f"
done
echo "artifacts (manifests + regress json) in $artifacts"
exit 1
