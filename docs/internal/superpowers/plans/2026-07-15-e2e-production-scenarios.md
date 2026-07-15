# Production-scenario E2E (subagents · skills · MCP) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Per repository AGENTS.md, execution is subagent-driven: a fresh subagent per task, reviewed between tasks; parallel implementer subagents are allowed for tasks that touch disjoint files, each in its own isolated worktree; tasks sharing a file stay serial.

**Goal:** Exercise real subagent dispatch, skill invocation, and a real stdio MCP server end-to-end through `bench → reduce → stepkey/phasekey → aggregate → regress` up to a gate, in both CI lanes, on a presence axis and a `verifier.pass` axis.

**Architecture:** Two lanes. The **hermetic** lane (per-PR, $0) drives fixture transcripts that contain `Task`/`Skill`/`mcp__e2ekit__record` nodes through the full offline pipeline and asserts the gate deterministically, plus a protocol-conformance smoke of the e2e MCP server, plus one composite multi-axis scenario. The **live** lane (weekly, ~$5–7) adds three focused per-feature `claude -p` baskets. Presence is anchored on the **tool_use step node** (`Task`/`Skill`/`mcp__e2ekit__record`), which gates identically to the existing `echo` step-axis; the synthesized `subagent`/`skill` node is asserted as an additional structural node.

**Tech Stack:** bash + python3 (stdlib) + YAML e2e fixtures; the real Claude Code CLI (`claude -p`) in the live lane; catacomb's `bench`/`verify`/`regress`/`replay`/`mcp` subcommands. No product-Go changes expected.

## Global Constraints

- **Zero production-Go changes are the target.** Only `e2e/`, `.github/workflows/`, and docs. If Task 0's spike proves a real reducer/gate gap (for example the `Task`/`Skill` tool node does not participate in the STEP-scope gate), that is a separate TDD'd Go task under the 100% coverage gate — do not work around it in fixtures.
- **e2e fixtures are bash/python/yaml — the Go no-comments rule and 100% coverage gate do NOT apply to them.** Follow the existing e2e convention: descriptive header comments on every script (see `e2e/presence.sh`).
- **Every basket keeps the PV-6b three-variant shape:** `baseline` (reference), `degraded` (seeded regression — MUST gate, exit 1), `baseline2` (A-vs-A control — MUST NOT gate, exit 0). `reps: 5`.
- **Live obedience runs on sonnet** (`CHILD_MODEL: claude-sonnet-5`) with single-axis, single-purpose prompts — the established recipe (`e2e/basket-sql.yaml`, `e2e/basket-presence.yaml`).
- **The e2e MCP server mirrors `mcp/server.go` byte-for-byte on the wire:** newline-delimited JSON-RPC 2.0 over stdio; `initialize` echoes the client `protocolVersion` (default `2025-06-18`) and returns `capabilities:{tools:{}}` + `serverInfo:{name,version}`; `tools/list` returns `{tools:[…]}`; `tools/call` returns `{content:[{type:"text",text}],isError}`; requests with no `id` (notifications) are ignored.
- **Transcript JSONL field names are exact** (`ingest/jsonl/jsonl.go`): top-level `type`,`uuid`,`sessionId`,`timestamp`,`parent_tool_use_id`,`isSidechain`,`agentId`,`message`,`version`,`cwd`; a subagent turn sets `isSidechain:true` and `agentId`; a tool_use block is `{"type":"tool_use","id","name","input"}`; skill invocation is `name:"Skill"`,`input:{"skill":"e2e-emit"}`; generic MCP is `name:"mcp__e2ekit__record"`.
- **Hermetic fixture mechanism** (copy `e2e/hermetic/agent.sh`): the cell `cmd` writes the rendered transcript to `$HERMETIC_PROJECTS/hermetic/$sid.jsonl` and prints two stream-json lines (`system`+`result`) to stdout; bench captures the transcript from `--projects-dir`.
- **Plan/spec references:** design spec `docs/internal/superpowers/specs/2026-07-15-e2e-production-scenarios-design.md`; spike findings `docs/internal/superpowers/plans/2026-07-15-e2e-production-scenarios-spike.md`.

## CONFIRMED CLI SURFACE (Task 0 spike — BINDING; overrides any illustrative code below)

These were verified offline against the built binary. Where a task's example code
below disagrees with this block, **this block wins** — the examples predate the spike.

1. **Step-node presence gate.** Dropping a `Task`/`Skill`/`mcp__e2ekit__record`
   tool step node yields a finding `{"scope":"step","name":"<Task|Skill|mcp__e2ekit__record>","metric":"presence","verdict":"notable","detail":"present 5/5 -> 0/5; step alignment coverage 0.00 below floor 0.70"}`.
   The verdict is **`notable`, not `regression`**, and plain `regress` returns **exit 0**.
   To make the seeded regression gate, add **`--fail-on-notable`** to the degraded
   comparison → exit 1. Assert the finding with `verdict == "notable"` and
   `metric == "presence"` (NOT `verdict == "regression"`). Run the A-vs-A control
   with `--fail-on-notable --metric-rel-delta 0.5` and assert **exit 0**.
2. **Structural node check.** There is **no `catacomb replay --json`**. Use
   `catacomb replay <transcript> --export-jsonl <snap.jsonl>` then grep the snapshot:
   node lines are `{"kind":"node",...,"type":"subagent"|"skill"|"tool_call"|...}`.
   `grep -q '"type":"subagent"'` / `'"type":"skill"'` on the exported snapshot.
   The transcript path is `<run-dir>/session.jsonl`.
3. **Phase (checkpoint) gate.** There is **no `--checkpoint` flag**. Declare
   `checkpoints: [<name>]` on the task; a dropped marker then yields
   `{"scope":"phase","name":"<name>","metric":"presence","verdict":"regression"}`
   which gates at exit 1 under plain `regress` (assert `verdict == "regression"`).
4. **Verifier idiom** (copy `e2e/verify_sql.py` exactly): `from catacomb_verifier import Cell, emit`; `cell = Cell.from_env()`; read `cell.artifact("out/result.csv")`; `emit(passed=<bool>, tool="<id>", tool_version="1")` writes the `verifier.pass` annotation. Do NOT call `emit("verifier.pass", 1)` — that signature is wrong.
5. **`ann:verifier.pass` gate.** A verifier.pass drop (5/5 → 0/5) yields a finding with `metric == "ann:verifier.pass"` and `verdict == "regression"`; gates under plain `regress` (the annotation-rate path, `--annotation-rate-delta` default 0.1).

---

## File Structure

```
e2e/mcp-e2ekit/server.py             real stdio MCP server (python stdlib), tool `record`
e2e/mcp-e2ekit/mcp.json              mcp-config: {"mcpServers":{"e2ekit":{"command":"python3","args":[".../server.py"]}}}
e2e/mcp-e2ekit/smoke.py              driver test: 3 JSON-RPC requests -> asserted responses

e2e/hermetic/prod/run.sh             dispatcher: runs every e2e/hermetic/prod/scenarios/*.sh
e2e/hermetic/prod/lib.sh             shared pass/fail/record/run_json helpers (sourced by scenarios)
e2e/hermetic/prod/scenarios/10-mcp-protocol.sh    protocol smoke of server.py + generic-mcp step node
e2e/hermetic/prod/scenarios/20-subagent.sh        subagent presence scenario
e2e/hermetic/prod/scenarios/30-skill.sh           skill presence scenario
e2e/hermetic/prod/scenarios/40-composite.sh       subagent+skill+mcp+verifier, all axes
e2e/hermetic/prod/fixtures/*.jsonl.tmpl           per-variant transcript templates
e2e/hermetic/prod/fixtures/*.basket.yaml.tmpl     per-scenario baskets
e2e/hermetic/prod/fixtures/emit.sh                fixture-emitting agent (renders a template to projects dir)

e2e/skills/e2e-emit/SKILL.md         real minimal project-scoped skill
e2e/verify_emit.py                   skill-artifact verifier (out/result.csv == known token)

e2e/basket-subagent.yaml + e2e/subagent.sh        live subagent basket
e2e/basket-skill.yaml    + e2e/skill.sh           live skill basket
e2e/basket-mcp.yaml      + e2e/mcp-record.sh      live MCP basket

e2e/run.sh                           + subagent/skill/mcp bench + controls + seeded regressions
e2e/hermetic/run.sh                  + one step invoking e2e/hermetic/prod/run.sh
.github/workflows/e2e-live.yml       timeout-minutes + cost header
AGENTS.md                            E2E table rows mention the production baskets
```

