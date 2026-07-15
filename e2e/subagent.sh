#!/usr/bin/env bash
# Live subagent basket cell wrapper. baseline delegates the seeded SQL task to a
# subagent (the Task tool); degraded does it inline. bench captures the session
# transcript incl. the subagent's isSidechain lines, so reduce synthesizes the
# subagent node and the Task step node. verify_sql.py scores out/result.csv into
# verifier.pass. Runs on sonnet for reliable multi-step obedience. Isolation flags
# match the other live wrappers so local runs match CI.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "There is a SQLite database (table orders(id, region, status, amount)) at ${SQL_DB}. ${SUBAGENT_INSTRUCTION} Compute the total paid amount (status='paid') per region, columns region and total, ordered by region, and write it as CSV with a header to out/result.csv using: sqlite3 -header -csv \"${SQL_DB}\" -cmd \".once out/result.csv\" \"<SELECT>\"" \
  --model "${CHILD_MODEL:-claude-sonnet-5}" \
  --output-format stream-json --verbose \
  --setting-sources project --strict-mcp-config \
  --allowedTools "Task,Bash(sqlite3:*)"
