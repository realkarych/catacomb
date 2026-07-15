#!/usr/bin/env bash
# Hermetic production scenarios dispatcher. Invoked by e2e/hermetic/run.sh after
# its own steps. Sources lib.sh, exports the paths every scenario needs, runs each
# scenarios/*.sh in sorted order (each self-asserts via record/run_json), and exits
# non-zero if any scenario recorded a failure. Zero API spend — every scenario
# drives fixture transcripts through the offline pipeline.
#
# Each scenario runs in a subshell so it still inherits the sourced helpers and
# exported vars but an unguarded hard failure (set -e abort) in one scenario cannot
# abort the dispatcher and skip the rest. PROD_FAILURES is per-subshell: a subshell
# that recorded failures calls prod_report (which prints that scenario's failure list
# and returns non-zero, captured into prod_fail); a subshell that passed exits 0
# silently. The single aggregate summary line prints once here in the parent — on
# success after the loop, on failure via a non-zero exit — so "prod: all scenarios
# passed" is emitted exactly once, without weakening the per-scenario gate.
set -euo pipefail
PROD="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$PROD/../../.." && pwd)"
export PROD REPO
: "${WORK:=$(mktemp -d)}"; export WORK
export HERMETIC_PROJECTS="$WORK/projects"
# shellcheck source=/dev/null
. "$PROD/lib.sh"
shopt -s nullglob
prod_fail=0
for s in "$PROD"/scenarios/*.sh; do
  # shellcheck source=/dev/null
  ( . "$s"; [ "${#PROD_FAILURES[@]}" -eq 0 ] || prod_report ) || prod_fail=1
done
if [ "$prod_fail" -ne 0 ]; then exit 1; fi
printf '\nprod: all scenarios passed\n'
