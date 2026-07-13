from __future__ import annotations

import dataclasses
import glob
import json
import math
import os
import sys


class FormatError(Exception):
    """Malformed or ambiguous input; the message names the offending
    file:line or (run_id, key[, step_key]) group."""


@dataclasses.dataclass(frozen=True)
class ScoreLine:
    key: str
    value: float
    run_id: str
    step_key: str | None
    tool: str


@dataclasses.dataclass(frozen=True)
class Label:
    run_id: str
    key: str
    label: float
    step_key: str | None


@dataclasses.dataclass(frozen=True)
class Pair:
    tool: str
    label: float
    score: float


@dataclasses.dataclass(frozen=True)
class JoinedKey:
    key: str
    pairs: tuple[Pair, ...]
    unmatched_labels: int
    unmatched_scores: int


def load_scores(path: str) -> list[ScoreLine]:
    """Load one scores-JSONL file (the SP1 dialect, unknown fields tolerated).

    Lines without a run_id cannot join to a label and are skipped, with one
    stderr note per file; a malformed line raises FormatError naming file:line.
    """
    scores: list[ScoreLine] = []
    skipped = 0
    for lineno, obj in _objects(path):
        key = _string_field(path, lineno, obj, "key")
        if key is None:
            raise FormatError(f'{path}:{lineno}: missing "key"')
        _validate_key(path, lineno, key)
        if "value" not in obj:
            raise FormatError(f'{path}:{lineno}: missing "value"')
        value = _number(path, lineno, obj["value"], "value")
        run_id = _string_field(path, lineno, obj, "run_id")
        if not run_id:
            skipped += 1
            continue
        step_key = _string_field(path, lineno, obj, "step_key") or None
        tool = _string_field(path, lineno, obj, "tool") or "unknown"
        scores.append(
            ScoreLine(key=key, value=value, run_id=run_id, step_key=step_key, tool=tool)
        )
    if skipped:
        print(
            f"note: {path}: skipped {skipped} score line(s) without run_id",
            file=sys.stderr,
        )
    return scores


def load_labels(path: str) -> list[Label]:
    """Load a hand-labeled JSONL gold set.

    Every line needs run_id, key, and a numeric label; step_key is optional and
    narrows the join. Duplicate (run_id, key[, step_key]) coordinates are a
    FormatError — a gold set must be unambiguous.
    """
    labels: list[Label] = []
    seen: set[tuple[str, str, str | None]] = set()
    for lineno, obj in _objects(path):
        run_id = _string_field(path, lineno, obj, "run_id")
        if not run_id:
            raise FormatError(f'{path}:{lineno}: label requires a non-empty "run_id"')
        key = _string_field(path, lineno, obj, "key")
        if not key:
            raise FormatError(f'{path}:{lineno}: label requires a non-empty "key"')
        _validate_key(path, lineno, key)
        if "label" not in obj:
            raise FormatError(f'{path}:{lineno}: missing "label"')
        value = _number(path, lineno, obj["label"], "label")
        step_key = _string_field(path, lineno, obj, "step_key") or None
        coord = (run_id, key, step_key)
        if coord in seen:
            suffix = f' step_key="{step_key}"' if step_key is not None else ""
            raise FormatError(
                f'{path}:{lineno}: duplicate label for run_id="{run_id}" key="{key}"'
                + suffix
            )
        seen.add(coord)
        labels.append(Label(run_id=run_id, key=key, label=value, step_key=step_key))
    return labels


def scan_runs_dir(runs_dir: str) -> list[str]:
    """List <runs_dir>/*/scores.jsonl in sorted order (the bench evidence layout)."""
    if not os.path.isdir(runs_dir):
        raise FileNotFoundError(f"runs dir not found: {runs_dir}")
    return sorted(glob.glob(os.path.join(runs_dir, "*", "scores.jsonl")))


def join(scores: list[ScoreLine], labels: list[Label]) -> list[JoinedKey]:
    """Join score lines to labels on (run_id, key[, step_key]), per label key.

    The gold set defines the reported keys; score lines for unlabeled keys are
    out of scope. Pairs come out in canonical (tool, run_id, step_key, value)
    order so downstream metrics are bit-identical regardless of input order,
    and unmatched lines on either side are counted, never silently dropped.
    """
    by_coord = {(l.run_id, l.key, l.step_key): l.label for l in labels}
    joined: list[JoinedKey] = []
    for key in sorted({l.key for l in labels}):
        key_scores = sorted(
            (s for s in scores if s.key == key),
            key=lambda s: (s.tool, s.run_id, s.step_key or "", s.value),
        )
        matched: set[tuple[str, str, str | None]] = set()
        pairs: list[Pair] = []
        unmatched_scores = 0
        for s in key_scores:
            coord = (s.run_id, key, s.step_key)
            if coord in by_coord:
                pairs.append(Pair(tool=s.tool, label=by_coord[coord], score=s.value))
                matched.add(coord)
            else:
                unmatched_scores += 1
        unmatched_labels = sum(
            1
            for l in labels
            if l.key == key and (l.run_id, key, l.step_key) not in matched
        )
        joined.append(
            JoinedKey(
                key=key,
                pairs=tuple(pairs),
                unmatched_labels=unmatched_labels,
                unmatched_scores=unmatched_scores,
            )
        )
    return joined


def _objects(path: str):
    with open(path, encoding="utf-8") as handle:
        lines = handle.readlines()
    for lineno, raw in enumerate(lines, start=1):
        if not raw.strip():
            continue
        try:
            obj = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise FormatError(f"{path}:{lineno}: invalid JSON: {exc}") from None
        if not isinstance(obj, dict):
            raise FormatError(f"{path}:{lineno}: not a JSON object")
        yield lineno, obj


def _string_field(path: str, lineno: int, obj: dict, name: str) -> str | None:
    value = obj.get(name)
    if value is not None and not isinstance(value, str):
        raise FormatError(f'{path}:{lineno}: "{name}" must be a string')
    return value


def _number(path: str, lineno: int, value: object, name: str) -> float:
    if (
        isinstance(value, bool)
        or not isinstance(value, (int, float))
        or not math.isfinite(value)
    ):
        raise FormatError(f'{path}:{lineno}: "{name}" must be a finite number')
    return float(value)


def _validate_key(path: str, lineno: int, key: str) -> None:
    owner, sep, rest = key.partition(".")
    if not sep or not owner or not rest or "." in rest:
        raise FormatError(f"{path}:{lineno}: key {key!r} must be owner.key")
