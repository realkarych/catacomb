# PR-L: Secrets at Rest — Write-Path Redaction (ADR-0024) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement ADR-0024 — redaction is enforced at the persist boundary so a `catacomb.db` at rest contains only redacted content: observations are redacted before they are applied to the graph and appended to the log, node deltas are redacted before `AppendDeltas`, `PayloadHash` becomes sha256 of the redacted payload (pre-redaction hash dropped), config gains `payloads.mode: redact|refs|all` (default `redact`) and `payloads.max_bytes` (default 262144) with redact-then-cap ordering and typed refs, and a schema-v4 data migration scrubs every existing `observations.body` and `nodes.body` through the redactor (idempotent fixed point, followed by `VACUUM` so old row images do not linger in free pages). Read/export-time redaction stays as defense in depth.

**Architecture:** the `redact` package remains the single rule-pack entry point (`redact.Redact`); it gains two surface wrappers used by both the write and read paths — `redact.Observation` (payload deep-walk + top-level string attrs + post-redaction `Payload.Hash` recompute) and the extended `redact.Node` (existing surface + `SubagentType` + post-redaction `Payload.Hash`/`PayloadHash` recompute) — plus a `redact.Policy` (`Mode`, `MaxBytes`, no global state) that layers mode/cap semantics (redact → hash → cap/ref) on those wrappers. The write-path choke points are: (1) `daemon.applyAndPersist` — the single funnel for every daemon ingest path (hooks, OTLP, stream-json, transcript tail, subagent meta, marks, reaper, repro meta) — redacts the observation **before** `applyFn(g, o)`, so the in-memory graph, the CDC deltas, and the append-only log all see only redacted bytes and `Recover()` rebuilds byte-identical graphs (ADR-0003 determinism); it additionally passes each delta node through `Policy.Node` before `AppendDeltas` (a fixed-point no-op that guarantees the disk invariant even for reducer-synthesized content); (2) `cmd/catacomb/replay.go` — the only graph builder outside the daemon that persists — redacts observations after parse (before `ApplyAll`) and nodes before `Persist`, with `redact.DefaultPolicy()`. The v4 migration lives in `store/migrate.go` on the existing ADR-0017 runner (one transaction per migration step); it scans both tables, buffers only changed rows, updates them, and `openSQLite` VACUUMs + WAL-checkpoints after any migration ran. `openSQLiteReadOnly` now refuses pre-v4 databases with `ErrSchemaOutdated` (same error and UX as today's outdated-schema experience) so read/export paths can never serve a raw pre-scrub file.

**Tech Stack:** Go 1.26, testify, table-driven tests, `modernc.org/sqlite`. Repo rules: NO comments in Go (codepolicy), 100% coverage, TDD, gofumpt via `make fmt`, `make lint` clean, no `time.Sleep` in tests, consumer-declares-interface, no global mutable state, deterministic outputs, commit per task, markdownlint for docs.

## Global Constraints

- Work in this worktree only; never touch the shared checkout.
- No comments in Go code — none, not even doc comments (`internal/codepolicy` fails the build otherwise).
- 100% test coverage (`make cover`); TDD: failing test first, minimal implementation, refactor under green. Do not add unreachable guard branches.
- Idempotency is a hard requirement: `Redact(Redact(x).Data)` must equal `Redact(x).Data` with zero findings on the second pass, for the full rule pack including high-entropy detection versus the `‹redacted:reason›` placeholders and the `‹binary:len,hash›`/`‹ref:len,hash›` typed refs; `Policy.Observation`/`Policy.Node` must be idempotent in every mode (the typed-ref guard exists exactly for `refs` mode and tiny caps). Every fixed-point test below is load-bearing: the v4 migration's re-entrancy AND stepkey stability across the migration both depend on this property.
- Determinism: redaction is a pure function applied before the append-only log; live graphs and `Recover()`-rebuilt graphs must be deep-equal (explicit test in Task 4).
- Hash semantics (recorded decisions): `Payload.Hash` = sha256 of the **redacted, pre-cap** payload (`model.HashPayload` over redacted bytes); a typed ref embeds `(length, sha256[:8])` of the redacted content it replaced; a node's `PayloadHash` always equals `model.HashPayload` of the node's stored payload bytes. No pre-redaction hash survives anywhere (ADR-0024 §2; no HMAC).
- The v4 migration always applies pure redaction (`redact.Observation`/`redact.Node` — no cap, no refs, regardless of the configured `payloads.mode`): the scrub is unconditional per ADR-0024, and capping historical data would destroy more than secrets.
- Migration bodies round-trip through the current `model` structs; unknown JSON fields would be dropped (none have ever been removed from `model.Observation`/`model.Node`) and numeric attrs re-encode via `float64` (lossless below 2^53 — all realistic attr magnitudes). Rows are rewritten only when bytes changed, so the second run is a guaranteed fixed point (0 updates).
- Out of scope (deliberate, documented in Task 7): per-sink mode overrides (ADR-0020 §6, deferred by ADR-0024), redaction of `runs.body` / `annotations` / `quarantine.body` (quarantine holds raw *malformed* input by design — flagged as residual risk in the privacy guide), an HMAC'd pre-redaction integrity hash, and new CLI flags for payload mode (config-file only).

---

### Task 1: `redact` core — `Observation` wrapper, `Node` surface extension, marker helper, fixed-point proof

**Files:**

- Create: `redact/observation.go`
- Modify: `redact/redact.go` (add `HasMarker`), `redact/node.go` (SubagentType, shared attr/payload helpers, hash recompute)
- Test: create `redact/fixedpoint_internal_test.go` (internal, `package redact` — needs `valueRules`/`placeholder`), create `redact/observation_test.go`, extend `redact/node_test.go`

**Interfaces:**

- Produces: `redact.Observation(o model.Observation) model.Observation`; `redact.Node(n *model.Node) *model.Node` (extended semantics: also scrubs `SubagentType`, and recomputes `Payload.Hash` + `PayloadHash` post-redaction when a payload is present); `redact.HasMarker(data []byte) bool`. Tasks 2, 4, 5, 6 depend on these exact names.
- Behavior change to existing consumers of `redact.Node` (all exporters): exported `payload_hash` is now always post-redaction (this closes the ADR-0020 §3 leak for `all`-mode stores too). Any exporter test asserting a pre-redaction hash passthrough on a secret-bearing fixture must be updated to expect the recomputed hash.

- [ ] **Step 1: Write failing tests**

Create `redact/fixedpoint_internal_test.go` (internal package — introspects the rule pack so a future rule addition cannot silently break the fixed point):

```go
package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func allReasons() []string {
	seen := map[string]bool{"sensitive-key": true, "binary": true}
	for _, rule := range valueRules {
		seen[rule.reason] = true
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	return out
}

func TestPlaceholdersNeverRematchRulePack(t *testing.T) {
	for _, reason := range allReasons() {
		r := Redact([]byte(placeholder(reason)))
		assert.False(t, r.Redacted, "placeholder for %q rematches the rule pack", reason)
		assert.Equal(t, placeholder(reason), string(r.Data))
	}
}

func TestTypedRefsNeverRematchRulePack(t *testing.T) {
	for _, ref := range []string{
		`"‹binary:1048576,0123456789abcdef›"`,
		`"‹ref:1048576,0123456789abcdef›"`,
		"note: ‹binary:1048576,0123456789abcdef› and ‹ref:42,fedcba9876543210›",
	} {
		r := Redact([]byte(ref))
		assert.False(t, r.Redacted, ref)
	}
}
```

Add to `redact/redact_test.go` (external package, as the file already is):

```go
func TestRedactFixedPoint(t *testing.T) {
	cases := []string{
		`{"api_key":"AKIAIOSFODNN7EXAMPLE"}`,
		`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}`,
		`export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE && curl -H "Authorization: Bearer abcdefghij1234567890"`,
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		`token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dozjgNryP4J3jVmNHl0w5N7XgL0n3I9PlFUP0THsR8U`,
		`AKIAIOSFODNN7EXAMPLE0123456789abcdef0123456789abcdef01234567`,
		`{"nested":{"password":"hunter2","cwd":"/home/kesha"},"n":3}`,
		string([]byte{0xff, 0xfe, 0x01}),
		`{"text":"no secrets here"}`,
		`plain prose without any secret`,
	}
	for _, in := range cases {
		once := redact.Redact([]byte(in))
		twice := redact.Redact(once.Data)
		assert.Equal(t, string(once.Data), string(twice.Data), "input %q", in)
		assert.False(t, twice.Redacted, "second pass must be a no-op for %q", in)
		assert.Empty(t, twice.Findings, "input %q", in)
	}
}

func TestHasMarker(t *testing.T) {
	assert.True(t, redact.HasMarker([]byte(`{"x":"‹redacted:aws-key›"}`)))
	assert.True(t, redact.HasMarker([]byte(`"‹binary:3,0123456789abcdef›"`)))
	assert.True(t, redact.HasMarker([]byte(`"‹ref:99,0123456789abcdef›"`)))
	assert.False(t, redact.HasMarker([]byte(`{"x":"clean"}`)))
	assert.False(t, redact.HasMarker(nil))
}
```

