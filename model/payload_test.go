package model

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashPayload(t *testing.T) {
	assert.Equal(t, "", HashPayload(nil))

	p := &Payload{Input: []byte(`{"a":1}`), Output: []byte(`"ok"`)}
	sum := sha256.Sum256([]byte(`{"a":1}"ok"`))
	assert.Equal(t, hex.EncodeToString(sum[:]), HashPayload(p))
}
