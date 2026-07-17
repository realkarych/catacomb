#!/usr/bin/env bash
# E2E live gate — the PV-6b calibration methodology as a self-asserting driver.
#
# Runs six heterogeneous live `claude -p` baskets through `catacomb bench` and
# then exercises the full offline pipeline against the real evidence:
#   - every A-vs-A control must NOT gate (zero false positives), and
#   - a seeded checkpoint-presence regression, a seeded continuous (tokens_out)
#     regression, a seeded verifier-contract regression (a wrong SQL result fails
#     verification), a seeded subagent-delegation regression (baseline delegates to a
#     subagent, degraded runs inline — gated on the real "type":"subagent" node reduced
#     from the FULL evidence: bench snapshots each subagent's turns into
#     subagents/agent-*.jsonl next to session.jsonl, and the pipeline reduces them
#     together, so the subagent node IS present; baseline has a subagents/ dir, degraded
#     does not), a seeded
#     skill-delegation regression (a dropped Skill step
#     node), AND a
#     seeded live-MCP regression (a dropped MCP record-tool step node together with a
#     failed record-tool verifier — the one basket where BOTH signals gate)
#     MUST each gate, attributed to the swapped instruction.
# It also smoke-tests baseline pin/record/trends, diff/subgraph/export, and the
# external-scores path — all on the live evidence.
#
# See docs/reviews/2026-07-08-pv6b-live-calibration.md for the methodology.
#
# Cost: ~$3–7 of real API spend (105 bench cells: presence/continuous/sql +
# subagent/skill/mcp production baskets on sonnet; subagent cells spawn children).
#
# An OPTIONAL codex leg (basket-codex.yaml: 6 live `codex exec` cells on
# gpt-5.4-mini, ~$0.05-equivalent) runs after the claude baskets when the codex
# CLI is present AND authenticated (stored `codex login` or CODEX_API_KEY), and
# SKIPS cleanly otherwise — the overall exit is unaffected. Codex reports token
# counts but no dollar cost, so the leg never contributes to the cost total.
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
#   CODEX_API_KEY   optional OpenAI auth for the codex leg's `codex exec` cells;
#                   when it is absent and no stored `codex login` exists, the
#                   codex leg skips (never fatal).
#
# The bench cells resolve `./presence.sh` / `./answer.sh` and `mcp.json` relative to
# the e2e directory, so this driver cd's into its own directory before invoking bench
# (the presence and continuous baskets declare `dir: .`). The SQL, subagent, skill, and
# MCP baskets instead run each cell in a fresh per-cell workspace whose setup cmd copies
# their wrapper (`./sql-live.sh` / `./subagent.sh` / `./skill.sh` / `./mcp-record.sh`) and
# verifier (`./verify_sql.py` / `./verify_emit.py`) from E2E_DIR (exported below); the
# skill workspace also stages the e2e-emit skill dir into the cell's `.claude/skills/`, and
# the MCP workspace stages the mcp-e2ekit server dir and renders its absolute path into a
# per-cell mcp.json. All other paths are absolute, so the cd does not affect them.
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
runs4="$work/runs-subagent"
runs5="$work/runs-skill"
runs6="$work/runs-mcp"
runs7="$work/runs-codex"
runs8="$work/runs-failmode"
manifest1="$work/manifest-presence.jsonl"
manifest2="$work/manifest-continuous.jsonl"
manifest3="$work/manifest-sql.jsonl"
manifest4="$work/manifest-subagent.jsonl"
manifest5="$work/manifest-skill.jsonl"
manifest6="$work/manifest-mcp.jsonl"
manifest7="$work/manifest-codex.jsonl"
manifest8="$work/manifest-failmode.jsonl"
db="$work/e2e.db"
mkdir -p "$runs1" "$runs2" "$runs3" "$runs4" "$runs5" "$runs6" "$runs7" "$runs8"

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
	cp -f "$manifest4" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest5" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest6" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest7" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest8" "$artifacts"/ 2>/dev/null || true
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

echo "== a0. bench presence --dry-run lists the planned cells, writes no evidence (\$0) =="
run_json 0 "$work/presence-dryrun.out" "bench presence --dry-run lists 30 planned cells" -- \
	catacomb bench basket-presence.yaml --dry-run
rc=0
planned=$(grep -c '^bench-e2e-presence-' "$work/presence-dryrun.out" || true)
[ "$planned" -eq 30 ] || rc=1
record "$rc" "dry-run planned-cell count: $planned/30 (2 tasks x 3 variants x 5 reps)"
rc=0
[ -f "$manifest1" ] && rc=1
[ -n "$(find "$runs1" -mindepth 1 -maxdepth 1 2>/dev/null)" ] && rc=1
record "$rc" "dry-run wrote no manifest ($manifest1) and created no evidence under \$runs1"

echo "== a. bench presence basket (15 live claude -p cells) =="
run_expect 0 "bench presence basket" -- \
	catacomb bench basket-presence.yaml --runs-dir "$runs1" --manifest "$manifest1"

echo "== a1. bench presence --resume + explicit --projects-dir: 0 newly-executed cells (\$0 idempotent re-invoke) =="
# --resume re-invokes over the manifest step a just completed (reused, not a fresh
# basket run — every cell is already in $manifest1, so no cell re-executes and no new
# live spend occurs). The explicit --projects-dir "$HOME/.claude/projects" — identical
# to bench's own default when unset — piggybacks the flag-parses smoke onto the same
# $0 call rather than paying for a second live presence bench.
run_json 0 "$work/presence-resume.out" \
	"bench presence --resume --projects-dir re-invoke over the completed manifest" -- \
	catacomb bench basket-presence.yaml --runs-dir "$runs1" --manifest "$manifest1" \
	--resume --projects-dir "$HOME/.claude/projects"
rc=0
skips=$(grep -c '(already completed)$' "$work/presence-resume.out" || true)
[ "$skips" -eq 30 ] || rc=1
record "$rc" "resume skip count: $skips/30 already-completed cells (0 newly executed)"
rc=0
grep -q '^marked ' "$work/presence-resume.out" && rc=1
record "$rc" "resume printed no 'marked N/M' summary (0 cells executed => zero incremental live spend; matches TestRunBenchCellsResumeAllSkippedOmitsMarked)"
rc=0
mlines=$(wc -l <"$manifest1" | tr -d ' ')
[ "$mlines" -eq 30 ] || rc=1
record "$rc" "manifest1 unchanged at $mlines/30 entries after --resume (identical cell count to the dry-run/bench expansion)"

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

