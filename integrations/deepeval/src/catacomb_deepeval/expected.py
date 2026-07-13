from __future__ import annotations

import json
from typing import Any, List


class ExpectedLoadError(Exception):
    """Raised when the expected-tools JSON has an unrecognized shape."""


def load_expected_names(path: str) -> List[str]:
    """Load expected tool names from a JSON file.

    Supported forms:
    - Name array: ["Bash", "mcp__fs__read"]
    - Object array: [{"name": "Bash"}, ...]
    - Envelope (name array): {"tools": ["Bash", ...]}
    - Envelope (object array): {"tools": [{"name": "Bash"}, ...]}
    """
    with open(path, encoding="utf-8") as fh:
        data: Any = json.load(fh)

    if isinstance(data, dict):
        tools = data.get("tools")
        if tools is None:
            raise ExpectedLoadError(
                f"Expected JSON object with 'tools' key, got keys: {list(data.keys())}"
            )
        return _parse_list(tools)

    if isinstance(data, list):
        return _parse_list(data)

    raise ExpectedLoadError(
        f"Expected a JSON list or object, got {type(data).__name__}"
    )


def expected_carries_field(path: str, field: str) -> bool:
    """Report whether every expected-tools entry is an object carrying *field*.

    Returns False for name arrays, object arrays missing *field* on any entry,
    and empty lists — i.e. whenever the file is effectively names-only for the
    purposes of matching on *field*.
    """
    with open(path, encoding="utf-8") as fh:
        data: Any = json.load(fh)

    items = data.get("tools") if isinstance(data, dict) else data
    if not isinstance(items, list) or not items:
        return False
    return all(isinstance(item, dict) and field in item for item in items)


def _parse_list(items: Any) -> List[str]:
    """Parse a list of names or objects into a list of tool name strings."""
    if not isinstance(items, list):
        raise ExpectedLoadError(f"Expected a list, got {type(items).__name__}")

    result: List[str] = []
    for item in items:
        if isinstance(item, str):
            result.append(item)
        elif isinstance(item, dict):
            if "name" not in item:
                raise ExpectedLoadError(
                    f"Expected object with 'name' key, got keys: {list(item.keys())}"
                )
            result.append(item["name"])
        else:
            raise ExpectedLoadError(
                f"Expected string or object, got {type(item).__name__}"
            )
    return result