Create `redact/observation_test.go`:

```go
package redact_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func secretObservation() model.Observation {
	return model.Observation{
		ObsID: "o1",
		Attrs: map[string]any{"prompt": "use AKIAIOSFODNN7EXAMPLE", "count": 3},
		Payload: &model.Payload{
			Input:  json.RawMessage(`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}`),
			Output: json.RawMessage(`{"password":"hunter2"}`),
			Hash:   "stale-pre-redaction",
		},
	}
}

func TestObservationRedactsPayloadAndAttrs(t *testing.T) {
	o := secretObservation()
	r := redact.Observation(o)
	assert.Equal(t, "use ‹redacted:aws-key›", r.Attrs["prompt"])
	assert.Equal(t, 3, r.Attrs["count"])
	assert.Contains(t, string(r.Payload.Input), "‹redacted:connection-string›")
	assert.Contains(t, string(r.Payload.Output), "‹redacted:")
	assert.NotContains(t, string(r.Payload.Input), "kesha_dev_password")
}

func TestObservationRecomputesPostRedactionHash(t *testing.T) {
	r := redact.Observation(secretObservation())
	require.NotNil(t, r.Payload)
	assert.Equal(t, model.HashPayload(r.Payload), r.Payload.Hash)
	assert.NotEqual(t, "stale-pre-redaction", r.Payload.Hash)
}

func TestObservationDoesNotMutateInput(t *testing.T) {
	o := secretObservation()
	_ = redact.Observation(o)
	assert.Equal(t, "use AKIAIOSFODNN7EXAMPLE", o.Attrs["prompt"])
	assert.Contains(t, string(o.Payload.Input), "kesha_dev_password")
	assert.Equal(t, "stale-pre-redaction", o.Payload.Hash)
}

func TestObservationNilPayloadAndAttrs(t *testing.T) {
	r := redact.Observation(model.Observation{ObsID: "o2"})
	assert.Nil(t, r.Payload)
	assert.Nil(t, r.Attrs)
}

func TestObservationIdempotent(t *testing.T) {
	once := redact.Observation(secretObservation())
	twice := redact.Observation(once)
	assert.Equal(t, once, twice)
}
```

Add to `redact/node_test.go`:

```go
func TestNode_RedactsSubagentType(t *testing.T) {
	n := &model.Node{SubagentType: "AKIAIOSFODNN7EXAMPLE"}
	rn := redact.Node(n)
	assert.Equal(t, "‹redacted:aws-key›", rn.SubagentType)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", n.SubagentType)
}

func TestNode_RecomputesPostRedactionHash(t *testing.T) {
	n := &model.Node{
		Payload: &model.Payload{
			Input: json.RawMessage(`{"command":"psql postgres://kesha:pw@localhost/db"}`),
			Hash:  "stale",
		},
		PayloadHash: "stale",
	}
	rn := redact.Node(n)
	require.NotNil(t, rn.Payload)
	assert.Equal(t, model.HashPayload(rn.Payload), rn.Payload.Hash)
	assert.Equal(t, rn.Payload.Hash, rn.PayloadHash)
	assert.Equal(t, "stale", n.PayloadHash)
}

func TestNode_Idempotent(t *testing.T) {
	n := &model.Node{
		Name:         "run AKIAIOSFODNN7EXAMPLE",
		SubagentType: "general-purpose",
		Attrs:        map[string]any{"cwd": "/home/kesha", "description": "psql postgres://u:p@h/db"},
		Payload:      &model.Payload{Input: json.RawMessage(`{"password":"hunter2"}`)},
	}
	once := redact.Node(n)
	twice := redact.Node(once)
	assert.Equal(t, once, twice)
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./redact/ 2>&1 | head -20`
Expected: FAIL (undefined `redact.Observation`, `redact.HasMarker`; SubagentType/hash tests fail).

- [ ] **Step 3: Implement**

Create `redact/observation.go`:

```go
package redact

import (
	"encoding/json"

	"github.com/realkarych/catacomb/model"
)

func Observation(o model.Observation) model.Observation {
	o.Attrs = redactAttrs(o.Attrs)
	o.Payload = redactPayload(o.Payload)
	return o
}

func redactPayload(p *model.Payload) *model.Payload {
	if p == nil {
		return nil
	}
	pc := *p
	if len(p.Input) > 0 {
		pc.Input = append(json.RawMessage(nil), Redact(p.Input).Data...)
	}
	if len(p.Output) > 0 {
		pc.Output = append(json.RawMessage(nil), Redact(p.Output).Data...)
	}
	pc.Hash = model.HashPayload(&pc)
	return &pc
}

func redactAttrs(attrs map[string]any) map[string]any {
	if attrs == nil {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if sv, ok := v.(string); ok {
			out[k] = redactString(sv)
		} else {
			out[k] = v
		}
	}
	return out
}
```

Rewrite `redact/node.go` to reuse the helpers (delete its now-duplicated attr/payload logic):

```go
package redact

import (
	"github.com/realkarych/catacomb/model"
)

func Node(n *model.Node) *model.Node {
	if n == nil {
		return nil
	}
	nc := *n
	nc.Name = redactString(n.Name)
	nc.SubagentType = redactString(n.SubagentType)
	nc.Attrs = redactAttrs(n.Attrs)
	if n.Payload != nil {
		nc.Payload = redactPayload(n.Payload)
		nc.PayloadHash = nc.Payload.Hash
	}
	return &nc
}

func redactString(s string) string {
	if s == "" {
		return s
	}
	return string(Redact([]byte(s)).Data)
}
```

Add to `redact/redact.go` (near `placeholder`; `strings` is already imported):

```go
func HasMarker(data []byte) bool {
	s := string(data)
	return strings.Contains(s, "‹redacted:") || strings.Contains(s, "‹binary:") || strings.Contains(s, "‹ref:")
}
```

- [ ] **Step 4: Repo-wide check for hash-recompute fallout**

Run: `go test ./redact/ ./export/... ./daemon/ ./diff/ ./stepkey/`
Expected: `redact` PASS. If any exporter test fails, it asserts a pre-redaction `payload_hash` passthrough on a secret-bearing fixture — update the expectation to the post-redaction hash (compute via `model.HashPayload` of the redacted payload in the test). Do NOT weaken the assertion to ignore the hash.

- [ ] **Step 5: Green + commit**

Run: `go test -race ./redact/ && go test ./redact/ -cover`
Expected: PASS, coverage 100.0%.

```bash
git add redact/ export/ daemon/ diff/ stepkey/
git commit -m "feat(redact): Observation wrapper, whole-node surface incl. subagent_type, post-redaction hashing, fixed-point proof (ADR-0024)"
```

---

### Task 2: `redact.Policy` — modes, cap, typed refs

**Files:**

- Create: `redact/policy.go`
- Test: create `redact/policy_test.go`

**Interfaces:**

