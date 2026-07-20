#!/usr/bin/env bash
# Live nested-subagent basket cell wrapper. Two FORCED levels of delegation:
# baseline/baseline2 give the MAIN agent ONLY "Task", so it must delegate to the
# staged custom subagent sql-delegator (tools: Task), which in turn has ONLY Task
# and must delegate again to a general-purpose subagent that runs sqlite3 — depth 2.
# degraded gives the main agent "Task" but instructs it to delegate to a
# general-purpose subagent that runs the query ITSELF (depth 1, no nested node) —
# the seeded nesting regression. The workspace stages e2e/agents/ into the cell's
# .claude/agents/ so --setting-sources project discovers the custom agent. reduce
# synthesizes a subagent node per agentId from the subagents/agent-*.jsonl
# sub-transcripts; run.sh counts DEPTH (>=2 nodes for baseline, <=1 for degraded).
# The gate is subagent-node depth ONLY: no out/result.csv is asserted (a subagent's
# file write can be sandbox-blocked in CI — probe finding), so the leaf just runs the
# read query and reports the totals in its reply, matching basket-subagent.yaml's
# "no artifact verified" posture. Sonnet for reliable multi-level obedience.
set -euo pipefail
exec claude -p "There is a SQLite database (table orders(id, region, status, amount)) at ${SQL_DB}. ${NESTED_INSTRUCTION} The final subagent must run this read-only query and report the rows in its reply (do NOT write any file): sqlite3 -header -csv \"${SQL_DB}\" \"SELECT region, SUM(amount) AS total FROM orders WHERE status='paid' GROUP BY region ORDER BY region\"" \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--setting-sources project --strict-mcp-config \
	--allowedTools "${NESTED_TOOLS}"
