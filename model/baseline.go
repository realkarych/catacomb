package model

import "time"

type Baseline struct {
	Name      string            `json:"name"`
	RunIDs    []string          `json:"run_ids"`
	Selector  map[string]string `json:"selector,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}
