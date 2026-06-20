package model

func SessionNodeID(executionID string) string { return "session:" + executionID }

func UserPromptID(executionID, uuid string) string { return executionID + ":prompt:" + uuid }

func AssistantTurnID(executionID, messageID string) string {
	return executionID + ":turn:" + messageID
}

func ToolCallID(executionID, toolUseID string) string { return executionID + ":tool:" + toolUseID }

func SubagentID(executionID, agentID string) string { return executionID + ":agent:" + agentID }

func EdgeID(executionID string, t EdgeType, src, dst string) string {
	return executionID + ":" + string(t) + ":" + src + ">" + dst
}
