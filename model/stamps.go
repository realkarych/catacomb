package model

type Stamps struct {
	CatacombVersion string `json:"catacomb_version,omitempty"`
	StepKeyScheme   string `json:"stepkey_scheme,omitempty"`
}

func (s Stamps) Zero() bool {
	return s == Stamps{}
}

func (s Stamps) Mismatch(other Stamps) bool {
	return s != other
}
