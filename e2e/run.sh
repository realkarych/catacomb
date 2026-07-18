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

# --- model-policy guardrail (Task 10) -----------------------------------------
# The mixed Haiku+Sonnet policy is DELIBERATE, not incidental: the five
# delegation-sensitive baskets — presence (the haiku checkpoint-mark task), sql,
# subagent, skill, and mcp — pin CHILD_MODEL: claude-sonnet-5 because
# claude-haiku-4-5 no longer reliably follows their mark / multi-step / delegation
# instructions (each basket's own comment records the measured baseline failure).
# A silent blanket-Haiku swap would re-introduce those failures and quietly defang
# the gate, so this $0 static check over the REAL basket files fails loudly and
# early if any sensitive basket loses its Sonnet pin. The cheap baskets —
# continuous, the presence `echo` step task, and failmode — stay on the default
# Haiku, and are asserted to NOT pin Sonnet. Runs on every invocation, before any
# spend; no fixtures, no auth, no network.
sonnet_pin='CHILD_MODEL: claude-sonnet-5'
for b in basket-presence.yaml basket-sql.yaml basket-subagent.yaml basket-skill.yaml basket-mcp.yaml; do
	grep -q "$sonnet_pin" "$e2e_dir/$b" ||
		fatal "model-policy guardrail: $b lost its Sonnet pin ('$sonnet_pin') — a delegation-sensitive basket must NOT default to Haiku (a blanket-Haiku swap would re-introduce the measured mark/sql/delegation failures)"
done
# presence pins Sonnet on the haiku checkpoint-mark task ONLY — exactly one pin
# line — so the `echo` step task is left on the default Haiku.
presence_pins="$(grep -c "$sonnet_pin" "$e2e_dir/basket-presence.yaml" || true)"
[ "$presence_pins" -eq 1 ] ||
	fatal "model-policy guardrail: basket-presence.yaml has $presence_pins Sonnet pins (want exactly 1 — the haiku checkpoint-mark task; the echo step task must stay on Haiku)"
# The cheap baskets must NOT pin Sonnet (they default to Haiku).
for b in basket-continuous.yaml basket-failmode.yaml; do
	if grep -q "$sonnet_pin" "$e2e_dir/$b"; then
		fatal "model-policy guardrail: $b pins Sonnet ('$sonnet_pin') but must stay on the default Haiku (the mixed-model policy keeps Sonnet off the cheap baskets)"
	fi
done
# failmode.sh's live cells default to Haiku when CHILD_MODEL is unset (its basket sets none).
grep -q 'CHILD_MODEL:-claude-haiku-4-5' "$e2e_dir/failmode.sh" ||
	fatal "model-policy guardrail: failmode.sh no longer defaults to claude-haiku-4-5 (expected \${CHILD_MODEL:-claude-haiku-4-5})"

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
runs9="$work/runs-codex-mcp"
runs10="$work/runs-codex-subagent"
runs11="$work/runs-codex-skill"
manifest1="$work/manifest-presence.jsonl"
manifest2="$work/manifest-continuous.jsonl"
manifest3="$work/manifest-sql.jsonl"
manifest4="$work/manifest-subagent.jsonl"
manifest5="$work/manifest-skill.jsonl"
manifest6="$work/manifest-mcp.jsonl"
manifest7="$work/manifest-codex.jsonl"
manifest8="$work/manifest-failmode.jsonl"
manifest9="$work/manifest-codex-mcp.jsonl"
manifest10="$work/manifest-codex-subagent.jsonl"
manifest11="$work/manifest-codex-skill.jsonl"
db="$work/e2e.db"
mkdir -p "$runs1" "$runs2" "$runs3" "$runs4" "$runs5" "$runs6" "$runs7" "$runs8" \
	"$runs9" "$runs10" "$runs11"

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
	cp -f "$manifest9" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest10" "$artifacts"/ 2>/dev/null || true
	cp -f "$manifest11" "$artifacts"/ 2>/dev/null || true
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

echo "== e2. paired axis (--paired-test sign|wilcoxon) on the pooled presence selector =="
# Dropping ",task=haiku" from the selector pools BOTH presence tasks (haiku, echo) into
# one regress call. regress/paired.go's pairedTasks() matches baseline/candidate by
# aggregate.TaskStats.Task, so >=2 distinct task ids are required before the paired axis
# has anything to pair; the narrower task=haiku selector used in steps c-e never had more
# than 1 task and so could never exercise this axis at all.
#
# CAUTION (live-flakiness): basket-presence.yaml's PairedMinTasks default is 5 (see
# regress.DefaultThresholds), but only 2 task ids exist here — so the paired axis is NOT
# reachable at default settings (matched=2 < 5), on EITHER test. That is fine: a paired
# finding still renders (verdict="insufficient", not filtered — regress.filterFindings
# only drops scope!="total" findings whose verdict IS "ok"), proving --paired-test
# wilcoxon/sign are wired end to end on real evidence. What we do NOT do is hard-assert a
# paired VERDICT (regression/ok) here — that depends on live model variance and, at this
# task count, can never fire anyway. The deterministic sign-vs-wilcoxon verdict FLIP is
# proved hermetically over fixed fixtures with 6 tasks in 82-wilcoxon.sh.
rc=0
catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,variant=baseline \
	--candidate label:basket=e2e-presence,variant=degraded \
	--paired-test wilcoxon --json \
	>"$artifacts/regress-presence-paired-wilcoxon.json" 2>"$artifacts/regress-presence-paired-wilcoxon.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "paired wilcoxon (pooled haiku+echo): flag accepted (exit $rc)"
else
	failrec "paired wilcoxon (pooled haiku+echo): flag accepted (exit $rc, want 0 or 1)"
	sed 's/^/        stderr: /' "$artifacts/regress-presence-paired-wilcoxon.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/regress-presence-paired-wilcoxon.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