**Dependency graph:** Task 0 (spike) ∥ Task 1 (server) → Task 2 (prod harness + mcp smoke) → { Task 3 ∥ Task 4 } → Task 5 (composite). Live: Task 6 → Task 7 → Task 8 (all edit `e2e/run.sh`, serial). Task 9 last. Tasks 3 and 4 touch disjoint files → parallel-safe.

---

### Task 0: Spike — validate the three live/gate assumptions

**Files:**

- Create: `docs/internal/superpowers/plans/2026-07-15-e2e-production-scenarios-spike.md` (findings record)

**Interfaces:**

- Produces: three decisions consumed by Tasks 3/6 (presence anchor node), 4/7 (skill source set), 6 (sidechain capture confirmed).

This task is an investigation, not TDD. It resolves the §9 risks before fixtures are finalized. Risk 1 is offline (no API); risks 2–3 need an Anthropic auth secret (`ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`) — run those steps where auth exists (locally or a CI dispatch). Record every finding.

- [ ] **Step 1: Risk 1 (offline) — does a `Task` tool node gate at STEP scope?**

Build a two-line proof by hand and drive it through reduce+regress. Create `/tmp/spike/base.jsonl`:

```json
{"type":"user","uuid":"u1","sessionId":"S","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"S","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t1","name":"Task","input":{"subagent_type":"claude","description":"run sql"}}]}}
{"type":"assistant","uuid":"a2","sessionId":"S","timestamp":"2026-06-20T10:00:02Z","isSidechain":true,"agentId":"agent1","parent_tool_use_id":"t1","message":{"role":"assistant","id":"m2","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"sqlite3 x"}}]}}
```

And `/tmp/spike/degraded.jsonl` (same but the assistant runs Bash inline, no `Task`, no sidechain):

```json
{"type":"user","uuid":"u1","sessionId":"D","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"D","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sqlite3 x"}}]}}
```

- [ ] **Step 2: Reduce both and inspect the graph via `catacomb replay`**

Run: `catacomb replay /tmp/spike/base.jsonl` and `catacomb replay /tmp/spike/degraded.jsonl`
Expected: base reports strictly more nodes (a `Task` tool node + a `subagent` node) than degraded. Record the node count delta and whether a `subagent`-type node appears. Confirm `catacomb reduce`/`replay` exposes step keys (inspect `--json` if available: `catacomb replay --json /tmp/spike/base.jsonl` and grep for `"type":"subagent"` and `"name":"Task"`).

- [ ] **Step 3: Record the presence-anchor decision**

Write to the spike doc: does a bench+regress comparison of a subagent-present vs subagent-absent run raise a STEP-scope regression? (The existing `echo` step-axis proves any tool_use node does; confirm `Task` specifically, since it is the anchor for Tasks 3/6.) Decision recorded: **anchor presence on the `Task`/`Skill`/`mcp__e2ekit__record` tool_use step node**; assert the `subagent`/`skill` node as an additional structural presence check. If `Task` does NOT gate, open a product task.

- [ ] **Step 4: Risk 2 (live, needs auth) — project skill discovery**

Create a throwaway workspace with `.claude/skills/e2e-emit/SKILL.md` (frontmatter `name: e2e-emit`, `description: ...`, body: "Write the exact text `CATACOMB-SKILL-OK` to out/result.csv"). Run:

```bash
claude -p "Use the e2e-emit skill to produce the output file." \
  --model claude-sonnet-5 --output-format stream-json --verbose \
  --setting-sources project --allowedTools "Skill,Write"
```

Expected: the stream-json shows a `Skill` tool_use with `input.skill == "e2e-emit"`. Record whether `--setting-sources project` suffices, or `project,user`/a plugin is required. This decides Task 7's flags.

- [ ] **Step 5: Risk 3 (live, needs auth) — sidechain capture in the bench transcript**

Run a one-cell dispatch that delegates to a subagent and capture the session transcript bench would read:

```bash
claude -p "Use a subagent (the Task tool) to run: echo hi. Then reply done." \
  --model claude-sonnet-5 --output-format stream-json --verbose \
  --setting-sources project --allowedTools "Task,Bash"
```

Then locate the session `.jsonl` under the projects dir and confirm it contains lines with `"isSidechain":true` and an `agentId`. Record the exact field names/shape observed (they must match the `ingest/jsonl` struct tags; note any drift).

- [ ] **Step 6: Commit the findings**

```bash
git add docs/internal/superpowers/plans/2026-07-15-e2e-production-scenarios-spike.md
git commit -m "docs: spike findings — production E2E gate/live assumptions"
```

---

### Task 1: Real e2e stdio MCP server + protocol driver test

**Files:**

- Create: `e2e/mcp-e2ekit/server.py`
- Create: `e2e/mcp-e2ekit/smoke.py`
- Create: `e2e/mcp-e2ekit/mcp.json`

**Interfaces:**

- Produces: a runnable MCP server `python3 e2e/mcp-e2ekit/server.py` speaking newline-JSON-RPC on stdio, serving one tool `record` (`inputSchema` requires a string `value`); `tools/call record {value:X}` writes `X` to the file named by env `E2EKIT_OUT` (default `out/mcp-record.txt`) and returns `{content:[{type:"text",text:"recorded X"}],isError:false}`. `mcp.json` points a Claude Code client at it. Consumed by Tasks 2 and 8.

- [ ] **Step 1: Write the failing driver test (`smoke.py`)**

```python
#!/usr/bin/env python3
"""Protocol-conformance smoke of e2e/mcp-e2ekit/server.py.

Drives the server over stdio with a fixed 3-request JSON-RPC script and asserts
the responses match the MCP contract catacomb's own `mcp` server implements
(mcp/server.go): newline-delimited JSON-RPC 2.0, initialize -> tools/list ->
tools/call. Mirrors e2e/hermetic/run.sh step 19's `catacomb mcp` smoke. Exit 0 on
full conformance, 1 otherwise (message on stderr)."""
import json, os, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))


def main() -> int:
    tmp = tempfile.mkdtemp()
    out = os.path.join(tmp, "rec.txt")
    reqs = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize",
         "params": {"protocolVersion": "2025-06-18"}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
        {"jsonrpc": "2.0", "id": 3, "method": "tools/call",
         "params": {"name": "record", "arguments": {"value": "ALPHA"}}},
    ]
    stdin = "".join(json.dumps(r) + "\n" for r in reqs)
    env = dict(os.environ, E2EKIT_OUT=out)
    proc = subprocess.run([sys.executable, os.path.join(HERE, "server.py")],
                          input=stdin, capture_output=True, text=True, env=env)
    lines = [ln for ln in proc.stdout.splitlines() if ln.strip()]
    errs = []
    if len(lines) != 3:
        errs.append(f"want 3 responses (notification has no id -> no reply), got {len(lines)}: {lines!r}")
    resp = [json.loads(ln) for ln in lines] if len(lines) == 3 else []
    if resp:
        init = resp[0]
        if init.get("result", {}).get("protocolVersion") != "2025-06-18":
            errs.append(f"initialize protocolVersion: {init!r}")
        if "tools" not in init.get("result", {}).get("capabilities", {}):
            errs.append(f"initialize capabilities.tools missing: {init!r}")
        tools = resp[1].get("result", {}).get("tools", [])
        if [t.get("name") for t in tools] != ["record"]:
            errs.append(f"tools/list names: {tools!r}")
        call = resp[2].get("result", {})
        if call.get("isError") is not False:
            errs.append(f"tools/call isError not False: {call!r}")
        text = "".join(c.get("text", "") for c in call.get("content", []))
        if "ALPHA" not in text:
            errs.append(f"tools/call content lacks ALPHA: {call!r}")
    try:
        if open(out).read().strip() != "ALPHA":
            errs.append(f"server did not persist value to {out}")
    except OSError as e:
        errs.append(f"output file unreadable: {e}")
    if errs:
        for x in errs:
            print("  -", x, file=sys.stderr)
        return 1
    print("mcp-e2ekit: protocol conformance OK (initialize/tools.list/tools.call, value persisted)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Run it to verify it fails**

Run: `python3 e2e/mcp-e2ekit/smoke.py`
Expected: FAIL — `server.py` does not exist yet (subprocess errors / no responses).

- [ ] **Step 3: Implement `server.py`**

```python
#!/usr/bin/env python3
"""Real stdio MCP server fixture for the catacomb E2E suite.

Speaks the exact protocol catacomb's own `mcp` server speaks (see mcp/server.go):
newline-delimited JSON-RPC 2.0 over stdio, methods initialize / tools/list /
tools/call, notifications (no id) ignored. It is NOT a mock — it is a genuine,
minimal MCP server. It serves one tool, `record`, distinct from catacomb's `mark`
so the reducer's general `mcp__server__tool` step-key path is exercised (not the
mark phase-marker special case). `record {value}` persists the value to
$E2EKIT_OUT (default out/mcp-record.txt) so a live cell's verifier can read it
back, and echoes it in the tool result."""
import json, os, sys

