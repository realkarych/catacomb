from __future__ import annotations

import argparse
import json
import sys

from catacomb_judge._io import (
    FormatError,
    JoinedKey,
    Pair,
    join,
    load_labels,
    load_scores,
    scan_runs_dir,
)
from catacomb_judge._metrics import binarize, kappa, spearman, tnr, tpr

_COLUMNS = ["KEY", "JUDGE", "N", "SPEARMAN", "KAPPA", "TPR", "TNR"]
_METRICS = ["spearman", "kappa", "tpr", "tnr"]


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(
        prog="catacomb-judge",
        description="Judge meta-evaluation utilities over catacomb scores-JSONL.",
    )
    subparsers = parser.add_subparsers(dest="command", required=True, metavar="command")
    agreement = subparsers.add_parser(
        "agreement",
        help="judge-vs-gold agreement metrics (Spearman, kappa, TPR/TNR)",
        description="Join judge scores to a hand-labeled gold set and report "
        "per-judge agreement metrics, with an optional kappa calibration gate.",
    )
    agreement.add_argument("scores", nargs="*", help="scores JSONL file paths")
    agreement.add_argument("--labels", required=True, help="hand-labeled JSONL gold set")
    agreement.add_argument("--runs-dir", help="scan DIR/*/scores.jsonl (sorted)")
    agreement.add_argument("--key", help="restrict to one annotation key")
    agreement.add_argument(
        "--threshold",
        type=float,
        default=0.5,
        help="binarization threshold for kappa/TPR/TNR (default: 0.5)",
    )
    agreement.add_argument(
        "--min-kappa",
        type=float,
        default=None,
        help="exit 1 when any judge kappa is omitted or below this value",
    )
    agreement.add_argument("--json", action="store_true", help="emit a JSON document")
    args = parser.parse_args(argv)
    if not args.scores and args.runs_dir is None:
        agreement.error("at least one scores file or --runs-dir is required")
    sys.exit(_run_agreement(args))


def _run_agreement(args: argparse.Namespace) -> int:
    try:
        score_paths = list(args.scores)
        if args.runs_dir is not None:
            score_paths.extend(scan_runs_dir(args.runs_dir))
        scores = [line for path in score_paths for line in load_scores(path)]
        labels = load_labels(args.labels)
    except (OSError, FormatError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    joined = join(scores, labels)
    if args.key is not None:
        joined = [entry for entry in joined if entry.key == args.key]
    entries = [_key_entry(entry, args.threshold) for entry in joined]
    if args.json:
        print(json.dumps({"keys": entries}, indent=2))
    else:
        _print_table(entries)
    if args.min_kappa is not None:
        failures = _gate_failures(entries, args.min_kappa)
        if failures:
            for failure in failures:
                print(failure, file=sys.stderr)
            return 1
    return 0


def _key_entry(joined_key: JoinedKey, threshold: float) -> dict:
    judges = [
        {"tool": tool, **_metric_fields(pairs, threshold)}
        for tool, pairs in _by_tool(joined_key.pairs)
    ]
    return {
        "key": joined_key.key,
        "unmatched_labels": joined_key.unmatched_labels,
        "unmatched_scores": joined_key.unmatched_scores,
        "judges": judges,
        "overall": _metric_fields(list(joined_key.pairs), threshold),
    }


def _by_tool(pairs: tuple[Pair, ...]) -> list[tuple[str, list[Pair]]]:
    tools = sorted({pair.tool for pair in pairs})
    return [(tool, [pair for pair in pairs if pair.tool == tool]) for tool in tools]


def _metric_fields(pairs: list[Pair], threshold: float) -> dict:
    label_values = [pair.label for pair in pairs]
    score_values = [pair.score for pair in pairs]
    label_bits = binarize(label_values, threshold)
    score_bits = binarize(score_values, threshold)
    fields: dict = {"n": len(pairs)}
    computed = {
        "spearman": spearman(label_values, score_values),
        "kappa": kappa(label_bits, score_bits),
        "tpr": tpr(label_bits, score_bits),
        "tnr": tnr(label_bits, score_bits),
    }
    fields.update({name: value for name, value in computed.items() if value is not None})
    return fields


def _print_table(entries: list[dict]) -> None:
    rows = [_COLUMNS]
    for entry in entries:
        for judge in entry["judges"]:
            rows.append(_row(entry["key"], judge["tool"], judge))
        rows.append(_row(entry["key"], "overall", entry["overall"]))
    widths = [max(len(row[i]) for row in rows) for i in range(len(_COLUMNS))]
    for row in rows:
        print("  ".join(cell.ljust(width) for cell, width in zip(row, widths)).rstrip())
    notes = [e for e in entries if e["unmatched_labels"] or e["unmatched_scores"]]
    if notes:
        print()
        for entry in notes:
            print(
                f"{entry['key']}: {entry['unmatched_labels']} unmatched labels, "
                f"{entry['unmatched_scores']} unmatched scores"
            )


def _row(key: str, judge: str, fields: dict) -> list[str]:
    cells = [key, judge, str(fields["n"])]
    cells.extend(
        f"{fields[name]:.3f}" if name in fields else "-" for name in _METRICS
    )
    return cells


def _gate_failures(entries: list[dict], min_kappa: float) -> list[str]:
    failures = []
    for entry in entries:
        for judge in entry["judges"]:
            if "kappa" not in judge:
                failures.append(
                    f"{entry['key']}/{judge['tool']}: kappa omitted "
                    f"(--min-kappa {min_kappa})"
                )
            elif judge["kappa"] < min_kappa:
                failures.append(
                    f"{entry['key']}/{judge['tool']}: "
                    f"kappa {judge['kappa']:.3f} < {min_kappa}"
                )
    return failures