echo "== d3. regress --presence-delta 1.5: seeded presence regression no longer gates the verify phase =="
# Presence deltas are probability differences bounded in [0,1]; 1.5 exceeds the maximum
# possible delta, so the verify-phase presence finding this step's default assertion
# proves as a regression (step d) can mathematically never gate here — a deterministic,
# non-live-variance-dependent contrast (unlike the continuous-metric flags below).
catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,task=haiku,variant=baseline \
	--candidate label:basket=e2e-presence,task=haiku,variant=degraded \
	--presence-delta 1.5 --json \
	>"$artifacts/regress-presence-loosedelta.json" 2>/dev/null || true
rc=0
python3 - "$artifacts/regress-presence-loosedelta.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "phase" and f.get("name") == "verify"
    and f.get("metric") == "presence" and f.get("verdict") in ("regression", "notable")
]
if hits:
    print("phase verify presence still gates at --presence-delta 1.5:", hits, file=sys.stderr)
    sys.exit(1)
print("phase verify presence gate disabled at --presence-delta 1.5 (contrast vs step d's default regression)")
PY
record "$rc" "--presence-delta 1.5 disables the verify-phase presence gate that step d's default asserts as a regression"

echo "== e. baseline pin + strict record (--project e2e-live) + trends =="
run_expect 0 "baseline set e2e-presence-main" -- \
	catacomb baseline set e2e-presence-main \
	--label basket=e2e-presence,task=haiku,variant=baseline --runs-dir "$runs1" --db "$db"
run_json 1 "$artifacts/regress-presence-strict-record.json" \
	"strict+record name:e2e-presence-main vs degraded must gate" -- \
	catacomb regress --db "$db" --runs-dir "$runs1" \
	--baseline name:e2e-presence-main \
	--candidate label:basket=e2e-presence,task=haiku,variant=degraded \
	--record --strict --project e2e-live --json
rc=0
catacomb trends e2e-presence-main --db "$db" >"$artifacts/trends-presence.txt" 2>&1 || rc=$?
record "$rc" "trends e2e-presence-main exits 0"
if [ -s "$artifacts/trends-presence.txt" ]; then pass "trends output non-empty"; else failrec "trends output empty"; fi
rc=0
catacomb trends e2e-presence-main --db "$db" --json >"$artifacts/trends-presence.json" 2>/dev/null || rc=$?
record "$rc" "trends e2e-presence-main --json exits 0"
rc=0
python3 - "$artifacts/trends-presence.json" <<'PY' || rc=$?
import json, sys

entries = json.load(open(sys.argv[1]))
hits = [e for e in entries if e.get("record", {}).get("project") == "e2e-live"]
if not hits:
    print("no trends entry carries record.project=='e2e-live'; entries were:", file=sys.stderr)
    for e in entries:
        print("  ", {"project": e.get("record", {}).get("project")}, file=sys.stderr)
    sys.exit(1)
print(f"trends --json: {len(hits)}/{len(entries)} entries carry record.project=='e2e-live'")
PY
record "$rc" "--project e2e-live stamps the recorded history row; trends --json surfaces it at record.project"

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

echo "== f2. regress --min-support 6 --strict: continuous baseline-vs-baseline2 (5 runs/group) is insufficient at floor 6 =="
# Real behavior (cmd/catacomb/regress_test.go TestRegressStrictInsufficientExitOne):
# --strict on an overall_verdict=insufficient report maps to exit 1 (errRegressionDetected),
# NOT exit 2 — exit 2 is reserved for operational/parse errors (cmd/catacomb/run.go).
# basket-continuous.yaml runs 5 reps/variant, so --min-support 6 pushes every group below
# the trusted floor and every finding downgrades to insufficient.
run_json 1 "$artifacts/regress-continuous-minsupport.json" \
	"min-support 6 --strict: continuous baseline-vs-baseline2 (5 runs/group) insufficient" -- \
	catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=baseline2 \
	--min-support 6 --strict --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["overall_verdict"]=="insufficient" else 1)' \
	"$artifacts/regress-continuous-minsupport.json" || rc=$?
record "$rc" "--min-support 6 flips continuous baseline-vs-baseline2 to overall_verdict=insufficient (exit 1 under --strict)"

echo "== f3. regress --iqr-factor tightened: a continuous metric band narrows vs the step f default (same evidence) =="
# Real API duration/cost/token jitter is unpredictable in advance, so this does not assert
# an exact verdict flip (unlike the fabricated hermetic mirror in 80-cli-contracts.sh, which
# proves the flip deterministically). Instead it asserts the flag's DEMONSTRABLE, always-
# computable effect on the finding itself: shrinking --iqr-factor can only narrow or hold
# the reported band (band = max(metric-rel-delta*median, iqr-factor*IQR)), and narrows it
# for real whenever the IQR term was contributing — virtually certain for at least one of
# duration_ms/cost_usd/tokens_in/tokens_out/nodes across 5 independent live API replicates.
catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=baseline2 \
	--iqr-factor 0.01 --json \
	>"$artifacts/regress-continuous-iqrtight.json" 2>/dev/null || true
rc=0
python3 - "$artifacts/regress-continuous-AvA-default.json" "$artifacts/regress-continuous-iqrtight.json" <<'PY' || rc=$?
import json, sys

def widths(path):
    rep = json.load(open(path))
    out = {}
    for f in rep.get("findings", []):
        if f.get("scope") == "total" and f.get("metric") in (
            "duration_ms", "cost_usd", "tokens_in", "tokens_out", "nodes"
        ):
            out[f["metric"]] = f.get("band_hi", 0) - f.get("band_lo", 0)
    return out

default_w = widths(sys.argv[1])
tight_w = widths(sys.argv[2])
narrowed = {
    m: (default_w[m], tight_w[m])
    for m in default_w
    if m in tight_w and tight_w[m] < default_w[m] - 1e-9
}
if not narrowed:
    print(f"no continuous metric band narrowed under --iqr-factor 0.01 vs default 1.5; "
          f"widths default={default_w!r} tight={tight_w!r}", file=sys.stderr)
    sys.exit(1)
print(f"iqr-factor 0.01 narrows the band on {sorted(narrowed)} vs default 1.5: {narrowed}")
PY
record "$rc" "--iqr-factor 0.01 narrows the total-scope band on >=1 continuous metric vs the step f default (1.5)"

