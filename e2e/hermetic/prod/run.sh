#!/usr/bin/env bash
# Hermetic production scenarios dispatcher. Invoked by e2e/hermetic/run.sh after
# its own steps. Sources lib.sh, exports the paths every scenario needs, runs each
# scenarios/*.sh in sorted order (each self-asserts via record/run_json), and exits
# non-zero if any scenario recorded a failure. Zero API spend — every scenario
# drives fixture transcripts through the offline pipeline.
set -euo pipefail
PROD="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$PROD/../../.." && pwd)"
export PROD REPO
: "${WORK:=$(mktemp -d)}"; export WORK
export HERMETIC_PROJECTS="$WORK/projects"
# shellcheck source=/dev/null
. "$PROD/lib.sh"
for s in "$PROD"/scenarios/*.sh; do
  # shellcheck source=/dev/null
  . "$s"
done
prod_report
