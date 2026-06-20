# Claude Code instructions

The agent and contributor guide for this repository is **[AGENTS.md](AGENTS.md)**. Read it before making any change.

Two rules that are easy to forget and expensive to get wrong:

- **No comments in Go code** — none, not even doc comments (only `//go:` directives are allowed). Enforced by `internal/codepolicy`.
- **100% test coverage**, TDD-first. The threshold never goes down.