echo "== f4. regress --audit-iqr-factor tightened: the per-cell audit block flags a cell the default run does not =="
# Same live-variance caveat as f3: the audit block (regress/audit.go computeAudit) is
# purely informational (never affects overall_verdict/exit code), so this is a $0-safe,
# best-effort check over real evidence — the hermetic mirror in 80-cli-contracts.sh proves
# the mechanism deterministically on fabricated cells.
catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=baseline2 \
	--audit-iqr-factor 0.01 --json \
	>"$artifacts/regress-continuous-auditiqr.json" 2>/dev/null || true
rc=0
python3 - "$artifacts/regress-continuous-AvA-default.json" "$artifacts/regress-continuous-auditiqr.json" <<'PY' || rc=$?
import json, sys

def audit_count(path):
    a = json.load(open(path)).get("audit") or {}
    return len(a.get("baseline") or []) + len(a.get("candidate") or [])

d, t = audit_count(sys.argv[1]), audit_count(sys.argv[2])
print(f"audit-flagged cells: default(3.0)={d} -> audit-iqr-factor(0.01)={t}")
if t <= d:
    print("no NEW audit-flagged cell observed under --audit-iqr-factor 0.01", file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "--audit-iqr-factor 0.01 grows the per-cell audit-flagged count vs the step f default (3.0)"

echo "== f5. regress --audit-rel-delta tightened: the per-cell audit block flags a cell the default run does not =="
catacomb regress --runs-dir "$runs2" \
	--baseline label:basket=e2e-continuous,variant=baseline \
	--candidate label:basket=e2e-continuous,variant=baseline2 \
	--audit-rel-delta 0.01 --json \
	>"$artifacts/regress-continuous-auditreldelta.json" 2>/dev/null || true
rc=0
python3 - "$artifacts/regress-continuous-AvA-default.json" "$artifacts/regress-continuous-auditreldelta.json" <<'PY' || rc=$?
import json, sys

def audit_count(path):
    a = json.load(open(path)).get("audit") or {}
    return len(a.get("baseline") or []) + len(a.get("candidate") or [])

d, t = audit_count(sys.argv[1]), audit_count(sys.argv[2])
print(f"audit-flagged cells: default(0.5)={d} -> audit-rel-delta(0.01)={t}")
if t <= d:
    print("no NEW audit-flagged cell observed under --audit-rel-delta 0.01", file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "--audit-rel-delta 0.01 grows the per-cell audit-flagged count vs the step f default (0.5)"

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
mkdir -p "$work/live-workspaces" || fatal "cannot create the sql --workspaces-dir root"
run_expect 0 "bench sql basket (--workspaces-dir + --keep-workspaces)" -- \
	catacomb bench basket-sql.yaml --runs-dir "$runs3" --manifest "$manifest3" \
	--workspaces-dir "$work/live-workspaces" --keep-workspaces

echo "== i2. sql --keep-workspaces: 15 per-cell workspace dirs, each holding the copied wrapper + verifier =="
rc=0
ws_count=0
for d in "$work"/live-workspaces/bench-e2e-sql-sql-*; do
	[ -d "$d" ] || continue
	ws_count=$((ws_count + 1))
	[ -f "$d/sql-live.sh" ] || rc=1
	[ -f "$d/verify_sql.py" ] || rc=1
done
[ "$ws_count" -eq 15 ] || rc=1
record "$rc" "sql --keep-workspaces: $ws_count/15 per-cell workspace dirs kept, each holding sql-live.sh + verify_sql.py"

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

echo "== j2. verify sql basket standalone (offline re-verify) + --label filtering =="
# Standalone `verify` re-runs the verify hook offline over recorded evidence dirs and
# prints one "verify <run_id>: ok" line per matched cell that re-verified cleanly (a
# hook that runs to completion, independent of whether verifier.pass scored 1 or 0 —
# degraded's wrong SQL result still re-verifies "ok", it just re-records
# verifier.pass=0). Unfiltered, all 15 sql cells (1 task x 3 variants x 5 reps) match
# the basket label and re-verify; --label variant=baseline narrows the match to the 5
# baseline-only cells, proving the label filter actually restricts which cells get
# re-verified (mirrored hermetically: e2e/hermetic/prod/scenarios/90-analysis-cmds.sh).
run_json 0 "$work/verify-sql-all.out" \
	"verify sql basket standalone (all 15 cells)" -- \
	catacomb verify basket-sql.yaml --runs-dir "$runs3"
rc=0
ok_all=$(grep -c ': ok$' "$work/verify-sql-all.out" || true)
[ "$ok_all" -eq 15 ] || rc=1
record "$rc" "verify sql basket standalone: $ok_all/15 cells re-verified ok"

run_json 0 "$work/verify-sql-baseline.out" \
	"verify sql basket --label variant=baseline (5 cells)" -- \
	catacomb verify basket-sql.yaml --runs-dir "$runs3" --label variant=baseline
rc=0
ok_baseline=$(grep -c ': ok$' "$work/verify-sql-baseline.out" || true)
[ "$ok_baseline" -eq 5 ] || rc=1
record "$rc" "verify sql basket --label variant=baseline: $ok_baseline/5 cells re-verified ok"

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

echo "== k2. regress --format markdown: sql seeded regression renders a markdown report =="
# regress/render.go RenderMarkdown: a bold "**Verdict: <emoji> <verdict>**" header line,
# a "baseline N runs ..." summary, the findings table (header row + one row per finding),
# and (when Reliability/Audit are non-nil) a collapsible <details> block. No "#" headers
# are emitted by this renderer — the bold marker and the table are the real, checkable shape.
run_json 1 "$work/regress-sql-degraded.md" \
	"sql seeded regression (baseline vs degraded) --format markdown" -- \
	catacomb regress --runs-dir "$runs3" \
	--baseline label:basket=e2e-sql,variant=baseline \
	--candidate label:basket=e2e-sql,variant=degraded \
	--format markdown
rc=0
grep -q '^\*\*Verdict: .*regression\*\*$' "$work/regress-sql-degraded.md" || rc=1
grep -q '^| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |$' "$work/regress-sql-degraded.md" || rc=1
grep -qE '^\| regression \|' "$work/regress-sql-degraded.md" || rc=1
record "$rc" "--format markdown renders a bold '**Verdict: ... regression**' header, the findings table header, and >=1 'regression' table row"

echo "== k3. regress --annotation-rate-delta 0.99: sql seeded regression still gates =="
# Defense-in-depth: even with the annotation-rate axis nearly disabled (0.99, close to the
# widest possible probability delta), the SAME baseline-vs-degraded comparison must still
# gate overall. Worst case this is tight: if baseline achieves a clean 5/5 verifier.pass,
# the bad-rate delta is exactly 1.0 (> 0.99); the one-stochastic-miss floor step j tolerates
# (4/5) would drop the delta to 0.8, under 0.99 — a live-flakiness risk inherent to this
# extreme threshold value, not asserted away here per the plan's explicit recipe.
run_json 1 "$artifacts/regress-sql-ratedelta.json" \
	"annotation-rate-delta 0.99: sql seeded regression (baseline vs degraded)" -- \
	catacomb regress --runs-dir "$runs3" \
	--baseline label:basket=e2e-sql,variant=baseline \
	--candidate label:basket=e2e-sql,variant=degraded \
	--annotation-rate-delta 0.99 --json

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

echo "== l2. regress --z 3.0: sql A-vs-A still reports zero regressions (non-flip smoke) =="
# A wider one-sided Wilson z only WIDENS rate confidence intervals (regress/wilson.go),
# making the bHi<cLo separation needed for a rate-based regression HARDER to satisfy, never
# easier — a purely monotonic safety direction. Since step l's default z=1.645 (plus the
# widened continuous band) already proves zero regressions, z=3.0 on the SAME evidence and
# SAME widened band can only stay at zero; this is a guaranteed non-flip, not live-variance-
# dependent.
run_json 0 "$artifacts/regress-sql-AvA-z.json" \
	"z 3.0: sql A-vs-A (baseline vs baseline2), continuous band widened" -- \
	catacomb regress --runs-dir "$runs3" \
	--baseline label:basket=e2e-sql,variant=baseline \
	--candidate label:basket=e2e-sql,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --z 3.0 --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$artifacts/regress-sql-AvA-z.json" || rc=$?
record "$rc" "sql A-vs-A reports zero regressions at --z 3.0 (non-flip: widening z only widens Wilson CIs, never causes a false gate)"

echo "== m. bench e2e-subagent basket (15 live claude -p cells) — Task delegation =="
# baseline/baseline2 delegate the seeded SQL task to a subagent (the Task tool);
# degraded runs sqlite3 inline. Claude Code writes each subagent's turns to
# subagents/agent-*.jsonl sub-transcripts, and bench snapshots them into the run dir
# next to session.jsonl; the offline pipeline reduces the main session and the
# sub-transcripts TOGETHER, so the real "type":"subagent" node IS present in bench
# evidence (baseline runs produce a subagents/ dir; degraded, running inline, produce
# none). The gate below reduces the FULL evidence per run (session.jsonl +
# subagents/agent-*.jsonl) and counts that real subagent node. Same default projects-dir
# as the SQL basket: live `claude -p` writes transcripts under ~/.claude/projects, so
# bench reads from there too (no --projects-dir override).
run_expect 0 "bench e2e-subagent basket" -- \
	catacomb bench basket-subagent.yaml --runs-dir "$runs4" --manifest "$manifest4"

echo "== n. subagent delegation separation (seeded regression): baseline delegates in a majority, degraded near-never =="
# Delegating to a real subagent is inherently nondeterministic: the child agent's internal
# tool sequence jitters run-to-run and its latency can time a cell out. But the delegation
# IS observable in bench evidence as the real "type":"subagent" node: Claude Code writes each
# subagent's turns to subagents/agent-*.jsonl sub-transcripts, bench snapshots them next to
# session.jsonl, and the offline pipeline reduces the main session and the sub-transcripts
# TOGETHER, synthesizing the subagent node. The signal is therefore that subagent node:
# baseline is instructed to delegate (subagent node present in a majority of the runs that
# produced a transcript, ~5/5), degraded to run inline (no subagents/ dir -> no subagent
# node, ~0/5). We gate on that separation -- tolerant of timeouts (they only shrink the
# denominator) and an occasional stray delegation. The helper reduces the FULL evidence per
# run (session.jsonl + subagents/agent-*.jsonl); a bare `replay session.jsonl` would miss the
# sub-transcripts and see 0 subagent nodes, hence the concat. The import scenario exercises
# the separate `import` entry point on an interactive session; this live gate proves the real
# subagent node is present in bench evidence and drops under the seeded instruction.
count_subagent_nodes() { # <variant> -> "hits total"
	local variant="$1" hits=0 total=0 d comb snap sf
	for d in "$runs4"/bench-e2e-subagent-subagent-"$variant"-r*; do
		[ -f "$d/session.jsonl" ] || continue
		total=$((total + 1))
		comb="$work/subagent-comb-$(basename "$d").jsonl"
		cat "$d/session.jsonl" > "$comb"
		for sf in "$d"/subagents/agent-*.jsonl; do
			[ -f "$sf" ] && cat "$sf" >> "$comb"
		done
		snap="$work/subagent-full-$(basename "$d").jsonl"
		catacomb replay "$comb" --export-jsonl "$snap" >/dev/null 2>&1 || continue
		if grep -q '"type":"subagent"' "$snap"; then hits=$((hits + 1)); fi
	done
	printf '%s %s' "$hits" "$total"
}
read -r subagent_base_hits subagent_base_total <<<"$(count_subagent_nodes baseline)"
read -r subagent_deg_hits subagent_deg_total <<<"$(count_subagent_nodes degraded)"
read -r subagent_base2_hits subagent_base2_total <<<"$(count_subagent_nodes baseline2)"
rc=0
{ [ "$subagent_base_hits" -ge 3 ] && [ "$subagent_deg_hits" -le 1 ]; } || rc=1
record "$rc" "subagent delegation: real subagent node present in a majority of baseline runs, absent in degraded (baseline $subagent_base_hits/$subagent_base_total >=3 vs degraded $subagent_deg_hits/$subagent_deg_total <=1)"

echo "== o. subagent A-vs-A control (baseline vs baseline2) must NOT gate =="
# baseline and baseline2 both delegate to a subagent, so presence and the annotation axis
# are equal. Only the continuous metrics can drift on live API latency/cost/token jitter, so the
# continuous band is WIDENED (same rationale as steps c/f/l). --fail-on-notable is deliberately
# OMITTED: subagent-internal tool jitter produces spurious step-presence notables between
# identical batches (the reason step n gates structurally on the real subagent node reduced from
# the full evidence, not on notables), so this control widens only the continuous band and
# asserts zero regressions.
run_json 0 "$artifacts/regress-subagent-AvA.json" \
	"subagent A-vs-A must NOT gate (continuous band widened)" -- \
	catacomb regress --runs-dir "$runs4" \
	--baseline label:basket=e2e-subagent,variant=baseline \
	--candidate label:basket=e2e-subagent,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$artifacts/regress-subagent-AvA.json" || rc=$?
record "$rc" "subagent A-vs-A reports zero regressions"

echo "== o2. subagent A-vs-A delegation specificity: baseline2 also delegates in a majority (no spurious separation) =="
# The seeded gate (step n) fires on baseline-vs-degraded real subagent-node presence separation
# (the node reduced from the full evidence: session.jsonl + subagents/agent-*.jsonl). Its
# specificity control is that two IDENTICAL delegating variants show NO separation: baseline2
# must also delegate in a majority, so the separation step n detects is a real
# degraded-instruction effect, not batch-to-batch noise. Reuses the counts from step n.
rc=0
[ "$subagent_base2_hits" -ge 3 ] || rc=1
record "$rc" "subagent A-vs-A: baseline2 also delegates in a majority ($subagent_base2_hits/$subagent_base2_total >=3), no spurious separation"

echo "== p. bench e2e-skill basket (15 live claude -p cells) — Skill delegation =="
# baseline/baseline2 invoke the real project-scoped e2e-emit skill (the Skill tool ->
# a Skill step node; the skill writes the CATACOMB-SKILL-OK token to out/result.csv);
# degraded writes the SAME token inline with Write. The workspace stages the skill dir
# into each cell's .claude/skills/ so --setting-sources project discovers it. Same default
# projects-dir as the SQL/subagent baskets: live `claude -p` writes transcripts under
# ~/.claude/projects, so bench reads from there too (no --projects-dir override).
run_expect 0 "bench e2e-skill basket" -- \
	catacomb bench basket-skill.yaml --runs-dir "$runs5" --manifest "$manifest5"

echo "== q. skill seeded regression (baseline vs degraded) — dropped Skill node must gate =="
# Both baseline and degraded write the CORRECT token (degraded just uses Write inline),
# so verifier.pass stays green on both — the verifier is a co-signal that the artifact is
# still correct, NOT the regression axis here. The only seeded regression is the missing
# Skill step node: baseline invokes the skill (Skill step present ~5/5), degraded does not
# (0/5). A Skill step in baseline and none in degraded drops step alignment coverage below
# --coverage-floor, so regress downgrades the step-presence regression to `notable` (the
# same applyDowngrade path as the echo step d2 / subagent step n cases). --fail-on-notable
# is therefore REQUIRED to gate it (exit 1); a notable-only report otherwise exits 0.
# Matched by detail (a step present across baseline reps dropping to `-> 0/5` in degraded),
# NOT by node name — for parity with the subagent case, where the live reduced name differs
# from the fixture (`Agent`) and the decisive aggregate finding carries a null name. The
# clean A-vs-A control below keeps this attribution non-vacuous.
run_json 1 "$artifacts/regress-skill-degraded.json" \
	"skill seeded regression (baseline vs degraded, dropped skill node)" -- \
	catacomb regress --runs-dir "$runs5" \
	--baseline label:basket=e2e-skill,variant=baseline \
	--candidate label:basket=e2e-skill,variant=degraded --fail-on-notable --json
rc=0
python3 - "$artifacts/regress-skill-degraded.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "step" and f.get("metric") == "presence"
    and f.get("verdict") in ("regression", "notable")
    and "-> 0/5" in str(f.get("detail", ""))
]
if not hits:
    print("no step-scope presence-drop (-> 0/5) notable finding; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
h = hits[0]
print(f"decisive finding: step {h.get('name')!r} presence notable ({h.get('detail', '')})")
PY
record "$rc" "skill degraded gate attributed to a dropped skill step-scope presence notable"

echo "== q2. skill node synthesis: skill graph node present in >=1 baseline run, absent in ALL degraded runs =="
# As with the subagent case (step n2), the `-> 0/5` matcher in step q proves a step dropped
# and the gate fired, but not that catacomb attributed the drop to the skill node itself
# (the decisive aggregate presence finding carries a null name). This is the live equivalent
# of the hermetic node-type proof (hermetic/prod/scenarios/30-skill.sh): replay each run's
# session to a JSONL graph snapshot and assert the synthesized "type":"skill" node appears
# in >=1 baseline run and in NO degraded run. Live invocation is not always 5/5, so the
# baseline side is asserted as "at least one"; the degraded side stays strict (none).
rc=0
skill_base_hits=0
for d in "$runs5"/bench-e2e-skill-skill-baseline-r*; do
	[ -f "$d/session.jsonl" ] || continue
	snap="$work/skill-node-$(basename "$d").jsonl"
	catacomb replay "$d/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1 || continue
	if grep -q '"type":"skill"' "$snap"; then skill_base_hits=$((skill_base_hits + 1)); fi
done
[ "$skill_base_hits" -ge 1 ] || rc=1
for d in "$runs5"/bench-e2e-skill-skill-degraded-r*; do
	[ -f "$d/session.jsonl" ] || continue
	snap="$work/skill-node-$(basename "$d").jsonl"
	if ! catacomb replay "$d/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1; then rc=1; continue; fi
	if grep -q '"type":"skill"' "$snap"; then rc=1; fi
done
record "$rc" "skill node present in >=1 baseline run and absent in all degraded runs"

echo "== q3. regress --coverage-floor 0: dropped-skill finding reports regression, not the default-downgraded notable =="
# Step q's gate needs --fail-on-notable because low step-alignment coverage (the Skill step
# key present in baseline, absent from degraded) downgrades the true regression to `notable`
# (regress/regress.go applyDowngrade). --coverage-floor 0 is satisfied by ANY coverage
# fraction (always >= 0), so StepsTrusted is always true and the downgrade never fires: the
# same underlying finding reports `regression` outright, gating WITHOUT --fail-on-notable —
# a deterministic mechanism, not dependent on real API variance.
run_json 1 "$artifacts/regress-skill-coveragefloor0.json" \
	"coverage-floor 0: skill baseline-vs-degraded, step alignment always trusted" -- \
	catacomb regress --runs-dir "$runs5" \
	--baseline label:basket=e2e-skill,variant=baseline \
	--candidate label:basket=e2e-skill,variant=degraded \
	--coverage-floor 0 --json
rc=0
python3 - "$artifacts/regress-skill-coveragefloor0.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "step" and f.get("metric") == "presence"
    and f.get("verdict") == "regression"
    and "-> 0/5" in str(f.get("detail", ""))
]
if not hits:
    print("no step-scope presence REGRESSION (undowngraded) with a -> 0/5 drop; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
h = hits[0]
print(f"decisive finding: step {h.get('name')!r} presence regression (not notable) at coverage-floor 0 ({h.get('detail', '')})")
PY
record "$rc" "--coverage-floor 0 reports the dropped-skill step finding as regression (step q's default floor downgrades it to notable)"

echo "== r. skill A-vs-A control (baseline vs baseline2) must NOT gate =="
# baseline and baseline2 both invoke the skill, so both carry the Skill step node and both
# verify (~5/5) — presence and the annotation axis are equal. Only the continuous metrics
# can drift on live API latency/cost/token jitter, so the continuous band is WIDENED (same
# rationale as steps c/f/l/o). Unlike the seeded gate above, --fail-on-notable is deliberately
# OMITTED here: real API duration outliers between identical batches would false-gate as
# notables (the other A-vs-A controls omit it for the same reason), so this control widens
# only the continuous band and asserts zero regressions.
run_json 0 "$artifacts/regress-skill-AvA.json" \
	"skill A-vs-A must NOT gate (continuous band widened)" -- \
	catacomb regress --runs-dir "$runs5" \
	--baseline label:basket=e2e-skill,variant=baseline \
	--candidate label:basket=e2e-skill,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$artifacts/regress-skill-AvA.json" || rc=$?
record "$rc" "skill A-vs-A reports zero regressions"

echo "== s. bench e2e-mcp basket (15 live claude -p cells) — live MCP handshake =="
# baseline/baseline2 call the record tool over a real stdio MCP server (mcp__e2ekit__record
# -> a general MCP step node; the server persists CATACOMB-SKILL-OK to the cell's
# out/result.csv via the absolute E2EKIT_OUT, which verify_emit.py scores into
# verifier.pass ~5/5); degraded uses no tool, so it drops BOTH the MCP step node AND the
# artifact — the one basket where both the presence and the verifier axes gate. Same default
# projects-dir as the SQL/subagent/skill baskets: live `claude -p` writes transcripts under
# ~/.claude/projects, so bench reads from there too (no --projects-dir override).
run_expect 0 "bench e2e-mcp basket" -- \
	catacomb bench basket-mcp.yaml --runs-dir "$runs6" --manifest "$manifest6"

echo "== t. mcp seeded regression (baseline vs degraded) — dropped MCP node + failed verifier must gate =="
# Unlike the subagent/skill baskets (where degraded still produces the correct artifact and
# only the step node drops), the MCP degraded cell uses NO tool, so it neither records the
# value nor writes out/result.csv. TWO seeded regressions therefore fire together:
#   (1) ann:verifier.pass drops ~5/5 -> 0/5 — a `regression` verdict that gates by DEFAULT
#       (the primary signal), and
#   (2) the mcp__e2ekit__record step node is present in baseline and absent in degraded;
#       that drops step alignment coverage below --coverage-floor, so regress downgrades the
#       step-presence regression to `notable` (the same applyDowngrade path as the echo/
#       subagent/skill step cases). --fail-on-notable is passed so the notable also gates,
#       and both findings are asserted below.
run_json 1 "$artifacts/regress-mcp-degraded.json" \
	"mcp seeded regression (baseline vs degraded, dropped MCP node + failed verifier)" -- \
	catacomb regress --runs-dir "$runs6" \
	--baseline label:basket=e2e-mcp,variant=baseline \
	--candidate label:basket=e2e-mcp,variant=degraded --fail-on-notable --json
rc=0
python3 - "$artifacts/regress-mcp-degraded.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
findings = rep.get("findings", [])
ver = [
    f for f in findings
    if f.get("metric") == "ann:verifier.pass" and f.get("verdict") == "regression"
]
step = [
    f for f in findings
    if f.get("scope") == "step" and f.get("metric") == "presence"
    and f.get("verdict") in ("regression", "notable")
    and "e2ekit" in str(f.get("name", "")).lower()
]
errs = []
if not ver:
    errs.append("no ann:verifier.pass regression finding (the primary DEFAULT-gating signal)")
if not step:
    errs.append("no step-scope mcp__e2ekit__record presence notable finding (the dropped MCP node)")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    print("findings were:", file=sys.stderr)
    for f in findings:
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print(f"decisive findings: ann:verifier.pass {ver[0].get('detail', '')} (regression) + "
      f"step {step[0].get('name')!r} presence notable ({step[0].get('detail', '')})")
PY
record "$rc" "mcp degraded gate: BOTH ann:verifier.pass regression AND dropped mcp__e2ekit__record step notable"

echo "== u. mcp A-vs-A control (baseline vs baseline2) must NOT gate =="
# baseline and baseline2 both call the record tool, so both carry the mcp__e2ekit__record
# step node and both verify (~5/5) — presence and the annotation axis are equal. Only the
# continuous metrics can drift on live API latency/cost/token jitter, so the continuous band
# is WIDENED (same rationale as steps c/f/l/o/r). Unlike the seeded gate above,
# --fail-on-notable is deliberately OMITTED here: real API duration outliers between identical
# batches would false-gate as notables (the other A-vs-A controls omit it for the same
# reason), so this control widens only the continuous band and asserts zero regressions.
run_json 0 "$artifacts/regress-mcp-AvA.json" \
	"mcp A-vs-A must NOT gate (continuous band widened)" -- \
	catacomb regress --runs-dir "$runs6" \
	--baseline label:basket=e2e-mcp,variant=baseline \
	--candidate label:basket=e2e-mcp,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$artifacts/regress-mcp-AvA.json" || rc=$?
record "$rc" "mcp A-vs-A reports zero regressions"

echo "== v. optional codex live leg (runtime: codex — 6 live codex exec cells) =="
# The codex leg is OPTIONAL: unlike claude (hard-required at the top of this
# driver), a missing or unauthenticated codex CLI SKIPS this section and leaves
# the overall exit unaffected. Auth is probed cheapest-first:
#   (1) `codex login status` — reads the stored credentials (a ChatGPT login or a
#       key saved via `codex login --with-api-key`) locally, with NO network call
#       and NO spend; exit 0 means authenticated. It cannot see env-var auth, so
#   (2) when it fails and CODEX_API_KEY is set (the CI path: e2e-live.yml exports
#       the secret unconditionally, absent -> empty -> this arm is skipped), one
#       minimal live `codex exec` ping (gpt-5.4-mini, low reasoning effort, 60s
#       cap) verifies the key end-to-end — codex only honors CODEX_API_KEY on
#       `exec`, so a real exec is the cheapest reliable check. That ping is
#       token-billed and counted in the spend note below. The cap is mandatory:
#       when timeout(1) is unavailable the leg SKIPS rather than running a paid
#       ping uncapped.
# The basket mirrors basket-continuous on the codex runtime: `main` answers
# tersely, `candidate` reasons verbosely (a tokens_out growth a regress SHOULD
# eventually flag) — but with n=3 on a brand-new runtime the verdict is LOGGED,
# not asserted (gather calibration data first); only exit 2 — regress itself
# erroring — fails. Rollouts land under the DEFAULT --sessions-dir
# (~/.codex/sessions), exactly where live `codex exec` writes them — the codex
# analogue of the claude baskets' default-projects-dir contract.
codex_ping_probe() {
	timeout 60 codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "ping" </dev/null >/dev/null 2>&1
}
codex_leg_ran=0
codex_probe_paid=0
if ! command -v codex >/dev/null 2>&1; then
	skip "codex live leg: codex CLI not on PATH (npm install -g @openai/codex) — leg not run, overall exit unaffected"
elif codex login status >/dev/null 2>&1; then
	echo "  [info] codex auth: stored login (codex login status — no-spend probe)"
	codex_leg_ran=1
elif [ -n "${CODEX_API_KEY:-}" ] && ! command -v timeout >/dev/null 2>&1; then
	skip "codex live leg: timeout(1) unavailable — refusing to run the paid CODEX_API_KEY ping uncapped — leg not run, overall exit unaffected"
elif [ -n "${CODEX_API_KEY:-}" ] && codex_ping_probe; then
	echo "  [info] codex auth: CODEX_API_KEY verified via one live exec ping (token-billed, counted in the spend note)"
	codex_leg_ran=1
	codex_probe_paid=1
else
	skip "codex live leg: codex unauthenticated (no stored login; CODEX_API_KEY unset, rejected, or probe failed/timed out) — leg not run, overall exit unaffected"
fi

if [ "$codex_leg_ran" -eq 1 ]; then
	run_expect 0 "bench codex basket (6 live codex exec cells)" -- \
		catacomb bench basket-codex.yaml --runs-dir "$runs7" --manifest "$manifest7"
	rc=0
	python3 - "$manifest7" <<'PY' || rc=$?
import json, os, sys

entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
errs = []
if len(entries) != 6:
    errs.append(f"expected 6 cells (1 task x 2 variants x 3 reps), got {len(entries)}")
for e in entries:
    rid = e.get("run_id", "?")
    if not e.get("marked"):
        errs.append(f"{rid}: not marked (thread.started peek -> rollout resolution failed) note={e.get('note','')}")
    if e.get("exit_code") != 0:
        errs.append(f"{rid}: exit_code={e.get('exit_code')} note={e.get('note','')}")
    if not e.get("session_id"):
        errs.append(f"{rid}: empty session_id note={e.get('note','')}")
    if "cost_usd" in e:
        errs.append(f"{rid}: cost_usd present in the manifest (codex reports no dollar cost; want the key absent)")
    ev = e.get("evidence_dir", "")
    if not ev or not os.path.isdir(ev):
        errs.append(f"{rid}: evidence_dir missing on disk: {ev!r}")
        continue
    try:
        meta = json.load(open(os.path.join(ev, "meta.json")))
    except (OSError, ValueError) as ex:
        errs.append(f"{rid}: meta.json unreadable: {ex}")
        continue
    rt = (meta.get("env") or {}).get("agent_runtime")
    if rt != "codex":
        errs.append(f"{rid}: meta agent_runtime={rt!r} (want 'codex')")
if errs:
    print("codex manifest assertion failures:", file=sys.stderr)
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"codex manifest OK: {len(entries)} cells marked, all exit 0, session ids + "
      f"evidence present, agent_runtime=codex stamped on every meta, no cost_usd")
PY
	record "$rc" "codex manifest: 6/6 marked, exit 0, session+evidence present, every meta stamps agent_runtime=codex"

	echo "-- codex regress (main vs candidate): must render + --json must parse; verdict logged, NOT asserted --"
	rc=0
	catacomb regress --runs-dir "$runs7" \
		--baseline label:basket=e2e-codex,variant=main \
		--candidate label:basket=e2e-codex,variant=candidate \
		>"$artifacts/regress-codex.txt" 2>&1 || rc=$?
	if [ "$rc" -le 1 ] && [ -s "$artifacts/regress-codex.txt" ]; then
		pass "codex regress renders (exit $rc; 0 and 1 both acceptable)"
	else
		failrec "codex regress render failed (exit $rc, want 0 or 1 with output; report: $artifacts/regress-codex.txt)"
	fi
	rc=0
	catacomb regress --runs-dir "$runs7" \
		--baseline label:basket=e2e-codex,variant=main \
		--candidate label:basket=e2e-codex,variant=candidate --json \
		>"$artifacts/regress-codex.json" 2>/dev/null || rc=$?
	if [ "$rc" -le 1 ] && python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$artifacts/regress-codex.json" 2>/dev/null; then
		pass "codex regress --json parses (exit $rc; 0 and 1 both acceptable)"
	else
		failrec "codex regress --json failed (exit $rc, want 0 or 1 with parseable json: $artifacts/regress-codex.json)"
	fi
	echo "  [info] codex main-vs-candidate: overall_verdict=$(verdict_of "$artifacts/regress-codex.json") (informational — n=3 on a new runtime, calibration data only)"
fi

echo "== x. bench e2e-failmode basket — --fail-fast (\$0) + --error-delta (near-\$0 Haiku) =="
# basket-failmode.yaml declares its `prefail` variant FIRST, and bench's cell
# expansion is rep-major (rep 1 x every task x every variant, in declaration
# order) — so under --fail-fast this call executes ONLY rep 1's prefail cell
# (failmode.sh: FAILMODE=prefail exits 1 BEFORE any claude invocation) and stops
# right there: a single manifest entry, true $0, regardless of `reps`. A
# follow-up --resume call over the same manifest then executes the remaining 8
# cells: 2 more $0 prefail retries (harmless noise — bench does not propagate a
# single cell's failure into its own exit code without --fail-fast, see the
# hermetic 81-failmode.sh non-vacuity check) plus the 3 clean + 3 errseed live
# Haiku cells `regress --error-delta` needs.
run_json 1 "$work/failmode-prefail.out" \
	"bench failmode --fail-fast stops after the first (prefail) cell" -- \
	catacomb bench basket-failmode.yaml --runs-dir "$runs8" --manifest "$manifest8" --fail-fast
rc=0
grep -q 'fail-fast' "$work/failmode-prefail.out.stderr" || rc=1
record "$rc" "fail-fast error names the flag on stderr"
rc=0
mlines=$(wc -l <"$manifest8" | tr -d ' ')
[ "$mlines" -eq 1 ] || rc=1
record "$rc" "failmode manifest has exactly 1 entry after --fail-fast ($mlines/1) — \$0 stop"
rc=0
grep -q '"variant":"prefail"' "$manifest8" || rc=1
grep -q '"exit_code":1' "$manifest8" || rc=1
record "$rc" "the sole failmode manifest entry is the prefail cell, exit_code 1"

echo "== x2. bench failmode --resume: the remaining toolerr cells (clean/errseed) execute live =="
run_expect 0 "bench failmode --resume executes the remaining cells" -- \
	catacomb bench basket-failmode.yaml --runs-dir "$runs8" --manifest "$manifest8" --resume
rc=0
python3 - "$manifest8" <<'PY' || rc=$?
import json, os, sys

entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
errs = []
if len(entries) != 9:
    errs.append(f"expected 9 cells (1 task x 3 variants x 3 reps), got {len(entries)}")
by_variant = {"prefail": [], "clean": [], "errseed": []}
for e in entries:
    v = e.get("variant")
    if v in by_variant:
        by_variant[v].append(e)
for e in by_variant["prefail"]:
    if e.get("exit_code") != 1:
        errs.append(f"{e.get('run_id')}: prefail exit_code={e.get('exit_code')} want 1")
for v in ("clean", "errseed"):
    for e in by_variant[v]:
        rid = e.get("run_id", "?")
        if e.get("exit_code") != 0:
            errs.append(f"{rid}: exit_code={e.get('exit_code')} note={e.get('note','')}")
        if not e.get("session_id"):
            errs.append(f"{rid}: empty session_id note={e.get('note','')}")
        ev = e.get("evidence_dir", "")
        if not ev or not os.path.isdir(ev):
            errs.append(f"{rid}: evidence_dir missing on disk: {ev!r}")
if len(by_variant["clean"]) != 3 or len(by_variant["errseed"]) != 3:
    errs.append(f"clean={len(by_variant['clean'])}/3 errseed={len(by_variant['errseed'])}/3")
if errs:
    print("failmode manifest assertion failures:", file=sys.stderr)
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"failmode manifest OK: {len(entries)} cells; prefail 3x exit1 (\$0), "
      f"clean 3/3 + errseed 3/3 live cells exit0/session/evidence present")
