#!/usr/bin/env bash
# Shared assertion bookkeeping for the hermetic production scenarios. Sourced by
# the dispatcher (run.sh) and each scenarios/*.sh. Mirrors the pass/failrec/
# record/run_json helpers in e2e/hermetic/run.sh so scenario code reads the same.

# Continuous-metric band for the A-vs-A controls below, mirroring ava_metric_band in
# e2e/run.sh. Calibration saw duration ~2x between identical batches (inter-batch
# latency), so a tighter band false-flags wall-clock jitter as a regression: these
# fixtures are byte-identical, and duration_ms is the only axis that can differ at
# all. --metric-rel-delta touches continuous metrics ONLY — presence, annotation and
# error-rate stay at default sensitivity, so a real presence false positive still
# fails the control, which is the property A-vs-A exists to protect.
# shellcheck disable=SC2034  # read by the scenarios/*.sh that run.sh sources
PROD_AVA_METRIC_BAND="2.0"

PROD_FAILURES=()
pass() { printf '  PASS  %s\n' "$1"; }
failrec() { printf '  FAIL  %s\n' "$1"; PROD_FAILURES+=("$1"); }
record() { if [ "$1" -eq 0 ]; then pass "$2"; else failrec "$2"; fi; }
run_json() { # <want> <out> <label> -- cmd...
  local want="$1" out="$2" label="$3"; shift 3; [ "${1:-}" = "--" ] && shift
  local rc=0; "$@" >"$out" 2>"$out.stderr" || rc=$?
  if [ "$rc" -eq "$want" ]; then pass "$label (exit $rc)"
  else failrec "$label (exit $rc, want $want; out: $out)"; sed 's/^/        stderr: /' "$out.stderr" >&2 || true; fi
}
prod_report() {
  if [ "${#PROD_FAILURES[@]}" -eq 0 ]; then printf '\nprod: all scenarios passed\n'; return 0; fi
  printf '\nprod: %d failure(s):\n' "${#PROD_FAILURES[@]}"; printf '  - %s\n' "${PROD_FAILURES[@]}"; return 1
}
