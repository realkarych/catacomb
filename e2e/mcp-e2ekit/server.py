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