paired = [f for f in rep.get("findings", []) if f.get("scope") == "paired"]
if not paired:
    print("no paired-scope finding rendered; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "metric", "verdict")}, file=sys.stderr)
    sys.exit(1)
print("--paired-test wilcoxon renders " + str(len(paired)) + " paired-scope finding(s): "
      + ", ".join(f"{f['metric']}={f['verdict']}" for f in paired))
PY
record "$rc" "--paired-test wilcoxon --json renders >=1 paired-scope finding on the pooled haiku+echo selector"

rc=0
catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,variant=baseline \
	--candidate label:basket=e2e-presence,variant=degraded \
	--paired-test sign --json \
	>"$artifacts/regress-presence-paired-sign.json" 2>"$artifacts/regress-presence-paired-sign.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "paired sign (pooled haiku+echo): flag accepted (exit $rc)"
else
	failrec "paired sign (pooled haiku+echo): flag accepted (exit $rc, want 0 or 1)"
	sed 's/^/        stderr: /' "$artifacts/regress-presence-paired-sign.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/regress-presence-paired-sign.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
paired = [f for f in rep.get("findings", []) if f.get("scope") == "paired"]
if not paired:
    print("no paired-scope finding rendered; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "metric", "verdict")}, file=sys.stderr)
    sys.exit(1)
print("--paired-test sign renders " + str(len(paired)) + " paired-scope finding(s): "
      + ", ".join(f"{f['metric']}={f['verdict']}" for f in paired))
PY
record "$rc" "--paired-test sign --json renders >=1 paired-scope finding on the pooled haiku+echo selector"

# LOG only (not asserted): the sign-vs-wilcoxon per-metric verdict comparison on THIS
# live evidence. At 2 matched tasks (< PairedMinTasks=5) both tests report the SAME
# "insufficient" verdict on every metric regardless of model output, so there is nothing
# to gate on here; a real flip needs >=5 matched tasks, which this basket does not have.
python3 - "$artifacts/regress-presence-paired-wilcoxon.json" "$artifacts/regress-presence-paired-sign.json" <<'PY'
import json, sys


def paired(path):
    rep = json.load(open(path))
    return {f["metric"]: f["verdict"] for f in rep.get("findings", []) if f.get("scope") == "paired"}


wil, sig = paired(sys.argv[1]), paired(sys.argv[2])
print(f"  [info] paired axis per-metric verdicts: wilcoxon={wil!r} sign={sig!r} (logged only, not gated — see 82-wilcoxon.sh for the asserted flip on deterministic fixtures)")
PY

echo "== e3. --paired-min-tasks 3 --paired-alpha 0.15: paired axis reports NOT reachable =="
# Deterministic regardless of live model output: regress/paired.go's pairedSensitivity
# matches tasks by id across the baseline/candidate groups, requiring
# TaskStats.Runs>=MinSupport(3) on both sides. basket-presence.yaml's 2 task ids (haiku,
# echo) each ran 5 reps/variant with exit_code==0 for all 30 cells (already asserted in
# step b), so exactly 2 tasks are ALWAYS matched here, independent of any model verdict.
#
# regress/paired.go's smallestFiringTasks() = max(--paired-min-tasks,
# minUnanimousTasks(--paired-alpha)), where minUnanimousTasks(alpha) is the smallest n
# with 2^-n <= alpha. At alpha=0.15: 2^-2=0.25 > 0.15 but 2^-3=0.125 <= 0.15, so
# minUnanimousTasks(0.15)=3 — matching --paired-min-tasks 3 exactly, so BOTH flags bind
# the same floor (3) and neither is redundant with the other. 2 matched tasks stays below
# that floor, so the axis reports reachable=false with min_tasks==3 (proving both flags
# parsed and took effect, not just accepted as no-ops).
rc=0
catacomb regress --runs-dir "$runs1" \
	--baseline label:basket=e2e-presence,variant=baseline \
	--candidate label:basket=e2e-presence,variant=degraded \
	--paired-test sign --paired-min-tasks 3 --paired-alpha 0.15 --json \
	>"$artifacts/regress-presence-paired-mintasks.json" 2>"$artifacts/regress-presence-paired-mintasks.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "paired-min-tasks 3 + paired-alpha 0.15: flags accepted (exit $rc)"
else
	failrec "paired-min-tasks 3 + paired-alpha 0.15: flags accepted (exit $rc, want 0 or 1)"
	sed 's/^/        stderr: /' "$artifacts/regress-presence-paired-mintasks.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/regress-presence-paired-mintasks.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
sens = rep.get("sensitivity") or {}
paired = sens.get("paired")
if paired is None:
    print(f"sensitivity.paired absent; sensitivity was {sens!r} (want paired.reachable=false)", file=sys.stderr)
    sys.exit(1)
if paired.get("reachable") is not False:
    print(f"sensitivity.paired.reachable={paired.get('reachable')!r} want False (2 matched tasks < 3)", file=sys.stderr)
    sys.exit(1)
if paired.get("min_tasks") != 3:
    print(f"sensitivity.paired.min_tasks={paired.get('min_tasks')!r} want 3 (max(--paired-min-tasks 3, minUnanimousTasks(0.15)=3))", file=sys.stderr)
    sys.exit(1)
print(f"--paired-min-tasks 3 --paired-alpha 0.15: sensitivity.paired={paired!r} (not reachable, min_tasks=3, as designed)")
PY
record "$rc" "--paired-min-tasks 3 --paired-alpha 0.15: sensitivity reports the paired axis NOT reachable at min_tasks=3 (deterministic on matched-task COUNT, independent of live model verdicts)"

echo "== e4. baseline list --db --json: e2e-presence-main pinned with 5 runs =="
run_json 0 "$artifacts/baseline-list.json" "baseline list --db --json" -- \
	catacomb baseline list --db "$db" --json
rc=0
python3 - "$artifacts/baseline-list.json" <<'PY' || rc=$?
import json, sys

bs = json.load(open(sys.argv[1])) or []
hits = [b for b in bs if b.get("name") == "e2e-presence-main"]
errs = []
if len(hits) != 1:
    errs.append(f"want exactly 1 baseline named e2e-presence-main, got {len(hits)} (all: {[b.get('name') for b in bs]!r})")
b = hits[0] if hits else {}
if len(b.get("run_ids") or []) != 5:
    errs.append(f"run_ids={b.get('run_ids')!r} want 5 (the haiku-baseline cells pinned at step e)")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"baseline list: e2e-presence-main pinned with {len(b['run_ids'])} runs")
PY
record "$rc" "baseline list --json shows e2e-presence-main with runs=5"

echo "== e5. baseline export e2e-presence-main -> non-empty bundle =="
run_expect 0 "baseline export e2e-presence-main" -- \
	catacomb baseline export e2e-presence-main --db "$db" --runs-dir "$runs1" \
	--out "$work/live-baseline.tar.gz"
rc=0
[ -s "$work/live-baseline.tar.gz" ] || rc=1
record "$rc" "baseline export bundle is non-empty"

echo "== e6. baseline import into a fresh db+runs: reimported count matches; regress gates IDENTICALLY to step d =="
# Import only lands the pinned baseline's own runs (the 5 haiku-baseline cells); the
# degraded candidate evidence is copied in separately, exactly as the hermetic mirror
# (e2e/hermetic/run.sh step 18) copies its own candidate cells alongside an imported
# baseline. The regress call below reuses step d's EXACT selector shape (name:
# e2e-presence-main resolves to the identical 5 run ids the label selector did at step
# d, since no new haiku-baseline runs have been created since) so the comparison is
# byte-for-byte the same input — the "gates IDENTICALLY" claim is a determinism claim
# about the pipeline (same evidence -> same verdict), not a live-variance guess.
run_json 0 "$work/baseline-import.out" "baseline import into a fresh db+runs" -- \
	catacomb baseline import "$work/live-baseline.tar.gz" \
	--db "$work/import.db" --runs-dir "$work/import-runs"
rc=0
grep -q "imported baseline e2e-presence-main: 5 runs" "$work/baseline-import.out" || rc=1
record "$rc" "import lands the 5 pinned haiku-baseline cells (reimported run count matches)"
cp -R "$runs1"/bench-e2e-presence-haiku-degraded-r* "$work/import-runs/" 2>/dev/null || true
rc=0
deg_count=$(find "$work/import-runs" -maxdepth 1 -type d -name 'bench-e2e-presence-haiku-degraded-r*' 2>/dev/null | wc -l | tr -d ' ')
[ "$deg_count" -eq 5 ] || rc=1
record "$rc" "candidate (degraded) evidence copied into the fresh import-runs dir ($deg_count/5)"
run_json 1 "$work/regress-imported-degraded.json" \
	"regress over the imported db (name:e2e-presence-main vs degraded)" -- \
	catacomb regress --db "$work/import.db" --runs-dir "$work/import-runs" \
	--baseline name:e2e-presence-main \
	--candidate label:basket=e2e-presence,task=haiku,variant=degraded --json
rc=0
python3 - "$artifacts/regress-presence-degraded.json" "$work/regress-imported-degraded.json" <<'PY' || rc=$?
import json, sys

def proj(p):
    r = json.load(open(p))
    # Matched by (scope, metric, verdict), NOT by finding name -- same convention as
    # the hermetic bundle round-trip (e2e/hermetic/run.sh step 18): a decisive
    # aggregate finding can carry a null name, and mixing None with str in a sort key
    # is a TypeError waiting to happen.
    return (r.get("overall_verdict"), r.get("regressions"),
            sorted((f.get("scope"), f.get("metric"), f.get("verdict"))
                   for f in r.get("findings", [])))

src, imported = proj(sys.argv[1]), proj(sys.argv[2])
if src != imported:
    print(f"source (step d) {src} != imported-db {imported}", file=sys.stderr)
    sys.exit(1)
print("regress over the reimported baseline gates IDENTICALLY to step d (same overall_verdict, regression count, and finding set)")
PY
record "$rc" "reimported baseline's regress vs degraded gates IDENTICALLY to step d"

echo "== e7. record a 2nd history row on e2e-presence-main (baseline vs baseline2, A-vs-A) -> trends --json/--metric =="
# Step e already recorded one row (baseline vs degraded, --strict --record). This adds a
# SECOND row on the SAME baseline (baseline vs baseline2, the A-vs-A control, continuum
# band widened for the same inter-batch drift reason as steps c/f/l/o/r/u) so
# trends --json has >=2 rows to parse and --metric duration_ms has >=2 rows to render.
run_json 0 "$artifacts/regress-presence-record-ava.json" \
	"record 2nd history row (name:e2e-presence-main vs baseline2, A-vs-A must NOT gate)" -- \
	catacomb regress --db "$db" --runs-dir "$runs1" \
	--baseline name:e2e-presence-main \
	--candidate label:basket=e2e-presence,task=haiku,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --record --json
rc=0
catacomb trends e2e-presence-main --db "$db" --json >"$work/trends-presence-2rows.json" 2>"$work/trends-presence-2rows.json.stderr" || rc=$?
if [ "$rc" -eq 0 ]; then
	pass "trends e2e-presence-main --json exits 0 after 2 recorded rows"
else
	failrec "trends e2e-presence-main --json exits 0 after 2 recorded rows (exit $rc)"
	sed 's/^/        stderr: /' "$work/trends-presence-2rows.json.stderr" >&2 || true
fi
rc=0
python3 - "$work/trends-presence-2rows.json" <<'PY' || rc=$?
import json, sys

entries = json.load(open(sys.argv[1]))
errs = []
if len(entries) != 2:
    errs.append(f"want exactly 2 recorded history rows, got {len(entries)}")
for e in entries:
    if "seq" not in e or "record" not in e:
        errs.append(f"entry missing seq/record: {e!r}")
        continue
    rec = e.get("record") or {}
    if "candidate_selector" not in rec:
        errs.append(f"record missing candidate_selector: {rec!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"trends --json: {len(entries)} rows parse (seq+record present on both)")
PY
record "$rc" "trends --json: both recorded rows (degraded + baseline2 A-vs-A) parse with seq/record fields"
rc=0
catacomb trends e2e-presence-main --db "$db" --metric duration_ms \
	>"$work/trends-presence-metric.txt" 2>"$work/trends-presence-metric.txt.stderr" || rc=$?
if [ "$rc" -eq 0 ]; then
	pass "trends e2e-presence-main --metric duration_ms exits 0"
else
	failrec "trends e2e-presence-main --metric duration_ms exits 0 (exit $rc)"
	sed 's/^/        stderr: /' "$work/trends-presence-metric.txt.stderr" >&2 || true
fi
rc=0
{ [ -s "$work/trends-presence-metric.txt" ] && grep -q 'BASELINE-VALUE' "$work/trends-presence-metric.txt"; } || rc=1
mlines=$(grep -c . "$work/trends-presence-metric.txt" || true)
[ "$mlines" -ge 3 ] || rc=1
record "$rc" "trends --metric duration_ms renders the metric table header + 2 data rows ($mlines lines)"

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

echo "== f3. regress --iqr-factor tightened: continuous bands are monotone non-increasing vs step f default; strict narrowing LOGGED =="
# Real API duration/cost/token jitter is unpredictable in advance, so this does not assert
# an exact verdict flip (unlike the fabricated hermetic mirror in 80-cli-contracts.sh, which
# proves the flip deterministically). Instead it asserts the flag's DEMONSTRABLE, always-
# computable effect on the finding itself: shrinking --iqr-factor can only narrow or hold
# the reported band (band = max(metric-rel-delta*median, iqr-factor*IQR)); the monotone
# non-increase is HARD-asserted, and the strict narrowing (near-certain for live duration_ms
# but NOT mathematically guaranteed for any single one of duration_ms/cost_usd/tokens_in/
# tokens_out/nodes across 5 independent live API replicates) is LOGGED, not gated.
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
# Monotone assert only: for fixed evidence band = max(metric-rel-delta*median, iqr-factor*IQR),
# so a SMALLER --iqr-factor can only narrow or hold each band (tight_w <= default_w) -- that
# non-increase is guaranteed and HARD-asserted (a widening would be a real bug). A STRICT
# narrowing needs the IQR term to have been contributing: near-certain for live duration_ms
# but NOT mathematically guaranteed, so it is LOGGED, not gated. The deterministic strict
# ok->regression band flip is proven hermetically in 80-cli-contracts.sh's iqr group.
widened = {
    m: (default_w[m], tight_w[m])
    for m in default_w
    if m in tight_w and tight_w[m] > default_w[m] + 1e-9
}
narrowed = {
    m: (default_w[m], tight_w[m])
    for m in default_w
    if m in tight_w and tight_w[m] < default_w[m] - 1e-9
}
if widened:
    print(f"a band WIDENED under a tighter --iqr-factor (impossible for fixed evidence): {widened!r}", file=sys.stderr)
    sys.exit(1)
if narrowed:
    print(f"iqr-factor 0.01 narrows the band on {sorted(narrowed)} vs default 1.5: {narrowed}")
else:
    print(f"  [info] iqr-factor 0.01 strictly narrowed no band this run (monotone non-increase holds; strict narrowing is live-variance-dependent, logged not gated); widths default={default_w!r} tight={tight_w!r}")
PY
record "$rc" "--iqr-factor 0.01: total-scope continuous bands are monotone non-increasing vs the step f default (1.5); strict narrowing logged, not gated (hard flip proof in hermetic 80-cli-contracts.sh iqr)"

echo "== f4. regress --audit-iqr-factor tightened: per-cell audit-flag count stays monotone (>= default); strict growth LOGGED =="
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
# LIVE-FLAKINESS: tightening the audit knob can only ADD flags (monotone: t >= d), never
# remove them; STRICT growth (t > d) needs a realized cell in the newly-exposed deviation
# window -- a property of live latency/token variance, so it is LOGGED, not gated. The
# deterministic strict per-cell flip (0 -> >=1) is proven hermetically in
# 80-cli-contracts.sh's auditiqr group, so no coverage is lost.
if t < d:
    print(f"audit-flagged count DROPPED under a tighter knob ({d} -> {t}); tightening must be monotone", file=sys.stderr)
    sys.exit(1)
if t == d:
    print(f"  [info] audit-iqr-factor 0.01 exposed no NEW cell this run (t==d=={d}); monotone holds, strict growth is live-variance-dependent (logged, not gated)")
PY
record "$rc" "--audit-iqr-factor 0.01: per-cell audit-flagged count is monotone (>= the step f default 3.0); strict growth logged, not gated (hard flip proof in hermetic 80-cli-contracts.sh auditiqr)"

echo "== f5. regress --audit-rel-delta tightened: per-cell audit-flag count stays monotone (>= default); strict growth LOGGED =="
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
# LIVE-FLAKINESS: tightening the audit knob can only ADD flags (monotone: t >= d), never
# remove them; STRICT growth (t > d) needs a realized cell in the newly-exposed deviation
# window -- a property of live latency/token variance, so it is LOGGED, not gated. The
# deterministic strict per-cell flip (0 -> >=1) is proven hermetically in
# 80-cli-contracts.sh's auditrd group, so no coverage is lost.
if t < d:
    print(f"audit-flagged count DROPPED under a tighter knob ({d} -> {t}); tightening must be monotone", file=sys.stderr)
    sys.exit(1)
if t == d:
    print(f"  [info] audit-rel-delta 0.01 exposed no NEW cell this run (t==d=={d}); monotone holds, strict growth is live-variance-dependent (logged, not gated)")
PY
record "$rc" "--audit-rel-delta 0.01: per-cell audit-flagged count is monotone (>= the step f default 0.5); strict growth logged, not gated (hard flip proof in hermetic 80-cli-contracts.sh auditrd)"

echo "== f6. calibrate standalone self-check over continuous baseline evidence (ADR-0034) =="
# calibrate (cmd/catacomb/calibrate.go) runs its own business logic to completion and
# returns either a rendered report (exit 0) or an *operationalError — bad selector,
# invalid thresholds, IO failure — which cmd/catacomb/run.go maps to exit 2; it never
# emits the regress errRegressionDetected path. LIVE-FLAKINESS: a reviewed sibling task
# was rejected for hard-asserting a specific calibration VERDICT, which depends on
# stochastic model output, so this step follows the same contract as f3-f5/k3 above
# (run.sh:1089-1090) — assert only that the exit stays in {0,1} (2 is a hard failure)
# and that the report structure renders/parses; LOG, never gate on, the verdict itself.
# --group selects ONLY the continuous basket's `baseline` variant: one task, reps=5 (a
# fixed property of basket-continuous.yaml, not live variance), which sits BELOW the
# default self-check floor (need = 2*min-support = 2*3 = 6) — so this call is
# deterministically `sufficient: false` with no `split` block. f7 below flips that with
# --min-support 2, proving threshold-flag passthrough on the identical evidence.
rc=0
catacomb calibrate --runs-dir "$runs2" \
	--group label:basket=e2e-continuous,variant=baseline --format json \
	>"$artifacts/calibrate-continuous.json" 2>"$artifacts/calibrate-continuous.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "calibrate continuous baseline: exit $rc (0 or 1, never operational-error 2)"
else
	failrec "calibrate continuous baseline: exit $rc (want 0 or 1, never operational-error 2)"
	sed 's/^/        stderr: /' "$artifacts/calibrate-continuous.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/calibrate-continuous.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
errs = []
for field in ("runs", "min_support", "sufficient", "thresholds"):
    if field not in rep:
        errs.append(f"missing top-level field {field!r}")
if rep.get("runs") != 5:
    errs.append(f"runs={rep.get('runs')!r} want 5 (basket-continuous.yaml reps=5, one task, variant=baseline)")
if rep.get("sufficient") is not False:
    errs.append(f"sufficient={rep.get('sufficient')!r} want False (5 runs < default need 2*min_support=6)")
if "split" in rep:
    errs.append(f"split present on a below-floor (k<need) report: {rep['split']!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"calibrate continuous baseline (default min-support 3): runs={rep['runs']} sufficient={rep['sufficient']} "
      f"detail={rep.get('detail')!r} (report renders/parses; below self-check floor at reps=5 — see f7)")
PY
record "$rc" "calibrate continuous baseline --json parses; report structure renders (runs/min_support/sufficient/thresholds)"

echo "== f7. calibrate --min-support 2 (threshold passthrough): flips sufficient=true, A/A-split renders =="
# Lowering --min-support to 2 drops the self-check floor to need=2*2=4 <= 5 runs, so this
# call over the IDENTICAL evidence as f6 is deterministically `sufficient: true` and emits
# a populated `split` block — proof --min-support threads from the CLI flag into
# calibrate.Calibrate's own floor (a structural contrast against f6's below-floor result,
# not a claim about which way the model's A/A split actually swung). Per the
# LIVE-FLAKINESS contract the split VERDICT value is only logged, never asserted — it is
# one of regress.Verdict's four values and the A/A outcome is stochastic model output.
rc=0
catacomb calibrate --runs-dir "$runs2" \
	--group label:basket=e2e-continuous,variant=baseline --format json --min-support 2 \
	>"$artifacts/calibrate-continuous-minsupport.json" 2>"$artifacts/calibrate-continuous-minsupport.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "calibrate --min-support 2: exit $rc (0 or 1, never operational-error 2)"
else
	failrec "calibrate --min-support 2: exit $rc (want 0 or 1, never operational-error 2)"
	sed 's/^/        stderr: /' "$artifacts/calibrate-continuous-minsupport.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/calibrate-continuous-minsupport.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
errs = []
if rep.get("runs") != 5:
    errs.append(f"runs={rep.get('runs')!r} want 5 (same evidence as f6)")
if rep.get("min_support") != 2:
    errs.append(f"min_support={rep.get('min_support')!r} want 2 (--min-support flag passthrough)")
if (rep.get("thresholds") or {}).get("min_support") != 2:
    errs.append(f"thresholds.min_support={(rep.get('thresholds') or {}).get('min_support')!r} want 2 (echoed threshold)")
if rep.get("sufficient") is not True:
    errs.append(f"sufficient={rep.get('sufficient')!r} want True (5 runs >= need 2*min_support=4 at --min-support 2)")
split = rep.get("split")
known_verdicts = {"ok", "regression", "notable", "insufficient"}
if split is None:
    errs.append("split missing on a sufficient report")
else:
    if split.get("verdict") not in known_verdicts:
        errs.append(f"split.verdict={split.get('verdict')!r} not one of {sorted(known_verdicts)}")
    for field in ("first_n", "second_n"):
        if field not in split:
            errs.append(f"split missing {field!r}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"calibrate --min-support 2 continuous baseline: sufficient=True, A/A-split renders "
      f"first_n={split.get('first_n')} second_n={split.get('second_n')} verdict={split.get('verdict')!r} "
      f"(LOGGED only, not asserted — stochastic model output)")
PY
record "$rc" "calibrate --min-support 2: threshold passthrough flips sufficient=true; split.verdict renders as a known enum value (logged, not gated)"

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

echo "== g2. subgraph --from/--to range mode + diff asymmetric per-side scoping (live evidence, \$0 incremental) =="
# Reuses step g's bench-verified, phase-resolving haiku cell(s) (chosen/d_a/d_b) and its
# subgraph --phase verify --json artifact — no new bench cells, no new spend.
#
# subgraph --from X --to X is NOT the same window as --phase X: RangeWindow scopes
# [from.start, to.start) (subgraph/spec.go's RangeWindow: `end := to.Start`, never
# `to.End`), so identical from/to selectors always collapse to a ZERO-WIDTH window,
# regardless of the phase's real (non-empty) duration — confirmed against
# subgraph/spec.go and subgraph/spec_test.go (which only exercises distinct from/to
# names). The same collapse applies to diff's --b-from/--b-to when pointed at the same
# checkpoint on side B. The assertions below are the mathematically correct contrast: a
# well-formed, EMPTY, strictly-narrower-than-`--phase verify` result on one side, not an
# equal one — deterministic given the evidence exists, per the LIVE-FLAKINESS rule.
if [ "${#resolved_dirs[@]}" -ge 1 ]; then
	run_json 0 "$artifacts/subgraph-range-verify.json" \
		"subgraph --from verify --to verify --json (zero-width range on the phase-resolving haiku cell)" -- \
		catacomb subgraph "$chosen/session.jsonl" --from verify --to verify --json
	rc=0
	python3 - "$artifacts/subgraph-verify.json" "$artifacts/subgraph-range-verify.json" <<'PY' || rc=$?
import json, sys

phase = json.load(open(sys.argv[1]))
rng = json.load(open(sys.argv[2]))
pn = len(phase.get("nodes") or [])
rn = len(rng.get("nodes") or [])
errs = []
if rn != 0:
    errs.append(f"--from verify --to verify returned {rn} nodes, want 0 (zero-width range: from.start == to.start)")
if pn == 0:
    errs.append("--phase verify returned 0 nodes on the same cell (step g's non-vacuity check should have caught this)")
if not (rn < pn):
    errs.append(f"--from/--to verify ({rn}) is not strictly narrower than --phase verify ({pn})")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"subgraph --from verify --to verify: {rn} nodes (zero-width range), strictly narrower than --phase verify's {pn} nodes")
PY
	record "$rc" "subgraph --from/--to verify is well-formed and empty, strictly narrower than --phase verify (range-mode plumbing reaches the CLI on live evidence)"
else
	skip "subgraph --from/--to range-mode smoke (no phase-resolving haiku cell — see step g's failure above)"
fi

if [ "${#resolved_dirs[@]}" -ge 2 ]; then
	run_json 0 "$artifacts/diff-haiku-unscoped.json" \
		"diff --json on the two bench-verified haiku sessions (unscoped, parses)" -- \
		catacomb diff "$d_a/session.jsonl" "$d_b/session.jsonl" --json
	rc=0
	python3 - "$artifacts/diff-haiku-unscoped.json" <<'PY' || rc=$?
import json, sys

d = json.load(open(sys.argv[1]))
missing = [k for k in ("unchanged", "changed", "added", "removed") if k not in d]
if missing:
    print(f"diff --json missing keys: {missing}", file=sys.stderr)
    sys.exit(1)
print(f"diff --json parses: unchanged={len(d['unchanged'])} changed={len(d['changed'])} "
      f"added={len(d['added'])} removed={len(d['removed'])}")
PY
	record "$rc" "diff --json output parses with the four delta buckets present"

	run_json 0 "$artifacts/diff-haiku-phase-verify.json" \
		"diff --phase verify --json (symmetric scoping, both sides narrowed to the verify window)" -- \
		catacomb diff "$d_a/session.jsonl" "$d_b/session.jsonl" --phase verify --json

	run_json 0 "$artifacts/diff-haiku-asym-verify.json" \
		"diff --a-phase verify --b-from verify --b-to verify --json (asymmetric per-side scoping)" -- \
		catacomb diff "$d_a/session.jsonl" "$d_b/session.jsonl" \
		--a-phase verify --b-from verify --b-to verify --json
	rc=0
	python3 - "$artifacts/diff-haiku-phase-verify.json" "$artifacts/diff-haiku-asym-verify.json" <<'PY' || rc=$?
import json, sys

sym = json.load(open(sys.argv[1]))
asym = json.load(open(sys.argv[2]))
errs = []
if asym["added"] or asym["changed"] or asym["unchanged"]:
    errs.append("asymmetric diff (side B force-emptied by --b-from/--b-to verify) should have no "
                f"added/changed/unchanged: added={len(asym['added'])} changed={len(asym['changed'])} "
                f"unchanged={len(asym['unchanged'])}")
sym_a_total = len(sym["removed"]) + len(sym["changed"]) + len(sym["unchanged"])
if len(asym["removed"]) != sym_a_total:
    errs.append(f"asymmetric removed={len(asym['removed'])} want {sym_a_total} (every side-A item the "
                "symmetric --phase verify diff scoped; --b-from/--b-to verify collapses side B to the "
                "empty zero-width range, so nothing can match)")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"asymmetric --a-phase verify --b-from/--b-to verify: side A matches the symmetric --phase verify "
      f"scoping ({sym_a_total} items), side B collapses to empty -> all {len(asym['removed'])} unmatched "
      "(--a-phase and --b-from/--b-to independently reach the CLI's per-side scoping layer)")