- Produces: `redact.Mode` (`ModeRedact`/`ModeRefs`/`ModeAll`), `redact.DefaultMaxBytes = 262144`, `redact.Policy{Mode, MaxBytes}`, `redact.DefaultPolicy()`, `(Policy).Observation(model.Observation) model.Observation`, `(Policy).Node(*model.Node) *model.Node`. Tasks 4 and 5 depend on these exact names.
- Semantics: order is redact → hash → cap/ref. `redact`: full redaction; a side longer than `MaxBytes` becomes `"‹ref:len,sha256[:8]›"` of the redacted bytes. `refs`: full redaction + hash, then every non-empty side becomes a typed ref (no bodies at rest; ADR-0008 refs-only). `all`: no redaction (identity), parser hash retained, cap still applies. A typed ref (`‹ref:…›` or `‹binary:…›`) is never re-wrapped — this is what makes `refs` mode and tiny caps idempotent. The zero-value `Policy` normalizes to `redact`/`DefaultMaxBytes` so an unconfigured daemon is safe by default.

- [ ] **Step 1: Write failing tests**

Create `redact/policy_test.go`:

```go
package redact_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func refFor(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf(`"‹ref:%d,%x›"`, len(data), h[:8])
}

func TestDefaultPolicy(t *testing.T) {
	p := redact.DefaultPolicy()
	assert.Equal(t, redact.ModeRedact, p.Mode)
	assert.Equal(t, redact.DefaultMaxBytes, p.MaxBytes)
}

func TestPolicyRedactModeRedactsUnderCap(t *testing.T) {
	r := redact.DefaultPolicy().Observation(secretObservation())
	want := redact.Observation(secretObservation())
	assert.Equal(t, want, r)
}

func TestPolicyCapsOversizedSideAfterRedaction(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRedact, MaxBytes: 16}
	r := p.Observation(secretObservation())
	redacted := redact.Observation(secretObservation())
	assert.Equal(t, refFor(redacted.Payload.Input), string(r.Payload.Input))
	assert.Equal(t, refFor(redacted.Payload.Output), string(r.Payload.Output))
	assert.Equal(t, redacted.Payload.Hash, r.Payload.Hash)
}

func TestPolicyRefsModeStoresRefsAndHashOnly(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRefs, MaxBytes: redact.DefaultMaxBytes}
	r := p.Observation(secretObservation())
	redacted := redact.Observation(secretObservation())
	assert.Equal(t, refFor(redacted.Payload.Input), string(r.Payload.Input))
	assert.Equal(t, refFor(redacted.Payload.Output), string(r.Payload.Output))
	assert.Equal(t, redacted.Payload.Hash, r.Payload.Hash)
	assert.NotContains(t, string(r.Payload.Input), "redacted:connection-string")
}

func TestPolicyAllModeSkipsRedactionButCaps(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeAll, MaxBytes: redact.DefaultMaxBytes}
	o := secretObservation()
	r := p.Observation(o)
	assert.Equal(t, o, r)

	tiny := redact.Policy{Mode: redact.ModeAll, MaxBytes: 8}
	capped := tiny.Observation(o)
	assert.Equal(t, refFor(o.Payload.Input), string(capped.Payload.Input))
	assert.Equal(t, "stale-pre-redaction", capped.Payload.Hash)
	assert.Equal(t, "use AKIAIOSFODNN7EXAMPLE", capped.Attrs["prompt"])
}

func TestPolicyZeroValueNormalizesToRedact(t *testing.T) {
	var p redact.Policy
	r := p.Observation(secretObservation())
	assert.Equal(t, redact.DefaultPolicy().Observation(secretObservation()), r)
}

func TestPolicyIdempotentAcrossModes(t *testing.T) {
	policies := []redact.Policy{
		redact.DefaultPolicy(),
		{Mode: redact.ModeRedact, MaxBytes: 8},
		{Mode: redact.ModeRefs, MaxBytes: 64},
		{Mode: redact.ModeAll, MaxBytes: 8},
	}
	for _, p := range policies {
		once := p.Observation(secretObservation())
		twice := p.Observation(once)
		assert.Equal(t, once, twice, "mode %q max %d", p.Mode, p.MaxBytes)
	}
}

func TestPolicyNode(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRedact, MaxBytes: 16}
	assert.Nil(t, p.Node(nil))

	bare := &model.Node{Name: "clean"}
	assert.Equal(t, "clean", p.Node(bare).Name)

	n := &model.Node{
		Name:    "Bash",
		Payload: &model.Payload{Input: json.RawMessage(`{"command":"psql postgres://kesha:pw@localhost/db"}`)},
	}
	rn := p.Node(n)
	require.NotNil(t, rn.Payload)
	assert.True(t, strings.HasPrefix(string(rn.Payload.Input), `"‹ref:`))
	assert.Equal(t, rn.Payload.Hash, rn.PayloadHash)
	assert.Contains(t, string(n.Payload.Input), "kesha")
	again := p.Node(rn)
	assert.Equal(t, rn, again)
}

func TestPolicyBinaryPayloadBecomesBinaryRefNotDoubleWrapped(t *testing.T) {
	p := redact.Policy{Mode: redact.ModeRefs, MaxBytes: redact.DefaultMaxBytes}
	o := model.Observation{Payload: &model.Payload{Input: json.RawMessage{0xff, 0xfe, 0x01}}}
	once := p.Observation(o)
	assert.True(t, strings.HasPrefix(string(once.Payload.Input), `"‹binary:`))
	twice := p.Observation(once)
	assert.Equal(t, once, twice)
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./redact/ 2>&1 | head -20`
Expected: FAIL (undefined `Policy`, `Mode*`, `DefaultPolicy`, `DefaultMaxBytes`).

- [ ] **Step 3: Implement `redact/policy.go`**

```go
package redact

import (
	"crypto/sha256"
	"fmt"
	"regexp"

	"github.com/realkarych/catacomb/model"
)

type Mode string

const (
	ModeRedact Mode = "redact"
	ModeRefs   Mode = "refs"
	ModeAll    Mode = "all"
)

const DefaultMaxBytes = 262144

type Policy struct {
	Mode     Mode
	MaxBytes int
}

func DefaultPolicy() Policy {
	return Policy{Mode: ModeRedact, MaxBytes: DefaultMaxBytes}
}

func (p Policy) normalized() Policy {
	if p.Mode != ModeRefs && p.Mode != ModeAll {
		p.Mode = ModeRedact
	}
	if p.MaxBytes <= 0 {
		p.MaxBytes = DefaultMaxBytes
	}
	return p
}

func (p Policy) Observation(o model.Observation) model.Observation {
	p = p.normalized()
	if p.Mode != ModeAll {
		o = Observation(o)
	}
	o.Payload = p.capPayload(o.Payload)
	return o
}

func (p Policy) Node(n *model.Node) *model.Node {
	if n == nil {
		return nil
	}
	p = p.normalized()
	if p.Mode != ModeAll {
		n = Node(n)
	}
	if n.Payload == nil {
		return n
	}
	nc := *n
	nc.Payload = p.capPayload(n.Payload)
	return &nc
}

var reTypedRef = regexp.MustCompile(`^"‹(?:ref|binary):\d+,[0-9a-f]+›"$`)

func (p Policy) capPayload(pl *model.Payload) *model.Payload {
	if pl == nil {
		return nil
	}
	pc := *pl
	pc.Input = p.capSide(pl.Input)
	pc.Output = p.capSide(pl.Output)
	return &pc
}

func (p Policy) capSide(data []byte) []byte {
	if len(data) == 0 || reTypedRef.Match(data) {
		return data
	}
	if p.Mode == ModeRefs || len(data) > p.MaxBytes {
		return typedRef(data)
	}
	return data
}

func typedRef(data []byte) []byte {
	h := sha256.Sum256(data)
	return fmt.Appendf(nil, `"‹ref:%d,%x›"`, len(data), h[:8])
}
```

- [ ] **Step 4: Green + commit**

Run: `go test -race ./redact/ && go test ./redact/ -cover`
Expected: PASS, coverage 100.0%.

```bash
git add redact/
git commit -m "feat(redact): Policy with redact|refs|all modes, redact-then-cap ordering, typed refs (ADR-0024)"
```

---

### Task 3: config `payloads` section

**Files:**

- Modify: `config/config.go` (struct, constants, errors, `Defaults`), `config/merge.go`, `config/validate.go`
- Test: `config/config_test.go`, `config/merge_test.go`, `config/validate_test.go`, `config/parse_test.go`

**Interfaces:**

- Produces: `config.PayloadsConfig{Mode string, MaxBytes int}` (yaml `payloads: {mode, max_bytes}`), constants `PayloadModeRedact|PayloadModeRefs|PayloadModeAll`, `DefaultPayloadMaxBytes = 262144`, errors `ErrUnknownPayloadMode`, `ErrInvalidPayloadMaxBytes`. `Defaults()` fills `redact`/262144. Merge semantics match the rest of the file: `""`/`0` mean "unset". Validation is lenient on zero values (consistent with merge) and rejects unknown modes and negative `max_bytes`. Task 4 consumes `cfg.Payloads`.

- [ ] **Step 1: Write failing tests**

`config/config_test.go`:

```go
func TestDefaultsPayloads(t *testing.T) {
	c := Defaults()
	assert.Equal(t, PayloadModeRedact, c.Payloads.Mode)
	assert.Equal(t, DefaultPayloadMaxBytes, c.Payloads.MaxBytes)
}
```

`config/parse_test.go`:

```go
func TestParsePayloadsSection(t *testing.T) {
	c, err := Parse([]byte("payloads:\n  mode: refs\n  max_bytes: 1024\n"))
	require.NoError(t, err)
	assert.Equal(t, PayloadModeRefs, c.Payloads.Mode)
	assert.Equal(t, 1024, c.Payloads.MaxBytes)
}
```

`config/merge_test.go`:

```go
func TestMergePayloads(t *testing.T) {
	base := Defaults()
	out := Merge(base, Config{Payloads: PayloadsConfig{Mode: PayloadModeAll}})
	assert.Equal(t, PayloadModeAll, out.Payloads.Mode)
	assert.Equal(t, DefaultPayloadMaxBytes, out.Payloads.MaxBytes)

	out = Merge(base, Config{Payloads: PayloadsConfig{MaxBytes: 4096}})
	assert.Equal(t, PayloadModeRedact, out.Payloads.Mode)
	assert.Equal(t, 4096, out.Payloads.MaxBytes)

	out = Merge(base, Config{})
	assert.Equal(t, base.Payloads, out.Payloads)
}
```

`config/validate_test.go` (reuse the file's pattern of a minimal valid sqlite store config):

```go
func TestValidatePayloadsModeAndMaxBytes(t *testing.T) {
	valid := Defaults()
	require.NoError(t, Validate(valid))

	bad := Defaults()
	bad.Payloads.Mode = "everything"
	err := Validate(bad)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownPayloadMode)

	neg := Defaults()
	neg.Payloads.MaxBytes = -1
	err = Validate(neg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPayloadMaxBytes)

	unset := Defaults()
	unset.Payloads = PayloadsConfig{}
	assert.NoError(t, Validate(unset))
}
```

- [ ] **Step 2: Run tests, verify they fail** — `go test ./config/ 2>&1 | head -20` → compile errors.

- [ ] **Step 3: Implement**

`config/config.go` — add to the constant block and error block:

```go
const (
	PayloadModeRedact = "redact"
	PayloadModeRefs   = "refs"
	PayloadModeAll    = "all"

	DefaultPayloadMaxBytes = 262144
)
```

```go
	ErrUnknownPayloadMode     = errors.New("config: unknown payloads.mode")
	ErrInvalidPayloadMaxBytes = errors.New("config: payloads.max_bytes must not be negative")
