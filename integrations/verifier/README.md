# catacomb-verifier

A stdlib-only Python SDK for authoring catacomb verifiers. A verifier is the
small program `catacomb bench` runs after each cell completes: it reads the
cell's recorded evidence and prints scores JSONL to stdout, which the gate
aggregates into pass rates and continuous metrics.

This directory is outside the Go 100%-coverage gate and the no-comments rule;
both apply only to Go packages. The package depends on nothing but the standard
library (`csv`, `dataclasses`, `json`, `math`, `os`, `sys`).

## The exec contract

`bench` invokes the verifier command in the cell's working directory with these
environment variables set:

| Variable | Meaning |
| --- | --- |
| `CATACOMB_EVIDENCE_DIR` | the cell's evidence dir (transcripts, `meta.json`, `artifacts/`) |
| `CATACOMB_WORKDIR` | the cell's live working directory (bench mode only; empty offline) |
| `CATACOMB_RUN_ID` | the cell's run id |
| `CATACOMB_BASKET`, `CATACOMB_TASK`, `CATACOMB_VARIANT`, `CATACOMB_REP` | cell coordinates |
| `CATACOMB_AGENT_EXIT_CODE` | the agent child's exit code |

`Cell.from_env()` parses these. `CATACOMB_EVIDENCE_DIR` and `CATACOMB_RUN_ID`
are required; a missing one exits 2 (an operational failure of the verifier, not
a task failure). The rest default to `""` (an empty `workdir` means offline) or 0.

## Reading artifacts

`cell.artifact(rel)` resolves a captured output to an on-disk path. It prefers
the redacted evidence copy under `evidence_dir/artifacts/`, so the same verifier
reads the same bytes during a live bench run and during offline re-verification.
In bench mode it falls back to the live `workdir`; when the artifact is in
neither it raises `FileNotFoundError`. A verifier that claims re-verifiability
should read only through `artifact()`.

## Emitting scores

`emit()` prints one scores-JSONL line per call. Exactly one of `passed=` or
`key=` is required (`ValueError` otherwise). `passed=` writes the reserved
`verifier.pass` key that the gate treats as a higher-is-better pass rate;
`key=` writes any `owner.key` with a numeric `value=`. `run_id=` and provenance
kwargs (`tool=`, `tool_version=`, `prompt_hash=`) pass through as extra fields.

```python
from catacomb_verifier import Cell, emit

cell = Cell.from_env()
result_csv = cell.artifact("out/result.csv")

emit(passed=True)                      # {"key":"verifier.pass","value":1}
emit(key="verifier.row_diff", value=3) # {"key":"verifier.row_diff","value":3}
```

The exit code is not the verdict: a failed task is `verifier.pass: 0` with exit
0. A non-zero exit means the verifier itself broke, and the gate records that as
a missing verdict rather than silently passing.

## Comparing tables

`compare_tables(got, want, ...)` compares two result-set files under the
benchmark canon and returns a `CompareResult(equal, row_diff, mismatches)` — the
verdict, `abs` row-count delta, and the first 10 human-readable cell/row diffs.

```python
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()                        # parses CATACOMB_*; .artifact(path) reads
                                              # from evidence, falls back to workdir in bench mode
res = compare_tables(
    cell.artifact("out/result.csv"), "golden.csv",
    float_tol=1e-4, ordered=False)            # defaults: strict (extra rows/cols fail),
                                              # unordered, tolerance 1e-4, header normalization
emit(passed=res.equal)                        # -> {"key":"verifier.pass","value":1}
emit(key="verifier.row_diff", value=res.row_diff)
```

The rules follow the benchmark canon (Text2Analysis / DABStep / Databricks
strictness): numeric tolerance instead of exact floats (`abs(a - b) <=
float_tol`), row-order insensitivity unless `ordered=True`, header normalization
(lower-case, spaces and dashes to underscores) unless `normalize_headers=False`,
type coercion (`int → float → stripped string`), and **strict by default** —
extra rows or columns fail, the execution-accuracy false-positive lesson. Pass
`strict=False` for the lenient mode where `want` need only be contained in `got`
(`ordered` is ignored in this mode).
Formats are selected by extension: `.csv` (via the `csv` module) and `.jsonl`
(one JSON object per line); the two can be compared cross-format. `mismatches`
is a structured diff meant to be printed to stderr for a human. A nested JSON
value (an object or array cell) has no numeric form, so it compares via its
Python string form rather than structurally. Two headers that normalize to the
same column name raise `ValueError` rather than silently collapsing to one.

## Testing

```bash
cd integrations/verifier
pip install -e '.[test]'
pytest -q
```
