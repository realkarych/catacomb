#!/usr/bin/env bash
# Live nested-subagent basket cell wrapper. Two FORCED levels of delegation:
# baseline/baseline2 give the MAIN agent ONLY "Task", so it must delegate to the
# staged custom subagent sql-delegator (tools: Task), which in turn has ONLY Task
# and must delegate again to a general-purpose subagent that runs sqlite3 — depth 2.
# degraded gives the main agent "Task" but instructs it to delegate to a
# general-purpose subagent that runs the query ITSELF (depth 1, no nested node) —
# the seeded nesting regression. The workspace stages e2e/agents/ into the cell's
# .claude/agents/ so --setting-sources project discovers the custom agent, seeds the
# SQL db, and copies verify_sql.py in. reduce synthesizes a subagent node per
# agentId from the subagents/agent-*.jsonl sub-transcripts; run.sh counts depth (>=2
# nodes for baseline, <=1 for degraded). Sonnet for reliable multi-level obedience.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "There is a SQLite database (table orders(id, region, status, amount)) at ${SQL_DB}. ${NESTED_INSTRUCTION} The final subagent must compute the total paid amount (status='paid') per region, columns region and total, ordered by region, and write it as CSV with a header to out/result.csv using: sqlite3 -header -csv \"${SQL_DB}\" -cmd \".once out/result.csv\" \"<SELECT>\"" \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--setting-sources project --strict-mcp-config \
	--allowedTools "${NESTED_TOOLS}"
