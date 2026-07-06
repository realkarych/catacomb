package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStampsZero(t *testing.T) {
	cases := []struct {
		name string
		s    Stamps
		want bool
	}{
		{"both empty", Stamps{}, true},
		{"version only", Stamps{CatacombVersion: "v1.2.3"}, false},
		{"scheme only", Stamps{StepKeyScheme: "stepkey/v1"}, false},
		{"both set", Stamps{CatacombVersion: "v1.2.3", StepKeyScheme: "stepkey/v1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.s.Zero())
		})
	}
}

func TestStampsMismatch(t *testing.T) {
	full := Stamps{CatacombVersion: "v1", StepKeyScheme: "stepkey/v1"}
	cases := []struct {
		name string
		a    Stamps
		b    Stamps
		want bool
	}{
		{"both zero", Stamps{}, Stamps{}, false},
		{"equal nonzero", full, full, false},
		{"version differs", full, Stamps{CatacombVersion: "v2", StepKeyScheme: "stepkey/v1"}, true},
		{"scheme differs", full, Stamps{CatacombVersion: "v1", StepKeyScheme: "stepkey/v2"}, true},
		{"both differ", full, Stamps{CatacombVersion: "v2", StepKeyScheme: "stepkey/v2"}, true},
		{"zero vs nonzero", Stamps{}, full, true},
		{"nonzero vs zero", full, Stamps{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.a.Mismatch(tc.b))
			assert.Equal(t, tc.want, tc.b.Mismatch(tc.a))
		})
	}
}

func TestStampsJSONTags(t *testing.T) {
	raw, err := json.Marshal(Stamps{CatacombVersion: "v1.2.3", StepKeyScheme: "stepkey/v1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"catacomb_version":"v1.2.3","stepkey_scheme":"stepkey/v1"}`, string(raw))
	empty, err := json.Marshal(Stamps{})
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(empty))
}