```

Add the field to `Config` (after `Sinks`):

```go
	Payloads PayloadsConfig `yaml:"payloads"`
```

Add the type (near the other section types):

```go
type PayloadsConfig struct {
	Mode     string `yaml:"mode,omitempty"`
	MaxBytes int    `yaml:"max_bytes,omitempty"`
}
```

In `Defaults()` add:

```go
		Payloads: PayloadsConfig{Mode: PayloadModeRedact, MaxBytes: DefaultPayloadMaxBytes},
```

`config/merge.go` — in `Merge` add `out.Payloads = mergePayloads(base.Payloads, override.Payloads)` and:

```go
func mergePayloads(base, o PayloadsConfig) PayloadsConfig {
	if o.Mode != "" {
		base.Mode = o.Mode
	}
	if o.MaxBytes != 0 {
		base.MaxBytes = o.MaxBytes
	}
	return base
}
```

`config/validate.go` — in `Validate`, before `return validateSinks(c.Sinks)`:

```go
	if err := validatePayloads(c.Payloads); err != nil {
		return err
	}
```

```go
func validatePayloads(p PayloadsConfig) error {
	switch p.Mode {
	case "", PayloadModeRedact, PayloadModeRefs, PayloadModeAll:
	default:
		return fmt.Errorf("config.Validate: %w", ErrUnknownPayloadMode)
	}
	if p.MaxBytes < 0 {
		return fmt.Errorf("config.Validate: %w", ErrInvalidPayloadMaxBytes)
	}
	return nil
}
```

- [ ] **Step 4: Green + commit**

Run: `go test -race ./config/ ./cmd/... && go test ./config/ -cover`
Expected: PASS, coverage 100.0% (`resolveConfig` picks the section up through the generic `Parse`/`Merge`/`Validate` path — no cmd change needed yet).

```bash
git add config/
git commit -m "feat(config): payloads.mode + payloads.max_bytes section, defaults redact/262144 (ADR-0024)"
```

---

### Task 4: daemon write-path choke point + cmd wiring

**Files:**

- Modify: `daemon/daemon.go` (`payloadPolicy` field, `New`, `SetPayloadPolicy`, `applyAndPersist`), `daemon/payload.go` (`Redacted` flag marker awareness), `cmd/catacomb/daemon.go` (`daemonParams.payloads`, `SetPayloadPolicy` call)
- Test: `daemon/daemon_test.go`, `daemon/payload_test.go`, `cmd/catacomb/config_resolve_test.go`

**Interfaces:**

- Consumes: `redact.Policy` (Task 2), `config.PayloadsConfig` (Task 3), `redact.HasMarker` (Task 1).
- Produces: `(*Daemon).SetPayloadPolicy(redact.Policy)` (logs the ADR-0008 warning via `d.logger` when mode is `all`); `applyAndPersist` redacts the observation before `applyFn` and every delta node before `AppendDeltas`. `PayloadView.Redacted` is true when serve-time findings occurred OR stored content already carries markers; `Redactions` keeps listing serve-time findings only (write-path findings are not persisted — documented in Task 7).
- Determinism invariant: because the observation is redacted before `applyFn`, the in-memory graph is built from redacted bytes; `Policy.Node` on deltas is a fixed-point no-op, so store == memory == recovery. Stepkeys are unchanged for uncapped payloads (`stepkey.salientHash` already redacts before hashing, and redaction is idempotent); only >`max_bytes` or `refs`-mode payloads get ref-based stepkeys.

- [ ] **Step 1: Write failing tests**

Add to `daemon/daemon_test.go` (internal `package daemon`; reuse `tempStore(t)`, `driftLogBuffer(d)`, `GraphsForTest`; add imports `database/sql`, `maps`, `path/filepath`, `"github.com/realkarych/catacomb/redact"` as needed — `modernc.org/sqlite` is already linked via the store):

```go
func ingestSecretToolUse(t *testing.T, d *Daemon, session string) {
	t.Helper()
	payload := fmt.Sprintf(`{"session_id":%q,"tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE && psql postgres://kesha:kesha_dev_password@localhost/appdb"}}`, session)
	require.NoError(t, d.Ingest("PreToolUse", []byte(payload)))
}

func TestApplyAndPersistScrubsObservationLog(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	ingestSecretToolUse(t, d, "s1")
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	raw, err := json.Marshal(obs)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "AKIAIOSFODNN7EXAMPLE")
	assert.NotContains(t, string(raw), "kesha_dev_password")
	assert.Contains(t, string(raw), "‹redacted:connection-string›")
}

func TestPersistedPayloadHashIsPostRedaction(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	ingestSecretToolUse(t, d, "s1")
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	var found bool
	for i := range obs {
		if obs[i].Payload == nil {
			continue
		}
		found = true
		assert.Equal(t, model.HashPayload(obs[i].Payload), obs[i].Payload.Hash)
		assert.Contains(t, string(obs[i].Payload.Input), "‹redacted:connection-string›")
	}
	assert.True(t, found)
}

