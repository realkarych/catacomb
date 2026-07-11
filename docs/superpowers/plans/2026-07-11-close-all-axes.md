# Close-All-Axes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the five critical-analysis gap axes — CI security scanning, release integrity, governance/agent files, redaction hardening, and subprocess timeout/cancellation — under the repo's TDD + 100%-coverage + no-comments gates.

**Architecture:** Five independent, file-disjoint axes, each on its own worktree and squash PR. Axis C (governance + model policy) lands first; A/B/D/E then run in parallel. Go axes (D, E) are TDD; config/docs axes (A, B, C) are validated by dry-runs (`goreleaser --snapshot`, `actionlint`/`zizmor`, `gitleaks`, `markdownlint`, codepolicy).

**Tech Stack:** Go 1.26 (stdlib-first, pure-Go no-cgo), GitHub Actions, GoReleaser v2, cosign, syft, gitleaks, CodeQL, govulncheck, gosec, actionlint, zizmor.

## Global Constraints

- **No comments in Go.** Only `//go:build`, `//go:embed`, `//go:generate` allowed. Enforced by `internal/codepolicy`. Never add `//nolint`.
- **100% test coverage** outside `.testcoverage.yml` exclusions; the threshold never drops. TDD: failing test first.
- **`go test -race`**; table-driven; `testify/require`/`assert`. No `time.Sleep` in tests (forbidigo) — use deadlines/channels/`testing/synctest`.
- **Formatting:** `gofumpt` (extra-rules) + `goimports` local-prefix `github.com/realkarych/catacomb`.
- **Dependency inversion:** consumer declares the interface; no reaching across packages for concrete structs. No new non-stdlib Go deps in D/E.
- **Errors:** sentinels via `errors.Is`/`As`; wrap `fmt.Errorf("pkg.Op: %w", err)`.
- **Model policy for subagents (this effort):** Fable for implementation, deep review, verification, decisions; Opus for read-only scouting and mechanical edits. Passed in-prompt to every subagent (AGENTS.md still says opus-only until Axis C lands).
- **Worktree paths:** every file write uses the axis worktree's absolute path, never the shared checkout `/Users/karych/src/catacomb/...`.
- **One PR = one logical change; squash merge; CI green before merge.**

---

## AXIS C — Governance & agent files (LANDS FIRST)

Branch: `chore/governance-model-policy`. No Go behavior change. Validate: `markdownlint '**/*.md'`, JSON validity, `go test ./internal/codepolicy/`.

### Task C1: Model-policy section in AGENTS.md

**Files:**

- Modify: `AGENTS.md` (the "Agent models: opus only" section, ~lines 17-19)

- [ ] **Step 1:** Replace the section titled `## Agent models: opus only` and its paragraph with:

```markdown
## Agent models: Fable for quality, Opus for mechanical

**Fable is the strongest model here — put it on everything where quality and
decisions matter:** all Go implementation (TDD), deep code review, verification, and
architectural decisions, in the main context window and every dispatched subagent.
**Opus handles the cheap, low-stakes work:** read-only repository scouting/exploration
and mechanical edits (for example, pinning Actions to commit SHAs). `haiku` and
`sonnet` are not used here. The repo's gates (100% coverage, no-comments, gofumpt,
adversarial review) punish weaker models with extra iterations, so the strongest model
carries the gated work. If Fable is momentarily unavailable, wait and retry.
```

- [ ] **Step 2:** `markdownlint AGENTS.md` → expect clean.
- [ ] **Step 3:** Commit: `git commit -m "docs(agents): model policy — Fable for quality/decisions, Opus for mechanical"`

### Task C2: SECURITY.md

**Files:**

- Create: `SECURITY.md`

- [ ] **Step 1:** Create `SECURITY.md`:

```markdown
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
```

- [ ] **Step 2:** `markdownlint SECURITY.md` → clean. Commit.

### Task C3: .claude/settings.json + Stop hook + agent definitions

**Files:**