PY
	record "$rc" "diff --a-phase/--b-from/--b-to: side A matches symmetric --phase verify scoping, side B correctly collapses to the empty range"
else
	skip "diff asymmetric-scoping smoke (need 2 phase-resolving haiku cells, have ${#resolved_dirs[@]})"
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

echo "== g3. export transcript-file branch + replay node/edge summary + mcp protocol smoke (live evidence, \$0 incremental) =="
# export accepts either an evidence dir (loadExportDir, exercised above) or a bare
# transcript file (loadExportInput's non-dir branch -> loadGraphOffline); both paths
# reduce the SAME underlying session.jsonl, so their step_key content must agree even
# though the dir branch additionally carries bench/verify metadata the bare-transcript
# branch does not synthesize.
if [ -n "$echo_base_dir" ]; then
	run_expect 0 "export echo baseline session.jsonl directly (transcript-file branch)" -- \
		catacomb export "$echo_base_dir/session.jsonl" --to jsonl --out "$work/export-transcript.jsonl"
	rc=0
	python3 - "$work/export.jsonl" "$work/export-transcript.jsonl" <<'PY' || rc=$?
import json, sys

def step_keys(path):
    keys = set()
    for line in open(path):
        line = line.strip()
        if not line:
            continue
        o = json.loads(line)
        if o.get("kind") == "node":
            sk = o.get("step_key")
            if sk:
                keys.add(sk)
    return keys

