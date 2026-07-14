# First-touch improvements — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the four trust leaks the first-touch audit found — release-channel drift, two silent basket path traps, README retention gaps, and a docs tree that reads as a workshop.

**Architecture:** Four workstreams shipped as four PRs. WS1 makes publishing zero-touch and adds a channel watchdog + `go install` version stamping. WS2 fixes the basket path contract in the loader (0.MINOR) plus load/report UX. WS3 amends the README tutorial page. WS4 splits internal docs out and adds a troubleshooting page + landing index.

**Tech Stack:** Go 1.26 (cobra CLI, `gopkg.in/yaml.v3`, testify), GitHub Actions (bash + `gh api`), goreleaser, Markdown docs.

## Global Constraints

- **Worktree isolation:** every task runs in its own git worktree; never edit the shared checkout. (CLAUDE.md rule)
- **No comments in Go:** none, not even doc comments; only `//go:build`/`//go:embed`/`//go:generate` directives are allowed. Enforced by `internal/codepolicy`. (CLAUDE.md rule)
- **100% test coverage, TDD-first:** the coverage gate never drops. Write the failing test before the implementation.
- **Subagent-driven execution:** one subagent per task, review after each; parallel implementers only for file-disjoint tasks.
- **Path resolution rule (WS2), verbatim for docs and code:** a relative path resolves against the directory containing the basket file, never the process cwd. `dir` is always resolved; every `./`- or `../`-prefixed element of `cmd` and `verify.cmd` is resolved; bare words (`python3`) and absolute paths are left untouched.
- **Two-tier naming:** hook = "Regression testing for Claude Code agents"; technical category = "offline eval gate", introduced once in the README and used consistently in guide + package descriptions.
- **Version bump:** the four PRs merge, then a single **v0.2.0** tag (WS2 is a basket-schema behavior change → 0.MINOR per `docs/VERSIONING.md`); annotate the tag with the migration line.
- **Commit trailer:** end every commit message with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

## PR-1 — WS1: Zero-touch release, channel watchdog, version stamping

Branch: `feat/zero-touch-release`. File-disjoint from all other PRs; runs in parallel.

### Task 1.1: `go install` version fallback

**Files:**
- Modify: `cmd/catacomb/version.go`
- Modify: `cmd/catacomb/run.go:13-14`
- Test: `cmd/catacomb/version_test.go`

**Interfaces:**
- Produces: `versionFromBuild(current string, read func() (*debug.BuildInfo, bool)) string` — returns `current` unchanged unless it equals `"dev"`, in which case it returns `read()`'s `Main.Version` when that is non-empty and not `"(devel)"`, else `"dev"`.

- [ ] **Step 1: Write the failing test** — add to `cmd/catacomb/version_test.go`:

```go
func TestVersionFromBuild(t *testing.T) {
	bi := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	none := func() (*debug.BuildInfo, bool) { return nil, false }

	require.Equal(t, "v1.2.3", versionFromBuild("v1.2.3", bi("v9.9.9")))
	require.Equal(t, "v0.1.1", versionFromBuild("dev", bi("v0.1.1")))
	require.Equal(t, "dev", versionFromBuild("dev", bi("(devel)")))
	require.Equal(t, "dev", versionFromBuild("dev", bi("")))
	require.Equal(t, "dev", versionFromBuild("dev", none))
}
```

Add `"runtime/debug"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/catacomb/ -run TestVersionFromBuild`
Expected: FAIL — `undefined: versionFromBuild`.

- [ ] **Step 3: Implement** — in `cmd/catacomb/version.go`, add the import and function:

```go
package main

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

var Version = "dev"

func versionFromBuild(current string, read func() (*debug.BuildInfo, bool)) string {
	if current != "dev" {
		return current
	}
	info, ok := read()
	if !ok {
		return current
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return current
}
```

(Keep `newVersionCmd` unchanged.)

- [ ] **Step 4: Wire it into `run()`** — in `cmd/catacomb/run.go`, add `"runtime/debug"` to imports and set `Version` before building the root command:

```go
func run(args []string, stdout, stderr io.Writer) int {
	Version = versionFromBuild(Version, debug.ReadBuildInfo)
	root := newRootCmd()
```

- [ ] **Step 5: Run the full command suite** (the fallback is a no-op under `go test`, where `Main.Version` is empty, so existing assertions on `"dev"` still hold):

