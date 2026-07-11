# Security Policy

## Reporting a vulnerability

Please do **not** open a public issue for security vulnerabilities. Report privately
via GitHub's [private vulnerability reporting](https://github.com/realkarych/catacomb/security/advisories/new)
or directly to the maintainer on Telegram: [`@karych`](https://t.me/karych). You will
get an acknowledgement within a few days.

## Supported versions

Security fixes target the latest released minor version. Older versions are best-effort.

## Baskets are executable code

`catacomb bench` runs the `cmd` and `setup` steps declared in a basket as local
processes with your environment. **A basket is code — run only baskets you trust.** Do
not run untrusted baskets, and treat committed baskets in CI with the same scrutiny as
any other executable in the pipeline.

## Redaction is best-effort

Evidence written by catacomb passes through secret redaction (ADR-0024), which reduces
the blast radius of leaked secrets but is a denylist and cannot guarantee zero secrets.
See the Privacy section of the README.
