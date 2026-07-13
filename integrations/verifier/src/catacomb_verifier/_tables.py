from __future__ import annotations

import csv
import dataclasses
import json
import os

_MAX_MISMATCHES = 10


@dataclasses.dataclass(frozen=True)
class CompareResult:
    """Structured diff of two tables: overall verdict, row-count delta, capped diffs."""

    equal: bool
    row_diff: int
    mismatches: list[str]


def compare_tables(
    got: str,
    want: str,
    *,
    float_tol: float = 1e-4,
    ordered: bool = False,
    strict: bool = True,
    normalize_headers: bool = True,
) -> CompareResult:
    """Compare two tables under the benchmark canon (numeric tolerance, order- and
    header-insensitive by default, strict about extra rows/columns).

    `got` and `want` are file paths whose extension picks the parser (`.csv`,
    `.jsonl`). Cells coerce int -> float -> stripped string; numeric cells compare
    within `float_tol`, while NaN/inf cells never compare equal (tolerance
    arithmetic yields NaN). Under `strict` (default) rows pair positionally after
    sorting by their canonical string tuple, falling back to a tolerance-aware
    match when that surfaces mismatches (`ordered=True` keeps file order and skips
    the fallback), and differing column sets or row counts make the tables
    unequal, each named in `mismatches`. Under non-strict `want` need only be
    contained in `got` (extra rows and columns tolerated, `ordered` ignored).
    `mismatches` holds the first 10 diffs.
    """
    got_cols, got_rows = _load(got, normalize_headers)
    want_cols, want_rows = _load(want, normalize_headers)

    row_diff = abs(len(got_rows) - len(want_rows))
    cols = want_cols or got_cols
    mismatches: list[str] = []

    if strict:
        for col in sorted(set(got_cols) - set(want_cols)):
            mismatches.append(f"column {col}: in got, not in want")
        for col in sorted(set(want_cols) - set(got_cols)):
            mismatches.append(f"column {col}: in want, not in got")
        if row_diff != 0:
            mismatches.append(f"row count: got {len(got_rows)}, want {len(want_rows)}")
        else:
            mismatches += _paired_mismatches(got_rows, want_rows, cols, ordered, float_tol)
    else:
        mismatches += _contained_mismatches(got_rows, want_rows, cols, float_tol)

    return CompareResult(
        equal=not mismatches, row_diff=row_diff, mismatches=mismatches[:_MAX_MISMATCHES]
    )


def _paired_mismatches(
    got_rows: list[dict[str, object]],
    want_rows: list[dict[str, object]],
    cols: list[str],
    ordered: bool,
    float_tol: float,
) -> list[str]:
    left = got_rows if ordered else _sorted(got_rows, cols)
    right = want_rows if ordered else _sorted(want_rows, cols)
    out = _positional_mismatches(left, right, cols, float_tol)
    if ordered or not out:
        return out
    return _matched_mismatches(left, right, cols, float_tol)


def _positional_mismatches(
    left: list[dict[str, object]],
    right: list[dict[str, object]],
    cols: list[str],
    float_tol: float,
) -> list[str]:
    out: list[str] = []
    for i in range(len(right)):
        for col in cols:
            a, b = left[i].get(col), right[i].get(col)
            if not _cell_eq(a, b, float_tol):
                out.append(f"row {i} col {col}: {_fmt(a)} != {_fmt(b)}")
                if len(out) >= _MAX_MISMATCHES:
                    return out
    return out


def _matched_mismatches(
    left: list[dict[str, object]],
    right: list[dict[str, object]],
    cols: list[str],
    float_tol: float,
) -> list[str]:
    compat = [
        [
            j
            for j, want_row in enumerate(right)
            if all(_cell_eq(got_row.get(c), want_row.get(c), float_tol) for c in cols)
        ]
        for got_row in left
    ]
    match_right = [-1] * len(right)
    for i in range(len(left)):
        _augment(i, compat, [False] * len(right), match_right)
    matched_left = [False] * len(left)
    for i in match_right:
        if i != -1:
            matched_left[i] = True
    leftover_left = [left[i] for i in range(len(left)) if not matched_left[i]]
    leftover_right = [right[j] for j in range(len(right)) if match_right[j] == -1]
    return _positional_mismatches(leftover_left, leftover_right, cols, float_tol)