dir_keys = step_keys(sys.argv[1])
file_keys = step_keys(sys.argv[2])
errs = []
if not dir_keys:
    errs.append("evidence-dir export (export.jsonl) carries no step_key at all")
if dir_keys != file_keys:
    errs.append(f"step_key sets differ: evidence-dir={sorted(dir_keys)} transcript-file={sorted(file_keys)}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"export transcript-file branch and evidence-dir branch agree on step_key content: {sorted(dir_keys)}")
PY
	record "$rc" "export <session.jsonl> (transcript-file branch) carries the SAME step_key content as export <evidence-dir>"
else
	skip "export transcript-file-branch smoke (no echo baseline evidence dir — see step g's failure above)"
fi

if [ "${#resolved_dirs[@]}" -ge 1 ]; then
	run_json 0 "$artifacts/replay-haiku.out" "replay one live session (node/edge summary)" -- \
		catacomb replay "$chosen/session.jsonl"
	rc=0
	python3 - "$artifacts/replay-haiku.out" <<'PY' || rc=$?
import re, sys

text = open(sys.argv[1]).read().strip()
m = re.search(r"-> (\d+) nodes, (\d+) edges\s*$", text)
if not m:
    print(f"replay stdout does not match the expected node/edge summary: {text!r}", file=sys.stderr)
    sys.exit(1)
nodes, edges = int(m.group(1)), int(m.group(2))
errs = []
if nodes <= 0:
    errs.append(f"replay reported {nodes} nodes, want > 0 for a real session carrying a verify checkpoint")
if edges < 0:
    errs.append(f"replay reported a negative edge count: {edges}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"replay summary: {nodes} nodes, {edges} edges")
PY
	record "$rc" "replay prints an explicit node/edge summary with a positive node count"
else
	skip "replay node/edge-summary smoke (no phase-resolving haiku cell — see step g's failure above)"
fi

echo "== g4. mcp protocol smoke: initialize over catacomb mcp stdio (live-leg binary, \$0) =="
# Mirrors e2e/hermetic/prod/scenarios/10-mcp-protocol.sh's protocol-conformance contract
# and hermetic run.sh step 19: the server answers one newline-delimited JSON-RPC
# response per request and exits 0 on stdin EOF, so a fixed single-request script is
# fully deterministic (Go sorts map keys when marshaling; mcp/server.go's
# initializeResult echoes the caller's protocolVersion and always reports
# serverInfo.name "catacomb").
printf '%s\n' \
	'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}' \
	>"$work/mcp-initialize-request.jsonl"
run_json 0 "$artifacts/mcp-initialize.out" "catacomb mcp: pipe one initialize request, exit 0 on stdin EOF" -- \
	catacomb mcp <"$work/mcp-initialize-request.jsonl"
rc=0
python3 - "$artifacts/mcp-initialize.out" <<'PY' || rc=$?
import json, sys

lines = [l for l in open(sys.argv[1]).read().splitlines() if l.strip()]
errs = []
if len(lines) != 1:
    errs.append(f"want exactly 1 response line, got {len(lines)}")
resp = None
if lines:
    try:
        resp = json.loads(lines[0])
    except ValueError as e:
        errs.append(f"response is not JSON: {e}")
if resp is not None:
    if resp.get("jsonrpc") != "2.0" or resp.get("id") != 1:
        errs.append(f"jsonrpc={resp.get('jsonrpc')!r} id={resp.get('id')!r} want 2.0/1")
    if "error" in resp or "result" not in resp:
        errs.append(f"error={resp.get('error')!r}, result present: {'result' in resp}")
    result = resp.get("result") or {}
    if result.get("protocolVersion") != "2024-11-05":
        errs.append(f"protocolVersion={result.get('protocolVersion')!r} want the client's 2024-11-05")
    if (result.get("serverInfo") or {}).get("name") != "catacomb":
        errs.append(f"serverInfo={result.get('serverInfo')!r} want name 'catacomb'")
    if "tools" not in (result.get("capabilities") or {}):
        errs.append(f"capabilities={result.get('capabilities')!r} lack tools")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("mcp initialize: well-formed JSON-RPC response (jsonrpc 2.0, id 1, result present, protocolVersion echoed, serverInfo catacomb)")
PY
record "$rc" "catacomb mcp initialize smoke: well-formed JSON-RPC response over stdio"

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

echo "== i3. pack label:basket=e2e-sql,variant=baseline --sample 3: audit bundle over real evidence =="
# Structural assertions ONLY (LIVE-FLAKINESS): run-dir/manifest-file shape and that a
# sampled session.jsonl carries real Claude Code stream-json, never model-content
# specifics. pack's --runs-dir is required for every selector kind (cmd/catacomb/pack.go
# runPack); the label: selector here scans $runs3 directly (no --db needed).
pack_sql_out="$work/pack-sql"
run_json 0 "$work/pack-sql.out" \
	"pack label:basket=e2e-sql,variant=baseline --sample 3" -- \
	catacomb pack label:basket=e2e-sql,variant=baseline --runs-dir "$runs3" --out "$pack_sql_out" --sample 3
rc=0
grep -Fqx "packed 3 of 5 runs into $pack_sql_out" "$work/pack-sql.out" || rc=1
record "$rc" "pack-sql stdout reports packed 3 of 5 sql-baseline runs"
rc=0
[ -f "$pack_sql_out/pack.json" ] || rc=1
[ -s "$pack_sql_out/INSTRUCTIONS.md" ] || rc=1
pack_sql_dircount=$(find "$pack_sql_out" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
[ "$pack_sql_dircount" -eq 3 ] || rc=1
record "$rc" "pack-sql bundle: pack.json + INSTRUCTIONS.md present, $pack_sql_dircount/3 sampled run dirs"
rc=0
python3 -c 'import json,sys
m=json.load(open(sys.argv[1]))
sys.exit(0 if m.get("selector")=="label:basket=e2e-sql,variant=baseline" and len(m.get("runs") or [])==3 else 1)' \
	"$pack_sql_out/pack.json" || rc=1
record "$rc" "pack-sql pack.json records the label: selector + 3 sampled run ids"
rc=0
pack_sql_session=""
for d in "$pack_sql_out"/*/; do
	[ -f "${d}session.jsonl" ] || continue
	pack_sql_session="${d}session.jsonl"
	break
done
[ -n "$pack_sql_session" ] || rc=1
[ -n "$pack_sql_session" ] && grep -q '"type":"assistant"' "$pack_sql_session" || rc=1
record "$rc" "a sampled pack-sql session.jsonl (${pack_sql_session:-none}) carries real stream-json (\"type\":\"assistant\")"

echo "== i4. pack name:e2e-presence-main --db \"\$db\": the --db-backed selector (pinned at step e) produces the same bundle shape =="
# Reuses the e2e-presence-main baseline pinned at step e (name:, not label:) — the ONLY
# other pack-selector kind (cmd/catacomb/runsdir.go resolveSelectorRunsDir). ORDERING:
# this MUST run before step y's "baseline rm e2e-presence-main" (the LAST consumer of
# $db) tears the baseline down; placing it here, in the step-i region, is far enough
# upstream that the ordering constraint can never be violated by a later edit.
pack_presence_out="$work/pack-presence"
run_json 0 "$work/pack-presence.out" \
	"pack name:e2e-presence-main --db \$db --sample 3" -- \
	catacomb pack name:e2e-presence-main --db "$db" --runs-dir "$runs1" --out "$pack_presence_out" --sample 3
rc=0
grep -Fqx "packed 3 of 5 runs into $pack_presence_out" "$work/pack-presence.out" || rc=1
record "$rc" "pack-presence (--db selector) stdout reports packed 3 of 5 pinned runs"
rc=0
[ -f "$pack_presence_out/pack.json" ] || rc=1
[ -s "$pack_presence_out/INSTRUCTIONS.md" ] || rc=1
pack_presence_dircount=$(find "$pack_presence_out" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
[ "$pack_presence_dircount" -eq 3 ] || rc=1
record "$rc" "pack-presence bundle: pack.json + INSTRUCTIONS.md present, $pack_presence_dircount/3 sampled run dirs"
rc=0
python3 -c 'import json,sys
m=json.load(open(sys.argv[1]))
sys.exit(0 if m.get("selector")=="name:e2e-presence-main" and len(m.get("runs") or [])==3 else 1)' \
	"$pack_presence_out/pack.json" || rc=1
record "$rc" "pack-presence pack.json records the name: selector (proves the --db baseline lookup resolved) + 3 sampled run ids"
rc=0
pack_presence_session=""
for d in "$pack_presence_out"/*/; do
	[ -f "${d}session.jsonl" ] || continue
	pack_presence_session="${d}session.jsonl"
	break
done
[ -n "$pack_presence_session" ] || rc=1
[ -n "$pack_presence_session" ] && grep -q '"type":"assistant"' "$pack_presence_session" || rc=1
record "$rc" "a sampled pack-presence session.jsonl (${pack_presence_session:-none}) carries real stream-json (\"type\":\"assistant\")"

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

echo "== k3. regress --annotation-rate-delta 0.99: sql seeded regression (annotation axis near-disabled) =="
# Exit code is NOT hard-asserted here (mirrors d2 above, run.sh:336-343). At
# --annotation-rate-delta 0.99 the annotation axis only gates when baseline lands a
# clean 5/5 verifier.pass, since the bad-rate delta is then exactly 1.0 (> 0.99); step j
# above (run.sh:816/857-871) explicitly tolerates a stochastic 4/5 baseline elsewhere in
# THIS SAME sql basket, and at 4/5 the delta is only 0.8 (< 0.99), so the axis stops
# gating and `regress` exits 0 with no real defect present. A hard `run_json 1` here
# would flake red on exactly that tolerated baseline miss. The deterministic "gates at
# default 0.1 / clears at loosened 0.7" proof already lives hermetically, over fixed
# synthetic evidence immune to live-baseline variance, at
# e2e/hermetic/prod/scenarios/80-cli-contracts.sh:338-358 — so no coverage is lost here.
# We only assert the flag was accepted (exit 0 or 1, never an operational-error 2) and
# that --json parses; whether the annotation axis actually gated is LOGGED, not asserted.
rc=0
catacomb regress --runs-dir "$runs3" \
	--baseline label:basket=e2e-sql,variant=baseline \
	--candidate label:basket=e2e-sql,variant=degraded \
	--annotation-rate-delta 0.99 --json \
	>"$artifacts/regress-sql-ratedelta.json" 2>"$artifacts/regress-sql-ratedelta.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "annotation-rate-delta 0.99: flag accepted (exit $rc)"
else
	failrec "annotation-rate-delta 0.99: flag accepted (exit $rc, want 0 or 1)"
	sed 's/^/        stderr: /' "$artifacts/regress-sql-ratedelta.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/regress-sql-ratedelta.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "total" and f.get("metric") == "ann:verifier.pass"
]
if hits:
    print(f"ann:verifier.pass at rate-delta 0.99: verdict={hits[0].get('verdict')!r} detail={hits[0].get('detail', '')!r} (logged, not gated)")
else:
    print("ann:verifier.pass finding absent at rate-delta 0.99 (axis fully disabled for this evidence; logged, not gated)")
PY
record "$rc" "annotation-rate-delta 0.99: --json parses; whether the annotation axis gates is logged only, not asserted, since it depends on the tolerated baseline pass-rate from step j"

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

echo "== l3. pin a baseline off the sql verifier.pass axis; record >=2 rows; trends --pareto --json resolves =="
# ORDERING (moved): this block was relocated here from its old position right after step
# e7, where it was mislabeled "e8". It reads the SQL basket evidence ($runs3), which is
# only benched at step i and regressed at steps k/l -- running it earlier hit an EMPTY
# $runs3 and failed `baseline set e2e-sql-main` with ErrEmptyGroup on every run. At this
# position $runs3 is populated and $db still carries the e2e-presence-main pin; e2e-sql-main
# is a SEPARATE $db row, intentionally left in place, and this stays upstream of step m and
# step y (baseline rm e2e-presence-main).
# LIVE-FLAKINESS: verifier.pass is only asserted >=4/5 on real evidence (step j tolerates
# one stochastic miss), so the pareto accuracy VALUE is not hard-asserted here -- only
# that the table resolves, carries the recorded rows, and every point exposes BOTH axes
# (accuracy + cost_usd), the same structural contract the hermetic mirror
# (e2e/hermetic/run.sh step 11) proves deterministically over fabricated verifier.pass.
run_expect 0 "baseline set e2e-sql-main (sql verifier.pass axis)" -- \
	catacomb baseline set e2e-sql-main \
	--label basket=e2e-sql,variant=baseline --runs-dir "$runs3" --db "$db"
run_json 0 "$artifacts/regress-sql-record-ava.json" \
	"record row 1 on e2e-sql-main (baseline vs baseline2, A-vs-A must NOT gate)" -- \
	catacomb regress --db "$db" --runs-dir "$runs3" \
	--baseline name:e2e-sql-main \
	--candidate label:basket=e2e-sql,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --record --json
run_json 1 "$artifacts/regress-sql-record-degraded.json" \
	"record row 2 on e2e-sql-main (baseline vs degraded, must gate on verifier.pass)" -- \
	catacomb regress --db "$db" --runs-dir "$runs3" \
	--baseline name:e2e-sql-main \
	--candidate label:basket=e2e-sql,variant=degraded \
	--record --json
run_json 0 "$artifacts/trends-sql-pareto.json" \
	"trends e2e-sql-main --pareto --json resolves over real verifier.pass" -- \
	catacomb trends e2e-sql-main --db "$db" --pareto --json
rc=0
python3 - "$artifacts/trends-sql-pareto.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
points = rep.get("points") or []
errs = []
if rep.get("baseline") != "e2e-sql-main":
    errs.append(f"baseline={rep.get('baseline')!r} want 'e2e-sql-main'")
if len(points) != 3:
    errs.append(f"want 3 pareto points (baseline pin + 2 recorded rows), got {len(points)}")
for p in points:
    tag = p.get("candidate") or p.get("source")
    if "accuracy" not in p or "cost_usd" not in p:
        errs.append(f"point {tag!r} lacks an axis: keys={sorted(p)}")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
accs = {(p.get("candidate") or p.get("source")): p.get("accuracy") for p in points}
print(f"trends --pareto --json: {len(points)} points over real verifier.pass, every point carries accuracy+cost_usd; accuracy={accs!r} (logged, not asserted)")
PY
record "$rc" "trends --pareto --json resolves 3 points (baseline + 2 recorded rows) with real verifier.pass accuracy axes"

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

echo "== o3. import --session-id: a subagent baseline cell imports and reduces to the SAME subagent-node presence bench captured =="
# Structural/determinism assertion ONLY (LIVE-FLAKINESS): the imported evidence must
# reduce to the SAME subagent-node presence the bench evidence for the SAME session
# already carries -- same underlying transcripts in, same reduction out, a claim about
# the pipeline's determinism, not a live-content guess. count_subagent_nodes (step n)
# counts across a whole variant; this checks ONE specific session (bench-side vs
# re-imported) so the comparison is apples-to-apples on identical transcripts.
subagent_node_in_dir() { # <evidence-dir> -> exit 0 iff session.jsonl + subagents/agent-*.jsonl reduce a "type":"subagent" node
	local d="$1" comb snap sf
	comb="$work/subagent-presence-$(basename "$d").jsonl"
	: >"$comb"
	[ -f "$d/session.jsonl" ] && cat "$d/session.jsonl" >>"$comb"
	for sf in "$d"/subagents/agent-*.jsonl; do
		[ -f "$sf" ] && cat "$sf" >>"$comb"
	done
	snap="$work/subagent-presence-$(basename "$d").snap.jsonl"
	catacomb replay "$comb" --export-jsonl "$snap" >/dev/null 2>&1 || return 1
	grep -q '"type":"subagent"' "$snap"
}

rc=0
read -r subagent_import_sid subagent_import_benchdir <<<"$(python3 - "$manifest4" <<'PY'
import json, sys

for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    e = json.loads(line)
    if e.get("task") == "subagent" and e.get("variant") == "baseline":
        print(e.get("session_id", ""), e.get("evidence_dir", ""))
        break
PY
)"
{ [ -n "$subagent_import_sid" ] && [ -n "$subagent_import_benchdir" ]; } || rc=1
record "$rc" "picked a subagent baseline cell session_id + bench evidence_dir from manifest4"

subagent_import_runs="$work/import-runs-subagent"
subagent_bench_present=""
if [ -n "$subagent_import_sid" ] && [ -n "$subagent_import_benchdir" ]; then
	run_json 0 "$work/import-subagent-sid.out" \
		"import basket-subagent.yaml --task subagent --variant baseline --session-id (manifest4 cell)" -- \
		catacomb import basket-subagent.yaml --task subagent --variant baseline \
		--session-id "$subagent_import_sid" --projects-dir "$HOME/.claude/projects" \
		--runs-dir "$subagent_import_runs" --rep 1
	import_sid_dir="$subagent_import_runs/import-e2e-subagent-subagent-baseline-r1"
	rc=0
	[ -f "$import_sid_dir/session.jsonl" ] || rc=1
	record "$rc" "import --session-id wrote a bench-cell evidence dir (session.jsonl present)"

	if subagent_node_in_dir "$subagent_import_benchdir"; then subagent_bench_present=1; else subagent_bench_present=0; fi
	if subagent_node_in_dir "$import_sid_dir"; then subagent_import_sid_present=1; else subagent_import_sid_present=0; fi
	rc=0
	[ "$subagent_bench_present" -eq "$subagent_import_sid_present" ] || rc=1
	record "$rc" "import --session-id reduces to the SAME subagent-node presence bench captured for this session (bench=$subagent_bench_present import=$subagent_import_sid_present)"
else
	failrec "import --session-id: no subagent baseline cell resolved from manifest4, skipping the session-id import"
fi

echo "== o4. import --transcript: repeat against the resolved .jsonl (direct-path branch + subagents/agent-*.jsonl glob) =="
if [ -n "$subagent_import_sid" ] && [ -n "$subagent_bench_present" ]; then
	subagent_import_main=$(find "$HOME/.claude/projects" -mindepth 2 -maxdepth 2 -type f \
		-name "${subagent_import_sid}.jsonl" 2>/dev/null | head -n1)
	rc=0
	[ -n "$subagent_import_main" ] && [ -f "$subagent_import_main" ] || rc=1
	record "$rc" "resolved the main transcript .jsonl for session $subagent_import_sid under \$HOME/.claude/projects"
	if [ -n "$subagent_import_main" ]; then
		run_json 0 "$work/import-subagent-transcript.out" \
			"import basket-subagent.yaml --task subagent --variant baseline --transcript (resolved .jsonl)" -- \
			catacomb import basket-subagent.yaml --task subagent --variant baseline \
			--transcript "$subagent_import_main" --runs-dir "$subagent_import_runs" --rep 2
		import_transcript_dir="$subagent_import_runs/import-e2e-subagent-subagent-baseline-r2"
		rc=0
		[ -f "$import_transcript_dir/session.jsonl" ] || rc=1
		record "$rc" "import --transcript wrote a bench-cell evidence dir (session.jsonl present)"

		if subagent_node_in_dir "$import_transcript_dir"; then subagent_import_transcript_present=1; else subagent_import_transcript_present=0; fi
		rc=0
		[ "$subagent_import_transcript_present" -eq "$subagent_bench_present" ] || rc=1
		record "$rc" "import --transcript (direct-path + subagents/agent-*.jsonl glob) reduces to the SAME subagent-node presence bench captured (bench=$subagent_bench_present import_transcript=$subagent_import_transcript_present)"
	else
		failrec "import --transcript: cannot resolve the main .jsonl for session $subagent_import_sid under \$HOME/.claude/projects"
	fi
else
	failrec "import --transcript: skipped (step o3 did not resolve a session to repeat against)"
fi

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

echo "== q3. regress --coverage-floor 0: flag accepted + --json parses; dropped-skill verdict LOGGED =="
# --coverage-floor 0 makes StepsTrusted always true, so the step-presence drop reports
# `regression` outright (undowngraded) instead of the default floor's `notable` -- BUT that
# regression verdict ALSO needs the baseline skill to have been invoked in enough reps for
# the Wilson bands to separate. This skill basket only tolerates >=1 baseline invocation
# (step q2: "Live invocation is not always 5/5"); at a tolerated 3/5 baseline the same
# finding is `notable`, not `regression`, so a hard `run_json 1` + `verdict=="regression"`
# would flake red on that tolerated baseline miss. LIVE asserts must not hard-fail on normal
# model variance: the deterministic regression-vs-notable un-downgrade proof already lives
# hermetically over fixed evidence in e2e/hermetic/prod/scenarios/80-cli-contracts.sh (cov
# group: notable at floor 0.70 -> regression at floor 0), so no coverage is lost. Assert only
# that the flag was accepted (exit 0 or 1, never operational-error 2) and that --json parses;
# whether the finding un-downgrades to regression is LOGGED, not asserted.
rc=0
catacomb regress --runs-dir "$runs5" \
	--baseline label:basket=e2e-skill,variant=baseline \
	--candidate label:basket=e2e-skill,variant=degraded \
	--coverage-floor 0 --json \
	>"$artifacts/regress-skill-coveragefloor0.json" 2>"$artifacts/regress-skill-coveragefloor0.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "coverage-floor 0 (skill baseline-vs-degraded): flag accepted (exit $rc)"
else
	failrec "coverage-floor 0 (skill baseline-vs-degraded): flag accepted (exit $rc, want 0 or 1)"
	sed 's/^/        stderr: /' "$artifacts/regress-skill-coveragefloor0.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/regress-skill-coveragefloor0.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "step" and f.get("metric") == "presence"
    and "-> 0/5" in str(f.get("detail", ""))
]
if hits:
    h = hits[0]
    print(f"step {h.get('name')!r} presence at coverage-floor 0: verdict={h.get('verdict')!r} "
          f"({h.get('detail', '')}) (logged, not gated)")
