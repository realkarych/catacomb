package model

import "strings"

func SessionNodeID(executionID string) string { return "session:" + executionID }

func NodeSourceKey(nodeID string) string {
	if strings.HasPrefix(nodeID, "session:") {
		return nodeID[len("session:"):]
	}
	i := strings.Index(nodeID, ":")
	if i < 0 {
		return ""
	}
	rest := nodeID[i+1:]
	j := strings.Index(rest, ":")
	if j < 0 {
		return ""
	}
	return rest[j+1:]
}

func UserPromptID(executionID, uuid string) string { return executionID + ":prompt:" + uuid }

func AssistantTurnID(executionID, messageID string) string {
	return executionID + ":turn:" + messageID
}

func ToolCallID(executionID, toolUseID string) string { return executionID + ":tool:" + toolUseID }

func SubagentID(executionID, agentID string) string { return executionID + ":agent:" + agentID }

func MarkerID(executionID, obsID string) string { return executionID + ":marker:" + obsID }

func EdgeID(executionID string, t EdgeType, src, dst string) string {
	return executionID + ":" + string(t) + ":" + src + ">" + dst
}
