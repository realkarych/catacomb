# Architecture Decision Records

Catacomb records consequential architecture decisions as ADRs (MADR-lite). Each captures the context, the decision, the alternatives considered, and the consequences.

| # | Decision | Status |
|---|---|---|
| [0001](0001-form-factor-daemon-over-core-library.md) | Form factor: daemon sidecar over a reusable core library | Accepted |
| [0002](0002-four-source-capture-hooks-backbone.md) | Four-source capture with hooks as the backbone | Accepted |
| [0003](0003-reconciliation-canonical-entity-precedence.md) | Reconciliation: canonical entity with per-field precedence and provenance | Accepted |
| [0004](0004-graph-native-model-with-otel-openinference-mappers.md) | Graph-native model with OTel/OpenInference boundary mappers | Accepted |
| [0005](0005-run-model-forest-session-anchor-env-runid.md) | Run model: persistent forest, `session_id` anchor, env wrapper run-id | Accepted |
| [0006](0006-embedded-sqlite-durable-store.md) | Embedded SQLite as the default durable store | Accepted |
| [0007](0007-export-materialized-upsert-cdc-snapshot.md) | Export: materialized graph (upsert + CDC) and snapshot | Accepted |
| [0008](0008-payload-storage-hash-redaction.md) | Payload handling: store with hash and redaction | Accepted |

Design spec: [`../specs/2026-06-20-catacomb-design.md`](../specs/2026-06-20-catacomb-design.md).

## Format

`NNNN-kebab-title.md` with sections: Status · Date · Deciders · Related · Context · Decision · Alternatives considered · Consequences. New decisions get the next number; superseded ones are marked `Superseded by ADR-NNNN` rather than deleted.
