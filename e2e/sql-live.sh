#!/usr/bin/env bash
# SQL-basket cell wrapper: the SP1 verifier contract exercised on a real `claude -p`.
#
# `catacomb bench` execs this directly, with NO shell, using the task's `dir` (.) as
# the working directory — e2e/, since run.sh cd's there before benching. The agent is
# handed a seeded SQLite database (SQL_DB, an absolute path in the driver's work dir,
# outside this workdir) and a per-variant SQL_INSTRUCTION (variant.env): baseline/
# baseline2 ask for the paid-only total per region (the correct result the golden
# encodes), degraded asks for the all-orders total (wrong by construction). It saves
# out/result.csv, bench captures it as an artifact, and the verify hook (verify_sql.py)
# scores it against the golden into verifier.pass — the run-level annotation e2e/run.sh
# gates on. `set -u` makes an unset SQL_DB or SQL_INSTRUCTION a loud failure the run.sh
# manifest assertions catch, rather than a silent baseline-shaped prompt.
#
# --strict-mcp-config (with no --mcp-config) loads NO MCP servers, and
# --setting-sources project restricts the child to project-scope settings, so ambient
# user-scope hooks/plugins/MCP do not inject into the run — local runs match CI, exactly
# like echo.sh. --allowedTools "Bash(sqlite3:*)" scopes the child to sqlite3 alone: one
# allowed tool plus a verbose, single-purpose imperative prompt is the PV-6b recipe for
# reliable tool obedience.
#
# The agent may only run sqlite3, so it cannot create the output directory itself:
# pre-create out/ here and clear any prior cell's result. All cells share this workdir,
# so clearing it means a cell that fails to write leaves no stale CSV for bench to
# capture — the artifact is then simply missing and verification fails, which is the
# honest outcome for a cell that produced nothing.
#
# The prompt steers the agent to sqlite3's own `.once` file output rather than a shell
# `>` redirect: under a scoped --allowedTools, Claude Code blocks shell output
# redirection, `mkdir`, and `cd` outside the workdir. A naive prompt makes the agent
# burn turns fighting that sandbox — trying to `cd` next to the (absolute, out-of-tree)
# database, or `> out/result.csv`. Handing it the schema, the exact single-command
# shape with the absolute db path already filled in, and an explicit "stay put, use
# .once" keeps the cell to one obedient sqlite3 call.
set -euo pipefail

mkdir -p out
rm -f out/result.csv

exec claude -p "There is a SQLite database (its only table is orders(id, region, status, amount)) at the absolute path ${SQL_DB}. ${SQL_INSTRUCTION} Write the result as CSV with a header row to the relative path out/result.csv, which already exists as a directory named out/ in your current directory. Stay where you are — do NOT cd — and produce the file with sqlite3's own .once output, not a shell '>' redirect. Run exactly this one command, filling in the SELECT: sqlite3 -header -csv \"${SQL_DB}\" -cmd \".once out/result.csv\" \"<your SELECT>\"" \
	--model "${CHILD_MODEL:-claude-haiku-4-5}" \
	--output-format stream-json \
	--verbose \
	--setting-sources project \
	--strict-mcp-config \
	--allowedTools "Bash(sqlite3:*)"
