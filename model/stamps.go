package model

import "strings"

type Stamps struct {
	CatacombVersion string `json:"catacomb_version,omitempty"`
	StepKeyScheme   string `json:"stepkey_scheme,omitempty"`
}

func (s Stamps) Zero() bool {
	return s == Stamps{}
}

func (s Stamps) Mismatch(other Stamps) bool {
	return s.normalized() != other.normalized()
}

func (s Stamps) normalized() Stamps {
	s.CatacombVersion = strings.TrimPrefix(s.CatacombVersion, "v")
	return s
}
