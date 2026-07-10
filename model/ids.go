package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func SessionNodeID(executionID string) string { return "session:" + executionID }

func PromptUUID(sessionID, content string) string {
	h := sha256.New()
	h.Write([]byte(sessionID))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(content)))
	return "pc-" + hex.EncodeToString(h.Sum(nil))
}

func UserPromptID(executionID, uuid string) string { return executionID + ":prompt:" + uuid }

func AssistantTurnID(executionID, messageID string) string {
	return executionID + ":turn:" + messageID
}

func ToolCallID(executionID, toolUseID string) string { return executionID + ":tool:" + toolUseID }

func SubagentID(executionID, agentID string) string { return executionID + ":agent:" + agentID }

func MarkerID(executionID, obsID string) string { return executionID + ":marker:" + obsID }

func PhaseMarkerID(executionID, name string, occ int) string {
	return MarkerID(executionID, name+":"+strconv.Itoa(occ))
}

func EdgeID(executionID string, t EdgeType, src, dst string) string {
	return executionID + ":" + string(t) + ":" + src + ">" + dst
}
