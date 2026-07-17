# Catacomb documentation

New here? Start with the [README tutorial](../README.md#-tutorial), then dive
into the guide.

- **[User guide](guide/README.md)** — concepts, workflows, CLI reference,
  configuration, ingestion, privacy and operations.
- **[Basket schema](guide/basket.md)** — every field you can put in a basket file.
- **[Troubleshooting](guide/troubleshooting.md)** — symptoms and fixes.
- **[Architecture decisions](adr/README.md)** — the ADR log.
- **[Evaluation brief](PITCH.md)** — the case for a statistical gate: science,
  methodology, competitive landscape, roadmap (for technical leadership).

`internal/` holds **historical** development material — plans, specs, reviews, and
agent tooling accumulated while catacomb was built, including designs for a superseded
daemon architecture (see [ADR-0026](adr/0026-form-factor-pivot-offline-eval-gate.md)).
None of it is needed to use catacomb; read it only for project archaeology.
