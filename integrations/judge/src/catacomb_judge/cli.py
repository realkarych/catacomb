from __future__ import annotations

import argparse
import json
import sys

from catacomb_judge import __version__
from catacomb_judge._io import (
    FormatError,
    JoinedKey,
    Pair,
    ScoreLine,
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
    panel = subparsers.add_parser(
        "panel",
        help="aggregate a judge panel into one scores-JSONL stream",
        description="Group judge scores by (run_id, key[, step_key]) and aggregate "
        "each panel into a single scores-JSONL line (mean, or strict-majority "
        "--vote), usable as regress --scores input.",
    )
    panel.add_argument("scores", nargs="*", help="scores JSONL file paths")
    panel.add_argument("--runs-dir", help="scan DIR/*/scores.jsonl (sorted)")
    panel.add_argument("--key", help="restrict to one annotation key")
    panel.add_argument(
        "--vote",
        action="store_true",
        help="strict-majority vote on values binarized at 0.5 (odd panels only)",
    )
    panel.add_argument(
        "--min-judges",
        type=int,
        default=2,
        help="skip groups with fewer distinct judges (default: 2)",
    )
    panel.add_argument("--out", help="write output lines to FILE instead of stdout")
    args = parser.parse_args(argv)
    subparser = agreement if args.command == "agreement" else panel
    if not args.scores and args.runs_dir is None:
        subparser.error("at least one scores file or --runs-dir is required")
    if args.command == "agreement":
        sys.exit(_run_agreement(args))
    sys.exit(_run_panel(args))


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


def _run_panel(args: argparse.Namespace) -> int:
    try:
        score_paths = list(args.scores)
        if args.runs_dir is not None:
            score_paths.extend(scan_runs_dir(args.runs_dir))
        scores = [line for path in score_paths for line in load_scores(path)]
        lines = _panel_lines(scores, args.key, args.min_judges, args.vote)
        output = "".join(line + "\n" for line in lines)
        if args.out is not None:
            with open(args.out, "w", encoding="utf-8") as handle:
                handle.write(output)
        else:
            sys.stdout.write(output)
    except (OSError, FormatError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    return 0


def _panel_lines(
    scores: list[ScoreLine], key: str | None, min_judges: int, vote: bool
) -> list[str]:
    if key is not None:
        scores = [s for s in scores if s.key == key]
    provenanced = [s for s in scores if s.tool != "unknown"]
    if len(provenanced) < len(scores):
        print(
            f"note: skipped {len(scores) - len(provenanced)} score line(s) "
            "without tool provenance",
            file=sys.stderr,
        )
    groups: dict[tuple[str, str, str | None], list[ScoreLine]] = {}
    for s in provenanced:
        groups.setdefault((s.run_id, s.key, s.step_key), []).append(s)
    lines: list[str] = []
    for coord in sorted(groups, key=lambda c: (c[0], c[1], c[2] or "")):
        members = groups[coord]
        _require_unambiguous(coord, members)
        if len(members) < min_judges:
            print(
                f"note: skipped {_group_name(coord)}: "
                f"{len(members)} judge(s) < --min-judges {min_judges}",
                file=sys.stderr,
            )
            continue
        lines.append(_panel_json(coord, members, vote))
    return lines


def _require_unambiguous(
    coord: tuple[str, str, str | None], members: list[ScoreLine]
) -> None:
    for tool in sorted({s.tool for s in members}):
        count = sum(1 for s in members if s.tool == tool)
        if count > 1:
            raise FormatError(
                f'ambiguous panel input: tool "{tool}" emitted {count} lines '
                f"for {_group_name(coord)}"
            )


def _panel_json(
    coord: tuple[str, str, str | None], members: list[ScoreLine], vote: bool
) -> str:
    run_id, key, step_key = coord
    values = [s.value for s in sorted(members, key=lambda s: s.tool)]
    if vote:
        if len(values) % 2 == 0:
            raise FormatError(
                f"--vote requires an odd panel: {len(values)} judges "
                f"for {_group_name(coord)}"
            )
        ones = sum(binarize(values, 0.5))
        value: int | float = 1 if 2 * ones > len(values) else 0
    else:
        mean = sum(values) / len(values)
        value = int(mean) if mean.is_integer() else mean
    line: dict[str, object] = {"key": key, "value": value, "run_id": run_id}
    if step_key is not None:
        line["step_key"] = step_key
    line["tool"] = "panel"
    line["tool_version"] = __version__
    line["panel_size"] = len(values)
    return json.dumps(line, separators=(",", ":"))


def _group_name(coord: tuple[str, str, str | None]) -> str:
    run_id, key, step_key = coord
    name = f'run_id="{run_id}" key="{key}"'
    if step_key is not None:
        name += f' step_key="{step_key}"'
    return name