- Modify: `.claude/settings.json` (currently `{}`)
- Create: `.claude/agents/implementer.md`, `.claude/agents/reviewer.md`, `.claude/agents/verifier.md`, `.claude/agents/scout.md`

- [ ] **Step 1:** Write `.claude/settings.json` with a read-only Bash allowlist and a fast Stop hook. Resolve the exact hook schema against the current Claude Code docs (use the claude-code-guide agent or `/hooks` reference) before finalizing; the hook MUST run only fast checks:

```json
{
  "permissions": {
    "allow": [
      "Bash(go build ./...)",
      "Bash(go test ./...)",
      "Bash(go vet ./...)",
      "Bash(gofumpt -l .)",
      "Bash(git status)",
      "Bash(git diff:*)",
      "Bash(git log:*)",
      "Bash(make fmt)",
      "Bash(make lint)"
    ]
  },
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "gofumpt -l . && go test ./internal/codepolicy/ 2>&1 | tail -5" }
        ]
      }
    ]
  }
}
```

- [ ] **Step 2:** Create the four agent definition files. Each is frontmatter + a short body. Example `implementer.md`:

```markdown
---
name: implementer
description: Implements a single plan task TDD-first under the repo gates.
model: fable
---
You implement exactly one plan task. Failing test first, minimal code to green, refactor
under green. No comments in Go (only //go:build|embed|generate). 100% coverage. gofumpt
+ goimports. Wire deps explicitly; consumer declares interfaces. Commit when green.
```

Use `model: fable` for `implementer`, `reviewer`, `verifier`; `model: opus` for `scout` (read-only mapping). Give `reviewer` an adversarial-verification brief, `verifier` a "run make cover lint + go test -race and report evidence" brief, `scout` a "map files/callers/tests, read-only" brief.

- [ ] **Step 3:** Validate `.claude/settings.json` is valid JSON: `python3 -c "import json;json.load(open('.claude/settings.json'))"`. Commit.

### Task C4: Community files

**Files:**

- Create: `CONTRIBUTING.md`, `CODEOWNERS`, `.github/PULL_REQUEST_TEMPLATE.md`, `.github/ISSUE_TEMPLATE/bug_report.md`, `.github/ISSUE_TEMPLATE/feature_request.md`

- [ ] **Step 1:** `CONTRIBUTING.md` (thin, points to AGENTS.md):

```markdown
# Contributing

Read **[AGENTS.md](AGENTS.md)** first — it is the contributor and agent guide. The repo
runs under a 100%-test-coverage, TDD-first gate with no comments in Go code.

- Branch from `master`: `git checkout -b <type>/<short-desc>`. One PR = one logical change.
- `make cover lint fmt` must pass locally. CI must be green before merge (squash).
- Never commit secrets. See [SECURITY.md](SECURITY.md) to report vulnerabilities.
```

- [ ] **Step 2:** `CODEOWNERS`:

```
* @realkarych
```

- [ ] **Step 3:** `.github/PULL_REQUEST_TEMPLATE.md` and the two issue templates — short, conventional. Bug report: description / repro / expected vs actual / version. Feature request: use case / proposed behavior.
- [ ] **Step 4:** `markdownlint '**/*.md' --ignore node_modules` → clean. Commit.

### Task C5 (orchestrator, outside repo): memory rewrite

Not a repo change. The orchestrator rewrites `~/.claude/projects/-Users-karych-src-catacomb/memory/subagent-models-opus-only.md` to the new policy and updates the `MEMORY.md` pointer. Done by the main window directly, not a subagent.

---

## AXIS A — Security scanning in CI

Branch: `ci/security-scanners`. Validate: `actionlint`, `zizmor`, `gitleaks detect` (with allowlist), `govulncheck ./...`. Owns all workflows EXCEPT `publish.yml` (Axis B).

### Task A1: gitleaks config + job

**Files:**

- Create: `.gitleaks.toml`
- Create: `.github/workflows/security.yml` (gitleaks job first; more jobs added in A2/A3)