Run: `go test ./cmd/catacomb/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/catacomb/version.go cmd/catacomb/run.go cmd/catacomb/version_test.go
git commit -m "feat(version): report module version for go install builds

Fall back to debug.ReadBuildInfo().Main.Version when the ldflags
Version is still \"dev\", so go install @vX.Y.Z reports the tag and
evidence/baseline stamps stop recording \"dev\".

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 1.2: `verify-channels` job in publish.yml

**Files:**
- Modify: `.github/workflows/publish.yml` (append a job after `update-apt`)

**Interfaces:**
- Consumes: `goreleaser` job output `tag`.

- [ ] **Step 1: Add the job** at the end of `.github/workflows/publish.yml`:

```yaml
  verify-channels:
    needs: [goreleaser, update-apt]
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read
    env:
      TAG: ${{ needs.goreleaser.outputs.tag }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    steps:
      - name: Assert release assets present
        run: |
          want="checksums.txt"
          assets=$(gh release view "$TAG" -R realkarych/catacomb --json assets -q '.assets[].name')
          echo "$assets"
          for a in "$want"; do
            printf '%s\n' "$assets" | grep -Fq "$a" || { echo "::error::missing asset $a"; exit 1; }
          done
          printf '%s\n' "$assets" | grep -Eq 'catacomb_linux_amd64\.tar\.gz' || { echo "::error::missing linux archive"; exit 1; }
          printf '%s\n' "$assets" | grep -Eq 'checksums\.txt\.sigstore\.json' || { echo "::error::missing sigstore bundle"; exit 1; }

      - name: Assert Homebrew cask matches tag
        run: |
          ver="${TAG#v}"
          for i in $(seq 1 10); do
            got=$(gh api repos/realkarych/homebrew-tap/contents/Casks/catacomb.rb \
              -q '.content' | base64 -d | sed -n 's/^  version "\(.*\)"/\1/p')
            [ "$got" = "$ver" ] && { echo "cask ok: $got"; break; }
            echo "cask has $got, want $ver (retry $i)"; sleep 15
          done
          [ "$got" = "$ver" ] || { echo "::error::cask version $got != $ver"; exit 1; }

      - name: Assert GHCR latest == tag digest
        run: |
          dt=$(gh api "/orgs/realkarych/packages/container/catacomb/versions" \
            -q "[.[] | select(.metadata.container.tags[]? == \"$TAG\")][0].name" 2>/dev/null \
            || gh api "/users/realkarych/packages/container/catacomb/versions" \
            -q "[.[] | select(.metadata.container.tags[]? == \"$TAG\")][0].name")
          dl=$(gh api "/users/realkarych/packages/container/catacomb/versions" \
            -q "[.[] | select(.metadata.container.tags[]? == \"latest\")][0].name" 2>/dev/null || true)
          echo "tag digest=$dt latest digest=$dl"
          [ -n "$dt" ] && [ "$dt" = "$dl" ] || { echo "::error::ghcr latest ($dl) != $TAG ($dt)"; exit 1; }
```

- [ ] **Step 2: Lint the workflow**

Run: `actionlint .github/workflows/publish.yml && zizmor .github/workflows/publish.yml`
Expected: no errors. (If `actionlint`/`zizmor` are unavailable locally, note it; CI's `security.yml` runs them.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/publish.yml
git commit -m "ci(release): assert channel parity after publish

New verify-channels job fails the release run if the tap cask, GHCR
:latest digest, or release assets do not match the published tag.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 1.3: `channels-watch.yml` scheduled watchdog

**Files:**
- Create: `.github/workflows/channels-watch.yml`

- [ ] **Step 1: Create the workflow**:

```yaml
name: Channels watch

on:
  schedule:
    - cron: '17 6 * * 1'
  workflow_dispatch: {}

permissions: {}

jobs:
  watch:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      contents: read
      issues: write
    env:
      GH_TOKEN: ${{ github.token }}
      REPO: ${{ github.repository }}
    steps:
      - name: Compare channels to latest release
        id: check
        run: |
          tag=$(gh release view -R "$REPO" --json tagName -q .tagName)
          ver="${tag#v}"
          problems=""
          cask=$(gh api repos/realkarych/homebrew-tap/contents/Casks/catacomb.rb \
            -q '.content' | base64 -d | sed -n 's/^  version "\(.*\)"/\1/p')
          [ "$cask" = "$ver" ] || problems="${problems}- Homebrew cask is \`${cask}\`, latest release is \`${ver}\`\n"
          if [ -n "$problems" ]; then
            {
              echo "desync=true"
              echo "body<<EOF"
              echo "Channel drift detected for latest release \`${tag}\`:"
              printf '%b' "$problems"
              echo "EOF"
            } >>"$GITHUB_OUTPUT"
          else
            echo "desync=false" >>"$GITHUB_OUTPUT"
          fi

      - name: Open or update tracking issue
        if: steps.check.outputs.desync == 'true'
        env:
          BODY: ${{ steps.check.outputs.body }}
        run: |
          title="Release channel desync"
          num=$(gh issue list -R "$REPO" --label release-desync --state open \
            --search "$title in:title" --json number -q '.[0].number')
          if [ -n "$num" ]; then
            gh issue comment "$num" -R "$REPO" --body "$BODY"
          else
            gh label create release-desync -R "$REPO" --color b60205 --force || true
            gh issue create -R "$REPO" --label release-desync --title "$title" --body "$BODY"
          fi
```

- [ ] **Step 2: Lint**

Run: `actionlint .github/workflows/channels-watch.yml && zizmor .github/workflows/channels-watch.yml`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/channels-watch.yml
git commit -m "ci: weekly channel-desync watchdog

Compares the Homebrew cask to the latest GitHub release each week and
opens/updates a release-desync issue on drift between releases.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 1.4: Document zero-touch release + the environment change

**Files:**
- Modify: `docs/RELEASING.md`

- [ ] **Step 1: Rewrite the "Cutting a release" section** of `docs/RELEASING.md` to describe the zero-touch flow. Add, verbatim:

```markdown
## Cutting a release

```sh
git tag v0.2.0
git push origin v0.2.0
```

Pushing a `v*.*.*` tag runs `publish.yml` end to end with no manual step:
`verify` refuses the tag unless its commit is an ancestor of `master` and
carries green required checks, then goreleaser publishes every channel, and
`verify-channels` asserts the tap cask, GHCR `:latest` digest, and release
assets all match the tag. A weekly `channels-watch.yml` re-checks the cask
against the latest release and files a `release-desync` issue on drift.

The `release` environment is scoped to `v*.*.*` tag refs and holds the
channel secrets; it has **no** required reviewers, because the automatic
`verify` gate already refuses unqualified tags. Configure the ref policy once:

```sh
gh api -X PUT repos/realkarych/catacomb/environments/release \
  -f 'deployment_branch_policy[protected_branches]=false' \
  -f 'deployment_branch_policy[custom_branch_policies]=true'
gh api -X POST repos/realkarych/catacomb/environments/release/deployment-branch-policies \
  -f 'name=v*.*.*' -f 'type=tag'
gh api -X PUT repos/realkarych/catacomb/environments/release \
  --input - <<'JSON'
{"reviewers":[],"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}
JSON
```
```

- [ ] **Step 2: Commit**

```bash
git add docs/RELEASING.md
git commit -m "docs(releasing): document zero-touch publish and release env policy

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

> **Operational note (not a code task):** applying the `gh api` commands above requires repo-admin rights and is executed by the maintainer alongside PR-1. If the token lacks admin scope, the plan executor surfaces this to the user rather than working around it.

---

## PR-2 — WS2: Basket path contract + load/report UX (0.MINOR)

Branch: `feat/basket-path-contract`. **Depends on PR-3 for `docs/guide/cli.md` and `docs/adr/README.md`** (both edit them): merge PR-3 first, then rebase this PR's doc tasks (2.6 ADR-0029 index row, 2.7 `basket.md`/`cli.md` shrink). The Go work (Tasks 2.1–2.5) is file-disjoint from PR-3 and can proceed in parallel; only the doc tasks (2.6, 2.7) wait.

### Task 2.1: Resolve exec paths against the basket directory

**Files:**
- Modify: `bench/basket.go` (add resolution funcs; call from `decodeBasket`, basket.go:142-158)
- Test: `bench/basket_internal_test.go`

**Interfaces:**
- Produces: `resolveExecPaths(b *Basket, baseDir string)` — mutates `b`: sets `Task.Dir` to `filepath.Join(baseDir, dir)` when `dir` is non-empty and relative; rewrites each `./`/`../`-prefixed element of every `Task.Cmd` and `Task.Verify.Cmd` to `filepath.Join(baseDir, elem)`. Bare words and absolute paths unchanged.
- Consumes: called inside `decodeBasket` with `baseDir = filepath.Dir(path)`, after `validate` succeeds, so both `Load` and `LoadOffline` apply it.

- [ ] **Step 1: Write the failing test** — add to `bench/basket_internal_test.go`:

```go
func TestResolveExecPaths(t *testing.T) {
	b := Basket{
		Tasks: []Task{{
			ID:  "t1",
			Cmd: []string{"./agent.sh"},
			Dir: "work",
			Verify: &Verify{Cmd: []string{"python3", "./verify.py", "--x"}},
		}, {
			ID:  "t2",
			Cmd: []string{"echo", "hi"},
			Dir: "/abs",
		}},
	}
	resolveExecPaths(&b, "/base")

	assert.Equal(t, filepath.Join("/base", "agent.sh"), b.Tasks[0].Cmd[0])
	assert.Equal(t, filepath.Join("/base", "work"), b.Tasks[0].Dir)
	assert.Equal(t, "python3", b.Tasks[0].Verify.Cmd[0])
	assert.Equal(t, filepath.Join("/base", "verify.py"), b.Tasks[0].Verify.Cmd[1])
	assert.Equal(t, "--x", b.Tasks[0].Verify.Cmd[2])
	assert.Equal(t, []string{"echo", "hi"}, b.Tasks[1].Cmd)
	assert.Equal(t, "/abs", b.Tasks[1].Dir)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bench/ -run TestResolveExecPaths`
Expected: FAIL — `undefined: resolveExecPaths`.

- [ ] **Step 3: Implement** — in `bench/basket.go`, add (place near `resolvePatches`):

```go
func resolveExecPaths(b *Basket, baseDir string) {
	for i := range b.Tasks {
		t := &b.Tasks[i]
		if t.Dir != "" && !filepath.IsAbs(t.Dir) {
			t.Dir = filepath.Join(baseDir, t.Dir)
		}
		resolveArgvRel(t.Cmd, baseDir)
		if t.Verify != nil {
			resolveArgvRel(t.Verify.Cmd, baseDir)
		}
	}
}

func resolveArgvRel(argv []string, baseDir string) {
	for i, a := range argv {
		if strings.HasPrefix(a, "./") || strings.HasPrefix(a, "../") {
			argv[i] = filepath.Join(baseDir, a)
		}
	}
}
```

Then call it in `decodeBasket`, immediately after `validate(b)` succeeds and before the `sha256`/return:

```go
	if err := validate(b); err != nil {
		return Basket{}, "", err
	}
	resolveExecPaths(&b, filepath.Dir(path))
	sum := sha256.Sum256(data)
	return b, hex.EncodeToString(sum[:]), nil
```

(`strings` is already imported in basket.go; if not, add it.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./bench/ -run TestResolveExecPaths`
Expected: PASS.

- [ ] **Step 5: Run the full suite and fix fixture fallout** — loading now absolutizes `dir`/`./`-argv, so any test asserting a literal relative `dir`/`cmd` after `Load` must compare against the resolved path.

Run: `go test ./...`
Expected: PASS. Fix any bench/cmd test that asserts a pre-resolution relative path by wrapping the expectation in `filepath.Join(<basketDir>, …)`.

- [ ] **Step 6: Commit**

```bash
git add bench/basket.go bench/basket_internal_test.go
git commit -m "feat(bench): resolve dir and ./ argv against the basket directory

dir (always) and ./ or ../ elements of cmd and verify.cmd now resolve
against the basket file's directory in decodeBasket, so both inline
bench and offline verify find scripts regardless of the process cwd.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2.2: Prove offline verify finds a basket-relative script from any cwd

**Files:**
- Test: `cmd/catacomb/verify_test.go` (or `verifycell_test.go` — match where offline verify is already tested)

**Interfaces:**
- Consumes: `resolveExecPaths` behavior from Task 2.1 (offline verify re-parses via `bench.LoadOffline`, which now resolves).

- [ ] **Step 1: Write the failing test** — an end-to-end offline-verify test where the verifier script lives next to the basket, `verify.cmd` is `["python3", "./verify.py"]` (or a shell stub), the evidence dir is elsewhere, and `catacomb verify` runs with `os.Chdir` set to an unrelated temp dir. Assert the verify record's exit code is success (script was found and ran). Model it on the existing offline-verify test in the package; build the basket + script in `t.TempDir()`, write evidence via the existing helper, then invoke the verify command.

- [ ] **Step 2: Run to verify it fails** on the pre-2.1 code path (it would fail with "script not found"); on top of Task 2.1 it should pass. Run: `go test ./cmd/catacomb/ -run <TestName> -v`.

- [ ] **Step 3: If the test already passes on 2.1**, that is the expected green — this task only adds the regression guard. Ensure it fails when `resolveExecPaths` is reverted (temporarily comment the call, confirm RED, restore).

- [ ] **Step 4: Commit**

```bash
git add cmd/catacomb/verify_test.go
git commit -m "test(verify): offline verify finds basket-relative script from any cwd

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2.3: Human-readable YAML type errors + de-doubled prefix

**Files:**
- Modify: `bench/basket.go` (sentinels basket.go:28-47; decode wrap basket.go:151)
- Test: `bench/basket_test.go`

**Interfaces:**
- Produces: `humanizeDecodeErr(err error) error` — rewrites `*yaml.TypeError` messages, mapping `cannot unmarshal !!X into Y` to `expected <Y-human>, but got <X-human>`, preserving the `line N:` prefix; returns non-TypeError errors unchanged.

- [ ] **Step 1: Write the failing tests** — add to `bench/basket_test.go`:

```go
func TestLoadTypeErrorIsHuman(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
basket: b
reps: 1
tasks:
  - id: t1
    cmd: "echo hi"
variants:
  - id: v1
`), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a list of strings")
	assert.NotContains(t, err.Error(), "!!str")
}

func TestLoadValidationErrorNotDoubled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
basket: b
reps: 0
tasks:
  - id: t1
    cmd: ["echo"]
variants:
  - id: v1
`), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "bench: ")
	assert.ErrorIs(t, err, bench.ErrReps)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./bench/ -run 'TestLoadTypeErrorIsHuman|TestLoadValidationErrorNotDoubled'`
Expected: FAIL (message still contains `!!str` / `bench: `).

- [ ] **Step 3: Implement.** (a) Strip the `bench: ` prefix from every sentinel in basket.go:28-47, e.g. `ErrReps = errors.New("reps must be >= 1")`, `ErrTimeout = errors.New("invalid timeout")`, `ErrEmptyBasketName = errors.New("basket name is empty")`, and so on for all sentinels. (b) Add the humanizer and apply it at the decode wrap (basket.go:151):

```go
	if err := dec.Decode(&b); err != nil {
		return Basket{}, "", fmt.Errorf("bench.Load: %w", humanizeDecodeErr(err))
	}
```

```go
var yamlKindHuman = map[string]string{
	"!!str": "a single value", "!!int": "a number", "!!seq": "a list",
	"!!map": "a mapping", "!!bool": "a true/false value", "!!float": "a number",
}

var goTypeHuman = map[string]string{
	"[]string": "a list of strings", "int": "a whole number",
	"string": "a single value", "map[string]string": "a mapping of strings",
	"[]bench.Task": "a list of tasks", "[]bench.Variant": "a list of variants",
}

func humanizeDecodeErr(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	re := regexp.MustCompile(`cannot unmarshal (\S+) into (\S+)`)
	lines := make([]string, 0, len(te.Errors))
	for _, m := range te.Errors {
		lines = append(lines, re.ReplaceAllStringFunc(m, func(s string) string {
			g := re.FindStringSubmatch(s)
			got, ok1 := yamlKindHuman[g[1]]
			want, ok2 := goTypeHuman[g[2]]
			if !ok1 || !ok2 {
				return s
			}
			return "expected " + want + ", but got " + got
		}))
	}
	return errors.New(strings.Join(lines, "; "))
}
```

Add imports `"errors"`, `"regexp"` if absent.

- [ ] **Step 4: Run the full bench suite** and fix any sentinel-string assertions that expected the old `bench: ` text (use `ErrorIs` / field-substring, which still pass).

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add bench/basket.go bench/basket_test.go
git commit -m "feat(bench): human-readable load errors, no doubled prefix

Translate yaml.v3 type errors into plain language and drop the bench:
prefix from validation sentinels so a load failure reads once.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2.4: Timeout unit hint + single-variant advisory

**Files:**
- Modify: `bench/basket.go` (`parseTimeout`, basket.go:87-96)
- Modify: `cmd/catacomb/bench.go` (`runBench`, after load / before dry-run branch, bench.go:79-82)
- Test: `bench/basket_internal_test.go`, `cmd/catacomb/bench_test.go`

**Interfaces:**
- Consumes: `runBench` already has the cobra `cmd` in scope; use `cmd.ErrOrStderr()` for the advisory.

- [ ] **Step 1: Write the failing timeout test** — add to `bench/basket_internal_test.go`:

```go
func TestParseTimeoutHint(t *testing.T) {
	_, err := parseTimeout("30")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `use a duration with units`)
	assert.ErrorIs(t, err, ErrTimeout)

	_, err = parseTimeout("-5s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./bench/ -run TestParseTimeoutHint`
Expected: FAIL.

- [ ] **Step 3: Implement** — rewrite `parseTimeout` (basket.go:87-96):

```go
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf(`%w: %q (use a duration with units, e.g. "30s")`, ErrTimeout, s)
	}
	if d < 0 {
		return 0, fmt.Errorf("%w: %q must not be negative", ErrTimeout, s)
	}
	return d, nil
}
```

- [ ] **Step 4: Write the failing single-variant advisory test** — add to `cmd/catacomb/bench_test.go` a test that runs `bench --dry-run` on a one-variant basket and asserts stderr contains `1 variant` and `regress needs`. Follow the package's existing pattern for invoking the bench command and capturing stderr.

- [ ] **Step 5: Run to verify it fails**

Run: `go test ./cmd/catacomb/ -run <AdvisoryTestName>`
Expected: FAIL.

- [ ] **Step 6: Implement the advisory** in `runBench`, placed after the basket loads and before the `--dry-run` return (bench.go:79-82), so both dry-run and real runs emit it:

```go
	cells := basket.Cells()
	if len(basket.Variants) == 1 {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"note: basket has 1 variant; bench records evidence, but regress needs >= 2 variants to gate")
	}
	if f.dryRun {
		printDryRun(stdout, cells)
		return nil
	}
```

Confirm `cmd` and `fmt` are in scope in `runBench` (add `"fmt"` if needed).

- [ ] **Step 7: Run tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add bench/basket.go cmd/catacomb/bench.go bench/basket_internal_test.go cmd/catacomb/bench_test.go
git commit -m "feat(bench): timeout unit hint and single-variant advisory

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2.5: Deduplicate the transcript-version warning

**Files:**
- Modify: `cmd/catacomb/offline.go` (`warnVersion`, offline.go:62-67; package var near offline.go:21)
- Test: `cmd/catacomb/offline_test.go`

**Interfaces:**
- Produces: `warnVersion` emits at most one line per distinct observed version; `resetDriftWarnings()` clears the seen-set (test-only — a CLI process runs a single command, so the seen-set naturally starts empty and dedupes across that command's baseline+candidate groups; no production reset needed, so `run.go` is untouched and PR-1/PR-2 stay file-disjoint).

- [ ] **Step 1: Write the failing test** — add to `cmd/catacomb/offline_test.go`:

```go
func TestWarnVersionDedupes(t *testing.T) {
	resetDriftWarnings()
	var buf bytes.Buffer
	old := driftOut
	driftOut = &buf
	defer func() { driftOut = old }()

	high := "999.0.0"
	warnVersion(high)
	warnVersion(high)
	assert.Equal(t, 1, strings.Count(buf.String(), "newer than tested"))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/catacomb/ -run TestWarnVersionDedupes`
Expected: FAIL — two lines (and `resetDriftWarnings` undefined).

- [ ] **Step 3: Implement** — in `cmd/catacomb/offline.go`, add the seen-set and dedupe:

```go
var driftSeen = map[string]struct{}{}

func resetDriftWarnings() { driftSeen = map[string]struct{}{} }

func warnVersion(observed string) {
	if !drift.NewerThanTested(observed) {
		return
	}
	if _, ok := driftSeen[observed]; ok {
		return
	}
	driftSeen[observed] = struct{}{}
	fmt.Fprintf(driftOut, "warning: transcript Claude Code version %s is newer than tested %s\n", observed, drift.TestedClaudeCodeVersion)
}
```

`resetDriftWarnings()` is called only from tests (the test above). Production needs no reset: each CLI invocation is a fresh process whose `driftSeen` starts empty and dedupes across the single command's baseline+candidate resolution. `run.go` is deliberately left untouched so PR-1 and PR-2 stay file-disjoint.

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/offline.go cmd/catacomb/offline_test.go
git commit -m "feat(regress): collapse repeated transcript-version warnings

Emit the newer-than-tested warning once per distinct observed version
instead of once per evidence dir.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2.6: ADR-0029 for the path contract

**Files:**
- Create: `docs/adr/0029-basket-relative-path-resolution.md`
- Modify: `docs/adr/README.md` (index table)

- [ ] **Step 1: Write the ADR** following the repo format (`Status · Date · Deciders · Related · Context · Decision · Alternatives considered · Consequences`). Context: cwd-relative resolution silently broke offline verify (cwd = evidence dir). Decision: resolve `dir` and `./`/`../` argv elements of `cmd`/`verify.cmd` against the basket file's directory, in `decodeBasket`, so inline and offline agree. Alternatives: stamp resolved argv into `meta.json` (rejected — offline verify re-parses the basket, so a stamped field would be dead data and would expand the evidence-layout surface); document cwd behavior without changing it (rejected — leaves the trap). Consequences: basket-schema behavior change → 0.MINOR; migration line; evidence layout untouched.

- [ ] **Step 2: Add the index row** to `docs/adr/README.md` (after the 0028 row — see Task 4.5 which adds 0028):

```markdown
| [0029](0029-basket-relative-path-resolution.md) | Basket-relative path resolution for dir and ./ argv | Accepted |
```

- [ ] **Step 3: Commit**

```bash
git add docs/adr/0029-basket-relative-path-resolution.md docs/adr/README.md
git commit -m "docs(adr): 0029 basket-relative path resolution

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2.7: `docs/guide/basket.md` — single source of truth (rebases on PR-3)

**Files:**
- Create: `docs/guide/basket.md`
- Modify: `docs/guide/cli.md` (shrink the `#bench` schema prose to a pointer)
- Modify: `docs/guide/configuration.md` (one-line pointer at the top)

- [ ] **Step 1: Write `docs/guide/basket.md`** with a full field table for `Basket`, `Task`, `Variant`, `Verify`, `Workspace` — columns: field, type, required/optional, default, notes. Copy field facts from `bench/basket.go:49-110`. Include:
  - `reps`: int, **required**, no default (a missing/zero `reps` fails with `reps must be >= 1`).
  - `basket`, `tasks`, `variants`, task `id`, task `cmd`, variant `id`: **required**.
  - `dir`, `env`, `checkpoints`, `timeout`, `artifacts`, `verify`, `workspace`, variant `setup`: optional.
  - The **path resolution rule** (verbatim from Global Constraints).
  - `dir` and `workspace` are mutually exclusive (task-level and cross-level).
  - `timeout` needs units (`"30s"`); empty verify timeout defaults to 1 minute.
  - `artifacts` are globs relative to the working directory and must be local (no `..` escape).
  - The three "what happens if" answers: omitted `reps` → error; `dir`+`workspace` → error; a single variant → runs but cannot be gated (advisory printed).

- [ ] **Step 2: Shrink `cli.md#bench`** schema prose to a one-line pointer: "The basket schema is documented in [basket.md](basket.md)." Keep the command's flag list.

- [ ] **Step 3: Add the pointer** to the top of `configuration.md`: "Looking for the basket file schema? See [basket.md](basket.md); this page covers CLI flags, environment variables, and defaults."

- [ ] **Step 4: Verify links** — run the WS4 link checker (Task 4.6) over the guide; expect no broken links/anchors.

- [ ] **Step 5: Commit**

```bash
git add docs/guide/basket.md docs/guide/cli.md docs/guide/configuration.md
git commit -m "docs(guide): basket.md as the single basket-schema reference

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## PR-3 — WS4: docs/ hygiene (merges before PR-2's doc task)

Branch: `chore/docs-internal-split`. Pure docs; merge first so PR-2 rebases its `cli.md` deltas on the moved tree. (Ordered before WS3 conceptually but file-disjoint from WS3 except final README links, which WS3 owns.)

### Task 3.1 (4.1): Move internal docs under `docs/internal/`

**Files:**
- Move: `docs/superpowers/`, `docs/plans/`, `docs/specs/`, `docs/reviews/` → `docs/internal/{…}`

- [ ] **Step 1: Move with history preserved**

```bash
mkdir -p docs/internal
git mv docs/superpowers docs/internal/superpowers
git mv docs/plans docs/internal/plans
git mv docs/specs docs/internal/specs
git mv docs/reviews docs/internal/reviews
```

- [ ] **Step 2: Find every inbound link to the moved paths**

Run: `grep -rn -E 'docs/(superpowers|plans|specs|reviews)/|\.\./(superpowers|plans|specs|reviews)/|\((superpowers|plans|specs|reviews)/' --include='*.md' .`
Record each hit; these are fixed in Steps 3-4.

- [ ] **Step 3: Fix guide → reviews links** — in `docs/guide/cli.md` and `docs/guide/workflows.md`, repoint `reviews/…` links to `../internal/reviews/…`. For each load-bearing figure (e.g. the "~5× cost" claim), inline the number with its date in the guide prose and keep the internal link as provenance: e.g. "roughly 5× the baseline cost (measured 2026-07-08)".

- [ ] **Step 4: Fix the ADR index spec link** — `docs/adr/README.md` ends with `Design spec: [../specs/2026-06-20-catacomb-design.md]`; repoint to `../internal/specs/2026-06-20-catacomb-design.md`. Sweep `docs/adr/*.md` for any other `../specs/`/`../plans/` references and repoint to `../internal/…`.

- [ ] **Step 5: Verify no dangling references remain**

Run: `grep -rn -E '\]\((\.\./)*(superpowers|plans|specs|reviews)/' --include='*.md' docs/ README.md`
Expected: every remaining hit points at `internal/…`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "docs: move plans, specs, reviews, superpowers under docs/internal

Fence development material off from user docs; repoint inbound links.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3.2 (4.2): `docs/README.md` landing index + drop the stub

**Files:**
- Create: `docs/README.md`
- Delete: `docs/guide/getting-started.md`
- Modify: `docs/guide/README.md` (remove the getting-started pointer line)

- [ ] **Step 1: Write `docs/README.md`**:

```markdown
# Catacomb documentation

New here? Start with the [README tutorial](../README.md#-tutorial), then dive
into the guide.

- **[User guide](guide/README.md)** — concepts, workflows, CLI reference,
  configuration, ingestion, privacy and operations.
- **[Basket schema](guide/basket.md)** — every field you can put in a basket file.
- **[Troubleshooting](guide/troubleshooting.md)** — symptoms and fixes.
- **[Architecture decisions](adr/README.md)** — the ADR log.

`internal/` holds development material (plans, specs, reviews, agent tooling);
it is not needed to use catacomb.
```

- [ ] **Step 2: Confirm the stub has no inbound links**

Run: `grep -rn 'getting-started' --include='*.md' . `
Expected: only `docs/guide/README.md` (the pointer line) and possibly the stub itself.

- [ ] **Step 3: Delete the stub and remove the pointer**

```bash
git rm docs/guide/getting-started.md
```

In `docs/guide/README.md`, delete the final line `([Getting started](getting-started.md) is kept as a pointer for old links.)`.

- [ ] **Step 4: Commit**

```bash
git add docs/README.md docs/guide/README.md
git commit -m "docs: add docs/ landing index, drop getting-started stub

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3.3 (4.3): `docs/guide/troubleshooting.md`

**Files:**
- Create: `docs/guide/troubleshooting.md`
- Modify: `docs/guide/privacy-and-operations.md` (replace the inline table with a pointer)
- Modify: `docs/guide/README.md` (add troubleshooting to the reading order)

- [ ] **Step 1: Create `docs/guide/troubleshooting.md`** — move the table from `privacy-and-operations.md:146-158` verbatim, and add these first-session rows:

```markdown
| Symptom | Action |
| --- | --- |
| `brew` installed an older version than the latest release | Run `brew update && brew upgrade --cask catacomb`; channels converge within minutes of a release |
| Offline `catacomb verify` cannot find the verifier script | Paths in a basket resolve against the basket file's directory; keep the verifier next to the basket and reference it as `./verify.py`. See [basket.md](basket.md#paths) |
```

Preserve the existing "Format drift" anchor links.

- [ ] **Step 2: Replace the inline table** in `privacy-and-operations.md` with: "See [Troubleshooting](troubleshooting.md) for symptoms and fixes." Keep the surrounding prose about drift lines.

- [ ] **Step 3: Add to the guide reading order** in `docs/guide/README.md`:

```markdown
7. [Troubleshooting](troubleshooting.md) — symptoms and fixes for common errors
```

- [ ] **Step 4: Commit**

```bash
git add docs/guide/troubleshooting.md docs/guide/privacy-and-operations.md docs/guide/README.md
git commit -m "docs(guide): dedicated troubleshooting page with first-session fixes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3.4 (4.4): Single-source the `CATACOMB_*` contract and the bench→verify→regress example

**Files:**
- Modify: `docs/guide/cli.md`, `docs/guide/workflows.md`, `integrations/verifier/README.md`

- [ ] **Step 1: Keep the canonical `CATACOMB_*` table** in `workflows.md` (its contextual home). In `cli.md` and `integrations/verifier/README.md`, replace their copies with a one-line summary plus a link: "Verifiers receive the `CATACOMB_*` environment contract; see [workflows.md](../guide/workflows.md#verifying-task-outcomes) (adjust the relative path per file)."

- [ ] **Step 2: De-duplicate the bench→verify→regress example** — keep it in `workflows.md`; in `cli.md` replace the duplicated block with a link to it.

- [ ] **Step 3: Verify the `CATACOMB_*` facts still match** the code contract referenced in `docs/VERSIONING.md` surface #3 (env var names unchanged; docs-only edit).

- [ ] **Step 4: Commit**

```bash
git add docs/guide/cli.md docs/guide/workflows.md integrations/verifier/README.md
git commit -m "docs: single-source the CATACOMB_* contract and bench→verify→regress example

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3.5 (4.5): Add ADR-0028 to the index

**Files:**
- Modify: `docs/adr/README.md`

- [ ] **Step 1: Add the row** after the 0027 line:

```markdown
| [0028](0028-per-cell-workspace-isolation.md) | Per-cell workspace isolation (fresh workdir, patch handover, teardown) | Accepted |
```

- [ ] **Step 2: Commit**

```bash
git add docs/adr/README.md
git commit -m "docs(adr): add missing ADR-0028 to the index

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3.6 (4.6): Advisory link-check CI job

**Files:**
- Create: `.github/workflows/docs-links.yml`

- [ ] **Step 1: Create the workflow** (offline relative-link + anchor check; advisory, not required):

```yaml
name: Docs links

on:
  pull_request:
    paths: ['**/*.md']
  workflow_dispatch: {}

permissions:
  contents: read

jobs:
  links:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          persist-credentials: false
      - name: Check relative links and anchors
        uses: lycheeverse/lychee-action@v2
        with:
          args: >-
            --offline --include-fragments --no-progress
            'README.md' 'docs/**/*.md' 'integrations/**/*.md'
          fail: true
```

- [ ] **Step 2: Lint**

Run: `actionlint .github/workflows/docs-links.yml`
Expected: no errors. (Do not add this job to branch-protection required checks in this PR.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/docs-links.yml
git commit -m "ci: advisory offline docs link check

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## PR-4 — WS3: README FastAPI-tutorial amendments (lands last)

Branch: `docs/readme-first-touch`. Depends on PR-1 (version badge / channel notes), PR-2 (`basket.md`, path semantics in examples), PR-3 (moved doc paths, troubleshooting link). Rebase on all three before finishing.

### Task 4.1: Hero, badges, maturity, requirement

**Files:**
- Modify: `README.md:1-55`

- [ ] **Step 1: Tagline axes** — extend the centered tagline (README.md:10-15) to name the regression axes, e.g. append a line: "See whether **cost, latency, or correctness** regressed — from statistics over real transcripts, not vibes." Keep the existing hook line.

- [ ] **Step 2: Canonical category sentence** — immediately below the tagline block, add one sentence introducing the technical category once: "Catacomb is an *offline eval gate*: a local CLI that runs your Claude Code tasks repeatedly and gates regressions in CI."

- [ ] **Step 3: Version badge** — add a release badge to the badge row (README.md:18-25):

```html
<a href="https://github.com/realkarych/catacomb/releases"><img alt="release" src="https://img.shields.io/github/v/release/realkarych/catacomb"></a>&nbsp;<!--
-->
```

- [ ] **Step 4: Maturity line** — below the badge row: "Pre-1.0: minor releases may carry breaking changes, always with migration notes ([versioning policy](docs/VERSIONING.md))."

- [ ] **Step 5: Requirement on the first screen** — the `🧰 Requirements` content (Claude Code + `claude` on PATH) is already early; add one bold line right under the opening paragraph so a non-Claude-Code visitor learns the hard requirement before the tutorial: "**Requires [Claude Code](https://www.anthropic.com/claude-code)** installed with `claude` on your PATH."

- [ ] **Step 6: Verify render** — preview the README locally (`grip README.md` or GitHub preview) and confirm badges resolve and layout holds.

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs(readme): axes in tagline, category sentence, release badge, maturity line

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 4.2: Verdict visual + "why not" comparison

**Files:**
- Create: `docs/assets/regress-verdict-light.svg`, `docs/assets/regress-verdict-dark.svg`
- Modify: `README.md` (before the tutorial section; new comparison subsection after Features)

- [ ] **Step 1: Capture a real verdict** — run the tutorial's `catacomb regress` (or reuse captured output) and author a light/dark SVG pair rendering the verdict table (a small terminal-style card; hand-authored SVG, no external assets), saved under `docs/assets/`.

- [ ] **Step 2: Embed it** before the tutorial with the existing light/dark `<picture>` pattern used for the lockup (README.md:3-6).

- [ ] **Step 3: Add "Why not promptfoo / LangSmith / Inspect"** after Features — 5-7 neutral lines drawn from `futurum-promptfoo-snapshot.md` (kept factual): promptfoo targets prompt/RAG evals while catacomb scores whole agent sessions from real transcripts; LangSmith/Braintrust are hosted platforms while catacomb is local files + an exit code; Inspect is a research framework while catacomb is a purpose-built regression gate for Claude Code.

- [ ] **Step 4: Commit**

```bash
git add docs/assets/regress-verdict-light.svg docs/assets/regress-verdict-dark.svg README.md
git commit -m "docs(readme): verdict-table visual and a why-not comparison

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 4.3: Install clarity + tutorial cleanup + links

**Files:**
- Modify: `README.md` (Installation, Tutorial, How it works, Methodology, Documentation sections)

- [ ] **Step 1: Per-channel version note** in Installation: "`go install` serves the tagged source immediately; brew, apt, and docker converge within minutes of a release — run `brew update` if you see an older version." Move the old-formula migration note up beside the brew block. Mark docker "for CI and `version`; the tutorial needs `claude` and `~/.claude/projects` mounted."

- [ ] **Step 2: Cost + version honesty** — in the tutorial intro, add: "`bench` calls the Anthropic API and spends real money (a few cents on haiku for this demo)." Link the tested Claude Code version watchlist (`docs/guide/ingestion.md`).

- [ ] **Step 3: De-noise the first verdict table** — remove the `insufficient` and `audit:` rows from the first shown `regress` output; introduce an "Understanding the report" subsection (before those rows first appear) that explains `insufficient`/`audit`/`sensitivity` on a later, fuller example. Ensure no output line precedes its explanation.

- [ ] **Step 4: Add "check it" blocks** — after each tutorial command, a short "You should see:" block naming the expected signal (already partly present per the 2026-07-13 rewrite; fill any gaps).

- [ ] **Step 5: Compress Methodology** to one paragraph plus a compact link list (keep the verified-source framing rule from the 2026-07-13 design; do not add unverified claims).

- [ ] **Step 6: Fix the Documentation section links** — point to `docs/README.md`, `docs/guide/README.md`, `docs/guide/basket.md`, the tutorial anchor, `docs/guide/troubleshooting.md`, and `docs/adr/README.md`. Remove any link into `docs/internal/…` (formerly `docs/plans/`).

- [ ] **Step 7: Run the link checker**

Run: `actionlint` is N/A; instead run the lychee check locally if available, else rely on the `docs-links.yml` job. Manually confirm every README anchor/link resolves against the moved tree.

- [ ] **Step 8: Commit**

```bash
git add README.md
git commit -m "docs(readme): install version clarity, de-noised verdict, fixed doc links

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 4.4: Canonical package descriptions

**Files:**
- Modify: `.goreleaser.yaml` (nfpm `description`, homebrew_casks `description`)

- [ ] **Step 1: Set both descriptions** to the canonical category "Offline eval gate for Claude Code agents" (`.goreleaser.yaml:65,76`). Confirm they match the README's category sentence wording.

- [ ] **Step 2: Validate goreleaser config**

Run: `goreleaser check`
Expected: config OK.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "chore(release): canonical package description

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Integration and release (after all four PRs merge)

- [ ] Apply the `release` environment `gh api` changes from Task 1.4 (maintainer, admin rights).
- [ ] Run `go test ./...` on `master`; confirm the coverage gate is green.
- [ ] Trigger `channels-watch.yml` via `workflow_dispatch` to smoke-test the watchdog.
- [ ] Tag and push **v0.2.0** with an annotated message carrying the migration line: "Basket paths now resolve against the basket file's directory (was cwd). Baskets run from the basket's own directory are unaffected."
- [ ] Confirm the `Publish Release` run — including the new `verify-channels` job — concludes `success` (check conclusions, not the watch exit).

## Self-review notes (coverage of the spec)

- WS1 → Tasks 1.1-1.4 (version fallback, verify-channels, channels-watch, RELEASING + env policy). ✓
- WS2 → Tasks 2.1-2.7 (loader resolution replacing meta-stamping, offline-verify guard, human errors, timeout hint + single-variant advisory, warning dedupe, ADR-0029, basket.md). **Deviation from spec, flagged:** resolution lives in the loader, not a `meta.json` argv stamp — offline verify re-parses the basket, so the stamp would be dead data and would touch the evidence-layout compatibility surface for no benefit; the loader approach meets the spec goal ("identical inline/offline behavior regardless of cwd") and keeps WS2 to the basket-schema surface only. ✓
- WS3 → Tasks 4.1-4.4 (hero/badges/maturity/requirement, verdict visual + comparison, install/tutorial cleanup + links, package descriptions). ✓
- WS4 → Tasks 3.1-3.6 (internal move, landing index + stub removal, troubleshooting, contract dedup, ADR-0028, link-check CI). ✓
- Sequencing: PR-3 before PR-2's doc task (shared `cli.md`); PR-4 last (depends on all). ✓