def _augment(i: int, compat: list[list[int]], seen: list[bool], match_right: list[int]) -> bool:
    stack: list[tuple[int, int]] = [(i, 0)]
    chosen: list[int] = []
    while stack:
        row, pos = stack[-1]
        found = -1
        while pos < len(compat[row]):
            j = compat[row][pos]
            pos += 1
            if not seen[j]:
                found = j
                break
        stack[-1] = (row, pos)
        if found == -1:
            stack.pop()
            if chosen:
                chosen.pop()
            continue
        seen[found] = True
        if match_right[found] == -1:
            match_right[found] = row
            for depth in range(len(stack) - 1):
                match_right[chosen[depth]] = stack[depth][0]
            return True
        chosen.append(found)
        stack.append((match_right[found], 0))
    return False


def _contained_mismatches(
    got_rows: list[dict[str, object]],
    want_rows: list[dict[str, object]],
    cols: list[str],
    float_tol: float,
) -> list[str]:
    remaining = list(got_rows)
    out: list[str] = []
    for want_row in want_rows:
        for i, got_row in enumerate(remaining):
            if all(_cell_eq(got_row.get(c), want_row.get(c), float_tol) for c in cols):
                del remaining[i]
                break
        else:
            out.append("missing row: " + ", ".join(f"{c}={_fmt(want_row.get(c))}" for c in cols))
            if len(out) >= _MAX_MISMATCHES:
                break
    return out


def _load(path: str, normalize_headers: bool) -> tuple[list[str], list[dict[str, object]]]:
    ext = os.path.splitext(path)[1].lower()
    if ext == ".csv":
        return _load_csv(path, normalize_headers)
    if ext == ".jsonl":
        return _load_jsonl(path, normalize_headers)
    raise ValueError(f"unsupported table format: {path!r}")


def _load_csv(path: str, normalize_headers: bool) -> tuple[list[str], list[dict[str, object]]]:
    with open(path, newline="", encoding="utf-8") as handle:
        records = list(csv.reader(handle))
    if not records:
        return [], []
    originals = records[0]
    header = [_col_name(cell, normalize_headers) for cell in originals]
    _reject_collisions(header, originals, path)
    rows = [
        {col: _coerce(raw[i]) if i < len(raw) else None for i, col in enumerate(header)}
        for raw in records[1:]
    ]
    return header, rows


def _load_jsonl(path: str, normalize_headers: bool) -> tuple[list[str], list[dict[str, object]]]:
    header: list[str] = []
    seen: set[str] = set()
    rows: list[dict[str, object]] = []
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            obj = json.loads(line)
            originals = [str(key) for key in obj]
            names = [_col_name(key, normalize_headers) for key in obj]
            _reject_collisions(names, originals, path)
            row: dict[str, object] = {}
            for col, value in zip(names, obj.values()):
                if col not in seen:
                    seen.add(col)
                    header.append(col)
                row[col] = _coerce(value)
            rows.append(row)
    return header, rows


def _reject_collisions(names: list[str], originals: list[str], path: str) -> None:
    groups: dict[str, list[str]] = {}
    for original, name in zip(originals, names):
        groups.setdefault(name, []).append(original)
    collided = {name: origs for name, origs in groups.items() if len(origs) > 1}
    if collided:
        detail = "; ".join(f"{name!r} from {origs}" for name, origs in sorted(collided.items()))
        raise ValueError(f"duplicate column after header normalization in {path!r}: {detail}")


def _col_name(name: object, normalize: bool) -> str:
    text = str(name)
    if not normalize:
        return text
    return text.strip().lower().replace(" ", "_").replace("-", "_")


def _coerce(value: object) -> object:
    if isinstance(value, bool) or value is None:
        return value
    if isinstance(value, (int, float)):
        return value
    text = str(value).strip()
    try:
        return int(text)
    except ValueError:
        pass
    try:
        return float(text)
    except ValueError:
        return text


def _as_number(value: object) -> float | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return float(value)
    return None


def _cell_eq(a: object, b: object, float_tol: float) -> bool:
    an, bn = _as_number(a), _as_number(b)
    if an is not None and bn is not None:
        return abs(an - bn) <= float_tol
    return a == b


def _fmt(value: object) -> str:
    return str(value)


def _sorted(rows: list[dict[str, object]], cols: list[str]) -> list[dict[str, object]]:
    return sorted(rows, key=lambda row: tuple(_fmt(row.get(col)) for col in cols))