- [ ] **Step 1:** Create `.gitleaks.toml` extending the default and allowlisting fixture secrets:

```toml
title = "catacomb gitleaks config"

[extend]
useDefault = true

[allowlist]
description = "Test fixtures and redaction tables intentionally contain fake secrets"
paths = [
  '''redact/.*_test\.go''',
  '''redact/testdata/.*''',
  '''reduce/testdata/.*''',
  '''regress/testdata/.*''',
  '''bench/testdata/.*''',
  '''cmd/catacomb/testdata/.*''',
  '''e2e/.*''',
]
```

- [ ] **Step 2:** Verify locally if gitleaks is installed (`gitleaks detect --no-git -c .gitleaks.toml`); if it flags real tracked secrets, STOP and report. Expected: no findings.
- [ ] **Step 3:** Add the gitleaks job to `security.yml` (`gitleaks/gitleaks-action`), `permissions: contents: read`. Commit.

### Task A2: govulncheck + gosec jobs

- [ ] **Step 1:** Add a `govulncheck` job: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` on `go-version-file: go.mod`, `permissions: contents: read`.
- [ ] **Step 2:** Add a `gosec` job (`securego/gosec` action or `go run github.com/securego/gosec/v2/cmd/gosec@latest ./...`). If it flags existing code, triage: fix true positives in their own follow-up within this axis, or add a `.gosec.json` exclusion for justified false positives (config only, never inline `//nolint`).
- [ ] **Step 3:** `actionlint .github/workflows/security.yml` → clean. Commit.

### Task A3: CodeQL + dependency-review + actionlint + zizmor jobs

- [ ] **Step 1:** Add CodeQL: `github/codeql-action/init` (languages: go) → `autobuild` → `analyze`. `permissions: security-events: write, contents: read`.
- [ ] **Step 2:** Add `dependency-review` job gated on `pull_request` (`actions/dependency-review-action`).
- [ ] **Step 3:** Add an `actionlint` + `zizmor` job that lints all workflow files.
- [ ] **Step 4:** `actionlint` + `zizmor` over `.github/workflows/*` → clean. Commit.

### Task A4: Pin third-party Actions to commit SHAs

**Files:**

- Modify: `.github/workflows/ci.yml`, `.github/workflows/e2e-live.yml`, `.github/workflows/python-deepeval.yml`, `.github/workflows/security.yml`

- [ ] **Step 1:** For each third-party `uses:` at a version tag, resolve the tag → commit SHA (mechanical, Opus): `gh api repos/<owner>/<repo>/git/refs/tags/<tag> --jq .object.sha` (deref annotated tags with a second call to `.object.url`). Replace `uses: owner/repo@vX.Y.Z` with `uses: owner/repo@<sha> # vX.Y.Z`.
- [ ] **Step 2:** `actionlint` over all four files → clean.
- [ ] **Step 3:** Confirm `dependabot.yml` `github-actions` ecosystem is present (it is) so pins get bumped. Commit.

---

## AXIS B — Release integrity via GoReleaser (full replacement)

Branch: `ci/goreleaser-release-integrity`. Owns `publish.yml`. Validate: `goreleaser check` + `goreleaser release --snapshot --clean`, `actionlint`. Resolve exact GoReleaser v2 config syntax against current docs via context7 (`goreleaser` library) before finalizing — the keys below are the required feature set, not verbatim syntax.

### Task B1: .goreleaser.yaml

**Files:**

- Create: `.goreleaser.yaml`

