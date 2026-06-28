# Claude Code instructions

The agent and contributor guide for this repository is **[AGENTS.md](AGENTS.md)**. Read it before making any change.

Three rules that are easy to forget and expensive to get wrong:

- **Always work in a git worktree** — multiple agents work on this repository in parallel, so never edit the shared checkout directly. Create an isolated worktree for your changes before touching any file.
- **No comments in Go code** — none, not even doc comments. The only allowed comments are the `//go:build`, `//go:embed`, and `//go:generate` directives; files carrying the standard `// Code generated … DO NOT EDIT.` header are skipped wholesale. Enforced by `internal/codepolicy`.
- **100% test coverage**, TDD-first. The threshold never goes down.
