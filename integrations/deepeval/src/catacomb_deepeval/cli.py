from __future__ import annotations

import json
import os
import sys

from catacomb_deepeval.adapter import session_to_dicts, session_to_test_case
from catacomb_deepeval.expected import ExpectedLoadError, expected_carries_field, load_expected_names
from catacomb_deepeval.reader import list_run_ids, load_jsonl, parse_session

DEFAULT_THRESHOLD = 0.5


def main() -> None:
    import argparse

    parser = argparse.ArgumentParser(
        prog="catacomb-deepeval",
        description="Run DeepEval ToolCorrectnessMetric on a catacomb JSONL export.",
    )
    parser.add_argument("jsonl", help="Path to catacomb JSONL export file")
    parser.add_argument("--run", dest="run_id", help="Run ID to evaluate (required when file has multiple runs)")
    parser.add_argument("--expected", dest="expected", help="Path to expected-tools JSON file")
    parser.add_argument(
        "--match",
        choices=["name", "input", "output"],
        default="name",
        help="ToolCorrectness matching mode (default: name)",
    )
    parser.add_argument(
        "--argument-correctness",
        action="store_true",
        help="Reserved (not implemented); always exits with an error",
    )
    parser.add_argument(
        "--trace-metrics",
        action="store_true",
        help="Run StepEfficiencyMetric and TaskCompletionMetric via @observe replay; requires ANTHROPIC_API_KEY",
    )
    args = parser.parse_args()

    if args.argument_correctness:
        print("error: --argument-correctness is not implemented", file=sys.stderr)
        if not os.environ.get("ANTHROPIC_API_KEY"):
            print(
                "note: it would also require the ANTHROPIC_API_KEY environment variable "
                "(LLM judge makes network calls)",
                file=sys.stderr,
            )
        sys.exit(2)

    if args.trace_metrics and not os.environ.get("ANTHROPIC_API_KEY"):
        print(
            "error: --trace-metrics requires ANTHROPIC_API_KEY environment variable (LLM judge makes network calls)",
            file=sys.stderr,
        )
        sys.exit(2)

    try:
        lines = load_jsonl(args.jsonl)
    except (OSError, json.JSONDecodeError) as exc:
        print(f"error: cannot read {args.jsonl}: {exc}", file=sys.stderr)
        sys.exit(2)

    run_ids = list_run_ids(lines)

    if not run_ids:
        print(f"error: no runs found in {args.jsonl}", file=sys.stderr)
        sys.exit(2)

    run_id = args.run_id

    if run_id is not None and run_id not in run_ids:
        print(
            f"error: run {run_id} not found in {args.jsonl} (available: {', '.join(run_ids)})",
            file=sys.stderr,
        )
        sys.exit(2)

    if run_id is None:
        if len(run_ids) > 1:
            print(
                f"error: multiple runs found ({', '.join(run_ids)}); specify one with --run",
                file=sys.stderr,
            )
            sys.exit(2)
        run_id = run_ids[0]

    session = parse_session(lines, run_id)

    if args.trace_metrics:
        try:
            from catacomb_deepeval.trace_replay import (
                make_anthropic_judge,
                run_step_efficiency,
                run_task_completion,
            )
        except ImportError:
            print(
                "error: deepeval is not installed; run: pip install 'catacomb-deepeval[deepeval]'",
                file=sys.stderr,
            )
            sys.exit(2)

        try:
            judge = make_anthropic_judge()
            all_passed = True

            tc_score, tc_reason = run_task_completion(session, model=judge, threshold=DEFAULT_THRESHOLD)
            print(f"task_completion score: {tc_score:.3f}")
            print(f"task_completion reason: {tc_reason}")
            if tc_score < DEFAULT_THRESHOLD:
                all_passed = False

            se_score, se_reason = run_step_efficiency(session, model=judge, threshold=DEFAULT_THRESHOLD)
            print(f"step_efficiency score: {se_score:.3f}")
            print(f"step_efficiency reason: {se_reason}")
            if se_score < DEFAULT_THRESHOLD:
                all_passed = False
        except Exception as exc:
            print(f"error: judge failed: {exc}", file=sys.stderr)
            sys.exit(2)

        sys.exit(0 if all_passed else 1)

    if args.expected is None:
        result = session_to_dicts(session)
        print(json.dumps(result, indent=2))
        sys.exit(0)

    try:
        expected_names = load_expected_names(args.expected)
    except (OSError, json.JSONDecodeError, ExpectedLoadError) as exc:
        print(f"error: cannot load expected tools from {args.expected}: {exc}", file=sys.stderr)
        sys.exit(2)

    if args.match != "name":
        field = "input_parameters" if args.match == "input" else "output"
        try:
            carries_field = expected_carries_field(args.expected, field)
        except (OSError, json.JSONDecodeError) as exc:
            print(f"error: cannot load expected tools from {args.expected}: {exc}", file=sys.stderr)
            sys.exit(2)
        if carries_field:
            print(
                f"error: --match {args.match} is not implemented: "
                f"expected {field} are not carried through to the metric",
                file=sys.stderr,
            )
        else:
            print(
                f"error: --match {args.match} requires expected {field}, "
                f"but {args.expected} carries tool names only",
                file=sys.stderr,
            )
        sys.exit(2)

    try:
        from catacomb_deepeval.adapter import make_offline_metric

        metric = make_offline_metric()
        tc = session_to_test_case(session, expected_names=expected_names)
    except ImportError:
        print(
            "error: deepeval is not installed; run: pip install 'catacomb-deepeval[deepeval]'",
            file=sys.stderr,
        )
        sys.exit(2)

    metric.measure(tc)

    verdict = "PASS" if metric.score >= metric.threshold else "FAIL"
    print(f"score: {metric.score:.3f}")
    print(f"reason: {metric.reason}")
    print(verdict)

    sys.exit(0 if verdict == "PASS" else 1)
