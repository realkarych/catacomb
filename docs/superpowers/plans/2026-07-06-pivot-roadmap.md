# Pivot roadmap: offline eval gate over vendor observability (ADR-0026)

Executes [ADR-0026](../../adr/0026-form-factor-pivot-offline-eval-gate.md). Evidence base: [2026-07-05 CTO review](../../reviews/2026-07-05-competitive-cto-review.md).

Execution rules (every PV): own worktree, TDD, 100% coverage, subagent-driven task execution with review between tasks, review cycle + green CI before squash-merge. Deletion waves (PV-3 onward) start only after the ADR-0026 docs PR is merged.

## Sequence

| PR | Scope | Acceptance gate |
|---|---|---|
| PV-1 | Offline parity spike (additive; deletes nothing) | Parity test in CI: baseline-vs-degraded fixture groups gate `regression` (exit 1); A-vs-A gates `ok` with zero regressions |
| PV-2 | Slim eval store + version stamps + `--scores` annotations | `regress`/`baseline`/`trends` run with no graph store; records/baselines carry version stamps; scores file feeds annotation gates |
| PV-3 | Deletion I: `webui` (Go embed + TS + e2e), `tui`, `observe`/`ui`/`watch` commands | tag `v0-platform-final` on master first; CI drops the frontend job |
| PV-4 | Deletion II: `daemon`, `ingest/{hook,otel,streamjson,tail}`, `cdc`, `gen/`+proto+gRPC, lifecycle commands (`up/down/status/restart/logs/hook/install-hooks/mark/demo`), config slim, drift scoped to JSONL | `--offline` becomes the only bench path (flag removed); binary builds with cobra/yaml/ulid/sqlite only |
| PV-5 | Deletion III: `export/{otlp,neo4j,postgres,agentevals,evalview,build}` (jsonl export stays pure), DeepEval bridge retargeted and green, README/guide repositioning, ADR statuses final pass | DeepEval CI job green; docs describe the vendor-substrate (Phoenix) setup |
| PV-6 | Extended calibration: 2–3 heterogeneous baskets, deliberate regressions of varying magnitude, gate-power measurement at k=5/10 | calibration report in `docs/reviews/`; threshold defaults re-confirmed or amended |

## PV-1 (detailed plan)

[2026-07-06-pv1-offline-parity-spike.md](2026-07-06-pv1-offline-parity-spike.md)

Deliverables: `catacomb bench --offline` (no daemon: local child runner, transcript resolution from `~/.claude/projects`, in-process checkpoint verification, synthetic `task:<id>` boundary markers), evidence copies under `~/.catacomb/runs/<run_id>/` (redacted on write, `meta.json`), `catacomb regress --runs-dir` resolving `label:` selectors from evidence dirs, and the parity gate test.

Out of scope for PV-1 (lands in PV-2): baseline `name:` selectors over evidence dirs, version stamps, scores-file annotations.

## PV-2 sketch

- `store` shrinks to `baselines` + `regress_results` (+ migrate discipline); graph tables untouched until PV-4 deletes their writers.
- Baselines pin evidence directories (`RunIDs` → runs-dir paths) instead of graph-store rows; `name:` selector resolves offline.
- Records and baselines gain `catacomb_version` and `stepkey_scheme` stamps; mismatched comparisons warn, `--strict` refuses (ADR-0026 §6).
- `regress --scores <file.jsonl>` applies `owner.key` numeric values keyed by `step_key` to in-memory graphs before aggregation; DeepEval bridge documented as a producer.

## PV-3..PV-5 sketch

Deletion order is chosen so every intermediate master state builds, tests green, and keeps the gate usable: viewers first (nothing depends on them), then the daemon+ingest cluster once bench/regress no longer reference discovery, then exporters and packaging/doc repositioning. Each wave updates AGENTS.md, guide pages, and CI in the same PR so no doc references dead surface.

## PV-6 sketch

Baskets: (a) the V-1 calibration basket re-run offline (live `claude -p`, haiku); (b) a multi-checkpoint coding task with skill involvement; (c) an MCP-tool-swap A/B. Deliberate degradations at three magnitudes (checkpoint dropped; error-rate bump; latency/token inflation). Measure: verdict correctness, false-positive rate on A-vs-A, minimum detectable effect at k=5 and k=10. Output: a dated calibration report under `docs/reviews/`.
