# Catacomb

Real-time execution-graph observability for [Claude Code](https://www.anthropic.com/claude-code) agentic pipelines.

Catacomb runs as a sidecar daemon next to a Claude Code instance and captures everything it does — hooks, subagent allocation, tool calls, MCP calls — from four signal sources (hooks, native OpenTelemetry, `stream-json`, transcript JSONL). It reconciles them into one canonical **action graph**, persists it to embedded SQLite, serves it live (WebSocket/SSE, gRPC, an embedded web UI), and exports it as a materialized graph to `jsonl`, OTLP/OpenInference, `neo4j`, and `postgres`.

It is domain- and evaluation-agnostic: it builds a faithful, queryable graph and leaves a per-node annotation slot for downstream tooling to attach its own metadata.

> **Status:** early development. The design is settled; implementation is in progress (milestone M0.1).

## Documentation

- Design spec → [`docs/specs/2026-06-20-catacomb-design.md`](docs/specs/2026-06-20-catacomb-design.md)
- Architecture decisions (ADRs) → [`docs/adr/`](docs/adr/)
- Implementation plans → [`docs/plans/`](docs/plans/)
- Contributor & agent guide → [`AGENTS.md`](AGENTS.md)

## Development

```
make build   # build bin/catacomb
make test    # tests with -race + coverage profile
make cover   # enforce the 100% coverage gate
make lint    # golangci-lint
```

## License

[Apache-2.0](LICENSE).
