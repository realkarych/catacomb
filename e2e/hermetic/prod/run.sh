#!/usr/bin/env bash
# Hermetic production scenarios dispatcher. Invoked by e2e/hermetic/run.sh after
# its own steps. Sources lib.sh, exports the paths every scenario needs, runs each
# scenarios/*.sh in sorted order (each self-asserts via record/run_json), and exits
# non-zero if any scenario recorded a failure. Zero API spend — every scenario
# drives fixture transcripts through the offline pipeline.
#
# Each scenario runs in a subshell — `( . "$s"; prod_report )` — so it still inherits
# the sourced helpers and exported vars but an unguarded hard failure (set -e abort)
# in one scenario cannot abort the dispatcher and skip the rest. PROD_FAILURES is
# per-subshell, so each subshell calls prod_report itself (its non-zero exit on a
# recorded failure is captured into prod_fail), and the dispatcher exits non-zero if
# any scenario failed.
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
  ( . "$s"; prod_report ) || prod_fail=1
done
[ "$prod_fail" -eq 0 ] || exit 1
