package redact

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVendorPrefixRules(t *testing.T) {
	cases := []struct{ in, reason string }{
		{"sk_live_" + strings.Repeat("A1b2", 6), "stripe-key"},
		{"rk_test_" + strings.Repeat("C3d4", 5), "stripe-key"},
		{"SG." + strings.Repeat("a", 22) + "." + strings.Repeat("b", 22), "sendgrid-key"},
		{"SK" + strings.Repeat("0", 32), "twilio-key"},
		{"npm_" + strings.Repeat("x", 36), "npm-token"},
		{"pypi-" + strings.Repeat("y", 20), "pypi-token"},
		{"glpat-" + strings.Repeat("z", 20), "gitlab-token"},
		{"ya29." + strings.Repeat("w", 30), "google-oauth"},
	}
	for _, c := range cases {
		got := string(Redact([]byte(`{"v":"` + c.in + `"}`)).Data)
		require.Contains(t, got, placeholder(c.reason), c.in)
		require.NotContains(t, got, c.in)
	}
}

func TestVendorPrefixRulesNegative(t *testing.T) {
	cases := []string{
		"just-a-normal-value",
		"sk_live_short",
		"SG.tooshort.tooshort",
		"SK" + strings.Repeat("g", 32),
		"npm_" + strings.Repeat("x", 35),
		"pypi-short",
		"glpat-short",
		"ya29.short",
	}
	for _, c := range cases {
		in := `{"v":"` + c + `"}`
		result := Redact([]byte(in))
		require.False(t, result.Redacted, c)
		require.Equal(t, in, string(result.Data), c)
	}
}