else:
    print("step-scope presence -> 0/5 finding absent at coverage-floor 0 (logged, not gated)")
PY
record "$rc" "coverage-floor 0: --json parses; whether the dropped-skill finding un-downgrades to regression is logged only, not asserted (depends on the tolerated baseline skill presence from step q2; hard proof in hermetic 80-cli-contracts.sh cov group)"

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
# Shared DETERMINISTIC structural manifest assertion for the three extra codex
# baskets (v2/v3/v4): every cell marked, exit 0, session id + evidence dir on
# disk, agent_runtime=codex stamped on meta, no cost_usd (codex reports none).
# These are model-INDEPENDENT bench-completion facts — OK to hard-assert — the
# same contract the basket-codex leg above already asserts inline. Model-
# dependent outcomes (tool call / delegation / skill read) stay logged, per basket.
codex_manifest_assert() { # <manifest> <expected-cell-count>
	python3 - "$1" "$2" <<'PY'
import json, os, sys

manifest, want = sys.argv[1], int(sys.argv[2])
entries = [json.loads(l) for l in open(manifest) if l.strip()]
errs = []
if len(entries) != want:
    errs.append(f"expected {want} cells, got {len(entries)}")
for e in entries:
    rid = e.get("run_id", "?")
    if not e.get("marked"):
        errs.append(f"{rid}: not marked (thread.started peek -> rollout resolution failed) note={e.get('note','')}")
    if e.get("exit_code") != 0:
        errs.append(f"{rid}: exit_code={e.get('exit_code')} note={e.get('note','')}")
    if not e.get("session_id"):
        errs.append(f"{rid}: empty session_id note={e.get('note','')}")
    if "cost_usd" in e:
        errs.append(f"{rid}: cost_usd present (codex reports no dollar cost; want the key absent)")
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
      f"evidence present, agent_runtime=codex stamped, no cost_usd")