func TestNodeRowsAtRestAreRedacted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	d := New(s)
	ingestSecretToolUse(t, d, "s1")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.Query("SELECT body FROM nodes")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var count int
	for rows.Next() {
		var body string
		require.NoError(t, rows.Scan(&body))
		count++
		assert.NotContains(t, body, "AKIAIOSFODNN7EXAMPLE")
		assert.NotContains(t, body, "kesha_dev_password")
	}
	require.NoError(t, rows.Err())
	assert.Positive(t, count)
}

func snapshotNodes(d *Daemon) map[string]*model.Node {
	out := map[string]*model.Node{}
	for _, g := range d.GraphsForTest() {
		maps.Copy(out, g.Nodes)
	}
	return out
}

func TestRecoverRebuildsIdenticalRedactedGraph(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	ingestSecretToolUse(t, d, "s1")
	live := snapshotNodes(d)
	require.NotEmpty(t, live)
	d2 := New(s)
	require.NoError(t, d2.Recover())
	assert.Equal(t, live, snapshotNodes(d2))
}

func TestSetPayloadPolicyRefsModeStoresRefsOnly(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	d.SetPayloadPolicy(redact.Policy{Mode: redact.ModeRefs, MaxBytes: redact.DefaultMaxBytes})
	ingestSecretToolUse(t, d, "s1")
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	var found bool
	for _, o := range obs {
		if o.Payload != nil && len(o.Payload.Input) > 0 {
			found = true
			assert.Regexp(t, `^"‹ref:\d+,[0-9a-f]{16}›"$`, string(o.Payload.Input))
		}
	}
	assert.True(t, found)
}

func TestSetPayloadPolicyAllModePersistsRawAndWarns(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	buf := driftLogBuffer(d)
	d.SetPayloadPolicy(redact.Policy{Mode: redact.ModeAll, MaxBytes: redact.DefaultMaxBytes})
	assert.Contains(t, buf.String(), "payloads.mode=all")
	ingestSecretToolUse(t, d, "s1")
	obs, err := s.ObservationsSince(0)
	require.NoError(t, err)
	raw, err := json.Marshal(obs)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "kesha_dev_password")
}