- [ ] **Step 1:** Author `.goreleaser.yaml` covering, at minimum:
  - `builds`: main `./cmd/catacomb`, `env: [CGO_ENABLED=0]`, `flags: [-trimpath]`, `ldflags: -s -w -X main.Version={{.Version}}`, `goos: [linux, darwin, windows]`, `goarch: [amd64, arm64]`.
  - `archives`: tar.gz for unix, zip for windows; name template matching current `${TAG}_catacomb_${GOOS}_${GOARCH}`.
  - `checksum`: `checksums.txt`.
  - `sboms`: syft, artifacts: archive.
  - `signs` (cosign keyless): sign the checksum; `cmd: cosign`, `env: [COSIGN_EXPERIMENTAL=1]` or the current keyless recipe.
  - `nfpms`: deb, amd64+arm64, maintainer/license/homepage.
  - `brews`: repository `realkarych/homebrew-tap`, install `bin.install "catacomb"`, test `system "#{bin}/catacomb", "version"`.
  - `dockers` + `docker_manifests`: multi-arch `ghcr.io/realkarych/catacomb:{{.Tag}}` and `:latest`, label `org.opencontainers.image.source`.
  - `docker_signs` (cosign) for the image.
- [ ] **Step 2:** `goreleaser check` → valid config.
- [ ] **Step 3:** `goreleaser release --snapshot --clean` → build succeeds; assert `dist/` contains: 6 archives, `checksums.txt`, `*.sbom`/SBOM files, `.deb` packages, rendered Docker images. (Signing may be skipped in snapshot; verify the `signs`/`docker_signs` blocks pass `goreleaser check`.)
- [ ] **Step 4:** Commit `.goreleaser.yaml`.

### Task B2: Rewrite publish.yml

**Files:**

- Modify: `.github/workflows/publish.yml` (replace build/homebrew/docker jobs with a GoReleaser job; keep a slim APT-publish job)

- [ ] **Step 1:** Replace the matrix build + archive + homebrew + docker jobs with a single `goreleaser` job:
  - `permissions: { contents: write, packages: write, id-token: write }` (id-token for cosign keyless + attestations).
  - checkout (full history: `fetch-depth: 0`), setup-go, docker login to ghcr, cosign install, syft install (or rely on GoReleaser-managed), then `goreleaser/goreleaser-action` `release --clean`.
  - env: `GITHUB_TOKEN`, `HOMEBREW_TOKEN` (tap), `GHCR` creds.
  - Pin the GoReleaser action to a commit SHA (consistency with Axis A).