PROTOCOL_DEFAULT = "2025-06-18"


def initialize_result(params):
    version = PROTOCOL_DEFAULT
    if isinstance(params, dict) and params.get("protocolVersion"):
        version = params["protocolVersion"]
    return {"protocolVersion": version,
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "e2ekit", "version": "0.1.0"}}


def tools_list_result():
    return {"tools": [{
        "name": "record",
        "description": "Persist a value for the catacomb E2E verifier and echo it back.",
        "inputSchema": {"type": "object",
                        "properties": {"value": {"type": "string", "description": "text to record"}},
                        "required": ["value"]}}]}


def tools_call_result(params):
    if not isinstance(params, dict):
        return {"content": [{"type": "text", "text": "invalid params"}], "isError": True}
    if params.get("name") != "record":
        return {"content": [{"type": "text", "text": "unknown tool"}], "isError": True}
    args = params.get("arguments") or {}
    value = args.get("value")
    if not isinstance(value, str) or value == "":
        return {"content": [{"type": "text", "text": "value is required"}], "isError": True}
    out = os.environ.get("E2EKIT_OUT", "out/mcp-record.txt")
    d = os.path.dirname(out)
    if d:
        os.makedirs(d, exist_ok=True)
    with open(out, "w") as f:
        f.write(value)
    return {"content": [{"type": "text", "text": "recorded " + value}], "isError": False}


def dispatch(method, params):
    if method == "initialize":
        return initialize_result(params), None
    if method == "tools/list":
        return tools_list_result(), None
    if method == "tools/call":
        return tools_call_result(params), None
    return None, {"code": -32601, "message": "method not found: " + str(method)}


def main():
    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": None,
                             "error": {"code": -32700, "message": "parse error"}}) + "\n")
            sys.stdout.flush()
            continue
        if "id" not in req:
            continue
        result, err = dispatch(req.get("method"), req.get("params"))
        resp = {"jsonrpc": "2.0", "id": req["id"]}
        if err:
            resp["error"] = err
        else:
            resp["result"] = result
        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
