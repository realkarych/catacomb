package model

import "encoding/json"

type Annotation struct {
	ExecutionID string          `json:"execution_id"`
	SourceKey   string          `json:"source_key"`
	StepKey     string          `json:"step_key,omitempty"`
	Owner       string          `json:"owner"`
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	WriteSeq    uint64          `json:"write_seq"`
}

func SetAnnotation(dst map[string]any, owner, key string, value json.RawMessage) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	dst[owner+"."+key] = value
	return dst
}
