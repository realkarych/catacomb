from __future__ import annotations

import pathlib

import pytest

deepeval = pytest.importorskip("deepeval")

from deepeval.metrics.step_efficiency.schema import EfficiencyVerdict, Task
from deepeval.metrics.task_completion.schema import TaskAndOutcome, TaskCompletionVerdict
from deepeval.models import DeepEvalBaseLLM

from catacomb_deepeval.reader import load_jsonl, parse_session
from catacomb_deepeval.trace_replay import run_step_efficiency, run_task_completion

TESTDATA = pathlib.Path(__file__).parent / "testdata"


class _StubJudge(DeepEvalBaseLLM):
    """Deterministic stub — returns canned schema objects without any network calls."""

    def load_model(self) -> "_StubJudge":
        return self

    def generate(self, prompt: str, schema: object = None) -> object:
        if schema is Task:
            return Task(task="list files in current directory")
        if schema is EfficiencyVerdict:
            return EfficiencyVerdict(score=0.9, reason="only one tool call needed")
        if schema is TaskAndOutcome:
            return TaskAndOutcome(
                task="list files in current directory",
                outcome="task completed successfully",
            )
        if schema is TaskCompletionVerdict:
            return TaskCompletionVerdict(verdict=1.0, reason="task was completed")
        return "{}"

    async def a_generate(self, prompt: str, schema: object = None) -> object:
        return self.generate(prompt, schema=schema)

    def get_model_name(self) -> str:
        return "stub-judge"


def _load_session():
    lines = load_jsonl(str(TESTDATA / "session.jsonl"))
    return parse_session(lines, "run-001")


def test_run_task_completion_score():
    sd = _load_session()
    score, reason = run_task_completion(sd, model=_StubJudge(), threshold=0.5)
    assert score >= 1.0 - 1e-9


def test_run_task_completion_reason_nonempty():
    sd = _load_session()
    _, reason = run_task_completion(sd, model=_StubJudge(), threshold=0.5)
    assert reason and len(reason) > 0


def test_run_step_efficiency_score_in_range():
    sd = _load_session()
    score, reason = run_step_efficiency(sd, model=_StubJudge(), threshold=0.5)
    assert 0.0 <= score <= 1.0


def test_run_step_efficiency_reason_nonempty():
    sd = _load_session()
    _, reason = run_step_efficiency(sd, model=_StubJudge(), threshold=0.5)
    assert reason and len(reason) > 0


def test_stub_judge_deterministic():
    sd = _load_session()
    score1, reason1 = run_task_completion(sd, model=_StubJudge(), threshold=0.5)
    score2, reason2 = run_task_completion(sd, model=_StubJudge(), threshold=0.5)
    assert score1 == score2
    assert reason1 == reason2