PY
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

	echo "== v2. codex MCP basket (basket-codex-mcp.yaml — 9 live codex exec cells over a real stdio MCP server) =="
	# The same e2ekit stdio MCP server + wire protocol the Claude live-MCP basket
	# (step s) drives, now over the codex runtime's per-invocation `-c mcp_servers.*`
	# config (codex-mcp-live.sh). 1 task x 3 variants x 3 reps = 9 cells; rollouts
	# land under the DEFAULT --sessions-dir (~/.codex/sessions), the same
	# default-sessions contract basket-codex.yaml above relies on. VERDICT POSTURE:
	# only the DETERMINISTIC structural facts are hard-asserted (manifest 9/9 marked +
	# agent_runtime=codex + no cost_usd). The baseline-vs-degraded GATE (dropped
	# mcp__e2ekit__record node + verifier.pass drop) SHOULD fire, but whether
	# gpt-5.4-mini actually CALLS the tool at low reasoning effort is model-
	# discretionary — so the gate is soft (flag accepted + --json parses + LOGGED) and
	# the baseline tool-call rate is a logged observation. The deterministic hard gate
	# proof lives in hermetic 57-codex-mcp.sh, whose invocation shapes this mirrors.
	run_expect 0 "bench codex MCP basket (9 live codex exec cells)" -- \
		catacomb bench basket-codex-mcp.yaml --runs-dir "$runs9" --manifest "$manifest9"
	rc=0
	codex_manifest_assert "$manifest9" 9 || rc=$?
	record "$rc" "codex MCP manifest: 9/9 marked, exit 0, session+evidence present, agent_runtime=codex, no cost_usd"

	echo "-- codex MCP regress (baseline vs degraded): flag accepted + --json parses; dropped-node/verifier gate LOGGED (tool-call is model-discretionary) --"
	rc=0
	catacomb regress --runs-dir "$runs9" \
		--baseline label:basket=e2e-codex-mcp,variant=baseline \
		--candidate label:basket=e2e-codex-mcp,variant=degraded --json \
		>"$artifacts/regress-codex-mcp.json" 2>"$artifacts/regress-codex-mcp.json.stderr" || rc=$?
	if [ "$rc" -le 1 ]; then
		pass "codex MCP regress (baseline vs degraded): flag accepted (exit $rc; 0 or 1 both acceptable)"
	else
		failrec "codex MCP regress errored (exit $rc, want 0 or 1; report: $artifacts/regress-codex-mcp.json.stderr)"
		sed 's/^/        stderr: /' "$artifacts/regress-codex-mcp.json.stderr" >&2 || true
	fi
	rc=0
	python3 - "$artifacts/regress-codex-mcp.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