```

- [ ] **Step 4: Run the smoke to verify it passes**

Run: `python3 e2e/mcp-e2ekit/smoke.py`
Expected: PASS — `mcp-e2ekit: protocol conformance OK ...`

- [ ] **Step 5: Create the mcp-config**

```json
{"mcpServers":{"e2ekit":{"command":"python3","args":["__E2EKIT__/server.py"]}}}
```

Save as `e2e/mcp-e2ekit/mcp.json`. The `__E2EKIT__` token is rendered to the absolute dir by the live wrapper (Task 8), the same `sed`-render pattern `e2e/hermetic/run.sh` uses for `__WORK__`.

- [ ] **Step 6: Commit**

```bash
git add e2e/mcp-e2ekit/
git commit -m "test(e2e): real stdio MCP server fixture (tool record) + protocol smoke"
```

---

### Task 2: Hermetic prod harness + MCP protocol/step-node scenario

**Files:**

- Create: `e2e/hermetic/prod/run.sh`
- Create: `e2e/hermetic/prod/lib.sh`
- Create: `e2e/hermetic/prod/fixtures/emit.sh`
- Create: `e2e/hermetic/prod/scenarios/10-mcp-protocol.sh`
- Create: `e2e/hermetic/prod/fixtures/mcp.jsonl.tmpl`, `e2e/hermetic/prod/fixtures/mcp-degraded.jsonl.tmpl`, `e2e/hermetic/prod/fixtures/mcp.basket.yaml.tmpl`
- Modify: `e2e/hermetic/run.sh` (append one step invoking the prod dispatcher)

**Interfaces:**

- Consumes: `e2e/mcp-e2ekit/server.py`, `smoke.py` (Task 1).
- Produces: `lib.sh` exporting `pass/failrec/record/run_json` and a `PROD_FAILURES` array + `prod_report` summariser; `emit.sh` (renders `$SCENARIO_TMPL` to `$HERMETIC_PROJECTS/hermetic/$sid.jsonl`); the dispatcher `run.sh` that sources `lib.sh` then runs every `scenarios/*.sh` in sorted order and exits non-zero if any recorded a failure. Consumed by Tasks 3/4/5.

- [ ] **Step 1: Write `lib.sh` (shared bookkeeping, copied from the run.sh conventions)**

```bash
#!/usr/bin/env bash
# Shared assertion bookkeeping for the hermetic production scenarios. Sourced by
# the dispatcher (run.sh) and each scenarios/*.sh. Mirrors the pass/failrec/
# record/run_json helpers in e2e/hermetic/run.sh so scenario code reads the same.
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
```

- [ ] **Step 2: Write `emit.sh` (fixture agent, generalises `e2e/hermetic/agent.sh`)**

```bash
#!/usr/bin/env bash
# Fixture-emitting agent for the hermetic production scenarios. Like
# e2e/hermetic/agent.sh but transcript-only and template-parameterised: it renders
# the template named by SCENARIO_TMPL (a per-variant transcript) into the projects
# dir bench reads, and prints the two stream-json lines bench needs on stdout. When
# EMIT_CSV is set it also writes that literal text to out/result.csv so a verify
# hook can score the artifact (used by the composite scenario). No API spend.
set -euo pipefail
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p out "$HERMETIC_PROJECTS/hermetic"
[ -z "${EMIT_CSV:-}" ] || printf '%s' "$EMIT_CSV" > out/result.csv
sed "s/__SESSION_ID__/$sid/g" "$SCENARIO_TMPL" > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
```

- [ ] **Step 3: Write the MCP transcript templates**

`fixtures/mcp.jsonl.tmpl` (baseline/baseline2 — the generic MCP tool node is present):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t1","name":"mcp__e2ekit__record","input":{"value":"ALPHA"}}]}}
{"type":"user","uuid":"u2","parent_tool_use_id":"t1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"recorded ALPHA","is_error":false}]}}
```

`fixtures/mcp-degraded.jsonl.tmpl` (degraded — no MCP tool, a plain Bash step instead, so the `mcp__e2ekit__record` step node vanishes):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"true"}}]}}
{"type":"user","uuid":"u2","parent_tool_use_id":"t1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"","is_error":false}]}}
```

- [ ] **Step 4: Write the MCP basket template**

`fixtures/mcp.basket.yaml.tmpl` — three variants selecting the template via `SCENARIO_TMPL`:

```yaml
basket: prod-mcp
reps: 5
tasks:
  - id: mcp
    cmd: ["__PROD__/fixtures/emit.sh"]
    dir: __WORK__/cellwork
variants:
  - id: baseline
    env: { SCENARIO_TMPL: "__PROD__/fixtures/mcp.jsonl.tmpl" }
  - id: degraded
    env: { SCENARIO_TMPL: "__PROD__/fixtures/mcp-degraded.jsonl.tmpl" }
  - id: baseline2
    env: { SCENARIO_TMPL: "__PROD__/fixtures/mcp.jsonl.tmpl" }
```

- [ ] **Step 5: Write the scenario `scenarios/10-mcp-protocol.sh`**

```bash
#!/usr/bin/env bash
# Scenario 10 — real e2e MCP server: (a) protocol-conformance smoke of server.py,
# and (b) the generic mcp__server__tool step node gates. Sourced by run.sh with
# lib.sh already loaded and PROD/WORK/HERMETIC_* exported.
set -euo pipefail
echo "== prod.10 mcp: protocol smoke of e2e/mcp-e2ekit/server.py =="
rc=0; python3 "$REPO/e2e/mcp-e2ekit/smoke.py" || rc=$?
record "$rc" "e2e MCP server passes JSON-RPC protocol conformance"

echo "== prod.10 mcp: generic mcp step node present in baseline, absent in degraded =="
w="$WORK/mcp"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/mcp.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-mcp basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
run_json 1 "$w/regress.json" "degraded drops mcp step node -> STEP regression" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-mcp,variant=baseline \
  --candidate label:basket=prod-mcp,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hits = [f for f in rep.get("findings", []) if f.get("scope") == "step" and f.get("verdict") == "regression"]
if not hits:
    print("no step-scope regression; findings:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("step-scope regression present (mcp__e2ekit__record node dropped)")
PY
record "$rc" "regress attributes a STEP-scope regression to the dropped mcp node"
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-mcp,variant=baseline \
  --candidate label:basket=prod-mcp,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions"
```

- [ ] **Step 6: Write the dispatcher `run.sh`**

```bash
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
```

- [ ] **Step 7: Wire it into `e2e/hermetic/run.sh`**

Append after the final existing step (step 21), before the failure summary. Modify `e2e/hermetic/run.sh` to add:

```bash
echo "== 22. production scenarios (subagent/skill/mcp) =="
prod_rc=0
WORK="$work/prod" bash "$here/prod/run.sh" || prod_rc=$?
record "$prod_rc" "hermetic production scenarios (prod/run.sh) all pass"
```

(`$here` and `$work` already exist in run.sh. `record` is already defined there.)

- [ ] **Step 8: Run the whole hermetic driver**

Run: `make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh`
Expected: PASS through step 22; scenario 10 prints protocol OK + STEP regression + A-vs-A zero.

- [ ] **Step 9: Commit**

```bash
git add e2e/hermetic/prod/ e2e/hermetic/run.sh
git commit -m "test(e2e): hermetic prod harness + real-MCP protocol/step-node scenario"
```

---

### Task 3: Hermetic subagent presence scenario

**Files:**

- Create: `e2e/hermetic/prod/scenarios/20-subagent.sh`
- Create: `e2e/hermetic/prod/fixtures/subagent.jsonl.tmpl`, `subagent-degraded.jsonl.tmpl`, `subagent.basket.yaml.tmpl`

**Interfaces:**

- Consumes: `lib.sh`, `emit.sh`, dispatcher paths (Task 2).
- Produces: nothing downstream (a self-contained scenario). Parallel-safe with Task 4 (disjoint files).

- [ ] **Step 1: Write the subagent transcript templates**

`fixtures/subagent.jsonl.tmpl` (baseline/baseline2 — a `Task` tool node + a sidechain subagent turn, so a `subagent` node and a `Task` step node both appear):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tTask","name":"Task","input":{"subagent_type":"claude","description":"run the query"}}]}}
{"type":"assistant","uuid":"a2","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","isSidechain":true,"agentId":"agent1","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m2","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tChild","name":"Bash","input":{"command":"sqlite3 db"}}]}}
{"type":"user","uuid":"u3","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:03Z","isSidechain":true,"agentId":"agent1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tChild","content":"ok","is_error":false}]}}
{"type":"user","uuid":"u4","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tTask","content":"done","is_error":false}]}}
```

`fixtures/subagent-degraded.jsonl.tmpl` (degraded — the work is inline Bash, no `Task`, no sidechain):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tChild","name":"Bash","input":{"command":"sqlite3 db"}}]}}
{"type":"user","uuid":"u2","parent_tool_use_id":"tChild","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tChild","content":"ok","is_error":false}]}}
```

- [ ] **Step 2: Write the basket template**

`fixtures/subagent.basket.yaml.tmpl`:

```yaml
basket: prod-subagent
reps: 5
tasks:
  - id: subagent
    cmd: ["__PROD__/fixtures/emit.sh"]
    dir: __WORK__/cellwork
variants:
  - id: baseline
    env: { SCENARIO_TMPL: "__PROD__/fixtures/subagent.jsonl.tmpl" }
  - id: degraded
    env: { SCENARIO_TMPL: "__PROD__/fixtures/subagent-degraded.jsonl.tmpl" }
  - id: baseline2
    env: { SCENARIO_TMPL: "__PROD__/fixtures/subagent.jsonl.tmpl" }
```

- [ ] **Step 3: Write the scenario `scenarios/20-subagent.sh`**

```bash
#!/usr/bin/env bash
# Scenario 20 — subagent dispatch presence. baseline delegates via the Task tool
# (a Task step node + a synthesized subagent node); degraded does the work inline
# (neither node). The dropped Task step node gates at STEP scope; the graph also
# proves a subagent-type node exists in baseline and not in degraded. A-vs-A clean.
set -euo pipefail
echo "== prod.20 subagent: Task/subagent node present in baseline, absent in degraded =="
w="$WORK/subagent"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/subagent.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-subagent basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
run_json 1 "$w/regress.json" "degraded drops Task step node -> STEP regression" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-subagent,variant=baseline \
  --candidate label:basket=prod-subagent,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
if not [f for f in rep.get("findings", []) if f.get("scope") == "step" and f.get("verdict") == "regression"]:
    print("no step-scope regression:", rep.get("findings"), file=sys.stderr); sys.exit(1)
print("step-scope regression present (Task node dropped)")
PY
record "$rc" "regress attributes a STEP-scope regression to the dropped Task node"
# structural check: a subagent-type node exists in a baseline run, none in a degraded run
base_dir="$(echo "$w"/runs/bench-prod-subagent-subagent-baseline-r1)"
deg_dir="$(echo "$w"/runs/bench-prod-subagent-subagent-degraded-r1)"
rc=0; catacomb replay --json "$base_dir/session.jsonl" 2>/dev/null | grep -q '"type":"subagent"' || rc=1
record "$rc" "baseline graph contains a subagent node"
rc=0; if catacomb replay --json "$deg_dir/session.jsonl" 2>/dev/null | grep -q '"type":"subagent"'; then rc=1; fi
record "$rc" "degraded graph contains no subagent node"
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-subagent,variant=baseline \
  --candidate label:basket=prod-subagent,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["regressions"]==0 else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions"
```

> **Note for the implementer:** if Task 0 Step 2 found `catacomb replay --json` does not exist or does not emit node `type`, replace the two structural greps with the equivalent check the spike identified (for example `catacomb reduce ... | python3 -c '...'`), keeping the STEP-regression assertion as the primary gate. Do not delete the structural check — adapt it to the confirmed CLI surface.

- [ ] **Step 4: Run the hermetic driver**

Run: `make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh`
Expected: scenario 20 passes — STEP regression fires, subagent node present/absent as asserted, A-vs-A clean.

- [ ] **Step 5: Commit**

```bash
git add e2e/hermetic/prod/scenarios/20-subagent.sh e2e/hermetic/prod/fixtures/subagent*
git commit -m "test(e2e): hermetic subagent-presence scenario"
```

---

### Task 4: Hermetic skill presence scenario + real skill

**Files:**

- Create: `e2e/skills/e2e-emit/SKILL.md`
- Create: `e2e/hermetic/prod/scenarios/30-skill.sh`
- Create: `e2e/hermetic/prod/fixtures/skill.jsonl.tmpl`, `skill-degraded.jsonl.tmpl`, `skill.basket.yaml.tmpl`

**Interfaces:**

- Consumes: `lib.sh`, `emit.sh`, dispatcher paths (Task 2).
- Produces: `e2e/skills/e2e-emit/SKILL.md`, consumed by Task 5 (composite) and Task 7 (live skill basket). Parallel-safe with Task 3.

- [ ] **Step 1: Write the real skill `e2e/skills/e2e-emit/SKILL.md`**

```markdown
---
name: e2e-emit
description: Use when asked to produce the catacomb E2E output file — writes the fixed token CATACOMB-SKILL-OK to out/result.csv.
---

# e2e-emit

When invoked, create the directory `out` if it does not exist, then write exactly
the following single line (no trailing newline, no extra text) to `out/result.csv`:

```

CATACOMB-SKILL-OK

```

Then reply `done`. Do not perform any other action.
```

- [ ] **Step 2: Write the skill transcript templates**

`fixtures/skill.jsonl.tmpl` (baseline/baseline2 — a `Skill` tool node is present):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tSkill","name":"Skill","input":{"skill":"e2e-emit"}}]}}
{"type":"user","uuid":"u2","parent_tool_use_id":"tSkill","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tSkill","content":"done","is_error":false}]}}
```

`fixtures/skill-degraded.jsonl.tmpl` (degraded — no `Skill`, an inline Write instead):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tWrite","name":"Write","input":{"file_path":"out/result.csv"}}]}}
{"type":"user","uuid":"u2","parent_tool_use_id":"tWrite","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tWrite","content":"done","is_error":false}]}}
```

- [ ] **Step 3: Write the basket template `fixtures/skill.basket.yaml.tmpl`**

```yaml
basket: prod-skill
reps: 5
tasks:
  - id: skill
    cmd: ["__PROD__/fixtures/emit.sh"]
    dir: __WORK__/cellwork
variants:
  - id: baseline
    env: { SCENARIO_TMPL: "__PROD__/fixtures/skill.jsonl.tmpl" }
  - id: degraded
    env: { SCENARIO_TMPL: "__PROD__/fixtures/skill-degraded.jsonl.tmpl" }
  - id: baseline2
    env: { SCENARIO_TMPL: "__PROD__/fixtures/skill.jsonl.tmpl" }
```

- [ ] **Step 4: Write the scenario `scenarios/30-skill.sh`**

```bash
#!/usr/bin/env bash
# Scenario 30 — skill invocation presence. baseline invokes the Skill tool (a
# NodeSkill-upgraded step node); degraded does the work with an inline Write (no
# Skill node). The dropped Skill step node gates at STEP scope; the graph proves a
# skill-type node exists in baseline and not degraded. A-vs-A clean.
set -euo pipefail
echo "== prod.30 skill: Skill node present in baseline, absent in degraded =="
w="$WORK/skill"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/skill.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-skill basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
run_json 1 "$w/regress.json" "degraded drops Skill step node -> STEP regression" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-skill,variant=baseline \
  --candidate label:basket=prod-skill,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
if not [f for f in rep.get("findings", []) if f.get("scope") == "step" and f.get("verdict") == "regression"]:
    print("no step-scope regression:", rep.get("findings"), file=sys.stderr); sys.exit(1)
print("step-scope regression present (Skill node dropped)")
PY
record "$rc" "regress attributes a STEP-scope regression to the dropped Skill node"
base_dir="$w/runs/bench-prod-skill-skill-baseline-r1"
deg_dir="$w/runs/bench-prod-skill-skill-degraded-r1"
rc=0; catacomb replay --json "$base_dir/session.jsonl" 2>/dev/null | grep -q '"type":"skill"' || rc=1
record "$rc" "baseline graph contains a skill node"
rc=0; if catacomb replay --json "$deg_dir/session.jsonl" 2>/dev/null | grep -q '"type":"skill"'; then rc=1; fi
record "$rc" "degraded graph contains no skill node"
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-skill,variant=baseline \
  --candidate label:basket=prod-skill,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["regressions"]==0 else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions"
```

(Same `replay --json` adaptation note as Task 3 applies — keep the STEP gate primary; adapt the structural grep to the spike-confirmed surface.)

- [ ] **Step 5: Run the hermetic driver**

Run: `make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh`
Expected: scenario 30 passes.

- [ ] **Step 6: Commit**

```bash
git add e2e/skills/e2e-emit/SKILL.md e2e/hermetic/prod/scenarios/30-skill.sh e2e/hermetic/prod/fixtures/skill*
git commit -m "test(e2e): hermetic skill-presence scenario + real e2e-emit skill"
```

---

### Task 5: Hermetic composite scenario (subagent + skill + MCP + verifier)

**Files:**

- Create: `e2e/hermetic/prod/scenarios/40-composite.sh`
- Create: `e2e/hermetic/prod/fixtures/composite.jsonl.tmpl`, `composite-degraded.jsonl.tmpl`, `composite.basket.yaml.tmpl`
- Create: `e2e/hermetic/prod/fixtures/verify_token.py`

**Interfaces:**

- Consumes: `lib.sh`, `emit.sh`, dispatcher paths (Task 2); the `e2e-emit` skill (Task 4) conceptually.
- Produces: nothing downstream. Depends on Task 4 (runs after it).

- [ ] **Step 1: Write the artifact verifier `fixtures/verify_token.py`**

```python
#!/usr/bin/env python3
"""Composite-scenario verifier: pass iff out/result.csv equals the expected token.

The catacomb verify hook runs this against the captured artifact; it writes the
run-level verifier.pass annotation via the catacomb_verifier SDK, exactly like
e2e/verify_sql.py. Baseline emits CATACOMB-OK (pass); degraded emits WRONG (fail)."""
import os, sys
from catacomb_verifier import emit  # provided on PYTHONPATH by the hermetic driver

WANT = "CATACOMB-OK"


def main() -> int:
    try:
        got = open(os.path.join("out", "result.csv")).read().strip()
    except OSError:
        got = ""
    emit("verifier.pass", 1 if got == WANT else 0)
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

> **Implementer:** confirm the SDK entrypoint against `e2e/verify_sql.py` (the import path and the emit/score function name it uses) and mirror it exactly; the pseudo-import above is a placeholder for whatever `verify_sql.py` actually calls. Read `e2e/verify_sql.py` first and copy its scoring idiom.

- [ ] **Step 2: Write the composite transcript templates**

`fixtures/composite.jsonl.tmpl` (baseline/baseline2 — one session where a dispatched subagent marks a checkpoint, invokes the skill, calls the MCP tool):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tTask","name":"Task","input":{"subagent_type":"claude","description":"produce artifact"}}]}}
{"type":"assistant","uuid":"a2","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","isSidechain":true,"agentId":"agent1","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m2","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tMarkS","name":"mcp__catacomb__mark","input":{"name":"work","boundary":"start"}}]}}
{"type":"user","uuid":"u3","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:03Z","isSidechain":true,"agentId":"agent1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tMarkS","content":"","is_error":false}]}}
{"type":"assistant","uuid":"a3","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:04Z","isSidechain":true,"agentId":"agent1","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m3","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tSkill","name":"Skill","input":{"skill":"e2e-emit"}}]}}
{"type":"user","uuid":"u4","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:05Z","isSidechain":true,"agentId":"agent1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tSkill","content":"done","is_error":false}]}}
{"type":"assistant","uuid":"a4","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:06Z","isSidechain":true,"agentId":"agent1","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m4","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tRec","name":"mcp__e2ekit__record","input":{"value":"CATACOMB-OK"}}]}}
{"type":"user","uuid":"u5","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:07Z","isSidechain":true,"agentId":"agent1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tRec","content":"recorded CATACOMB-OK","is_error":false}]}}
{"type":"assistant","uuid":"a5","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:08Z","isSidechain":true,"agentId":"agent1","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m5","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tMarkE","name":"mcp__catacomb__mark","input":{"name":"work","boundary":"end"}}]}}
{"type":"user","uuid":"u6","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:09Z","isSidechain":true,"agentId":"agent1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tMarkE","content":"","is_error":false}]}}
{"type":"user","uuid":"u7","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:10Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tTask","content":"done","is_error":false}]}}
```

`fixtures/composite-degraded.jsonl.tmpl` (degraded — no subagent, no skill, no mark, no MCP; a single inline Bash step, and the emitter writes the WRONG artifact so `verifier.pass` also drops):

```json
{"type":"user","uuid":"u1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tB","name":"Bash","input":{"command":"true"}}]}}
{"type":"user","uuid":"u2","parent_tool_use_id":"tB","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tB","content":"","is_error":false}]}}
```

- [ ] **Step 3: Write the composite basket template `fixtures/composite.basket.yaml.tmpl`**

The emitter writes `out/result.csv` via `EMIT_CSV`, and a verify hook scores it. Baseline emits the correct token, degraded the wrong one:

```yaml
basket: prod-composite
reps: 5
tasks:
  - id: composite
    cmd: ["__PROD__/fixtures/emit.sh"]
    dir: __WORK__/cellwork
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "__PROD__/fixtures/verify_token.py"]
      timeout: 30s
variants:
  - id: baseline
    env: { SCENARIO_TMPL: "__PROD__/fixtures/composite.jsonl.tmpl", EMIT_CSV: "CATACOMB-OK" }
  - id: degraded
    env: { SCENARIO_TMPL: "__PROD__/fixtures/composite-degraded.jsonl.tmpl", EMIT_CSV: "WRONG" }
  - id: baseline2
    env: { SCENARIO_TMPL: "__PROD__/fixtures/composite.jsonl.tmpl", EMIT_CSV: "CATACOMB-OK" }
```

- [ ] **Step 4: Write the scenario `scenarios/40-composite.sh`**

```bash
#!/usr/bin/env bash
# Scenario 40 — composite production session: a dispatched subagent marks a phase,
# invokes the skill, calls the MCP tool, and produces a verifiable artifact, all in
# one fixture session. degraded strips every feature and emits the wrong artifact,
# so the gate fires on multiple axes at once (STEP: Task/Skill/mcp nodes dropped;
# PHASE: the subagent-scoped `work` marker dropped; ANNOTATION: verifier.pass
# 5/5 -> 0/5). A-vs-A stays clean. PYTHONPATH carries the verifier SDK.
set -euo pipefail
echo "== prod.40 composite: subagent+skill+mcp+verifier all gate; A-vs-A clean =="
w="$WORK/composite"; mkdir -p "$w/cellwork" "$w/runs"
export PYTHONPATH="$REPO/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/composite.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-composite basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
run_json 1 "$w/regress.json" "degraded strips features + wrong artifact -> gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-composite,variant=baseline \
  --candidate label:basket=prod-composite,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
step = any(x.get("scope") == "step" and x.get("verdict") == "regression" for x in f)
ann = any(x.get("metric") == "ann:verifier.pass" and x.get("verdict") == "regression" for x in f)
if not (step and ann):
    print("missing axis: step=%s ann=%s" % (step, ann), file=sys.stderr)
    for x in f: print("  ", {k: x.get(k) for k in ("scope","metric","verdict","detail")}, file=sys.stderr)
    sys.exit(1)
print("composite gate fires on STEP and ann:verifier.pass together")
PY
record "$rc" "composite gates on STEP (dropped nodes) AND ann:verifier.pass"
# PHASE axis: the subagent-scoped `work` marker exists in baseline, not in degraded
run_json 1 "$w/phase.json" "checkpoint presence: work phase dropped -> PHASE gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-composite,variant=baseline \
  --candidate label:basket=prod-composite,variant=degraded \
  --checkpoint work --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if any(f.get("scope")=="phase" for f in r.get("findings",[])) else 1)' "$w/phase.json" || rc=$?
record "$rc" "PHASE-scope finding present for the dropped subagent-scoped work marker"
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-composite,variant=baseline \
  --candidate label:basket=prod-composite,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["regressions"]==0 else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions"
```

> **Implementer:** verify the exact `--checkpoint` flag name and the phase-finding shape against `e2e/run.sh`'s presence-basket assertions and `catacomb regress --help`. The presence basket already gates on a `verify` checkpoint — copy that flag/JSON idiom exactly rather than the illustrative names above.

- [ ] **Step 5: Run the hermetic driver**

Run: `make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh`
Expected: scenario 40 passes — multi-axis gate + PHASE finding + A-vs-A clean.

- [ ] **Step 6: Commit**

```bash
git add e2e/hermetic/prod/scenarios/40-composite.sh e2e/hermetic/prod/fixtures/composite* e2e/hermetic/prod/fixtures/verify_token.py
git commit -m "test(e2e): hermetic composite production scenario (all axes)"
```

---

### Task 6: Live subagent basket

**Files:**

- Create: `e2e/basket-subagent.yaml`, `e2e/subagent.sh`
- Modify: `e2e/run.sh` (add bench + control + seeded-regression assertions)

**Interfaces:**

- Consumes: Task 0 findings (sidechain capture confirmed); the existing `verify_sql.py` + `sql-seed.sql` + `sql-golden.csv` machinery in `e2e/run.sh`.
- Produces: nothing downstream. Edits `e2e/run.sh` — serial with Tasks 7, 8.

- [ ] **Step 1: Write the live wrapper `e2e/subagent.sh`**

```bash
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
```

- [ ] **Step 2: Write the basket `e2e/basket-subagent.yaml`**

```yaml
# Live subagent basket: does delegating the SQL task to a subagent (Task tool)
# reduce to a subagent node + Task step node, and does the delegated work still
# verify? 1 task x 3 variants x 5 reps = 15 live cells. baseline/baseline2 delegate
# (subagent node present, verifier.pass ~5/5); degraded does it inline (no Task
# node — seeded STEP regression). Same anti-gaming layout as basket-sql.yaml: the
# workspace copies the wrapper + verifier in from E2E_DIR; SQL_DB and GOLDEN come
# from the driver env; the golden lives outside the cell workspace.
basket: e2e-subagent
reps: 5
tasks:
  - id: subagent
    cmd: ["./subagent.sh"]
    workspace:
      cmd: ["sh", "-c", "cp \"$E2E_DIR\"/subagent.sh \"$E2E_DIR\"/verify_sql.py ."]
    timeout: 180s
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_sql.py"]
      timeout: 30s
    env:
      CHILD_MODEL: claude-sonnet-5
variants:
  - id: baseline
    env: { SUBAGENT_INSTRUCTION: "Use a subagent (the Task tool) to run the query; have the subagent do the sqlite3 work." }
  - id: degraded
    env: { SUBAGENT_INSTRUCTION: "Do NOT use a subagent. Run the sqlite3 query yourself, directly." }
  - id: baseline2
    env: { SUBAGENT_INSTRUCTION: "Use a subagent (the Task tool) to run the query; have the subagent do the sqlite3 work." }
```

- [ ] **Step 3: Add the driver assertions to `e2e/run.sh`**

Locate the section that benches the SQL basket (`runs3`/`basket-sql.yaml`) and add, after it, a parallel block for the subagent basket. Add near the other `runs*` declarations: `runs4="$work/runs-subagent"; manifest4="$work/manifest-subagent.jsonl"; mkdir -p "$runs4"`. Then the bench+assert block (mirror the SQL basket's structure):

```bash
echo "== bench e2e-subagent basket (15 live cells) =="
( cd "$e2e_dir" && catacomb bench "$e2e_dir/basket-subagent.yaml" \
    --projects-dir "$work/projects-subagent" --runs-dir "$runs4" --manifest "$manifest4" ) \
  && rc=0 || rc=$?
record "$rc" "bench e2e-subagent basket"

run_json 0 "$artifacts/regress-subagent-AvA.json" "subagent A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$runs4" \
  --baseline label:basket=e2e-subagent,variant=baseline \
  --candidate label:basket=e2e-subagent,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["regressions"]==0 else 1)' "$artifacts/regress-subagent-AvA.json" || rc=$?
record "$rc" "subagent A-vs-A reports zero regressions"

run_json 1 "$artifacts/regress-subagent-degraded.json" "subagent degraded (no Task) MUST gate" -- \
  catacomb regress --runs-dir "$runs4" \
  --baseline label:basket=e2e-subagent,variant=baseline \
  --candidate label:basket=e2e-subagent,variant=degraded --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if any(f.get("scope")=="step" and f.get("verdict")=="regression" for f in r.get("findings",[])) else 1)' "$artifacts/regress-subagent-degraded.json" || rc=$?
record "$rc" "subagent degraded attributed to a STEP-scope (dropped Task) regression"
```

> **Implementer:** copy the exact bench-invocation idiom (the `cd "$e2e_dir"`, projects-dir handling, `record`/`run_json` helpers) from the SQL basket block already in `e2e/run.sh`; the block above is shaped to it but must match the file's live conventions (variable names, the `SQL_DB`/`GOLDEN`/`E2E_DIR` exports already set up earlier in the driver).

- [ ] **Step 4: Local dry check (no API) — YAML + wrapper shape**

Run: `catacomb bench --help >/dev/null && python3 -c "import yaml,sys; yaml.safe_load(open('e2e/basket-subagent.yaml'))"` (or the repo's yaml validation) and `bash -n e2e/subagent.sh e2e/run.sh`
Expected: PASS — the basket parses, the scripts have no syntax error. (Full live validation happens in Task 9's dispatch run.)

- [ ] **Step 5: Commit**

```bash
git add e2e/basket-subagent.yaml e2e/subagent.sh e2e/run.sh
git commit -m "test(e2e): live subagent basket (Task delegation, presence + verifier)"
```

---

### Task 7: Live skill basket

**Files:**

- Create: `e2e/basket-skill.yaml`, `e2e/skill.sh`, `e2e/verify_emit.py`
- Modify: `e2e/run.sh`
- Uses: `e2e/skills/e2e-emit/SKILL.md` (Task 4)

**Interfaces:**

- Consumes: Task 0 skill-source-set finding; the `e2e-emit` skill (Task 4).
- Produces: nothing downstream. Edits `e2e/run.sh` — serial after Task 6.

- [ ] **Step 1: Write the artifact verifier `e2e/verify_emit.py`**

```python
#!/usr/bin/env python3
"""Live skill-basket verifier: verifier.pass iff out/result.csv == CATACOMB-SKILL-OK
(the token the e2e-emit skill writes). Mirrors verify_sql.py's SDK scoring idiom."""
import os, sys
from catacomb_verifier import emit  # match verify_sql.py's actual import/entrypoint

WANT = "CATACOMB-SKILL-OK"


def main() -> int:
    try:
        got = open(os.path.join("out", "result.csv")).read().strip()
    except OSError:
        got = ""
    emit("verifier.pass", 1 if got == WANT else 0)
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

> **Implementer:** read `e2e/verify_sql.py` and copy its exact import path and scoring call; the `catacomb_verifier` import above is illustrative.

- [ ] **Step 2: Write the live wrapper `e2e/skill.sh`**

```bash
#!/usr/bin/env bash
# Live skill basket cell wrapper. The workspace.cmd stages the e2e-emit skill into
# the cell's .claude/skills/ so --setting-sources project discovers it. baseline
# invokes the skill (Skill tool -> NodeSkill; the skill writes the token artifact);
# degraded writes the token with an inline Write (no Skill node — seeded STEP
# regression). Sonnet for reliable obedience. --setting-sources value is whatever
# Task 0 Step 4 confirmed loads project skills in `claude -p`.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "${SKILL_INSTRUCTION}" \
  --model "${CHILD_MODEL:-claude-sonnet-5}" \
  --output-format stream-json --verbose \
  --setting-sources project --strict-mcp-config \
  --allowedTools "Skill,Write"
```

- [ ] **Step 3: Write the basket `e2e/basket-skill.yaml`**

```yaml
# Live skill basket: does invoking a real project-scoped skill reduce to a Skill
# node, and does the skill produce the verifiable artifact? 1 task x 3 variants x 5
# reps = 15 live cells. The workspace stages the e2e-emit skill dir into the cell's
# .claude/skills/; baseline/baseline2 invoke it (Skill node present, verifier.pass
# ~5/5); degraded writes the token inline (no Skill node — seeded STEP regression).
basket: e2e-skill
reps: 5
tasks:
  - id: skill
    cmd: ["./skill.sh"]
    workspace:
      cmd: ["sh", "-c", "mkdir -p .claude/skills && cp -R \"$E2E_DIR\"/skills/e2e-emit .claude/skills/ && cp \"$E2E_DIR\"/skill.sh \"$E2E_DIR\"/verify_emit.py ."]
    timeout: 180s
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_emit.py"]
      timeout: 30s
    env:
      CHILD_MODEL: claude-sonnet-5
variants:
  - id: baseline
    env: { SKILL_INSTRUCTION: "Use the e2e-emit skill to produce the output file out/result.csv. Invoke the skill; do not write the file yourself." }
  - id: degraded
    env: { SKILL_INSTRUCTION: "Do NOT use any skill. Write exactly the text CATACOMB-SKILL-OK to out/result.csv yourself using the Write tool." }
  - id: baseline2
    env: { SKILL_INSTRUCTION: "Use the e2e-emit skill to produce the output file out/result.csv. Invoke the skill; do not write the file yourself." }
```

- [ ] **Step 4: Add the driver assertions to `e2e/run.sh`**

Add `runs5="$work/runs-skill"; manifest5="$work/manifest-skill.jsonl"; mkdir -p "$runs5"` near the other `runs*`, then:

```bash
echo "== bench e2e-skill basket (15 live cells) =="
( cd "$e2e_dir" && catacomb bench "$e2e_dir/basket-skill.yaml" \
    --projects-dir "$work/projects-skill" --runs-dir "$runs5" --manifest "$manifest5" ) \
  && rc=0 || rc=$?
record "$rc" "bench e2e-skill basket"

run_json 0 "$artifacts/regress-skill-AvA.json" "skill A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$runs5" \
  --baseline label:basket=e2e-skill,variant=baseline \
  --candidate label:basket=e2e-skill,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["regressions"]==0 else 1)' "$artifacts/regress-skill-AvA.json" || rc=$?
record "$rc" "skill A-vs-A reports zero regressions"

run_json 1 "$artifacts/regress-skill-degraded.json" "skill degraded (no Skill) MUST gate" -- \
  catacomb regress --runs-dir "$runs5" \
  --baseline label:basket=e2e-skill,variant=baseline \
  --candidate label:basket=e2e-skill,variant=degraded --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if any(f.get("scope")=="step" and f.get("verdict")=="regression" for f in r.get("findings",[])) else 1)' "$artifacts/regress-skill-degraded.json" || rc=$?
record "$rc" "skill degraded attributed to a STEP-scope (dropped Skill) regression"
```

- [ ] **Step 5: Local dry check**

Run: `bash -n e2e/skill.sh e2e/run.sh && python3 -c "import yaml; yaml.safe_load(open('e2e/basket-skill.yaml'))"`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add e2e/basket-skill.yaml e2e/skill.sh e2e/verify_emit.py e2e/run.sh
git commit -m "test(e2e): live skill basket (real e2e-emit skill, presence + verifier)"
```

---

### Task 8: Live MCP basket

**Files:**

- Create: `e2e/basket-mcp.yaml`, `e2e/mcp-record.sh`
- Modify: `e2e/run.sh`
- Uses: `e2e/mcp-e2ekit/` (Task 1)

**Interfaces:**

- Consumes: the real e2e MCP server (Task 1).
- Produces: nothing downstream. Edits `e2e/run.sh` — serial after Task 7.

- [ ] **Step 1: Write the live wrapper `e2e/mcp-record.sh`**

```bash
#!/usr/bin/env bash
# Live MCP basket cell wrapper: real handshake between `claude -p` and the e2e
# MCP server (e2e/mcp-e2ekit/server.py) over stdio. The workspace renders the
# absolute server path into a per-cell mcp.json; baseline calls the record tool
# (mcp__e2ekit__record -> a general MCP step node; the server persists the value to
# out/result.csv, which the verifier scores); degraded uses no tool (no MCP node —
# seeded STEP regression). --strict-mcp-config loads ONLY the e2ekit server.
set -euo pipefail
mkdir -p out
rm -f out/result.csv out/mcp-record.txt
export E2EKIT_OUT="$PWD/out/result.csv"
exec claude -p "${MCP_INSTRUCTION}" \
  --model "${CHILD_MODEL:-claude-sonnet-5}" \
  --output-format stream-json --verbose \
  --mcp-config "$PWD/mcp.json" --strict-mcp-config \
  --setting-sources project \
  --allowedTools "mcp__e2ekit__record"
```

- [ ] **Step 2: Write the basket `e2e/basket-mcp.yaml`**

```yaml
# Live MCP basket: a real stdio MCP server (e2e/mcp-e2ekit/server.py) handshakes
# with `claude -p`. 1 task x 3 variants x 5 reps = 15 live cells. The workspace
# stages the server and renders its absolute path into a per-cell mcp.json;
# baseline/baseline2 call the record tool (mcp__e2ekit__record node present; the
# server writes out/result.csv, verifier.pass ~5/5); degraded uses no tool (no MCP
# node — seeded STEP regression). E2EKIT_OUT points the server at the cell's
# out/result.csv so the artifact is verifiable.
basket: e2e-mcp
reps: 5
tasks:
  - id: mcp
    cmd: ["./mcp-record.sh"]
    workspace:
      cmd: ["sh", "-c", "cp -R \"$E2E_DIR\"/mcp-e2ekit . && cp \"$E2E_DIR\"/mcp-record.sh \"$E2E_DIR\"/verify_emit.py . && sed \"s|__E2EKIT__|$PWD/mcp-e2ekit|g\" mcp-e2ekit/mcp.json > mcp.json"]
    timeout: 180s
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_emit.py"]
      timeout: 30s
    env:
      CHILD_MODEL: claude-sonnet-5
variants:
  - id: baseline
    env: { MCP_INSTRUCTION: "Call the mcp__e2ekit__record tool with value set to exactly CATACOMB-SKILL-OK. Use only that tool, then reply done." }
  - id: degraded
    env: { MCP_INSTRUCTION: "Do not use any tools. Just reply done." }
  - id: baseline2
    env: { MCP_INSTRUCTION: "Call the mcp__e2ekit__record tool with value set to exactly CATACOMB-SKILL-OK. Use only that tool, then reply done." }
```

> **Note:** `verify_emit.py` (Task 7) checks `out/result.csv == CATACOMB-SKILL-OK`; the MCP server writes exactly that token via `E2EKIT_OUT`, so the same verifier scores this basket. The workspace copies `verify_emit.py` in from `E2E_DIR`.

- [ ] **Step 3: Add the driver assertions to `e2e/run.sh`**

Add `runs6="$work/runs-mcp"; manifest6="$work/manifest-mcp.jsonl"; mkdir -p "$runs6"`, then:

```bash
echo "== bench e2e-mcp basket (15 live cells) =="
( cd "$e2e_dir" && catacomb bench "$e2e_dir/basket-mcp.yaml" \
    --projects-dir "$work/projects-mcp" --runs-dir "$runs6" --manifest "$manifest6" ) \
  && rc=0 || rc=$?
record "$rc" "bench e2e-mcp basket"

run_json 0 "$artifacts/regress-mcp-AvA.json" "mcp A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$runs6" \
  --baseline label:basket=e2e-mcp,variant=baseline \
  --candidate label:basket=e2e-mcp,variant=baseline2 --metric-rel-delta 0.5 --json
rc=0; python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["regressions"]==0 else 1)' "$artifacts/regress-mcp-AvA.json" || rc=$?
record "$rc" "mcp A-vs-A reports zero regressions"

run_json 1 "$artifacts/regress-mcp-degraded.json" "mcp degraded (no tool) MUST gate" -- \
  catacomb regress --runs-dir "$runs6" \
  --baseline label:basket=e2e-mcp,variant=baseline \
  --candidate label:basket=e2e-mcp,variant=degraded --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if any(f.get("scope")=="step" and f.get("verdict")=="regression" for f in r.get("findings",[])) else 1)' "$artifacts/regress-mcp-degraded.json" || rc=$?
record "$rc" "mcp degraded attributed to a STEP-scope (dropped mcp node) regression"
```

- [ ] **Step 4: Local dry check + server smoke**

Run: `python3 e2e/mcp-e2ekit/smoke.py && bash -n e2e/mcp-record.sh e2e/run.sh && python3 -c "import yaml; yaml.safe_load(open('e2e/basket-mcp.yaml'))"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add e2e/basket-mcp.yaml e2e/mcp-record.sh e2e/run.sh
git commit -m "test(e2e): live MCP basket (real e2ekit server, presence + verifier)"
```

---

### Task 9: Workflow + docs + full-suite validation

**Files:**

- Modify: `.github/workflows/e2e-live.yml` (timeout + cost header)
- Modify: `e2e/run.sh` (cost header comment)
- Modify: `AGENTS.md` (E2E table rows)

**Interfaces:**

- Consumes: everything. This task raises the CI budget/time headroom and documents the new baskets, then validates the live suite via a dispatch run.

- [ ] **Step 1: Bump the live workflow timeout + cost note**

In `.github/workflows/e2e-live.yml`, change `timeout-minutes: 45` to `timeout-minutes: 75`, and update the header cost comment from `~$1.7 per run — 60 live bench cells` to `~$5–7 per run — 105 live bench cells (adds subagent/skill/mcp production baskets)`.

- [ ] **Step 2: Update `e2e/run.sh` header cost figure**

Change the `Cost: ~$1.7 ... (60 bench cells; ...)` line to `Cost: ~$5–7 of real API spend (105 bench cells: presence/continuous/sql + subagent/skill/mcp production baskets on sonnet).` and extend the header overview to list the three new baskets and their seeded regressions (mirror the existing bullet style).

- [ ] **Step 3: Update `AGENTS.md` E2E rows**

In the CI/linters table, extend the E2E rows to mention the production baskets. Replace the `E2E live gate` row detail with: `real \`claude -p\` baskets — presence/continuous/sql + subagent/skill/mcp production scenarios; needs ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN`. Add an`E2E hermetic` row if not present: `\`.github/workflows/e2e-hermetic.yml\` — fixture-transcript pipeline incl. subagent/skill/real-MCP production scenarios (per PR, $0)`.

- [ ] **Step 4: Full hermetic run (must be green, no API)**

Run: `make build && CATACOMB_BIN=$PWD/bin/catacomb e2e/hermetic/run.sh`
Expected: PASS through step 22, all prod scenarios green.

- [ ] **Step 5: Live dispatch validation (needs auth)**

Trigger `e2e-live.yml` via `workflow_dispatch` on the branch (or run `bash e2e/run.sh` locally with auth exported). Confirm: all three new baskets bench, every A-vs-A control exits 0, every seeded regression exits 1, within the new timeout and ~$5–7 budget. Record the observed cost in the PR description.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/e2e-live.yml e2e/run.sh AGENTS.md
git commit -m "ci(e2e): raise live budget/timeout + document production baskets"
```

---

## Self-Review

**1. Spec coverage:**

- Both lanes → hermetic Tasks 2–5, live Tasks 6–8, workflow Task 9. ✓
- Both signals (presence + verifier) → presence in every scenario; verifier in composite (Task 5) and all three live baskets (6–8). ✓
- Real MCP server (not mock) → Task 1 (protocol-conformant server + smoke), used live in Task 8 and hermetic in Task 2. ✓
- Real minimal skill (real protocol) → Task 4 (`SKILL.md` with proper frontmatter), used live in Task 7. ✓
- Composite scenario → Task 5. ✓
- A-vs-A + seeded regression per case → every scenario/basket asserts both. ✓
- Zero product-Go target + risk validation first → Task 0 spike; product gap escalates to its own task. ✓
- Budget ~$5–7 → Task 9 raises timeout/cost; 105 cells. ✓

**2. Placeholder scan:** The three `catacomb_verifier` imports and the `--checkpoint`/`replay --json` flag names are explicitly flagged as *verify-against-the-real-surface* items with the exact source file to copy from (`e2e/verify_sql.py`, `e2e/run.sh`, `catacomb regress --help`). They are not silent TBDs — each carries a concrete resolution instruction. The `__E2EKIT__`/`__PROD__`/`__WORK__` tokens are real sed-render tokens matching the existing `__WORK__` pattern, not placeholders.

**3. Type consistency:** Basket names (`prod-mcp`/`prod-subagent`/`prod-skill`/`prod-composite`, `e2e-subagent`/`e2e-skill`/`e2e-mcp`) are used consistently across each scenario's bench, regress selectors, and run dirs. The `record`/`run_json`/`pass`/`failrec` helper names match `e2e/hermetic/run.sh`. The MCP tool is `record` everywhere; the skill is `e2e-emit` everywhere; the verifier token is `CATACOMB-OK` (composite) / `CATACOMB-SKILL-OK` (skill+mcp) — distinct and used consistently within each.

**Open dependency for the implementer:** Tasks 3/4/5's structural `replay --json` grep and Task 5's `--checkpoint` assertion depend on Task 0 confirming the CLI surface. Each carries an inline adaptation note. The STEP/ANNOTATION gate assertions do not depend on the spike and are the primary signals.
