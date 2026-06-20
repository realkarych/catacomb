package model

import (
	"crypto/sha256"
	"encoding/hex"
)

func HashPayload(p *Payload) string {
	if p == nil {
		return ""
	}
	h := sha256.New()
	h.Write(p.Input)
	h.Write(p.Output)
	return hex.EncodeToString(h.Sum(nil))
}
