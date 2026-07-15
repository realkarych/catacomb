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