step = any(
    x.get("scope") == "step" and x.get("metric") == "presence"
    and "e2ekit" in str(x.get("name", "")).lower()
    for x in f
)
ann = any(x.get("metric") == "ann:verifier.pass" and x.get("verdict") == "regression" for x in f)
print(f"codex MCP baseline-vs-degraded: overall_verdict={rep.get('overall_verdict')!r} "
      f"regressions={rep.get('regressions')} dropped-mcp-node={step} verifier.pass-drop={ann} "
      f"(logged, not gated -- deterministic gate proof in hermetic 57-codex-mcp.sh)")
PY
	record "$rc" "codex MCP regress --json parses; whether the dropped mcp__e2ekit__record node + verifier.pass drop gated is LOGGED (needs the live model to call the tool; hard proof in hermetic 57-codex-mcp.sh)"
	mcp_base_calls=0
	for d in "$runs9"/bench-e2e-codex-mcp-mcp-baseline-r*; do
		[ -d "$d" ] || continue
		snap="$work/codex-mcp-node-$(basename "$d").jsonl"
		catacomb export "$d" --out "$snap" >/dev/null 2>&1 || continue
		if grep -q '"type":"mcp_call"' "$snap"; then mcp_base_calls=$((mcp_base_calls + 1)); fi
	done
	echo "  [info] codex MCP baseline tool-call rate: $mcp_base_calls/3 baseline reps carry an mcp_call node (>=2/3 desired; logged, not gated — deterministic dropped-node gate proven in hermetic 57-codex-mcp.sh)"

	echo "-- codex MCP A-vs-A (baseline vs baseline2, widened continuous band): flag accepted + --json parses; verdict LOGGED --"
	rc=0
	catacomb regress --runs-dir "$runs9" \
		--baseline label:basket=e2e-codex-mcp,variant=baseline \
		--candidate label:basket=e2e-codex-mcp,variant=baseline2 \
		--metric-rel-delta "$ava_metric_band" --json \
		>"$artifacts/regress-codex-mcp-AvA.json" 2>/dev/null || rc=$?
	if [ "$rc" -le 1 ] && python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$artifacts/regress-codex-mcp-AvA.json" 2>/dev/null; then
		pass "codex MCP A-vs-A renders + --json parses (exit $rc; 0 or 1 both acceptable)"
	else
		failrec "codex MCP A-vs-A render/parse failed (exit $rc; json: $artifacts/regress-codex-mcp-AvA.json)"
	fi
	echo "  [info] codex MCP A-vs-A: overall_verdict=$(verdict_of "$artifacts/regress-codex-mcp-AvA.json") (logged — non-vacuity hard-proven in hermetic 57-codex-mcp.sh)"

	echo "== v3. codex subagent basket (basket-codex-subagent.yaml — 15 live codex exec cells; delegation is prompt-discretionary) =="
	# Does asking a real `codex exec` to delegate produce a child rollout catacomb
	# reduces into a "type":"subagent" node? 1 task x 3 variants x 5 reps = 15 cells
	# (reps 5 for base-rate headroom on a discretionary signal). VERDICT POSTURE
	# (LOGGED/soft, per basket-codex-subagent.yaml's header): delegation in Codex is
	# prompt-discretionary, not flag-forced — a live run may spawn 0/5 on baseline if
	# gpt-5.4-mini under-follows at low reasoning effort. So only the manifest
	# structural facts are hard-asserted (15/15 marked + agent_runtime=codex + no
	# cost_usd); the baseline-vs-degraded verdict is LOGGED: regress must RENDER +
	# --json must parse (exit 0 or 1 both fine), and only regress ITSELF erroring
	# (exit 2) fails the leg. spawn_agent firing is NEVER hard-asserted. The
	# documented lever if the live spawn rate proves too low is bumping reasoning
	# effort to `medium` (codex-subagent-live.sh). The deterministic reduce/regress
	# gate is hard-asserted hermetically over fixed fixtures in 58-codex-subagent.sh.
	run_expect 0 "bench codex subagent basket (15 live codex exec cells)" -- \
		catacomb bench basket-codex-subagent.yaml --runs-dir "$runs10" --manifest "$manifest10"
	rc=0
	codex_manifest_assert "$manifest10" 15 || rc=$?
	record "$rc" "codex subagent manifest: 15/15 marked, exit 0, session+evidence present, agent_runtime=codex, no cost_usd"

	echo "-- codex subagent regress (baseline vs degraded): must render + --json must parse; verdict LOGGED (only exit 2 fails the leg) --"
	rc=0
	catacomb regress --runs-dir "$runs10" \
		--baseline label:basket=e2e-codex-subagent,variant=baseline \
		--candidate label:basket=e2e-codex-subagent,variant=degraded --json \
		>"$artifacts/regress-codex-subagent.json" 2>"$artifacts/regress-codex-subagent.json.stderr" || rc=$?
	if [ "$rc" -le 1 ]; then
		pass "codex subagent regress (baseline vs degraded): renders (exit $rc; 0 or 1 both acceptable)"
	else
		failrec "codex subagent regress errored (exit $rc, want 0 or 1; report: $artifacts/regress-codex-subagent.json.stderr)"
		sed 's/^/        stderr: /' "$artifacts/regress-codex-subagent.json.stderr" >&2 || true
	fi
	rc=0
	python3 - "$artifacts/regress-codex-subagent.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
sub = any(
    x.get("scope") == "step" and x.get("metric") == "presence" and x.get("name") is None
    for x in f
)
print(f"codex subagent baseline-vs-degraded: overall_verdict={rep.get('overall_verdict')!r} "
      f"regressions={rep.get('regressions')} subagent-stepkey-presence-finding={sub} "
      f"(logged, not gated -- delegation is prompt-discretionary; hard gate in hermetic 58-codex-subagent.sh)")