- [ ] **Step 2:** Keep a slim `update-apt` job that `needs: goreleaser`, downloads `dist/*.deb` from the release (or an uploaded artifact) and runs the existing aptly + GPG publish into `catacomb-apt@gh-pages` verbatim (that custom repo is out of GoReleaser's scope).
- [ ] **Step 3:** `actionlint .github/workflows/publish.yml` → clean. Commit.

---

## AXIS D — Redaction hardening (Go, TDD)

Branch: `feat/redaction-hardening`. Package `redact`. Owns `README.md` (privacy claim). 100% coverage, `-race`, fuzz. RE2 keeps all patterns linear-time.

### Task D1: Shannon-entropy helper

**Files:**

- Modify: `redact/redact.go` (add `shannonEntropy`, import `math`)
- Test: `redact/redact_test.go` (or a focused `redact/entropy_test.go`)

**Interfaces:**

- Produces: `func shannonEntropy(s string) float64` — bits/char; `0` for empty.

- [ ] **Step 1: Write failing test**

```go
func TestShannonEntropy(t *testing.T) {
	require.Equal(t, 0.0, shannonEntropy(""))
	require.Equal(t, 0.0, shannonEntropy("aaaa"))
	require.InDelta(t, 1.0, shannonEntropy("abab"), 1e-9)
	require.Greater(t, shannonEntropy("deadbeefcafe1234deadbeefcafe1234"), 3.2)
	require.Less(t, shannonEntropy("thisisalonglowercaseenglishlooking"), 4.0)
}
```

- [ ] **Step 2:** Run `go test ./redact/ -run TestShannonEntropy -v` → FAIL (undefined).
- [ ] **Step 3: Implement**

```go
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}
```

- [ ] **Step 4:** Run → PASS. **Step 5:** Commit.

### Task D2: Vendor prefix rules

**Files:**

- Modify: `redact/redact.go` (add regex vars + entries to `valueRules`)
- Test: `redact/redact_test.go`

**Interfaces:**

- Produces: new reasons `"stripe-key"`, `"sendgrid-key"`, `"twilio-key"`, `"npm-token"`, `"pypi-token"`, `"gitlab-token"`, `"google-oauth"`. These auto-register in `knownPlaceholders` (built from `valueRules`).

- [ ] **Step 1: Write failing table test** asserting each secret is replaced by its placeholder and a benign lookalike is not:

```go
func TestVendorPrefixRules(t *testing.T) {
	cases := []struct{ in, reason string }{
		{`sk_live_` + strings.Repeat("A1b2", 6), "stripe-key"},
		{`SG.` + strings.Repeat("a", 22) + `.` + strings.Repeat("b", 22), "sendgrid-key"},
		{`SK` + strings.Repeat("0", 32), "twilio-key"},
		{`npm_` + strings.Repeat("x", 36), "npm-token"},
		{`pypi-` + strings.Repeat("y", 20), "pypi-token"},
		{`glpat-` + strings.Repeat("z", 20), "gitlab-token"},
		{`ya29.` + strings.Repeat("w", 30), "google-oauth"},
	}
	for _, c := range cases {
		got := string(Redact([]byte(`{"v":"` + c.in + `"}`)).Data)
		require.Contains(t, got, placeholder(c.reason), c.in)
	}
	clean := string(Redact([]byte(`{"v":"just-a-normal-value"}`)).Data)
	require.Equal(t, `{"v":"just-a-normal-value"}`, clean)
}
```

- [ ] **Step 2:** Run → FAIL. **Step 3: Implement** — add regex vars and prepend/append to `valueRules` (order: vendor-specific before generic entropy). Suggested patterns (tighten during review):

```go
reStripeKey  = regexp.MustCompile(`\b[rsp]k_(?:live|test)_[0-9A-Za-z]{16,}\b`)
reSendGrid   = regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\b`)
reTwilioKey  = regexp.MustCompile(`\bSK[0-9a-fA-F]{32}\b`)
reNPMToken   = regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)
rePyPIToken  = regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{16,}\b`)
reGitLabPAT  = regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)
reGoogleOAuth = regexp.MustCompile(`\bya29\.[A-Za-z0-9._-]{20,}\b`)
```

Add to `valueRules` with their reasons.

- [ ] **Step 4:** Run → PASS. **Step 5:** `make cover` for redact → 100%. Commit.

### Task D3: Entropy-gated generic detection

**Files:**

- Modify: `redact/redact.go` (introduce `entropyRules`, gate matching by `shannonEntropy`; lower length threshold 40→32; add base64url charset)
- Test: `redact/redact_test.go`

**Interfaces:**

- Produces: entropy-gated redaction for `"high-entropy"`; a 32+ char high-entropy hex/base64/base64url span is redacted, a 32+ char low-entropy span (repeated/dictionary) is NOT.

- [ ] **Step 1: Write failing tests** for both recall (short-but-high-entropy now caught) and precision (long-but-low-entropy not caught):

```go
func TestEntropyGatedDetection(t *testing.T) {
	secret := "a1B2c3D4e5F6g7H8j9K0mN1pQ2rS3tU4"
	got := string(Redact([]byte(`{"v":"` + secret + `"}`)).Data)
	require.Contains(t, got, placeholder("high-entropy"))

	lowEntropy := strings.Repeat("ab", 20)
	clean := string(Redact([]byte(`{"v":"` + lowEntropy + `"}`)).Data)
	require.Equal(t, `{"v":"`+lowEntropy+`"}`, clean)
}
```

- [ ] **Step 2:** Run → FAIL. **Step 3: Implement** — replace `reHexEntropy`/`reBase64Entropy` usage with an entropy-gated pass. Define:

