from __future__ import annotations

from typing import Any, Dict, List, Optional

from catacomb_deepeval.model import SessionData, StepData


def _steps_from_tools(session: SessionData) -> List[StepData]:
    """Fallback: derive steps from tools_called when steps list is empty."""
    result: List[StepData] = []
    for t in session.tools_called:
        inp: Optional[str] = None
        if t.input_parameters is not None:
            import json
            try:
                inp = json.dumps(t.input_parameters)
            except (TypeError, ValueError):
                inp = str(t.input_parameters)
        out: Optional[str] = None
        if t.output is not None:
            out = str(t.output)
        result.append(StepData(kind="tool", name=t.name, input=inp, output=out))
    return result


def build_trace_dict(session: SessionData) -> Dict[str, Any]:
    """Replay a catacomb session as a DeepEval @observe trace and return its dict.

    The trace has an agent root span with ordered llm/tool child spans.
    No LLM calls are made; this is a structural replay only.
    """
    from deepeval.tracing import (
        current_trace_context,
        observe,
        trace_manager,
        update_current_span,
    )

    trace_manager.configure(tracing_enabled=False)

    steps = session.steps if session.steps else _steps_from_tools(session)

    captured: Dict[str, Any] = {}

    @observe(type="agent")
    def _replay() -> None:
        update_current_span(
            name="catacomb-session",
            input=session.input or "",
            output=session.actual_output or "",
        )

        for step in steps:
            if step.kind == "llm":
                _emit_llm(step)
            else:
                _emit_tool(step)

        root = current_trace_context.get().root_spans[0]
        captured["root"] = root

    @observe(type="llm")
    def _emit_llm(step: StepData) -> None:
        update_current_span(
            name=step.name or "assistant_turn",
            input=step.input or "",
            output=step.output or "",
        )

    @observe(type="tool")
    def _emit_tool(step: StepData) -> None:
        update_current_span(
            name=step.name or "tool",
            input=step.input or "",
            output=step.output or "",
        )

    _replay()

    root = captured["root"]
    return trace_manager.create_nested_spans_dict(root)


def make_anthropic_judge() -> Any:
    """Build a real AnthropicModel judge. Requires ANTHROPIC_API_KEY at runtime."""
    from deepeval.models import AnthropicModel

    return AnthropicModel()


def run_task_completion(
    session: SessionData,
    model: Any,
    threshold: float = 0.5,
) -> tuple:
    """Measure TaskCompletionMetric on the session trace.

    Returns (score, reason). Uses the provided model as the LLM judge.
    """
    from deepeval.metrics import TaskCompletionMetric

    tc = session_to_trace_testcase(session)
    metric = TaskCompletionMetric(
        threshold=threshold,
        model=model,
        include_reason=True,
        async_mode=False,
    )
    metric.measure(tc)
    return (metric.score, metric.reason or "")


def run_step_efficiency(
    session: SessionData,
    model: Any,
    threshold: float = 0.5,
) -> tuple:
    """Measure StepEfficiencyMetric on the session trace.

    Returns (score, reason). Uses the provided model as the LLM judge.
    """
    from deepeval.metrics import StepEfficiencyMetric

    tc = session_to_trace_testcase(session)
    metric = StepEfficiencyMetric(
        threshold=threshold,
        model=model,
        include_reason=True,
        async_mode=False,
    )
    metric.measure(tc)
    return (metric.score, metric.reason or "")


def session_to_trace_testcase(session: SessionData) -> Any:
    """Build a DeepEval LLMTestCase with _trace_dict set from the session replay."""
    from deepeval.test_case import LLMTestCase

    trace_dict = build_trace_dict(session)
    tc = LLMTestCase(
        input=session.input or "",
        actual_output=session.actual_output or "",
    )
    tc._trace_dict = trace_dict
    return tc
