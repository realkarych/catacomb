# Architecture Decision Records

Catacomb records consequential architecture decisions as ADRs (MADR-lite). Each captures the context, the decision, the alternatives considered, and the consequences.

| # | Decision | Status |
|---|---|---|
| [0001](0001-form-factor-daemon-over-core-library.md) | Form factor: daemon sidecar over a reusable core library | Superseded by 0026 |
| [0002](0002-four-source-capture-hooks-backbone.md) | Four-source capture with hooks as the backbone | Superseded by 0026 |
| [0003](0003-reconciliation-canonical-entity-precedence.md) | Reconciliation: canonical entity with per-field precedence and provenance | Accepted |
| [0004](0004-graph-native-model-with-otel-openinference-mappers.md) | Graph-native model with OTel/OpenInference boundary mappers | Accepted |
| [0005](0005-run-model-forest-session-anchor-env-runid.md) | Run model: persistent forest, `session_id` anchor, env wrapper run-id | Accepted |
| [0006](0006-embedded-sqlite-durable-store.md) | Embedded SQLite as the default durable store | Accepted |
| [0007](0007-export-materialized-upsert-cdc-snapshot.md) | Export: materialized graph (upsert + CDC) and snapshot | Superseded by 0026 |
| [0008](0008-payload-storage-hash-redaction.md) | Payload handling: store with hash and redaction | Accepted |
| [0009](0009-conversation-threading-interruption-meta-records.md) | Conversation threading, interruption, and transcript meta-records | Accepted |
| [0010](0010-observation-identity-and-durability.md) | Observation identity (ULID) and durability (txn + watermark + WAL) | Accepted |
| [0011](0011-canonical-id-execution-scope.md) | Canonical-id contract: `execution_id` scope, `run_id` as grouping label | Accepted |
| [0012](0012-node-finalization-and-run-lifecycle.md) | Node finalization (`unknown`/`abandoned`) and run lifecycle / reaper | Accepted |
| [0013](0013-daemon-security-and-trust-boundary.md) | Daemon security and trust boundary (unix socket / bearer token) | Superseded by 0026 |
| [0014](0014-conditional-precedence-and-status-reconciliation.md) | Conditional structure precedence + status lattice + cancel/supersede cascade | Accepted |
| [0015](0015-exporter-correctness-under-failure.md) | Exporter correctness under failure (`rev` guard, snapshot-resume) | Superseded by 0026 |
| [0016](0016-cross-run-step-identity-and-annotations.md) | Cross-run `step_key`/`phase_key` and the annotations contract | Accepted |
| [0017](0017-data-format-versioning-and-migration.md) | Catacomb data-format versioning and migration | Accepted |
| [0018](0018-time-model.md) | Time model (event-time vs ingest-time; `seq` is the order) | Accepted |
| [0019](0019-operability-fault-isolation-self-observation.md) | Operability, fault isolation, and self-observation | Superseded by 0026 |
| [0020](0020-redaction-surface-and-secrets-at-rest.md) | Redaction surface and secrets-at-rest hardening | Accepted |
| [0021](0021-graph-invariants-and-validation.md) | Graph invariants and validation (acyclic forest, lean contraction) | Accepted |
| [0022](0022-regression-detection-over-repeated-runs.md) | Regression detection over repeated runs (baskets, baselines, aggregation) | Accepted |
| [0023](0023-regression-gate-sensitivity-at-small-k.md) | Regression gate sensitivity at small run counts | Accepted |
| [0024](0024-secrets-at-rest-write-path-redaction.md) | Secrets at rest: enforcing redaction on the write path | Accepted |
| [0025](0025-capture-format-drift-detection.md) | Capture format drift detection | Accepted |
| [0026](0026-form-factor-pivot-offline-eval-gate.md) | Form factor pivot: offline eval gate over vendor observability | Accepted |
| [0027](0027-verification-layer-and-reliability-metrics.md) | Verification layer and reliability metrics (post-pivot vector) | Accepted |
| [0028](0028-per-cell-workspace-isolation.md) | Per-cell workspace isolation (fresh workdir, patch handover, teardown) | Accepted |
| [0029](0029-basket-relative-path-resolution.md) | Basket-relative path resolution for dir and ./ argv | Accepted |
| [0030](0030-interactive-session-import.md) | Interactive session import (`catacomb import`, evidence-only second entry point) | Accepted |
| [0031](0031-multi-runtime-ingestion-codex.md) | Multi-runtime transcript ingestion, Codex CLI first | Accepted |

Design spec: [`../internal/specs/2026-06-20-catacomb-design.md`](../internal/specs/2026-06-20-catacomb-design.md).

## Format

`NNNN-kebab-title.md` with sections: Status · Date · Deciders · Related · Context · Decision · Alternatives considered · Consequences. New decisions get the next number; superseded ones are marked `Superseded by ADR-NNNN` rather than deleted.
