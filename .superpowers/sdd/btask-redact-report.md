# Task redact — Implementation Report

## API as Built

```go
package redact

type Finding struct {
    Path   string
    Reason string
}

type Result struct {
    Data     []byte
    Findings []Finding
    Redacted bool
}

func Redact(raw []byte) Result
```

Matches the brief exactly. `Redacted` is `len(Findings) > 0` (computed, not a separate bool tracked separately).

## Files

- `redact/redact.go` — pure implementation, no I/O, no globals beyond `var` regex/pattern slices
- `redact/redact_test.go` — 100% table-driven coverage

## Value-Scan Patterns (reason strings)

| Reason | Pattern | Notes |
|---|---|---|
| `connection-string` | `scheme://user:pass@host` URI with embedded credentials | Requires user:pass@ |
| `aws-key` | `AKIA[0-9A-Z]{16}` | Exact AWS access key shape |
| `github-token` | `gh[pousr]_[A-Za-z0-9]{36,}` | ghp/gho/ghu/ghs/gpr prefixes |
| `github-token` | `github_pat_[A-Za-z0-9_]{40,}` | Fine-grained PATs |
| `openai-key` | `sk-[A-Za-z0-9-]{20,}` | Covers sk-, sk-ant- (Anthropic), sk-proj- etc. |
| `slack-token` | `xox[baprs]-[A-Za-z0-9-]{10,}` | All Slack token types |
| `pem-private-key` | `-----BEGIN ... PRIVATE KEY-----` | RSA, EC, generic PRIVATE KEY |
| `google-api-key` | `AIza[A-Za-z0-9_-]{35,}` | Google API key shape |
| `bearer-token` | `Bearer <10+ chars>` | HTTP Bearer auth header values |
| `jwt` | `eyJ<seg>.<seg>.<seg>` | Three-part base64url JWT |
| `high-entropy` | `[0-9a-fA-F]{40,}` | Long hex runs (SHA hashes, tokens) |
| `high-entropy` | `[A-Za-z0-9+/]{40,}` | Long base64 runs |

## Key-Glob Patterns (reason: `sensitive-key`)

Case-insensitive exact matches: `password`, `passwd`, `secret`, `token`, `api_key`, `api-key`, `apikey`, `authorization`, `auth`, `access_token`, `access-token`, `refresh_token`, `refresh-token`, `client_secret`, `client-secret`, `private_key`, `private-key`, `credential`, `credentials`, `session_key`, `session-key`.

For sensitive keys: value-scan runs first; if a value-scan rule matches, that reason is used instead of `sensitive-key`. This means `{"token": "ghp_..."}` reports `github-token`, not `sensitive-key`.

## Behavior

- Empty input → `Result{Data: raw}`, no findings
- Non-UTF-8 binary → `"‹binary:len,sha256-prefix›"` string, one `{Path:"", Reason:"binary"}` finding
- Valid JSON → recursive walk; values redacted by value-scan or key-glob; re-marshaled (key order not preserved — `json.Marshal` of `map[string]any` sorts keys alphabetically)
- Non-JSON UTF-8 → free-text value-scan; redacted strings replaced in place
- Findings sorted by `Path` ascending
- Deterministic: same input → same output + findings

## Coverage

```
redact/redact.go: matchValueRule  100.0%
redact/redact.go: Redact          100.0%
redact/redact.go: redactFreeText  100.0%
redact/redact.go: walkNode        100.0%
redact/redact.go: walkObject      100.0%
redact/redact.go: walkArray       100.0%
redact/redact.go: redactStringValue 100.0%
redact/redact.go: isSensitiveKey  100.0%
redact/redact.go: joinPath        100.0%
total:                            100.0%
```

`make cover` → `Total coverage threshold (100%) satisfied: PASS`

## Known Gaps / Deliberate Exclusions

- **High-entropy false positives**: SHA256 hashes (64 hex chars) are flagged as `high-entropy`. This is intentional — conservative over-redaction is preferred. The threshold (40 chars) balances UUIDs (36 chars, excluded) vs. real tokens.
- **JSON key order not preserved**: `map[string]any` unmarshal loses insertion order. Re-marshaled output has alphabetically sorted keys. Acceptable for a viewing surface per the brief.
- **Value-only redaction for non-string JSON values**: `{"password": 12345}` is not redacted. Numeric/boolean/null values on sensitive keys are not secrets. Only string values are redacted by key-glob.
- **Full ADR-0020 surface not implemented**: Only payload Input/Output is covered. `name`/`attrs`/`subagent_type` redaction is a documented follow-up (pre-existing ADR gap).
- **Post-redaction hashing**: ADR-0020 §3 requires post-redaction hashing. The `PayloadHash` on `model.Node` is pre-redaction. B-be1 will surface this as-is (integrity reference only); post-redaction hashing is a documented follow-up.
- **Bearer-token near-miss**: `"Bearer "` (trailing space, no token) correctly does not match due to `{10,}` length requirement.
