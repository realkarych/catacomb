# B-be1 Implementation Report

## Route
`GET /v1/sessions/{hash}/nodes/{nodeId}/payload`

Registered in `daemon.Handler` via `authedAllowQuery` (bearer header or `?token=` query param). The `{nodeId}` path parameter resolves by node ID first, then by `payload_hash` within the session scope.

## Double-gate, default-OFF

Two independent gates on every request:
1. `authedAllowQuery` — 401 if the bearer token is missing or wrong.
2. `allowPayloadAccess bool` field on `Daemon` (zero-value `false`) — 403 if false, checked inside `nodePayloadView` before any graph access.

The cobra flag `--allow-payload-access` (default `false`) is wired in `cmd/catacomb/daemon.go` via `d.SetAllowPayloadAccess(allowPayloadAccess)`. The `daemon.New` constructor leaves the field false, so every existing test and every default deployment is OFF with no changes required to pre-existing code.

## PayloadView shape (pinned contract)

```go
type RedactionFinding struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type PayloadView struct {
	NodeID      string             `json:"node_id"`
	PayloadHash string             `json:"payload_hash,omitempty"`
	Input       json.RawMessage    `json:"input,omitempty"`
	Output      json.RawMessage    `json:"output,omitempty"`
	Redactions  []RedactionFinding `json:"redactions"`
	Redacted    bool               `json:"redacted"`
}
```

`redactions` is never null (initialized to `[]RedactionFinding{}`). `input`/`output` are omitted when the source side was empty.

## Redaction

`buildPayloadView` (called outside the lock) runs `redact.Redact(rawIn)` and `redact.Redact(rawOut)` and merges findings. The raw `*model.Payload` is never copied into the response — only the post-redaction bytes are returned. Binary/non-UTF8 input is replaced with `"‹binary:len,sha256›"`.

### Planted-secret test

`TestNodePayloadViewPlantedSecretRedacted` injects `{"api_key":"AKIAIOSFODNN7EXAMPLE"}` as the node's input payload, then asserts:
- The secret string is absent from `view.Input`
- `view.Redacted == true`
- `view.Redactions` is non-empty

## Status codes

| Code | Condition |
|------|-----------|
| 401  | Bearer token missing/wrong (`authedAllowQuery`) |
| 403  | `allowPayloadAccess == false` (`ErrPayloadAccessDisabled`) |
| 404  | Session not found, node not found by ID or hash, or node has nil/empty payload |
| 200  | Found + redacted |

## Coverage output

```
File coverage threshold (100%) satisfied:  PASS
Package coverage threshold (100%) satisfied: PASS
Total coverage threshold (100%) satisfied:  PASS
Total test coverage: 100% (2608/2608)
```

## Test summary

- `TestHandleNodePayloadHTTP401NoToken` — 401 without bearer
- `TestHandleNodePayloadHTTP403DisabledWrongToken` — 401 wrong bearer (gate 1 fires first)
- `TestHandleNodePayloadHTTP403Disabled` — 403 with valid bearer but access disabled (default)
- `TestHandleNodePayloadHTTP200Enabled` — 200 with planted secret verified absent from response
- `TestHandleNodePayloadHTTPHeaderAuth` — 200 via `Authorization: Bearer` header
- `TestHandleNodePayloadHTTP404UnknownSession` — 404
- `TestHandleNodePayloadHTTP404UnknownNode` — 404
- `TestHandleNodePayloadHTTP404NilPayload` — 404 for node with nil payload
- `TestNodePayloadViewDefaultOff` — `ErrPayloadAccessDisabled` sentinel
- `TestNodePayloadViewEnabledByID` — resolve by node ID
- `TestNodePayloadViewEnabledByPayloadHash` — resolve by payload hash
- `TestNodePayloadViewUnknownSession` — `ErrSessionNotFound` sentinel
- `TestNodePayloadViewUnknownNode` — `ErrPayloadNotFound` sentinel
- `TestNodePayloadViewNilPayload` — `ErrPayloadNotFound` for nil payload
- `TestNodePayloadViewRedactionsNeverNull` — `redactions` is `[]` not null in JSON
- `TestNodePayloadViewPlantedSecretRedacted` — AWS key redacted
- `TestNodePayloadViewEmptyInputOutput` — `ErrPayloadNotFound` for empty payload
- `TestHandleNodePayloadHTTPOutputOnlyRedacted` — output-only node, GitHub token redacted
- `TestSetAllowPayloadAccess` — setter toggles field correctly
- `TestAllowPayloadAccessFlagRegistered` — cobra flag exists with default `false`
- `TestRunDaemonWithAllowPayloadAccessTrue` — flag wires through to daemon

## Concerns

1. **Lock release inside `nodePayloadView`**: The method temporarily releases `d.mu` to run redaction CPU work outside the lock, then re-acquires it. This is safe because the node pointer is not held across the unlock — only the byte slices (copies of `json.RawMessage`) are retained. If large payloads are common, this keeps the critical section short.
2. **Pre-redaction hash returned**: `payload_hash` in the response is the pre-redaction sha256 computed at ingest time. A client cannot verify that the redacted output matches the hash. This is a pre-existing ADR gap (documented in the plan) and out of scope for B-be1.
3. **By-hash lookup is O(n) over all nodes in all session executions**: acceptable for current scale; a hash index would be a follow-up if sessions grow to thousands of nodes.
