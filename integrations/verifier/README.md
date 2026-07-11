# catacomb-verifier

A stdlib-only Python SDK for authoring catacomb verifiers. A verifier is the
small program `catacomb bench` runs after each cell completes: it reads the
cell's recorded evidence and prints scores JSONL to stdout, which the gate
aggregates into pass rates and continuous metrics.

This directory is outside the Go 100%-coverage gate and the no-comments rule;
both apply only to Go packages. The package depends on nothing but the standard
library (`dataclasses`, `json`, `os`, `sys`).

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

## Testing

```bash
cd integrations/verifier
pip install -e '.[test]'
pytest -q
```