PY
	record "$rc" "codex subagent regress --json parses; baseline-vs-degraded verdict is LOGGED (delegation is prompt-discretionary; hard gate in hermetic 58-codex-subagent.sh)"
	sub_spawns=0
	for d in "$runs10"/bench-e2e-codex-subagent-delegate-baseline-r*; do
		[ -d "$d" ] || continue
		snap="$work/codex-subagent-node-$(basename "$d").jsonl"
		catacomb export "$d" --out "$snap" >/dev/null 2>&1 || continue
		if grep -q '"type":"subagent"' "$snap"; then sub_spawns=$((sub_spawns + 1)); fi
	done
	echo "  [info] codex subagent spawn rate: $sub_spawns/5 baseline reps produced a subagent node (prompt-discretionary; logged, NEVER gated — lever: bump reasoning effort to medium in codex-subagent-live.sh)"

	echo "== v4. codex skill artifact-substitute basket (basket-codex-skill.yaml — 9 live codex exec cells; NO skill-invocation event) =="
	# Codex has no dedicated skill-invocation event — a SKILL.md is read via an
	# ordinary file-read/exec_command, indistinguishable in the rollout from any other
	# file read, so reduce/skill.go's isSkill() (which only matches the Claude
	# "Skill"/"SlashCommand" tool names) can NEVER synthesize a "type":"skill" node for
	# a codex rollout. 1 task x 3 variants x 3 reps = 9 cells. VERDICT POSTURE: the
	# manifest structural facts are hard-asserted (9/9 marked + agent_runtime=codex +
	# no cost_usd) AND the negative-space claim — NO skill node in any codex skill
	# graph — is hard-asserted (deterministic, model-independent; mirrors hermetic
	# 59-codex-skill.sh). The baseline-vs-degraded regress is SOFT: this basket's
	# degraded arm writes the SAME token DIRECTLY (verify stays green on BOTH by
	# design), so the live regress is NOT expected to gate on verifier.pass — the
	# artifact-gate mechanic (degraded with NO artifact -> gate) is proven ONLY
	# hermetically in 59-codex-skill.sh. The SKILL.md-read soft grep is LOGGED, never
	# gated — it is the only signal that distinguishes baseline from degraded live,
	# and Codex offers no structural handle on it.
	run_expect 0 "bench codex skill basket (9 live codex exec cells)" -- \
		catacomb bench basket-codex-skill.yaml --runs-dir "$runs11" --manifest "$manifest11"
	rc=0
	codex_manifest_assert "$manifest11" 9 || rc=$?
	record "$rc" "codex skill manifest: 9/9 marked, exit 0, session+evidence present, agent_runtime=codex, no cost_usd"

	echo "-- codex skill regress (baseline vs degraded): flag accepted + --json parses; verdict LOGGED (degraded also writes the artifact by design -> no live gate expected) --"
	rc=0
	catacomb regress --runs-dir "$runs11" \
		--baseline label:basket=e2e-codex-skill,variant=baseline \
		--candidate label:basket=e2e-codex-skill,variant=degraded --json \
		>"$artifacts/regress-codex-skill.json" 2>"$artifacts/regress-codex-skill.json.stderr" || rc=$?
	if [ "$rc" -le 1 ]; then
		pass "codex skill regress (baseline vs degraded): flag accepted (exit $rc; 0 or 1 both acceptable)"
	else
		failrec "codex skill regress errored (exit $rc, want 0 or 1; report: $artifacts/regress-codex-skill.json.stderr)"
		sed 's/^/        stderr: /' "$artifacts/regress-codex-skill.json.stderr" >&2 || true
	fi
	rc=0
	python3 - "$artifacts/regress-codex-skill.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
ann = any(
    x.get("metric") == "ann:verifier.pass" and x.get("verdict") == "regression"
    for x in rep.get("findings", [])
)
print(f"codex skill baseline-vs-degraded: overall_verdict={rep.get('overall_verdict')!r} "
      f"regressions={rep.get('regressions')} verifier.pass-drop={ann} "
      f"(logged, not gated -- degraded writes the artifact too by design; artifact-gate proof in hermetic 59-codex-skill.sh)")
PY
	record "$rc" "codex skill regress --json parses; verifier.pass verdict is LOGGED (degraded writes the artifact too by design; artifact-gate proof in hermetic 59-codex-skill.sh)"
	skill_md_reads=0
	for d in "$runs11"/bench-e2e-codex-skill-skill-baseline-r*; do
		[ -f "$d/session.jsonl" ] || continue
		if grep -q '\.agents/skills/e2e-emit/SKILL\.md' "$d/session.jsonl"; then skill_md_reads=$((skill_md_reads + 1)); fi
	done
	echo "  [info] codex skill soft-grep: $skill_md_reads/3 baseline reps reference .agents/skills/e2e-emit/SKILL.md in the rollout (logged, NEVER gated — Codex has no skill-invocation event)"
	rc=0
	skill_node_found=""
	for d in "$runs11"/bench-e2e-codex-skill-skill-*-r*; do
		[ -d "$d" ] || continue
		snap="$work/codex-skill-node-$(basename "$d").jsonl"
		catacomb export "$d" --out "$snap" >/dev/null 2>&1 || continue
		if grep -q '"type":"skill"' "$snap"; then rc=1; skill_node_found="$skill_node_found $(basename "$d")"; fi
	done
	record "$rc" "no \"type\":\"skill\" node in any codex skill-basket graph (Codex has no skill-invocation event; artifact + soft grep are the documented substitute)${skill_node_found:+ -- found in:$skill_node_found}"
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

echo "== x3. failmode --error-delta (clean vs errseed): flag accepted + --json parses; error_rate gate LOGGED =="
# LIVE-FLAKINESS (soften mirrors k3 / commit 7f4b4cc): the error_rate gate needs a PERFECT
# 0/3-clean vs 3/3-errseed split across 6 stochastic Haiku cells. A cheap model that replies
# without actually invoking the failing Bash(sh:*) tool leaves an errseed rep with no
# StatusError node; a single miss (2/3 vs 0/3, or 3/3 vs 1/3) drops the Wilson separation
# below the regression boundary -> verdict `notable`, exit 0. A hard `run_json 1` +
# `verdict=="regression"` would flake red (~17%) on exactly that tolerated stochastic miss.
# LIVE asserts must not hard-fail on normal model variance: the deterministic 0/3-vs-3/3 ->
# regression/exit-1 proof (plus a clean-vs-clean -> ok/exit-0 control) already lives
# hermetically over fixed is_error fixtures at e2e/hermetic/prod/scenarios/81-failmode.sh, so
# no live coverage is lost. Assert only that the flag was accepted (exit 0 or 1, never an
# operational-error 2) and that --json parses; whether the error_rate axis gated is LOGGED.
rc=0
catacomb regress --runs-dir "$runs8" \
	--baseline label:basket=e2e-failmode,variant=clean \
	--candidate label:basket=e2e-failmode,variant=errseed \
	--error-delta 0.5 --json \
	>"$artifacts/regress-failmode-errseed.json" 2>"$artifacts/regress-failmode-errseed.json.stderr" || rc=$?
if [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]; then
	pass "error-delta 0.5 (clean vs errseed): flag accepted (exit $rc)"
else
	failrec "error-delta 0.5 (clean vs errseed): flag accepted (exit $rc, want 0 or 1)"
	sed 's/^/        stderr: /' "$artifacts/regress-failmode-errseed.json.stderr" >&2 || true
fi
rc=0
python3 - "$artifacts/regress-failmode-errseed.json" <<'PY' || rc=$?
import json, sys

rep = json.load(open(sys.argv[1]))
hits = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "total" and f.get("metric") == "error_rate"
]
if hits:
    h = hits[0]
    print(f"total error_rate at --error-delta 0.5: verdict={h.get('verdict')!r} "
          f"{h.get('baseline')} -> {h.get('candidate')} (logged, not gated)")
else:
    print("total error_rate finding absent at --error-delta 0.5 (axis did not fire; logged, not gated)")
PY
record "$rc" "error-delta 0.5: --json parses; whether the error_rate axis gates is logged only, not asserted (needs a perfect stochastic 0/3-vs-3/3 Haiku split; hard proof in hermetic 81-failmode.sh)"

echo "== x4. failmode error tool-node synthesis: errseed carries a tool_call error node in >=1 run (clean-side LOGGED) =="
# The errseed side (>=1 run with a "status":"error" node) is robust and HARD-asserted. The
# clean side is LOGGED, not gated (LIVE-FLAKINESS): a stochastic Haiku fumble on the trivial
# succeeding command could emit a stray error node, and hard-asserting EXACTLY 0 across all
# clean reps would flake red on that model variance. The deterministic error-node contrast
# (errseed is_error:true vs clean is_error:false) is proven hermetically over fixed fixtures
# in e2e/hermetic/prod/scenarios/81-failmode.sh, so no coverage is lost.
rc=0
err_hits=0
for d in "$runs8"/bench-e2e-failmode-failmode-errseed-r*; do
	[ -f "$d/session.jsonl" ] || continue
	snap="$work/failmode-node-$(basename "$d").jsonl"
	catacomb replay "$d/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1 || continue
	if grep -q '"status":"error"' "$snap"; then err_hits=$((err_hits + 1)); fi
done
[ "$err_hits" -ge 1 ] || rc=1
record "$rc" "errseed carries a tool_call error node in >=1 run ($err_hits found)"
clean_err_hits=0
for d in "$runs8"/bench-e2e-failmode-failmode-clean-r*; do
	[ -f "$d/session.jsonl" ] || continue
	snap="$work/failmode-node-$(basename "$d").jsonl"
	catacomb replay "$d/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1 || continue
	if grep -q '"status":"error"' "$snap"; then clean_err_hits=$((clean_err_hits + 1)); fi
done
echo "  [info] clean-side error nodes: $clean_err_hits clean run(s) carry a status:error node (logged, not gated -- expected 0, a stochastic Haiku fumble tolerated; hard proof in hermetic 81-failmode.sh)"

echo "== y. baseline rm e2e-presence-main (LAST consumer of \$db in this driver) =="
# ORDERING CONSTRAINT: this MUST stay the final e2e-presence-main/\$db operation in the
# driver. Every other read of name:e2e-presence-main / \$db has already run by this
# point: step e's pin/strict-record/trends, steps e2/e3's paired axis (over \$runs1,
# not \$db, but still logically "before the baseline is torn down"), and e4-e7's
# list/export/import-regress-equivalence/2nd-record/trends round-trip above. The step l3 sql-pareto block's
# e2e-sql-main baseline is a SEPARATE row in the same \$db and is intentionally left in
# place (it is not the baseline this step tears down). Moving this rm any earlier would
# break any later \$db read of e2e-presence-main.
run_expect 0 "baseline rm e2e-presence-main" -- \
	catacomb baseline rm e2e-presence-main --db "$db"
run_json 0 "$work/baseline-list-after-rm.json" \
	"baseline list --db --json after rm" -- \
	catacomb baseline list --db "$db" --json
rc=0
python3 - "$work/baseline-list-after-rm.json" <<'PY' || rc=$?
import json, sys

bs = json.load(open(sys.argv[1])) or []
names = [b.get("name") for b in bs]
if "e2e-presence-main" in names:
    print(f"e2e-presence-main still present after rm: {names!r}", file=sys.stderr)
    sys.exit(1)
print(f"baseline list after rm: e2e-presence-main absent (remaining: {names!r})")
PY
record "$rc" "baseline list no longer shows e2e-presence-main after rm"

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
