#!/usr/bin/env bash
# Scenario 25 — subagent BREADTH the single-agent fixtures never exercise: (A) MULTIPLE
# sibling subagents in one run and (B) a NESTED subagent (a subagent that itself spawns a
# subagent). Every other hermetic subagent fixture carries exactly one agentId, so the
# per-agentId grouping in reduce/reduce.go (SubagentID -> a distinct node per agent;
# groupRoot -> each agent's prompts/turns/tools scoped under its own subagent node) is
# never driven with two agents, and no fixture exercises agent-inside-agent nesting.
#
# Both cases are built as the REAL on-disk layout (a main transcript plus separate
# subagents/agent-*.jsonl sub-transcripts — see cmd/catacomb/transcripts.go's glob and
# scenario 70's emitter), reduced by concatenating main + subs and replaying the concat
# (reduce is order-independent; the two-file capture path itself is covered by scenario
# 70). Assertions:
#   A. MULTIPLE: exactly 2 "type":"subagent" nodes with DISTINCT ids/agent_ids
#      (agent1, agent2) and DISTINCT subagent_type (general-purpose vs code-reviewer) —
#      fails if the per-agentId grouping collapses or collides. Each subagent's inner
#      tool_call groups under the CORRECT agent (Read carries agent_id=agent1, Grep
#      agent_id=agent2 — no cross-bleed) and each subagent node parents under its OWN
#      delegating Agent tool (tAgent1->agent1, tAgent2->agent2).
#   B. NESTED: agent1's turn spawns agent2 via an Agent tool_use (tTaskA1); agent2's
#      lines carry parent_tool_use_id=tTaskA1. Both subagent nodes exist and the nesting
#      REDUCES as a parent-child edge tool(tTaskA1, owned by agent1) -> subagent(agent2),
#      with the outer link tool(tMainAgent) -> subagent(agent1). FINDING: catacomb models
#      NO agent->agent field — there is no model.Node.ParentAgentID / "parent_agent_id"
#      anywhere in the source; nesting is expressed ONLY by the parent-child edge chain.
#      The assertion reflects that true behavior: the nesting edge must exist AND no node
#      may carry a parent_agent_id field.
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
w="$WORK/multi-nested-subagent"; mkdir -p "$w"

echo "== prod.25 multi/nested subagent: MULTIPLE sibling subagents in one run =="
msid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
multi="$w/multi.jsonl"
for f in 25-multi-main 25-multi-sub1 25-multi-sub2; do
  sed "s/__SESSION_ID__/$msid/g" "$PROD/fixtures/$f.jsonl.tmpl"
done > "$multi"
run_json 0 "$w/replay-multi.out" "replay concat (main + 2 sub-transcripts) -> multi snapshot" -- \
  catacomb replay "$multi" --export-jsonl "$w/multi.snap.jsonl"
rc=0; python3 - "$w/multi.snap.jsonl" <<'PY' || rc=$?
import json, sys
nodes, edges = [], []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    r = json.loads(line)
    if r.get("kind") == "node":
        nodes.append(r)
    elif r.get("kind") == "edge":
        edges.append(r)

errs = []
subs = [n for n in nodes if n.get("type") == "subagent"]
if len(subs) != 2:
    errs.append(f"want exactly 2 subagent nodes, got {len(subs)}")
if len({n["id"] for n in subs}) != 2:
    errs.append("subagent node ids are not distinct (grouping collapsed/collided)")
by_agent = {n.get("agent_id"): n for n in subs}
if set(by_agent) != {"agent1", "agent2"}:
    errs.append(f"subagent agent_ids {sorted(by_agent)} != agent1,agent2")
if by_agent.get("agent1", {}).get("subagent_type") != "general-purpose":
    errs.append("agent1 subagent_type != general-purpose (metadata cross-bled)")
if by_agent.get("agent2", {}).get("subagent_type") != "code-reviewer":
    errs.append("agent2 subagent_type != code-reviewer (metadata cross-bled)")

tools = {n.get("name"): n for n in nodes if n.get("type") == "tool_call"}
if tools.get("Read", {}).get("agent_id") != "agent1":
    errs.append("inner Read tool not grouped under agent1")
if tools.get("Grep", {}).get("agent_id") != "agent2":
    errs.append("inner Grep tool not grouped under agent2")


