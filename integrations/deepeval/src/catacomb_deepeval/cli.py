from __future__ import annotations

import json
import os
import sys
from typing import List, Optional

from catacomb_deepeval.adapter import session_to_dicts, session_to_test_case
from catacomb_deepeval.expected import ExpectedLoadError, load_expected_names
from catacomb_deepeval.reader import list_run_ids, load_jsonl, parse_session


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
    parser.add_argument("--json", dest="output_json", action="store_true", help="Output raw JSON (no-expected mode only)")
    parser.add_argument(
        "--argument-correctness",
        action="store_true",
        help="Also run ArgumentCorrectnessMetric (requires ANTHROPIC_API_KEY)",
    )
    args = parser.parse_args()

    if args.argument_correctness and not os.environ.get("ANTHROPIC_API_KEY"):
        print(
            "error: --argument-correctness requires ANTHROPIC_API_KEY environment variable (LLM judge makes network calls)",
            file=sys.stderr,
        )
        sys.exit(2)

    try:
        lines = load_jsonl(args.jsonl)
    except (OSError, json.JSONDecodeError) as exc:
        print(f"error: cannot read {args.jsonl}: {exc}", file=sys.stderr)
        sys.exit(2)

    run_ids = list_run_ids(lines)
    run_id = args.run_id

    if run_id is None:
        if len(run_ids) > 1:
            print(
                f"error: multiple runs found ({', '.join(run_ids)}); specify one with --run",
                file=sys.stderr,
            )
            sys.exit(2)
        run_id = run_ids[0] if run_ids else ""

    session = parse_session(lines, run_id)

    if args.expected is None:
        result = session_to_dicts(session)
        print(json.dumps(result, indent=2))
        sys.exit(0)

    try:
        expected_names = load_expected_names(args.expected)
    except (OSError, json.JSONDecodeError, ExpectedLoadError) as exc:
        print(f"error: cannot load expected tools from {args.expected}: {exc}", file=sys.stderr)
        sys.exit(2)

    try:
        from deepeval.metrics import ToolCorrectnessMetric
        from deepeval.test_case import ToolCallParams
    except ImportError:
        print(
            "error: deepeval is not installed; run: pip install 'catacomb-deepeval[deepeval]'",
            file=sys.stderr,
        )
        sys.exit(2)

    evaluation_params: Optional[List] = None
    if args.match == "input":
        evaluation_params = [ToolCallParams.INPUT_PARAMETERS]
    elif args.match == "output":
        evaluation_params = [ToolCallParams.OUTPUT]

    metric_kwargs = {}
    if evaluation_params is not None:
        metric_kwargs["evaluation_params"] = evaluation_params

    metric = ToolCorrectnessMetric(**metric_kwargs)
    tc = session_to_test_case(session, expected_names=expected_names)
    metric.measure(tc)

    verdict = "PASS" if metric.score >= metric.threshold else "FAIL"
    print(f"score: {metric.score:.3f}")
    print(f"reason: {metric.reason}")
    print(verdict)

    sys.exit(0 if verdict == "PASS" else 1)