func TestSetPayloadPolicyRedactModeDoesNotWarn(t *testing.T) {
	d := New(tempStore(t))
	buf := driftLogBuffer(d)
	d.SetPayloadPolicy(redact.DefaultPolicy())
	assert.NotContains(t, buf.String(), "payloads.mode")
}
```

Add to `daemon/payload_test.go` an end-to-end view test (mirror the file's existing e2e style for locating the session hash and node — reuse its helpers if present):

```go
func TestNodePayloadViewReportsStoredRedaction(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowPayloadAccess(true)
	ingestSecretToolUse(t, d, "s1")
	var nodeID string
	for _, g := range d.GraphsForTest() {
		for id, n := range g.Nodes {
			if n.Type == model.NodeToolCall {
				nodeID = id
			}
		}
	}
	require.NotEmpty(t, nodeID)
	d.mu.Lock()
	view, err := d.nodePayloadView("s1", nodeID)
	d.mu.Unlock()
	require.NoError(t, err)
	assert.True(t, view.Redacted)
	assert.Contains(t, string(view.Input), "‹redacted:connection-string›")
	assert.Empty(t, view.Redactions)
}
```

Add to `cmd/catacomb/config_resolve_test.go`:

```go
func TestResolveConfigDefaultsPayloads(t *testing.T) {
	cfg, err := resolveConfig(daemonFlags{},
		func(string) ([]byte, error) { return nil, os.ErrNotExist },
		func(string) (string, bool) { return "", false }, "/home/u")
	require.NoError(t, err)
	assert.Equal(t, config.PayloadModeRedact, cfg.Payloads.Mode)
	assert.Equal(t, config.DefaultPayloadMaxBytes, cfg.Payloads.MaxBytes)
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./daemon/ ./cmd/catacomb/ 2>&1 | head -20`
Expected: FAIL (undefined `SetPayloadPolicy`; scrub assertions fail because raw bytes persist).

- [ ] **Step 3: Implement `daemon/daemon.go`**

Add `"github.com/realkarych/catacomb/redact"` to imports. Add a struct field after `logger`:

```go
	payloadPolicy      redact.Policy
```

In `New`, add to the literal:

```go
		payloadPolicy: redact.DefaultPolicy(),
```

Add alongside the other setters:

```go
func (d *Daemon) SetPayloadPolicy(p redact.Policy) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.payloadPolicy = p
	if p.Mode == redact.ModeAll {
		d.logger.Warn("payloads.mode=all: persisting unredacted payloads at rest; serve/export-time redaction still applies")
	}
}
```

Change the top of `applyAndPersist`:

```go
func (d *Daemon) applyAndPersist(g *reduce.Graph, o model.Observation) error {
	o = d.payloadPolicy.Observation(o)
	applyFn(g, o)
	d.watchVersion(g, o)
	deltas := drainFn(g)
	for i := range deltas {
		if deltas[i].Node != nil {
			deltas[i].Node = d.payloadPolicy.Node(deltas[i].Node)
		}
	}
	if err := d.store.AppendDeltas(o, deltas); err != nil {
```

(the rest of the function is unchanged).

- [ ] **Step 4: Implement `daemon/payload.go`**

In `buildPayloadView`, change the returned `Redacted` field:

```go
		Redacted:    len(allFindings) > 0 || HasMarkerView(redactedIn, redactedOut),
```

and add:

```go
func HasMarkerView(in, out json.RawMessage) bool {
	return redact.HasMarker(in) || redact.HasMarker(out)
}
```

(If preferred inline without the helper, inline both `redact.HasMarker` calls — pick whichever keeps 100% coverage simplest; the helper is trivially covered by the e2e test plus a direct unit test on empty inputs.)

- [ ] **Step 5: Implement `cmd/catacomb/daemon.go`**

Add `"github.com/realkarych/catacomb/redact"` to imports. Add to `daemonParams`:

```go
	payloads           config.PayloadsConfig
```

In `newDaemonCmd`'s `params` literal add `payloads: cfg.Payloads,`. In `runDaemonWith`, after `d.SetAllowAnnotations(p.allowAnnotations)`:

```go
	d.SetPayloadPolicy(redact.Policy{Mode: redact.Mode(p.payloads.Mode), MaxBytes: p.payloads.MaxBytes})
```

- [ ] **Step 6: Green — fix mechanical fallout**

Run: `go test -race ./daemon/ ./cmd/... ./ingest/... ./reduce/ ./export/...`
Expected: PASS after mechanically updating any existing daemon/cmd test that asserted raw secret-bearing bytes at rest or `PayloadView{Redacted: false, ...}` for stored-marker content. Tests that ingest clean fixtures are byte-for-byte unaffected (redaction of clean content is the identity — that is the fixed-point property). Do not change parser packages: their transient pre-redaction `Hash` is always recomputed at the choke point and never persists.

- [ ] **Step 7: Commit**

```bash
git add daemon/ cmd/catacomb/
git commit -m "feat(daemon): redact observations and node deltas at the persist boundary, payload policy wiring (ADR-0024)"
```

---

### Task 5: replay choke point

**Files:**

- Modify: `cmd/catacomb/replay.go` (`loadGraph`, `persist`)
- Test: `cmd/catacomb/replay_test.go`

**Interfaces:**

- Consumes: `redact.DefaultPolicy` (Task 2). `catacomb replay` has no config file resolution; it uses the default policy (`redact`/262144) unconditionally — recorded decision, documented in Task 7.

- [ ] **Step 1: Write failing test**

Add to `cmd/catacomb/replay_test.go`:

```go
func TestReplayScrubsSecretsAtRest(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "s.jsonl")
	line := `{"type":"assistant","sessionId":"replay-s","message":{"id":"m1","model":"m","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}}]}}`
	require.NoError(t, os.WriteFile(transcript, []byte(line+"\n"), 0o600))
	dbPath := filepath.Join(dir, "replay.db")

	g, err := runReplayWith(store.OpenSQLite, func() string { return "exec-replay" }, replayArgs{input: transcript, dbPath: dbPath})
	require.NoError(t, err)

	var sawPayload bool
	for _, n := range g.Nodes {
		if n.Payload != nil && len(n.Payload.Input) > 0 {
			sawPayload = true
			assert.NotContains(t, string(n.Payload.Input), "kesha_dev_password")
			assert.Contains(t, string(n.Payload.Input), "‹redacted:connection-string›")
			assert.Equal(t, model.HashPayload(n.Payload), n.PayloadHash)
		}
	}
	assert.True(t, sawPayload)

	blob, err := os.ReadFile(dbPath)
	require.NoError(t, err)
	assert.NotContains(t, string(blob), "kesha_dev_password")
	if wal, werr := os.ReadFile(dbPath + "-wal"); werr == nil {
		assert.NotContains(t, string(wal), "kesha_dev_password")
	}
}
```

(add `"github.com/realkarych/catacomb/model"` and `"github.com/realkarych/catacomb/store"` to the file's imports if not present).

- [ ] **Step 2: Run, verify fail** — `go test ./cmd/catacomb/ -run TestReplayScrubs -v` → FAIL (raw secret found).

- [ ] **Step 3: Implement**

In `cmd/catacomb/replay.go`, add `"github.com/realkarych/catacomb/redact"` to imports. In `loadGraph`, redact observations before they reach the reducer:

```go
	obs, err := ijsonl.ParseReader(f, executionID)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	policy := redact.DefaultPolicy()
	for i := range obs {
		obs[i] = policy.Observation(obs[i])
	}

	g := reduce.NewGraph()
	g.ApplyAll(obs)
	return g, obs, nil
```

In `persist`, add the node-level belt-and-braces before `Persist`:

```go
	nodes, edges := g.Snapshot()
	policy := redact.DefaultPolicy()
	for i := range nodes {
		nodes[i] = policy.Node(nodes[i])
	}
	return s.Persist(obs, nodes, edges)
```

- [ ] **Step 4: Green + commit**

Run: `go test -race ./cmd/catacomb/`
Expected: PASS.

```bash
git add cmd/catacomb/
git commit -m "feat(replay): redact observations and snapshot nodes before standalone persist (ADR-0024)"
```

---

### Task 6: schema v4 scrub migration + read-only guard + VACUUM

**Files:**

- Modify: `store/migrate.go` (version bump, v4 migration, scrub helpers), `store/sqlite.go` (`openSQLiteReadOnly` outdated guard, `openSQLite` post-migration VACUUM)
- Test: `store/migrate_test.go`

**Interfaces:**

- Consumes: `redact.Observation`, `redact.Node` (Task 1) — pure redaction, no cap/refs (Global Constraints).
- Produces: `currentSchemaVersion = 4`; `applySchemaV4(tx *sql.Tx) error`; `scrubTable(conn migrationConn, selectQ, updateQ string, rewrite func([]byte) ([]byte, error)) (int, error)` returning the changed-row count (the idempotency observable); `migrationConn` interface (`Query`/`Exec`) declared here per consumer-declares-interface. `openSQLiteReadOnly` returns `ErrSchemaOutdated` for any on-disk version below current. `openSQLite` runs `VACUUM` + `PRAGMA wal_checkpoint(TRUNCATE)` (best-effort) after any migration ran, so pre-scrub row images do not survive in free pages — the raw-file-bytes test is what proves it worked.
- Batching decision: the ADR-0017 runner is one transaction per migration step; v4 stays a **single transaction** covering both tables (dev-scale databases per ADR-0024's O(rows) consequence). Rows are streamed once; only changed rows are buffered in memory (database/sql cannot interleave `Exec` with an open `Rows` on one Tx connection), then updated after the scan. No runner change, no chunking.

- [ ] **Step 1: Write failing tests**

Add to `store/migrate_test.go` (imports: add `"os"`, `"github.com/realkarych/catacomb/redact"`, `json` is present):

```go
func seedV3DB(t *testing.T, path string) (secretObsBody, cleanObsBody, secretNodeBody string) {
	t.Helper()
	obs := model.Observation{
		ObsID: "o1", RunID: "r1", ExecutionID: "e1", Source: model.SourceHook, Kind: "assistant_tool_use", Seq: 1,
		Attrs: map[string]any{"prompt": "use AKIAIOSFODNN7EXAMPLE"},
		Payload: &model.Payload{
			Input: json.RawMessage(`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}`),
			Hash:  "deadbeef",
		},
	}
	clean := model.Observation{ObsID: "o2", RunID: "r1", ExecutionID: "e1", Source: model.SourceHook, Kind: "stop", Seq: 2}
	node := model.Node{
		ID: "n1", RunID: "r1", Type: model.NodeToolCall, Name: "Bash",
		Payload: &model.Payload{
			Input: json.RawMessage(`{"command":"export TOKEN=AKIAIOSFODNN7EXAMPLE"}`),
			Hash:  "deadbeef",
		},
		PayloadHash: "deadbeef",
	}
	ob, err := json.Marshal(obs)
	require.NoError(t, err)
	cb, err := json.Marshal(clean)
	require.NoError(t, err)
	nb, err := json.Marshal(node)
	require.NoError(t, err)

	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	for _, stmt := range []string{schema, schemaBaselines, schemaRegressResults} {
		_, err = seed.Exec(stmt)
		require.NoError(t, err)
	}
	_, err = seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o1','r1','e1',1,?),('o2','r1','e1',2,?)`, string(ob), string(cb))
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO nodes(id, run_id, body) VALUES('n1','r1',?)`, string(nb))
	require.NoError(t, err)
	_, err = seed.Exec("PRAGMA user_version = 3")
	require.NoError(t, err)
	require.NoError(t, seed.Close())
	return string(ob), string(cb), string(nb)
}

func TestOpenMigratesV3ToV4ScrubbingBodies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	_, cleanBody, _ := seedV3DB(t, path)

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	db := migrated.(*sqliteStore).db
	assert.Equal(t, currentSchemaVersion, userVersion(t, db))

	var obsBody string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o1'").Scan(&obsBody))
	assert.NotContains(t, obsBody, "kesha_dev_password")
	assert.NotContains(t, obsBody, "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, obsBody, "‹redacted:connection-string›")
	assert.Contains(t, obsBody, "‹redacted:aws-key›")
	var o model.Observation
	require.NoError(t, json.Unmarshal([]byte(obsBody), &o))
	assert.Equal(t, model.HashPayload(o.Payload), o.Payload.Hash)
	assert.NotEqual(t, "deadbeef", o.Payload.Hash)

	var cleanGot string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o2'").Scan(&cleanGot))
	assert.Equal(t, cleanBody, cleanGot)

	var nodeBody string
	require.NoError(t, db.QueryRow("SELECT body FROM nodes WHERE id='n1'").Scan(&nodeBody))
	assert.NotContains(t, nodeBody, "AKIAIOSFODNN7EXAMPLE")
	var n model.Node
	require.NoError(t, json.Unmarshal([]byte(nodeBody), &n))
	assert.Equal(t, model.HashPayload(n.Payload), n.PayloadHash)
	assert.Equal(t, n.Payload.Hash, n.PayloadHash)
}

func TestApplySchemaV4IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	db := migrated.(*sqliteStore).db
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	obsChanged, err := scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.NoError(t, err)
	nodesChanged, err := scrubTable(tx, "SELECT id, body FROM nodes", "UPDATE nodes SET body = ? WHERE id = ?", scrubNodeBody)
	require.NoError(t, err)
	assert.Zero(t, obsChanged)
	assert.Zero(t, nodesChanged)
	require.NoError(t, tx.Rollback())
	require.NoError(t, migrated.Close())
}

func TestMigrationLeavesNoSecretBytesInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	for _, f := range []string{path, path + "-wal"} {
		blob, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		assert.NotContains(t, string(blob), "kesha_dev_password", f)
		assert.NotContains(t, string(blob), "AKIAIOSFODNN7EXAMPLE", f)
	}
}

func TestOpenSQLiteReadOnlyRefusesOutdatedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	_, err := openSQLiteReadOnly(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestOpenSQLiteReadOnlyAcceptsCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	ro, err := openSQLiteReadOnly(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, ro.Close())
}

func TestScrubTableSelectError(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = scrubTable(tx, "SELECT obs_id, body FROM observations", "", scrubObservationBody)
	require.Error(t, err)
}

func TestScrubTableRewriteError(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(schema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('bad','r','e',1,'not-json')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.Error(t, err)
}

func TestScrubNodeBodyRejectsInvalidJSON(t *testing.T) {
	_, err := scrubNodeBody([]byte("not-json"))
	require.Error(t, err)
}

func TestScrubTableUpdateError(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(schema)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TRIGGER obs_frozen BEFORE UPDATE ON observations BEGIN SELECT RAISE(ABORT, 'frozen'); END`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o1','r','e',1,'{"obs_id":"o1","attrs":{"prompt":"AKIAIOSFODNN7EXAMPLE"},"event_time":"0001-01-01T00:00:00Z","observed_at":"0001-01-01T00:00:00Z","run_id":"r","execution_id":"e","source":"hook","kind":"k","correlation":{},"seq":1}')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.Error(t, err)
}

type failingScanner struct {
	rowScanner
	next    int
	scanErr error
	rowsErr error
}

func (f *failingScanner) Next() bool  { f.next++; return f.next == 1 && f.scanErr != nil }
func (f *failingScanner) Close() error { return nil }
func (f *failingScanner) Err() error   { return f.rowsErr }
func (f *failingScanner) Scan(...any) error { return f.scanErr }

func TestCollectScrubbedScanAndRowsErrors(t *testing.T) {
	_, err := collectScrubbed(&failingScanner{scanErr: errors.New("scan boom")}, scrubObservationBody)
	require.Error(t, err)
	_, err = collectScrubbed(&failingScanner{rowsErr: errors.New("rows boom")}, scrubObservationBody)
	require.Error(t, err)
}

func TestOpenMigratesV3FailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = seed.Exec(schema)
	require.NoError(t, err)
	_, err = seed.Exec(schemaBaselines)
	require.NoError(t, err)
	_, err = seed.Exec(schemaRegressResults)
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('bad','r','e',1,'not-json')`)
	require.NoError(t, err)
	_, err = seed.Exec("PRAGMA user_version = 3")
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	_, err = openSQLite(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaMigrationFailed)

	check, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = check.Close() })
	assert.Equal(t, 3, userVersion(t, check))
	var body string
	require.NoError(t, check.QueryRow("SELECT body FROM observations WHERE obs_id='bad'").Scan(&body))
	assert.Equal(t, "not-json", body)
}
```

Also update `TestFreshAndV2UpgradeConvergeOnSchema`-style coverage by adding:

```go
func TestFreshAndV3UpgradeConvergeOnSchema(t *testing.T) {
	fresh := fileStore(t)
	path := filepath.Join(t.TempDir(), "v3.db")
	seedV3DB(t, path)
	upgraded, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = upgraded.Close() })
	assert.Equal(t, schemaDDL(t, fresh.db), schemaDDL(t, upgraded.(*sqliteStore).db))
}
```

- [ ] **Step 2: Run, verify fail** — `go test ./store/ 2>&1 | head -20` → compile errors / FAIL.

- [ ] **Step 3: Implement `store/migrate.go`**

Bump the constant and register the migration:

```go
const currentSchemaVersion = 4
```

```go
var schemaMigrations = []migration{
	{from: 0, to: 1, apply: applySchemaV1},
	{from: 1, to: 2, apply: applySchemaV2},
	{from: 2, to: 3, apply: applySchemaV3},
	{from: 3, to: 4, apply: applySchemaV4},
}
```

Add (imports gain `"encoding/json"`, `"github.com/realkarych/catacomb/model"`, `"github.com/realkarych/catacomb/redact"`):

```go
type migrationConn interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

func applySchemaV4(tx *sql.Tx) error {
	if _, err := scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody); err != nil {
		return fmt.Errorf("store.applySchemaV4 observations: %w", err)
	}
	if _, err := scrubTable(tx, "SELECT id, body FROM nodes", "UPDATE nodes SET body = ? WHERE id = ?", scrubNodeBody); err != nil {
		return fmt.Errorf("store.applySchemaV4 nodes: %w", err)
	}
	return nil
}

type scrubbedRow struct {
	id   string
	body string
}

func scrubTable(conn migrationConn, selectQ, updateQ string, rewrite func([]byte) ([]byte, error)) (int, error) {
	rows, err := conn.Query(selectQ)
	if err != nil {
		return 0, fmt.Errorf("store.scrubTable select: %w", err)
	}
	changed, err := collectScrubbed(rows, rewrite)
	if err != nil {
		return 0, err
	}
	for _, r := range changed {
		if _, err := conn.Exec(updateQ, r.body, r.id); err != nil {
			return 0, fmt.Errorf("store.scrubTable update: %w", err)
		}
	}
	return len(changed), nil
}

func collectScrubbed(rows rowScanner, rewrite func([]byte) ([]byte, error)) ([]scrubbedRow, error) {
	defer func() { _ = rows.Close() }()
	var out []scrubbedRow
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, fmt.Errorf("store.collectScrubbed scan: %w", err)
		}
		next, err := rewrite([]byte(body))
		if err != nil {
			return nil, fmt.Errorf("store.collectScrubbed rewrite: %w", err)
		}
		if string(next) != body {
			out = append(out, scrubbedRow{id: id, body: string(next)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.collectScrubbed rows: %w", err)
	}
	return out, nil
}

func scrubObservationBody(body []byte) ([]byte, error) {
	var o model.Observation
	if err := json.Unmarshal(body, &o); err != nil {
		return nil, err
	}
	return json.Marshal(redact.Observation(o))
}

func scrubNodeBody(body []byte) ([]byte, error) {
	var n model.Node
	if err := json.Unmarshal(body, &n); err != nil {
		return nil, err
	}
	return json.Marshal(redact.Node(&n))
}
```

- [ ] **Step 4: Implement `store/sqlite.go`**

In `openSQLiteReadOnly`, capture the guarded version and refuse outdated:

```go
	version, err := schemaVersionGuard(db, currentSchemaVersion)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly schema: %w", err)
	}
	if version < currentSchemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly schema: %w", ErrSchemaOutdated)
	}
```

In `openSQLite`, after the successful `migrate(...)` call and before constructing the store:

```go
	if version < currentSchemaVersion {
		_, _ = db.Exec("VACUUM")
		_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}
```

(best-effort by design — `TestMigrationLeavesNoSecretBytesInFile` is the enforcement; a failed VACUUM leaves a correct database and the test red).

- [ ] **Step 5: Green + repo-wide check + commit**

Run: `go test -race ./store/ && go test -race ./...`
Expected: PASS. Any test elsewhere that opened a pre-v4 fixture read-only must create it through `store.OpenSQLite` first (which migrates); dbs created in-test are already v4.

```bash
git add store/
git commit -m "feat(store): schema v4 scrub migration, read-only outdated guard, post-migration vacuum (ADR-0024)"
```

---

### Task 7: docs, full gates, live verify

**Files:**

- Modify: `docs/guide/privacy-and-operations.md`, `docs/guide/configuration.md`

- [ ] **Step 1: Update `docs/guide/privacy-and-operations.md`**

Replace the "Payloads go through serve-time secret redaction before being returned." sentence and its section framing with write-path-first language. The section should now say (adapt wording to the surrounding voice, keep the rule-pack bullet list as is):

```markdown
### Secrets at rest

Redaction is enforced on the write path
([ADR-0024](../adr/0024-secrets-at-rest-write-path-redaction.md)): every
observation is passed through the redaction rules before it is applied to the
graph and appended to the store, and every node delta is redacted again before
persistence. What is on disk in `catacomb.db` is already redacted; serve-time
and export-time redaction still run as defense in depth. `payload_hash` is the
sha256 of the redacted payload — no pre-redaction hash is stored or exported.

Payload handling is configurable via the `payloads` config section
(mode `redact` | `refs` | `all`, plus `max_bytes`; see
[Configuration](configuration.md)). Ordering is redact-then-cap: payloads are
redacted first, then sides larger than `max_bytes` are replaced by a typed
`‹ref:len,hash›` reference; non-UTF-8 payloads become `‹binary:len,hash›`
references. In `refs` mode no payload bodies are stored at all — note that the
`transcript_path` attr still points at the unredacted transcript file on disk.
`all` disables write-path redaction (a startup warning is logged); serve-time
redaction still applies. Redaction false positives destroy data at rest — there
is no raw copy to recover; `all` is the explicit opt-out.

Databases created before schema v4 are scrubbed by a one-time data migration on
the first write-path open (`catacomb up` / `catacomb daemon`): all existing
observation and node bodies are rewritten through the redactor, hashes are
recomputed, and the file is vacuumed so old row images do not linger. The
migration is idempotent. Read-only commands (`runs`, `inspect`, `export`, …)
refuse pre-v4 databases with a schema-outdated error until a write-path command
has migrated them. Quarantined records (malformed input that could not be
parsed) are stored raw by design and are not covered by write-path redaction.
```

Also update the payload-endpoint paragraph: `redacted` in the response is true when the stored content carries redaction markers or serve-time redaction fired; the `redactions` list reports serve-time findings only (write-path findings are not persisted). Update the "Graph holds structure, not content" section if its claims now contradict the above (payload content is stored, redacted, and served only via the gated endpoint).

- [ ] **Step 2: Update `docs/guide/configuration.md`**

Add to the YAML schema block after `sinks`:

```yaml
payloads:
  mode: redact                  # redact | refs | all
  max_bytes: 262144             # per-side payload cap; overflow becomes a typed ref
```

Add a short section:

```markdown
## Payload redaction and caps

`payloads.mode` controls what payload content is persisted
([ADR-0024](../adr/0024-secrets-at-rest-write-path-redaction.md)):
`redact` (default) stores redacted payloads; `refs` stores only typed
`‹ref:len,hash›` references plus post-redaction hashes; `all` stores raw
payloads (startup warning; serve/export-time redaction still applies).
`payloads.max_bytes` (default 262144) caps each payload side after redaction;
oversized sides are replaced by a typed reference. `catacomb replay` always
uses the defaults (`redact`/262144).
```

- [ ] **Step 3: Full gates**

Run: `make fmt && make lint && make cover && npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`
Expected: fmt clean, lint 0 issues, coverage 100%, markdownlint 0 errors.

- [ ] **Step 4: Live verify — write path**

```bash
make build
TMP=$(mktemp -d)
bin/catacomb daemon --db "$TMP/cat.db" --discovery "$TMP/disc.json" --allow-payload-access > "$TMP/daemon.log" 2>&1 &
DPID=$!
until [ -f "$TMP/disc.json" ]; do sleep 0.2; done
ADDR=$(python3 -c "import json;print(json.load(open('$TMP/disc.json'))['addr'])")
TOKEN=$(python3 -c "import json;print(json.load(open('$TMP/disc.json'))['token'])")
curl -s -X POST "http://$ADDR/hook/PreToolUse" -H "Authorization: Bearer $TOKEN" --data-binary \
  '{"session_id":"leak-live","tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE && psql postgres://kesha:kesha_dev_password@localhost/appdb"}}'
HASH=$(curl -s -H "Authorization: Bearer $TOKEN" "http://$ADDR/v1/sessions" | python3 -c "import json,sys;print(json.load(sys.stdin)[0]['session'])")
NODE=$(curl -s -H "Authorization: Bearer $TOKEN" "http://$ADDR/v1/sessions/$HASH/graph" | python3 -c "import json,sys;g=json.load(sys.stdin);print(next(n['id'] for n in g['nodes'] if n['type']=='tool_call'))")
curl -s -H "Authorization: Bearer $TOKEN" "http://$ADDR/v1/sessions/$HASH/nodes/$NODE/payload"
kill $DPID; wait $DPID 2>/dev/null
strings "$TMP/cat.db" "$TMP/cat.db-wal" 2>/dev/null | grep -cE "AKIAIOSFODNN7EXAMPLE|kesha_dev_password" || echo "no raw secrets on disk"
sqlite3 "$TMP/cat.db" "SELECT body FROM observations" | head -3
```

(if the sessions/graph JSON shapes differ, adapt the two `python3` extractors to the actual field names — the assertion targets are what matter). Expected:

- The payload endpoint returns `"input"` containing `‹redacted:connection-string›`, `"redacted": true`, and NO raw secret.
- The `strings | grep -c` pipeline prints `0` then `no raw secrets on disk` (grep finds nothing in the db or WAL).
- The observations bodies printed by `sqlite3` show `‹redacted:connection-string›` and a `hash` that is the post-redaction sha256.

- [ ] **Step 5: Live verify — v4 migration on a pre-seeded v3 database**

```bash
sqlite3 "$TMP/pre.db" <<'SQL'
CREATE TABLE observations (obs_id TEXT PRIMARY KEY, run_id TEXT, execution_id TEXT, seq INTEGER, body TEXT);
CREATE TABLE nodes (id TEXT PRIMARY KEY, run_id TEXT, body TEXT);
CREATE TABLE edges (id TEXT PRIMARY KEY, run_id TEXT, body TEXT);
CREATE TABLE runs (run_id TEXT PRIMARY KEY, status TEXT, body TEXT);
CREATE TABLE quarantine (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT);
CREATE TABLE tail_cursors (path TEXT PRIMARY KEY, offset INTEGER, fingerprint TEXT, size INTEGER, mtime INTEGER);
CREATE TABLE annotations (execution_id TEXT NOT NULL, source_key TEXT NOT NULL, step_key TEXT, owner TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, write_seq INTEGER NOT NULL, PRIMARY KEY (execution_id, source_key, owner, key));
CREATE TABLE baselines (name TEXT PRIMARY KEY, body TEXT NOT NULL);
CREATE TABLE regress_results (baseline TEXT NOT NULL, seq INTEGER NOT NULL, body TEXT NOT NULL, PRIMARY KEY (baseline, seq));
INSERT INTO observations VALUES('o1','r1','e1',1,'{"obs_id":"o1","run_id":"r1","execution_id":"e1","source":"hook","kind":"assistant_tool_use","correlation":{"session_id":"r1"},"payload":{"input":{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"},"hash":"deadbeef"},"event_time":"2026-07-01T00:00:00Z","observed_at":"2026-07-01T00:00:00Z","seq":1}');
INSERT INTO nodes VALUES('n1','r1','{"id":"n1","run_id":"r1","type":"tool_call","name":"Bash","payload":{"input":{"command":"export TOKEN=AKIAIOSFODNN7EXAMPLE"},"hash":"deadbeef"},"payload_hash":"deadbeef"}');
PRAGMA user_version = 3;
SQL
bin/catacomb runs --db "$TMP/pre.db" 2>&1 | head -2   # expect: schema-outdated error (read-only refuses pre-v4)
bin/catacomb daemon --db "$TMP/pre.db" --discovery "$TMP/disc2.json" > "$TMP/daemon2.log" 2>&1 &
D2=$!
until [ -f "$TMP/disc2.json" ]; do sleep 0.2; done
kill $D2; wait $D2 2>/dev/null
sqlite3 "$TMP/pre.db" "PRAGMA user_version; SELECT body FROM observations; SELECT body FROM nodes;"
strings "$TMP/pre.db" | grep -cE "kesha_dev_password|AKIAIOSFODNN7EXAMPLE" || echo "scrubbed"
```

(adjust the `runs` invocation to however the command takes a db path if `--db` differs — check `bin/catacomb runs --help`). Expected:

- The read-only command on the v3 db fails with the `ErrSchemaOutdated` message before migration.
- After the daemon ran once: `user_version` is `4`; the observation body shows `‹redacted:connection-string›` with a recomputed `hash`; the node body shows `‹redacted:aws-key›` with `payload_hash` equal to the payload `hash`; `strings` finds `0` raw secrets → prints `scrubbed`.

Capture the payload-endpoint JSON, both `strings` outputs, and the post-migration `sqlite3` output in the PR description.

- [ ] **Step 6: Commit docs**

```bash
git add docs/guide/privacy-and-operations.md docs/guide/configuration.md
git commit -m "docs(guide): secrets at rest — write-path redaction, payloads config, v4 scrub migration (ADR-0024)"
```
