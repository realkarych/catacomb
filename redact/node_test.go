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

func TestNode_CleanPayloadCopyDoesNotAliasBackingArray(t *testing.T) {
	clean := `{"file":"main.go"}`
	n := &model.Node{
		ID: "n1",
		Payload: &model.Payload{
			Input:  append(json.RawMessage(nil), clean...),
			Output: append(json.RawMessage(nil), clean...),
		},
	}
	out := redact.Node(n)
	require.NotNil(t, out.Payload)

	n.Payload.Input[0] = 'X'
	n.Payload.Output[0] = 'X'

	assert.Equal(t, clean, string(out.Payload.Input), "clean input copy must not share a backing array with the original")
	assert.Equal(t, clean, string(out.Payload.Output), "clean output copy must not share a backing array with the original")
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

func TestNode_RedactsName(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	n := &model.Node{ID: "n1", Name: "cwd=/home/user key=" + secret}
	out := redact.Node(n)
	assert.NotContains(t, out.Name, secret)
	assert.Contains(t, out.Name, "‹redacted:")
}

func TestNode_CleanNamePassesThroughUnchanged(t *testing.T) {
	n := &model.Node{ID: "n1", Name: "Read src/main.go"}
	out := redact.Node(n)
	assert.Equal(t, "Read src/main.go", out.Name)
}

func TestNode_EmptyNameUnchanged(t *testing.T) {
	n := &model.Node{ID: "n1"}
	out := redact.Node(n)
	assert.Empty(t, out.Name)
}

func TestNode_RedactsStringAttrs(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	n := &model.Node{
		ID:    "n1",
		Attrs: map[string]any{"command": "aws configure set key " + secret},
	}
	out := redact.Node(n)
	got, ok := out.Attrs["command"].(string)
	require.True(t, ok)
	assert.NotContains(t, got, secret)
	assert.Contains(t, got, "‹redacted:")
}

func TestNode_NonStringAttrsUnchanged(t *testing.T) {
	n := &model.Node{
		ID: "n1",
		Attrs: map[string]any{
			"tokens":  int64(42),
			"cost":    1.5,
			"success": true,
			"nested":  map[string]any{"k": "v"},
		},
	}
	out := redact.Node(n)
	assert.Equal(t, int64(42), out.Attrs["tokens"])
	assert.Equal(t, 1.5, out.Attrs["cost"])
	assert.Equal(t, true, out.Attrs["success"])
	assert.Equal(t, map[string]any{"k": "v"}, out.Attrs["nested"])
}

func TestNode_NilAttrsUnchanged(t *testing.T) {
	n := &model.Node{ID: "n1"}
	out := redact.Node(n)
	assert.Nil(t, out.Attrs)
}

func TestNode_DoesNotMutateOriginalAttrs(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	n := &model.Node{
		ID:    "n1",
		Attrs: map[string]any{"command": "aws configure set key " + secret},
	}
	out := redact.Node(n)

	assert.Equal(t, "aws configure set key "+secret, n.Attrs["command"], "original Attrs map must be unchanged")
	assert.NotContains(t, out.Attrs["command"], secret)
}

func TestNode_CleanAttrsPassThroughUnchanged(t *testing.T) {
	n := &model.Node{
		ID:    "n1",
		Attrs: map[string]any{"command": "ls -la"},
	}
	out := redact.Node(n)
	assert.Equal(t, "ls -la", out.Attrs["command"])
}
