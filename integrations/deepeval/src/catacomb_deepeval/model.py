from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, List, Literal, Optional


@dataclass
class ToolCallData:
    """Represents a single tool or MCP call extracted from a catacomb session."""

    name: str
    input_parameters: Optional[dict]
    output: Optional[Any]


@dataclass
class StepData:
    """Represents a single ordered step (LLM turn or tool call) in a catacomb session."""

    kind: Literal["llm", "tool"]
    name: str
    input: Optional[str]
    output: Optional[str]


@dataclass
class SessionData:
    """All eval-relevant data extracted from one catacomb run."""

    run_id: str
    input: str
    actual_output: str
    tools_called: List[ToolCallData] = field(default_factory=list)
    steps: List[StepData] = field(default_factory=list)
