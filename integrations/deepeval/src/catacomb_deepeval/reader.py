from __future__ import annotations

import json
from typing import Any, Dict, List, Optional, Tuple

from catacomb_deepeval.model import SessionData, StepData, ToolCallData


def load_jsonl(path: str) -> List[dict]:
    lines: List[dict] = []
    with open(path, encoding="utf-8") as fh:
        for raw in fh:
            raw = raw.strip()
            if raw:
                lines.append(json.loads(raw))
    return lines


def list_run_ids(lines: List[dict]) -> List[str]:
    found: set = set()
    for line in lines:
        rid = line.get("run_id") or line.get("id")
        if line.get("kind") in ("node", "run") and rid and rid not in found:
            found.add(rid)
    return sorted(found)


def parse_session(lines: List[dict], run_id: str) -> SessionData:
    nodes: List[dict] = []

    for line in lines:
        if line.get("kind") == "node" and line.get("run_id") == run_id:
            nodes.append(line)

    prompt_input = _extract_prompt_input(nodes)
    actual_output = _extract_actual_output(nodes)
    tools = _extract_tools(nodes)
    steps = _extract_steps(nodes)

    return SessionData(
        run_id=run_id,
        input=prompt_input,
        actual_output=actual_output,
        tools_called=tools,
        steps=steps,
    )


def _sort_key(node: dict) -> Tuple[str, str]:
    t = node.get("t_start") or ""
    return (t, node.get("id", ""))


def _text_of(raw: Optional[Any]) -> str:
    if raw is None:
        return ""
    if isinstance(raw, str):
        try:
            decoded = json.loads(raw)
            if isinstance(decoded, str):
                return decoded
            return raw
        except (json.JSONDecodeError, ValueError):
            return raw
    return str(raw)


def _payload_input(node: dict) -> Optional[Any]:
    p = node.get("payload")
    if p is None:
        return None
    return p.get("input")


def _payload_output(node: dict) -> Optional[Any]:
    p = node.get("payload")
    if p is None:
        return None
    return p.get("output")


def _extract_prompt_input(nodes: List[dict]) -> str:
    prompts = [n for n in nodes if n.get("type") == "user_prompt"]
    if not prompts:
        return ""
    prompts.sort(key=_sort_key)
    return _text_of(_payload_input(prompts[0]))


def _extract_actual_output(nodes: List[dict]) -> str:
    turns = [n for n in nodes if n.get("type") == "assistant_turn"]
    if not turns:
        return ""
    turns.sort(key=_sort_key)
    return _text_of(_payload_output(turns[-1]))


def _extract_tools(nodes: List[dict]) -> List[ToolCallData]:
    tool_nodes = [
        n for n in nodes if n.get("type") in ("tool_call", "mcp_call")
    ]
    tool_nodes.sort(key=_sort_key)

    result: List[ToolCallData] = []
    for tn in tool_nodes:
        raw_in = _payload_input(tn)
        raw_out = _payload_output(tn)

        input_params: Optional[dict] = None
        if raw_in is not None:
            try:
                parsed = json.loads(raw_in) if isinstance(raw_in, str) else raw_in
                if isinstance(parsed, dict):
                    input_params = parsed
            except (json.JSONDecodeError, ValueError):
                pass

        output: Optional[Any] = None
        if raw_out is not None:
            try:
                output = json.loads(raw_out) if isinstance(raw_out, str) else raw_out
            except (json.JSONDecodeError, ValueError):
                output = raw_out

        result.append(ToolCallData(
            name=tn.get("name", ""),
            input_parameters=input_params,
            output=output,
        ))

    return result


def _extract_steps(nodes: List[dict]) -> List[StepData]:
    step_nodes = [
        n for n in nodes
        if n.get("type") in ("assistant_turn", "tool_call", "mcp_call")
    ]
    step_nodes.sort(key=_sort_key)

    result: List[StepData] = []
    for sn in step_nodes:
        ntype = sn.get("type")
        raw_in = _payload_input(sn)
        raw_out = _payload_output(sn)

        inp: Optional[str] = None
        if raw_in is not None:
            inp = _text_of(raw_in)

        out: Optional[str] = None
        if raw_out is not None:
            out = _text_of(raw_out)

        if ntype == "assistant_turn":
            result.append(StepData(
                kind="llm",
                name=sn.get("name", "assistant_turn"),
                input=inp,
                output=out,
            ))
        else:
            result.append(StepData(
                kind="tool",
                name=sn.get("name", ""),
                input=inp,
                output=out,
            ))

    return result
