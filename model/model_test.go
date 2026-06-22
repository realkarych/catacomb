package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailCursorJSONRoundTrip(t *testing.T) {
	c := TailCursor{Path: "/a/b.jsonl", Offset: 128, Fingerprint: "deadbeef", Size: 256, Mtime: 999}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	var got TailCursor
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, c, got)
}