def edge(tool_suffix, agent_suffix):
    return any(
        e.get("type") == "parent_child"
        and e.get("src", "").endswith(tool_suffix)
        and e.get("dst", "").endswith(agent_suffix)
        for e in edges
    )


if not edge(":tool:tAgent1", ":agent:agent1"):
    errs.append("no parent-child edge tAgent1 -> agent1")
if not edge(":tool:tAgent2", ":agent:agent2"):
    errs.append("no parent-child edge tAgent2 -> agent2")

if errs:
    for e in errs:
        print("  -", e, file=sys.stderr)
    sys.exit(1)
print("2 distinct subagent nodes (agent1/general-purpose, agent2/code-reviewer); "
      "inner Read@agent1, Grep@agent2; each parents under its own Agent tool")
PY
record "$rc" "MULTIPLE: exactly 2 distinct subagent nodes, inner tools grouped per-agent (no cross-bleed), each under its own delegating tool"

echo "== prod.25 multi/nested subagent: NESTED subagent (a subagent spawns a subagent) =="
nsid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
nested="$w/nested.jsonl"
for f in 25-nested-main 25-nested-sub1 25-nested-sub2; do
  sed "s/__SESSION_ID__/$nsid/g" "$PROD/fixtures/$f.jsonl.tmpl"
done > "$nested"
run_json 0 "$w/replay-nested.out" "replay concat (main + agent1 + nested agent2) -> nested snapshot" -- \
  catacomb replay "$nested" --export-jsonl "$w/nested.snap.jsonl"
rc=0; python3 - "$w/nested.snap.jsonl" <<'PY' || rc=$?
import json, sys
nodes, edges = [], []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    r = json.loads(line)
    if r.get("kind") == "node":
        nodes.append(r)
    elif r.get("kind") == "edge":
        edges.append(r)

errs = []
subs = [n for n in nodes if n.get("type") == "subagent"]
if len(subs) != 2:
    errs.append(f"want exactly 2 subagent nodes, got {len(subs)}")
by_agent = {n.get("agent_id"): n for n in subs}
if set(by_agent) != {"agent1", "agent2"}:
    errs.append(f"subagent agent_ids {sorted(by_agent)} != agent1,agent2")


def tool_node(suffix):
    return next((n for n in nodes if n.get("type") == "tool_call" and n.get("id", "").endswith(suffix)), None)


task = tool_node(":tool:tTaskA1")
grep = tool_node(":tool:tGrep2")
if not task or task.get("agent_id") != "agent1":
    errs.append("agent1's delegating Agent tool (tTaskA1) not grouped under agent1")
if not grep or grep.get("agent_id") != "agent2":
    errs.append("agent2's inner Grep tool not grouped under agent2")


def edge(src_suffix, dst_suffix):
    return any(
        e.get("type") == "parent_child"
        and e.get("src", "").endswith(src_suffix)
        and e.get("dst", "").endswith(dst_suffix)
        for e in edges
    )


# THE nesting linkage: the nested subagent (agent2) parents under agent1's OWN delegating
# Agent tool_call (tTaskA1), which itself lives under agent1's subagent node.
if not edge(":tool:tTaskA1", ":agent:agent2"):
    errs.append("nesting NOT reduced: no parent-child edge tool(tTaskA1) -> subagent(agent2)")
# the outer link: agent1 parents under the main session's delegating Agent tool.
if not edge(":tool:tMainAgent", ":agent:agent1"):
    errs.append("outer link missing: no parent-child edge tool(tMainAgent) -> subagent(agent1)")

# FINDING (true behavior, not faked): catacomb has NO agent->agent field. There is no
# model.Node.ParentAgentID / "parent_agent_id" in the source, so no node carries one; the
# ONLY representation of nesting is the parent-child edge chain asserted above.
if any("parent_agent_id" in n for n in nodes):
    errs.append("unexpected parent_agent_id field on a node (model has none — fixture/assumption drift)")

if errs:
    for e in errs:
        print("  -", e, file=sys.stderr)
    sys.exit(1)
print("nesting reduces as edge chain tool(tMainAgent)->agent1, agent1's tool(tTaskA1)->agent2; "
      "no parent_agent_id field exists (nesting is edge-only)")
PY
record "$rc" "NESTED: both subagent nodes exist; nesting reduces as the tool(tTaskA1)->agent2 parent-child edge (no parent_agent_id field — edge-only linkage)"
