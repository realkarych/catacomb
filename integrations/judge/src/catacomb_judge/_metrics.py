from __future__ import annotations

import math
from collections.abc import Sized


def spearman(xs: list[float], ys: list[float]) -> float | None:
    """Spearman rank correlation with average ranks for ties on both sides.

    Ranks each side (ties receive the mean of the ranks they span) and returns
    the Pearson correlation of the ranks. Returns None when there are fewer
    than two pairs or either side has zero variance — the coefficient is
    undefined there, and the caller omits it rather than reporting a number.
    """
    _require_same_length("spearman", xs, ys)
    if len(xs) < 2:
        return None
    return _pearson(_average_ranks(xs), _average_ranks(ys))


def binarize(values: list[float], threshold: float) -> list[int]:
    """Map each value to 1 when it is >= threshold, else 0 (boundary -> 1)."""
    return [1 if value >= threshold else 0 for value in values]


def kappa(a: list[int], b: list[int]) -> float | None:
    """Binary Cohen's kappa: (p_o - p_e) / (1 - p_e).

    p_o is the observed agreement rate; p_e is the chance agreement implied by
    each side's marginal rate of 1s. Returns None when p_e == 1 (both
    marginals constant at the same value, kappa undefined) or when there
    are no pairs.
    """
    _require_same_length("kappa", a, b)
    n = len(a)
    if n == 0:
        return None
    p_o = sum(1 for x, y in zip(a, b) if x == y) / n
    a_ones = sum(1 for x in a if x == 1) / n
    b_ones = sum(1 for y in b if y == 1) / n
    p_e = a_ones * b_ones + (1 - a_ones) * (1 - b_ones)
    if p_e == 1:
        return None
    return (p_o - p_e) / (1 - p_e)


def tpr(labels: list[int], preds: list[int]) -> float | None:
    """True-positive rate TP/(TP+FN); None when the labels have no positives."""
    _require_same_length("tpr", labels, preds)
    positives = sum(1 for label in labels if label == 1)
    if positives == 0:
        return None
    true_positives = sum(
        1 for label, pred in zip(labels, preds) if label == 1 and pred == 1
    )
    return true_positives / positives


def tnr(labels: list[int], preds: list[int]) -> float | None:
    """True-negative rate TN/(TN+FP); None when the labels have no negatives."""
    _require_same_length("tnr", labels, preds)
    negatives = sum(1 for label in labels if label == 0)
    if negatives == 0:
        return None
    true_negatives = sum(
        1 for label, pred in zip(labels, preds) if label == 0 and pred == 0
    )
    return true_negatives / negatives


def _require_same_length(name: str, a: Sized, b: Sized) -> None:
    if len(a) != len(b):
        raise ValueError(f"{name} requires equal-length inputs: {len(a)} != {len(b)}")


def _average_ranks(values: list[float]) -> list[float]:
    order = sorted(range(len(values)), key=lambda i: values[i])
    ranks = [0.0] * len(values)
    start = 0
    while start < len(order):
        end = start
        while end + 1 < len(order) and values[order[end + 1]] == values[order[start]]:
            end += 1
        rank = (start + end) / 2 + 1
        for position in range(start, end + 1):
            ranks[order[position]] = rank
        start = end + 1
    return ranks


def _pearson(xs: list[float], ys: list[float]) -> float | None:
    n = len(xs)
    mean_x = sum(xs) / n
    mean_y = sum(ys) / n
    dx = [x - mean_x for x in xs]
    dy = [y - mean_y for y in ys]
    var_x = sum(d * d for d in dx)
    var_y = sum(d * d for d in dy)
    if var_x == 0 or var_y == 0:
        return None
    covariance = sum(x * y for x, y in zip(dx, dy))
    return covariance / math.sqrt(var_x * var_y)
