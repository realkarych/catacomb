#!/usr/bin/env bash
# Live subagent basket cell wrapper. Delegation is FORCED structurally via the
# per-variant SUBAGENT_TOOLS allowlist rather than trusting the model to obey a
# prompt (live sonnet did NOT reliably delegate: some runs did the SQL work
# inline, producing zero subagent nodes). baseline/baseline2 get ONLY "Task" — no
# Bash — so the main agent physically cannot run sqlite3 itself and MUST spawn a
# subagent, which appears as a subagent node every run; degraded gets Bash-only
# (no Task) so it is forced inline with no subagent node. bench captures the
# session transcript incl. the subagent's isSidechain lines, so reduce synthesizes
# the subagent node and the Task step node. verify_sql.py scores out/result.csv
# into verifier.pass. Runs on sonnet for reliable multi-step obedience. set -u
# makes an unset SUBAGENT_TOOLS a loud failure rather than a silent empty
# allowlist. Isolation flags match the other live wrappers so local runs match CI.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "There is a SQLite database (table orders(id, region, status, amount)) at ${SQL_DB}. ${SUBAGENT_INSTRUCTION} Compute the total paid amount (status='paid') per region, columns region and total, ordered by region, and write it as CSV with a header to out/result.csv using: sqlite3 -header -csv \"${SQL_DB}\" -cmd \".once out/result.csv\" \"<SELECT>\"" \
  --model "${CHILD_MODEL:-claude-sonnet-5}" \
  --output-format stream-json --verbose \
  --setting-sources project --strict-mcp-config \
  --allowedTools "${SUBAGENT_TOOLS}"