```go
type entropyRule struct {
	re      *regexp.Regexp
	reason  string
	minBits float64
}

var entropyRules = []entropyRule{
	{regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`), "high-entropy", 3.2},
	{regexp.MustCompile(`\b[A-Za-z0-9+/_-]{32,}={0,2}\b`), "high-entropy", 4.0},
}
```

Gate in `replaceSecretSpans` via `ReplaceAllStringFunc`, redacting a match only when `shannonEntropy(match) >= minBits`; likewise gate the single-value classifier used in the sensitive-key path. Keep vendor `valueRules` always-on. Preserve `knownPlaceholders`/`isTypedRefValue` idempotence handling.

- [ ] **Step 4:** Run full `go test ./redact/ -race` → PASS. **Step 5:** `make cover` redact → 100% (add negative/positive rows until every branch of the gate is covered). Commit.

### Task D4: Fuzz target

**Files:**

- Create: `redact/redact_fuzz_test.go`

- [ ] **Step 1: Write the fuzz target** (properties: no panic; idempotence; injected known secret never survives):

```go
func FuzzRedact(f *testing.F) {
	f.Add([]byte(`{"token":"sk_live_ABCDEFGHIJKLMNOP0123"}`))
	f.Add([]byte("plain text AKIAABCDEFGHIJKLMNOP"))
	f.Add([]byte{0xff, 0xfe, 0x00})
	f.Fuzz(func(t *testing.T, in []byte) {
		r1 := Redact(in)
		r2 := Redact(r1.Data)
		require.Equal(t, r1.Data, r2.Data)
	})
}
```

- [ ] **Step 2:** Run `go test ./redact/ -run FuzzRedact` (seed corpus) → PASS. **Step 3:** `go test ./redact/ -fuzz=FuzzRedact -fuzztime=30s` locally → no crashes. (Fuzzing is not part of the coverage/CI gate, mirroring `make fuzz`.)
- [ ] **Step 4:** Commit.

### Task D5: Soften README + guide privacy claim

**Files:**

- Modify: `README.md` (Privacy section; the "no artifact catacomb writes encodes a raw secret" sentence)
- Modify: `docs/guide/privacy-and-operations.md` (matching phrasing if present)

- [ ] **Step 1:** Change the absolute guarantee to best-effort framing, e.g.: "The transcript copies it stores as evidence pass through secret redaction on the write path (ADR-0024) — a denylist that reduces the blast radius of leaked secrets by replacing API keys, tokens, private keys, connection strings, and high-entropy values with typed markers before they touch disk. It is best-effort, not a guarantee: no denylist catches every secret."
- [ ] **Step 2:** `markdownlint README.md docs/guide/privacy-and-operations.md` → clean. Commit.

---

## AXIS E — Subprocess timeout/cancellation (Go, TDD)

Branch: `fix/subprocess-timeout`. Packages `cmd/catacomb` + `bench`. 100% coverage, `-race`.

### Task E1: Thread context into runChildLocal

**Files:**

- Modify: `cmd/catacomb/childlocal.go:71` (`runChildLocal` signature + `exec.CommandContext`)
- Modify: `cmd/catacomb/childlocal_test.go` (3 callers at lines 45, 59, 71; add cancellation + timeout tests)

**Interfaces:**

- Produces: `func runChildLocal(ctx context.Context, stdout, stderr io.Writer, args []string, dir string, extraEnv []string, observe func(line []byte)) error`.
- Consumes (test): `execCommand` stays `exec.Command`; switch to `exec.CommandContext(ctx, args[0], args[1:]...)` via a new `execCommandContext = exec.CommandContext` var for injectability.

- [ ] **Step 1: Write failing tests:**

```go
func TestRunChildLocalCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runChildLocal(ctx, io.Discard, io.Discard, []string{"sleep", "10"}, "", nil, func([]byte) {})
	require.Error(t, err)
}

func TestRunChildLocalTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := runChildLocal(ctx, io.Discard, io.Discard, []string{"sleep", "10"}, "", nil, func([]byte) {})
	require.Error(t, err)
}
```

(If `sleep` is not portable in CI/Windows, inject `execCommandContext` with a fake in tests to avoid a real process; prefer the injection approach to satisfy the no-`time.Sleep` rule and Windows matrix.)

- [ ] **Step 2:** Run → FAIL (signature mismatch). **Step 3: Implement** — add `ctx` first param, `child := execCommandContext(ctx, args[0], args[1:]...)`, keep the rest. Update the 3 existing test callers to pass `context.Background()`.
- [ ] **Step 4:** Run `go test ./cmd/catacomb/ -run RunChildLocal -race` → PASS. **Step 5:** Commit.

### Task E2: bench Task.Timeout schema + validation

**Files:**

- Modify: `bench/basket.go` (`Task` struct; `validateTasks`; new `ErrTimeout`; `parseTimeout` helper)
- Test: `bench/basket_test.go`

**Interfaces:**

- Produces: `Task.Timeout string` (yaml `timeout,omitempty`); `func (t Task) TimeoutDuration() (time.Duration, error)` returning `0, nil` when unset; `ErrTimeout` sentinel.

- [ ] **Step 1: Write failing tests:** valid `"30s"` parses to `30*time.Second`; empty → `0`; `"-1s"` → `ErrTimeout`; garbage → `ErrTimeout`; a basket with `timeout: "banana"` fails `Load`.
- [ ] **Step 2:** Run → FAIL. **Step 3: Implement** — add the field, a `parseTimeout(string) (time.Duration, error)` wrapping `time.ParseDuration` (reject negatives), call it in `validateTasks`, add `ErrTimeout`. Import `time`.
- [ ] **Step 4:** Run `go test ./bench/ -race` → PASS. **Step 5:** `make cover` bench → 100%. Commit.

### Task E3: Wire ctx + per-cell timeout in bench

**Files:**

- Modify: `cmd/catacomb/bench.go:165` (`runBenchCellOffline` gains `ctx`; wrap with `context.WithTimeout` when the task sets one) and the RunE loop that calls it (thread `cmd.Context()`)
- Test: `cmd/catacomb/bench_offline_test.go`

**Interfaces:**

- Consumes: `runChildLocal(ctx, ...)` (E1); `cell.Task.TimeoutDuration()` (E2).
- Produces: `runBenchCellOffline(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, ...)`.

- [ ] **Step 1: Write failing test** — a cell whose task has `Timeout: "1ms"` running a long child yields a non-zero exit / timeout note in the manifest entry (inject `execCommandContext` fake). Assert the deadline path is taken.
- [ ] **Step 2:** Run → FAIL. **Step 3: Implement** — add `ctx` param; inside, `d, _ := cell.Task.TimeoutDuration(); if d > 0 { var c func(); ctx, c = context.WithTimeout(ctx, d); defer c() }`; pass `ctx` to `runChildLocal`. Thread `cmd.Context()` from the bench RunE through the cell loop to `runBenchCellOffline`.
- [ ] **Step 4:** Run `go test ./cmd/catacomb/ -race` → PASS. **Step 5:** `make cover` → 100%; `make lint`. Commit.

---

## Self-review coverage map

- Spec §3 (Axis A) → Tasks A1–A4. §4 (Axis B) → B1–B2. §5 (Axis C) → C1–C5. §6 (Axis D) → D1–D5. §7 (Axis E) → E1–E3.
- Spec §2.1 model policy → C1 (AGENTS.md) + C3 (agent defs) + C5 (memory).
- Spec §8 execution architecture → orchestration (subagent-driven-development), not a code task.
- Spec §11 out-of-scope (SQLite, gitleaks corpus) → no tasks, correct.

## Execution notes for the orchestrator

- Land Axis C first (memory + AGENTS.md authoritative), then fan out A/B/D/E in parallel worktrees.
- Per axis: Opus scout (read-only map) → Fable implementer (TDD) → parallel Fable reviewers (correctness / security / coverage-honesty / repo-policy) with adversarial verification → Fable fixer → Fable verifier (`make cover lint` + `go test -race` + behavioral/dry-run) → orchestrator gates the PR.
- Final cross-axis integration review on `master` after all five merge.
