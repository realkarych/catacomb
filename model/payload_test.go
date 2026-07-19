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
	sum := sha256.Sum256([]byte("{\"a\":1}\x00\"ok\""))
	assert.Equal(t, hex.EncodeToString(sum[:]), HashPayload(p))
}

func TestHashPayloadSeparatesInputFromOutputSoAmbiguousSplitsDoNotCollide(t *testing.T) {
	spilled := &Payload{Input: []byte(`{"a":1}"ok"`)}
	split := &Payload{Input: []byte(`{"a":1}`), Output: []byte(`"ok"`)}

	assert.NotEqual(t, HashPayload(spilled), HashPayload(split))
}

func TestHashPayloadDistinguishesAdjacentNumericSplitsReduceCanProduce(t *testing.T) {
	left := &Payload{Input: []byte(`1`), Output: []byte(`23`)}
	right := &Payload{Input: []byte(`12`), Output: []byte(`3`)}

	assert.NotEqual(t, HashPayload(left), HashPayload(right))
}
