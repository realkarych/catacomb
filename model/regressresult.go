package model

import "encoding/json"

type RegressResult struct {
	Baseline string          `json:"baseline"`
	Seq      int             `json:"seq"`
	Body     json.RawMessage `json:"body"`
}
