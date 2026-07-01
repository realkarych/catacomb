package redact_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

func TestNode_RedactsPayloadInput(t *testing.T) {
	secret := "Authorization: Bearer sk-live_ABC123DEF456GHI789JKL"
	n := &model.Node{
		ID:      "n1",
		Payload: &model.Payload{Input: json.RawMessage(`{"cmd":"` + secret + `"}`)},
	}
	out := redact.Node(n)
	require.NotNil(t, out.Payload)
	assert.NotContains(t, string(out.Payload.Input), secret)
	assert.Contains(t, string(out.Payload.Input), "‹redacted:")
}

func TestNode_RedactsPayloadOutput(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	n := &model.Node{
		ID:      "n1",
		Payload: &model.Payload{Output: json.RawMessage(`{"result":"` + secret + `"}`)},
	}
	out := redact.Node(n)
	require.NotNil(t, out.Payload)
	assert.NotContains(t, string(out.Payload.Output), secret)
	assert.Contains(t, string(out.Payload.Output), "‹redacted:")
}

func TestNode_DoesNotMutateOriginal(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	originalInput := json.RawMessage(`{"cmd":"` + secret + `"}`)
	n := &model.Node{
		ID:      "n1",
		Payload: &model.Payload{Input: append(json.RawMessage(nil), originalInput...)},
	}
	before := string(n.Payload.Input)

	out := redact.Node(n)

	assert.Equal(t, before, string(n.Payload.Input), "original node payload must be unchanged")
	assert.Contains(t, before, secret)
	assert.NotSame(t, n.Payload, out.Payload, "redacted node must not alias the original Payload pointer")
	assert.NotContains(t, string(out.Payload.Input), secret)
}

func TestNode_NilPayloadUnchanged(t *testing.T) {
	n := &model.Node{ID: "n1"}
	out := redact.Node(n)
	assert.Nil(t, out.Payload)
}

func TestNode_EmptyPayloadFieldsUnchanged(t *testing.T) {
	n := &model.Node{ID: "n1", Payload: &model.Payload{}}
	out := redact.Node(n)
	require.NotNil(t, out.Payload)
	assert.Empty(t, out.Payload.Input)
	assert.Empty(t, out.Payload.Output)
}

func TestNode_CleanPayloadPassesThrough(t *testing.T) {
	n := &model.Node{
		ID: "n1",
		Payload: &model.Payload{
			Input:  json.RawMessage(`{"file":"main.go"}`),
			Output: json.RawMessage(`{"ok":true}`),
		},
	}
	out := redact.Node(n)
	assert.JSONEq(t, `{"file":"main.go"}`, string(out.Payload.Input))
	assert.JSONEq(t, `{"ok":true}`, string(out.Payload.Output))
}

func TestNode_NilNodeReturnsNil(t *testing.T) {
	assert.Nil(t, redact.Node(nil))
}
