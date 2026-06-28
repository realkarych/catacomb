package phasekey

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

const scheme = "phasekey/v1"

func Compute(enclosingStepKey, markerName string, occurrence int) string {
	sum := sha256.Sum256([]byte(scheme + "|" + enclosingStepKey + "|" + markerName + "|" + strconv.Itoa(occurrence)))
	return hex.EncodeToString(sum[:16])
}
