from __future__ import annotations

import dataclasses
import json
import math
import os
import sys

from catacomb_verifier._tables import CompareResult, compare_tables

__version__ = "0.1.0"

__all__ = ["Cell", "CompareResult", "compare_tables", "emit"]


@dataclasses.dataclass(frozen=True)
class Cell:
    """A single bench cell's exec-contract environment (the CATACOMB_* variables)."""

    evidence_dir: str
    workdir: str
    run_id: str
    basket: str
    task: str
    variant: str
    rep: int
    agent_exit_code: int

    @classmethod
    def from_env(cls) -> "Cell":
        """Build a Cell from the CATACOMB_* environment.

        EVIDENCE_DIR and RUN_ID are required; a missing one exits 2 (operational
        failure) rather than raising, so a broken contract stops the verifier
        cleanly. The remaining variables default to "" (workdir "" means offline)
        or 0.
        """
        env = os.environ
        try:
            evidence_dir = env["CATACOMB_EVIDENCE_DIR"]
            run_id = env["CATACOMB_RUN_ID"]
        except KeyError as exc:
            print(
                f"catacomb-verifier: missing required environment variable {exc.args[0]}",
                file=sys.stderr,
            )
            raise SystemExit(2) from None
        return cls(
            evidence_dir=evidence_dir,
            workdir=env.get("CATACOMB_WORKDIR", ""),
            run_id=run_id,
            basket=env.get("CATACOMB_BASKET", ""),
            task=env.get("CATACOMB_TASK", ""),
            variant=env.get("CATACOMB_VARIANT", ""),
            rep=int(env.get("CATACOMB_REP", "0")),
            agent_exit_code=int(env.get("CATACOMB_AGENT_EXIT_CODE", "0")),
        )

    def artifact(self, rel: str) -> str:
        """Resolve a captured artifact to an on-disk path.

        Prefers the redacted evidence copy (evidence_dir/artifacts/rel), so a
        verifier reads the same bytes in bench and offline re-verification. Falls
        back to the live workdir only in bench mode (workdir non-empty). Raises
        FileNotFoundError when the artifact is in neither.
        """
        evidence_path = os.path.join(self.evidence_dir, "artifacts", rel)
        if os.path.exists(evidence_path):
            return evidence_path
        if self.workdir:
            workdir_path = os.path.join(self.workdir, rel)
            if os.path.exists(workdir_path):
                return workdir_path
        raise FileNotFoundError(f"artifact not found in evidence or workdir: {rel}")


def emit(
    passed: bool | None = None,
    key: str | None = None,
    value: float | None = None,
    run_id: str | None = None,
    **provenance: str,
) -> None:
    """Print one scores-JSONL line to stdout.

    Exactly one of `passed` or `key` is required. `passed` emits the reserved
    `verifier.pass` key as 1/0 and takes no `value`; `key` emits an arbitrary
    owner.key with `value`. Numeric types are preserved verbatim (an int stays an
    int in the JSON), and `run_id` plus any provenance kwargs (tool, tool_version,
    prompt_hash) pass through as extra fields in insertion order.

    The output must survive the Go gate's scores parser, which unmarshals `value`
    into a `float64` and requires an `owner.key` key: a `value` that is a bool,
    non-numeric, or non-finite, or a `key` outside that grammar, is rejected here
    with `ValueError` rather than written as a line the gate would choke on.
    """
    if (passed is None) == (key is None):
        raise ValueError("emit requires exactly one of passed= or key=")
    if passed is not None:
        if value is not None:
            raise ValueError("emit(passed=...) takes no value=")
        line: dict[str, object] = {"key": "verifier.pass", "value": 1 if passed else 0}
    else:
        if value is None:
            raise ValueError("emit(key=...) requires a numeric value=")
        _validate_value(value)
        assert key is not None
        _validate_key(key)
        line = {"key": key, "value": value}
    if run_id is not None:
        line["run_id"] = run_id
    line.update(provenance)
    print(json.dumps(line, separators=(",", ":")))


def _validate_value(value: object) -> None:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise ValueError("emit value must be a finite number")


def _validate_key(key: str) -> None:
    owner, sep, rest = key.partition(".")
    if not sep or not owner or not rest or "." in rest:
        raise ValueError(f"emit key {key!r} must be owner.key")