PY
record "$rc" "failmode manifest: 9 cells; prefail exit1 x3, clean&errseed 3/3 exit0/session/evidence"

echo "== x3. failmode seeded error-delta regression (clean vs errseed) must gate on error_rate =="
run_json 1 "$artifacts/regress-failmode-errseed.json" \
	"failmode seeded regression (clean vs errseed, --error-delta)" -- \
	catacomb regress --runs-dir "$runs8" \
	--baseline label:basket=e2e-failmode,variant=clean \
	--candidate label:basket=e2e-failmode,variant=errseed \
	--error-delta 0.5 --json
rc=0
python3 - "$artifacts/regress-failmode-errseed.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "total" and f.get("metric") == "error_rate"
    and f.get("verdict") == "regression"
]
if not hits:
    print("no total error_rate regression finding; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "metric", "verdict", "baseline", "candidate")}, file=sys.stderr)
    sys.exit(1)
h = hits[0]
print(f"decisive finding: total error_rate {h.get('baseline')} -> {h.get('candidate')} (regression, --error-delta 0.5)")
PY
record "$rc" "failmode errseed gate attributed to total error_rate regression"

echo "== x4. failmode error tool-node synthesis: errseed carries a tool_call error node in >=1 run, clean in NONE =="
rc=0
err_hits=0
for d in "$runs8"/bench-e2e-failmode-failmode-errseed-r*; do
	[ -f "$d/session.jsonl" ] || continue
	snap="$work/failmode-node-$(basename "$d").jsonl"
	catacomb replay "$d/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1 || continue
	if grep -q '"status":"error"' "$snap"; then err_hits=$((err_hits + 1)); fi
done
[ "$err_hits" -ge 1 ] || rc=1
for d in "$runs8"/bench-e2e-failmode-failmode-clean-r*; do
	[ -f "$d/session.jsonl" ] || continue
	snap="$work/failmode-node-$(basename "$d").jsonl"
	if ! catacomb replay "$d/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1; then rc=1; continue; fi
	if grep -q '"status":"error"' "$snap"; then rc=1; fi
done
record "$rc" "errseed carries a tool_call error node in >=1 run; clean carries none"

echo "== w. cost report =="
python3 - "$manifest1" "$manifest2" "$manifest3" "$manifest4" "$manifest5" "$manifest6" "$manifest8" "$artifacts/cost.txt" <<'PY'
import json, sys

total = 0.0
for p in sys.argv[1:8]:
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
open(sys.argv[8], "w").write(f"total live spend: ${total:.2f}\n")
print(f"total live spend: ${total:.2f}")
PY
# The codex leg is token-billed and codex reports NO cost_usd, so it can never be
# part of the dollar total above — note it separately so the spend record is honest.
if [ "$codex_leg_ran" -eq 1 ]; then
	codex_probe_note=""
	if [ "$codex_probe_paid" -eq 1 ]; then
		codex_probe_note=" + 1 auth-probe ping"
	fi
	codex_note="codex leg: 6 cells${codex_probe_note} on gpt-5.4-mini — token-billed, no reported dollar cost (excluded from the total above)"
else
	codex_note="codex leg: skipped (no codex spend)"
fi
echo "$codex_note"
printf '%s\n' "$codex_note" >>"$artifacts/cost.txt"

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
