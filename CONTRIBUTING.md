# Contributing

Read **[AGENTS.md](AGENTS.md)** first — it is the contributor and agent guide. The repo
runs under a 100%-test-coverage, TDD-first gate with no comments in Go code.

- Open a GitHub issue for each task before branching; link it from the PR with `Closes #N`. Automated and hotfix PRs are exempt.
- Branch from `master`: `git checkout -b <type>/<short-desc>`. One PR = one logical change.
- `make cover lint fmt` must pass locally. CI must be green before merge (squash).
- Never commit secrets. See [SECURITY.md](SECURITY.md) to report vulnerabilities.
